package cmd

import (
	"strings"

	"github.com/gadenbuie/utpr/internal/cilog"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
)

// GitHub API calls used to resolve failure reasons, kept as package-level
// vars so tests can swap in fakes and count round trips.
var (
	ghGetCheckRuns            = gh.GetCheckRuns
	ghListWorkflowRunsForSHA  = gh.ListWorkflowRunsForSHA
	ghListCheckRunAnnotations = gh.ListCheckRunAnnotations
	ghListWorkflowRunJobs     = gh.ListWorkflowRunJobs
	ghGetJobLogs              = gh.GetJobLogs
)

// reasonLineMaxRunes bounds the inline reason shown per failed check.
const reasonLineMaxRunes = 160

// fetchCheckRunReasons resolves a one-line failure reason for each failed
// check run in runs: the first informative annotation, and only when the
// annotations are generic, the failed job's log. Runs without a usable
// reason are omitted from the map.
func fetchCheckRunReasons(ownerRepo string, runs []gh.CheckRun, wfRuns []gh.WorkflowRun) map[int64]string {
	c := newReasonCache()
	c.collectAnnotations(ownerRepo, runs)
	reasons := c.annotationReasons(runs)
	c.resolveLogReasons(ownerRepo, runs, wfRuns, reasons)
	return reasons
}

// informativeAnnotation returns the first failure-level annotation message
// that explains the failure, or "".
func informativeAnnotation(anns []gh.CheckRunAnnotation) string {
	for _, a := range anns {
		if a.AnnotationLevel != "failure" {
			continue
		}
		msg := strings.TrimSpace(a.Message)
		if msg == "" || cilog.IsGenericExitMessage(msg) {
			continue
		}
		return msg
	}
	return ""
}

// reasonCache stores per-check-run annotations, per-run job lists, and
// per-job logs so repeated polls and renders never re-fetch.
type reasonCache struct {
	annotations map[int64][]gh.CheckRunAnnotation
	jobs        map[int64][]gh.WorkflowJob
	logs        map[int64]string
}

func newReasonCache() *reasonCache {
	return &reasonCache{
		annotations: map[int64][]gh.CheckRunAnnotation{},
		jobs:        map[int64][]gh.WorkflowJob{},
		logs:        map[int64]string{},
	}
}

// collectAnnotations fetches annotations for failed check runs that are
// not cached yet. Annotations are immutable once a check run completes,
// so each run is fetched at most once. Fetch failures are cached as empty
// and reported once.
func (c *reasonCache) collectAnnotations(ownerRepo string, runs []gh.CheckRun) {
	for _, r := range runs {
		if !isFailedCheckRun(r) {
			continue
		}
		if _, ok := c.annotations[r.ID]; ok {
			continue
		}
		anns, err := ghListCheckRunAnnotations(ownerRepo, r.ID)
		if err != nil {
			ui.Warnf("Could not fetch annotations for '%s': %v", r.Name, err)
			anns = nil
		}
		c.annotations[r.ID] = anns
	}
}

// annotationReasons maps check run IDs to informative annotation messages
// for failed runs whose annotations are already cached.
func (c *reasonCache) annotationReasons(runs []gh.CheckRun) map[int64]string {
	reasons := map[int64]string{}
	for _, r := range runs {
		if !isFailedCheckRun(r) {
			continue
		}
		if msg := informativeAnnotation(c.annotations[r.ID]); msg != "" {
			reasons[r.ID] = normalizeReason(msg)
		}
	}
	return reasons
}

// resolveLogReasons completes the reasons map with log-derived reasons for
// failed check runs that have no annotation reason. Each failed job's log
// is fetched at most once.
func (c *reasonCache) resolveLogReasons(ownerRepo string, runs []gh.CheckRun, wfRuns []gh.WorkflowRun, reasons map[int64]string) map[int64]string {
	if reasons == nil {
		reasons = map[int64]string{}
	}
	for _, r := range runs {
		if !isFailedCheckRun(r) || reasons[r.ID] != "" {
			continue
		}
		if reason := c.logReason(ownerRepo, r, wfRuns); reason != "" {
			reasons[r.ID] = reason
		}
	}
	return reasons
}

// logReason fetches the failed job's log for check run r and extracts its
// failure landmark. It returns "" when no job or reason can be found.
func (c *reasonCache) logReason(ownerRepo string, r gh.CheckRun, wfRuns []gh.WorkflowRun) string {
	job := c.failedJob(ownerRepo, r, wfRuns)
	if job == nil {
		return ""
	}
	log, ok := c.logs[job.ID]
	if !ok {
		fetched, err := ghGetJobLogs(ownerRepo, job.ID)
		if err != nil {
			ui.Warnf("Could not fetch log for job '%s': %v", job.Name, err)
		}
		c.logs[job.ID] = fetched
		log = fetched
	}
	return normalizeReason(cilog.Reason(strings.Split(strings.TrimRight(log, "\n"), "\n")))
}

// failedJob locates the workflow job backing failed check run r.
// GitHub Actions check runs share their ID with the job they represent;
// their external_id is a UUID, not a job ID. Check runs without app
// information fall back to mapping the check run to a job by name via
// the workflow run's job list.
func (c *reasonCache) failedJob(ownerRepo string, r gh.CheckRun, wfRuns []gh.WorkflowRun) *gh.WorkflowJob {
	if r.App.Slug == "github-actions" {
		job := gh.WorkflowJob{ID: r.ID, Name: jobNameFromCheckRun(r.Name)}
		return &job
	}
	if r.App.Slug != "" {
		return nil // third-party apps have no Actions job logs
	}
	runID := workflowRunIDForSuite(wfRuns, r.CheckSuite.ID)
	if runID == 0 {
		return nil
	}
	jobs, ok := c.jobs[runID]
	if !ok {
		fetched, err := ghListWorkflowRunJobs(ownerRepo, runID)
		if err != nil {
			ui.Warnf("Could not fetch jobs for run %d: %v", runID, err)
		}
		c.jobs[runID] = fetched
		jobs = fetched
	}
	return matchFailedJob(r, jobs)
}

// jobNameFromCheckRun strips the workflow prefix from a check run name.
func jobNameFromCheckRun(name string) string {
	if _, rest, found := strings.Cut(name, " / "); found {
		return rest
	}
	return name
}

// workflowRunIDForSuite returns the workflow run sharing checkSuiteID.
func workflowRunIDForSuite(wfRuns []gh.WorkflowRun, checkSuiteID int64) int64 {
	for _, r := range wfRuns {
		if r.CheckSuiteID == checkSuiteID {
			return r.ID
		}
	}
	return 0
}

// matchFailedJob finds the failed job backing check run r: the job whose
// name matches, or the run's only failed job. When several failed jobs
// match none of them is used, to avoid misattribution.
func matchFailedJob(r gh.CheckRun, jobs []gh.WorkflowJob) *gh.WorkflowJob {
	name := jobNameFromCheckRun(r.Name)
	var unmatched *gh.WorkflowJob
	failed := 0
	for i := range jobs {
		j := &jobs[i]
		if j.Status != "completed" || !isFailedConclusion(j.Conclusion) {
			continue
		}
		failed++
		if j.Name == name {
			return j
		}
		if unmatched == nil {
			unmatched = j
		}
	}
	if failed == 1 {
		return unmatched
	}
	return nil
}

// needsSuiteMapping reports whether any failed check run without app
// information can only be resolved to a job through the workflow run's
// job list.
func needsSuiteMapping(runs []gh.CheckRun) bool {
	for _, r := range runs {
		if isFailedCheckRun(r) && r.App.Slug == "" {
			return true
		}
	}
	return false
}

// normalizeReason collapses a reason to one bounded line.
func normalizeReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= reasonLineMaxRunes {
		return s
	}
	return string(r[:reasonLineMaxRunes-1]) + "…"
}
