package cmd

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
)

// estimateSampleRuns is the number of recent successful runs of a workflow
// sampled when estimating job durations.
const estimateSampleRuns = 5

// estimateCache tracks median historical job durations, fetched
// asynchronously from the last estimateSampleRuns successful runs of each
// workflow. Results are keyed by check suite ID so the renderer can map a
// check run to its workflow's job history without extra lookups.
//
// All fetches are best-effort: any failure or missing history simply means
// no estimate is shown for that job.
type estimateCache struct {
	mu sync.Mutex
	// started holds workflow IDs whose history has been requested;
	// each workflow is fetched at most once.
	started map[int64]bool
	// est maps check suite ID -> job name -> median duration. Inner maps
	// are built before publishing and never mutated afterwards.
	est   map[int64]map[string]time.Duration
	ready chan struct{}
}

func newEstimateCache() *estimateCache {
	return &estimateCache{
		started: map[int64]bool{},
		est:     map[int64]map[string]time.Duration{},
		ready:   make(chan struct{}),
	}
}

// ensure kicks off background fetches for any workflow in wfRuns whose
// history has not been requested yet. Safe to call on every poll.
func (c *estimateCache) ensure(ownerRepo string, wfRuns []gh.WorkflowRun) {
	c.mu.Lock()
	var pending []gh.WorkflowRun
	for _, r := range wfRuns {
		if r.WorkflowID == 0 || r.CheckSuiteID == 0 || c.started[r.WorkflowID] {
			continue
		}
		c.started[r.WorkflowID] = true
		pending = append(pending, r)
	}
	c.mu.Unlock()
	for _, r := range pending {
		go c.fetchWorkflow(ownerRepo, r)
	}
}

// fetchWorkflow samples the last estimateSampleRuns successful runs of one
// workflow and records the median duration per job name under the current
// run's check suite ID.
func (c *estimateCache) fetchWorkflow(ownerRepo string, wf gh.WorkflowRun) {
	runs, err := ghListWorkflowRunsForWorkflow(ownerRepo, wf.WorkflowID, estimateSampleRuns, "success")
	if err != nil {
		return
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[string][]time.Duration{} // job name -> observed durations
	)
	for _, run := range runs {
		wg.Add(1)
		go func(run gh.WorkflowRun) {
			defer wg.Done()
			jobs, err := ghListWorkflowRunJobs(ownerRepo, run.ID)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, j := range jobs {
				name := strings.TrimSpace(j.Name)
				if name == "" || j.Conclusion != "success" || j.StartedAt == "" || j.CompletedAt == "" {
					continue
				}
				start, err1 := time.Parse(time.RFC3339, j.StartedAt)
				end, err2 := time.Parse(time.RFC3339, j.CompletedAt)
				if err1 != nil || err2 != nil {
					continue
				}
				seen[name] = append(seen[name], end.Sub(start))
			}
		}(run)
	}
	wg.Wait()
	if len(seen) == 0 {
		return
	}
	medians := make(map[string]time.Duration, len(seen))
	for name, ds := range seen {
		medians[name] = medianDuration(ds)
	}
	c.mu.Lock()
	c.est[wf.CheckSuiteID] = medians
	c.mu.Unlock()
	c.signalReady()
}

// signalReady wakes the poll loop so it repaints with the new estimates.
func (c *estimateCache) signalReady() {
	c.mu.Lock()
	close(c.ready)
	c.ready = make(chan struct{})
	c.mu.Unlock()
}

// readyChan returns a channel that closes each time new estimates land.
func (c *estimateCache) readyChan() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// snapshot returns a copy of the estimates safe to read without the lock.
func (c *estimateCache) snapshot() map[int64]map[string]time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[int64]map[string]time.Duration, len(c.est))
	for k, v := range c.est {
		out[k] = v
	}
	return out
}

// estimatesSnapshot returns nil for a nil cache so callers can pass the
// result straight through to the renderer.
func estimatesSnapshot(c *estimateCache) map[int64]map[string]time.Duration {
	if c == nil {
		return nil
	}
	return c.snapshot()
}

// estimateFor returns the historical duration for a check run's job, if
// history for it was collected. Only GitHub Actions jobs appear in workflow
// job listings, so other check runs never match.
func estimateFor(estimates map[int64]map[string]time.Duration, r gh.CheckRun) (time.Duration, bool) {
	jobs, ok := estimates[r.CheckSuite.ID]
	if !ok {
		return 0, false
	}
	d, ok := jobs[r.Name]
	return d, ok
}

// medianDuration returns the median of ds, averaging the two middle values
// for even sample counts.
func medianDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
