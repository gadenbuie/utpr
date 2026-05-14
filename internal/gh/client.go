package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// RESTClient returns a GitHub REST API client with auto-resolved auth.
func RESTClient() (*api.RESTClient, error) {
	return api.DefaultRESTClient()
}

// GraphQLClient returns a GitHub GraphQL API client with auto-resolved auth.
func GraphQLClient() (*api.GraphQLClient, error) {
	return api.DefaultGraphQLClient()
}

// IsAuthenticated checks if GitHub API auth is configured.
// This checks if the go-gh library can resolve auth credentials
// (from gh config or GITHUB_TOKEN) without making a network call.
func IsAuthenticated() bool {
	_, err := RESTClient()
	return err == nil
}

// IsReachable checks if the GitHub API is reachable by making a lightweight
// network request. Returns false if offline or if the request fails for any reason.
func IsReachable() bool {
	_, err := GetLogin()
	return err == nil
}

// GetLogin returns the current authenticated GitHub username.
func GetLogin() (string, error) {
	client, err := RESTClient()
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := client.Get("user", &user); err != nil {
		return "", fmt.Errorf("failed to get user info: %w", err)
	}
	return user.Login, nil
}

// RepoInfo holds information about a GitHub repository.
type RepoInfo struct {
	Fork        bool `json:"fork"`
	Permissions struct {
		Push bool `json:"push"`
	} `json:"permissions"`
	Parent struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
}

// GetRepo fetches repository info from the GitHub API.
func GetRepo(ownerRepo string) (*RepoInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var info RepoInfo
	if err := client.Get(fmt.Sprintf("repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo)), &info); err != nil {
		return nil, fmt.Errorf("failed to get repo %s: %w", ownerRepo, err)
	}
	return &info, nil
}

// PRInfo holds information about a pull request.
type PRInfo struct {
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// GetPR fetches pull request details.
func GetPR(ownerRepo string, number int) (*PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var pr PRInfo
	if err := client.Get(fmt.Sprintf("repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number), &pr); err != nil {
		return nil, fmt.Errorf("failed to get PR #%d: %w", number, err)
	}
	return &pr, nil
}

// IssueInfo holds information about a GitHub issue.
type IssueInfo struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	PullRequest *struct{} `json:"pull_request"` // non-nil if this is a PR
}

// GetIssue fetches issue details.
func GetIssue(ownerRepo string, number int) (*IssueInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var issue IssueInfo
	if err := client.Get(fmt.Sprintf("repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number), &issue); err != nil {
		return nil, fmt.Errorf("failed to get issue #%d: %w", number, err)
	}
	return &issue, nil
}

// DeleteRemoteBranch deletes a branch from a remote repository.
func DeleteRemoteBranch(ownerRepo, branch string) error {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return err
	}
	client, err := RESTClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	// Branch names can contain slashes (e.g. "feature/foo") and the git refs
	// API expects them as literal path segments, so we must not PathEscape the branch.
	return client.Delete(fmt.Sprintf("repos/%s/%s/git/refs/heads/%s",
		url.PathEscape(owner), url.PathEscape(repo), branch), nil)
}

// AddIssueAssignee assigns a user to an issue.
func AddIssueAssignee(ownerRepo string, number int, login string) error {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return err
	}
	client, err := RESTClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}
	bodyData := struct {
		Assignees []string `json:"assignees"`
	}{
		Assignees: []string{login},
	}
	jsonBytes, err := json.Marshal(bodyData)
	if err != nil {
		return fmt.Errorf("failed to marshal assignee data: %w", err)
	}
	var result interface{}
	return client.Post(fmt.Sprintf("repos/%s/%s/issues/%d/assignees",
		url.PathEscape(owner), url.PathEscape(repo), number), bytes.NewReader(jsonBytes), &result)
}

// BatchPRStatus holds the result of a batch PR status query.
type BatchPRStatus struct {
	Number int
	Title  string
	Merged bool
	Author string
}

// BatchGetMergedPRs uses GraphQL to fetch merged status for multiple PRs in one round-trip.
func BatchGetMergedPRs(ownerRepo string, prNumbers []int) ([]BatchPRStatus, error) {
	if len(prNumbers) == 0 {
		return nil, nil
	}

	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}

	client, err := GraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL client: %w", err)
	}

	// Build dynamic query with aliased fields — PR numbers are ints so no injection risk
	var queryParts []string
	for _, num := range prNumbers {
		queryParts = append(queryParts,
			fmt.Sprintf(`pr%d: pullRequest(number:%d) { number title merged author { login } }`, num, num))
	}

	query := fmt.Sprintf(
		`query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) { %s } }`,
		strings.Join(queryParts, "\n"))
	variables := map[string]interface{}{"owner": owner, "name": repo}

	var result struct {
		Repository map[string]struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Merged bool   `json:"merged"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"repository"`
	}

	if err := client.Do(query, variables, &result); err != nil {
		return nil, fmt.Errorf("failed to batch query PR status: %w", err)
	}

	var statuses []BatchPRStatus
	for _, pr := range result.Repository {
		if pr.Merged {
			statuses = append(statuses, BatchPRStatus{
				Number: pr.Number,
				Title:  pr.Title,
				Merged: pr.Merged,
				Author: pr.Author.Login,
			})
		}
	}
	return statuses, nil
}

// GetPRForBranch finds a PR whose head matches the given branch.
// state can be "open", "closed", or "all".
// Returns nil (not error) if no matching PR is found.
func GetPRForBranch(ownerRepo, branch, state string) (*PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/%s/pulls?head=%s:%s&state=%s&per_page=1",
		url.PathEscape(owner), url.PathEscape(repo),
		url.QueryEscape(owner), url.QueryEscape(branch), url.QueryEscape(state)), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs for branch %s: %w", branch, err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// ListIssues lists issues for a repository, filtering out pull requests.
// Returns up to 100 results sorted by most recently updated.
func ListIssues(ownerRepo, state string) ([]IssueInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var all []IssueInfo
	err = client.Get(fmt.Sprintf("repos/%s/%s/issues?state=%s&sort=updated&direction=desc&per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(state)), &all)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}
	var issues []IssueInfo
	for _, issue := range all {
		if issue.PullRequest == nil {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

// ListPRs lists pull requests for a repository.
// state can be "open", "closed", or "all". Note: the GitHub REST API does not
// support "merged" as a state — use "closed" and filter by Merged field.
// Returns up to 100 results sorted by most recently updated.
func ListPRs(ownerRepo, state string) ([]PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/%s/pulls?state=%s&sort=updated&direction=desc&per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(state)), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}
	return prs, nil
}

// SearchPRsByCommit finds pull requests associated with a given commit SHA.
func SearchPRsByCommit(ownerRepo, sha string) ([]PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/%s/commits/%s/pulls",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha)), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs by commit %s: %w", sha, err)
	}
	return prs, nil
}

// GetMergedPRForBranch finds a merged PR whose head matched the given branch.
// Returns nil (not error) if no matching merged PR is found.
func GetMergedPRForBranch(ownerRepo, branch string) (*PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/%s/pulls?head=%s:%s&state=closed&per_page=10",
		url.PathEscape(owner), url.PathEscape(repo),
		url.QueryEscape(owner), url.QueryEscape(branch)), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to search merged PRs for branch %s: %w", branch, err)
	}
	for i := range prs {
		if prs[i].Merged {
			return &prs[i], nil
		}
	}
	return nil, nil
}

// MergedPRInfo holds information about a recently merged pull request.
type MergedPRInfo struct {
	Number      int
	Title       string
	Author      string
	HeadRefName string
}

// SearchMergedPRs returns recently merged PRs using GraphQL.
// limit is clamped to 100 (GitHub API maximum).
func SearchMergedPRs(ownerRepo string, limit int) ([]MergedPRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	if limit > 100 {
		limit = 100
	}

	client, err := GraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL client: %w", err)
	}

	query := `query($owner: String!, $name: String!, $limit: Int!) {
		repository(owner: $owner, name: $name) {
			pullRequests(states: MERGED, first: $limit, orderBy: {field: UPDATED_AT, direction: DESC}) {
				nodes {
					number
					title
					headRefName
					author { login }
				}
			}
		}
	}`
	variables := map[string]interface{}{
		"owner": owner,
		"name":  repo,
		"limit": limit,
	}

	var result struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					Number      int    `json:"number"`
					Title       string `json:"title"`
					HeadRefName string `json:"headRefName"`
					Author      struct {
						Login string `json:"login"`
					} `json:"author"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}

	if err := client.Do(query, variables, &result); err != nil {
		return nil, fmt.Errorf("failed to search merged PRs: %w", err)
	}

	var merged []MergedPRInfo
	for _, pr := range result.Repository.PullRequests.Nodes {
		merged = append(merged, MergedPRInfo{
			Number:      pr.Number,
			Title:       pr.Title,
			Author:      pr.Author.Login,
			HeadRefName: pr.HeadRefName,
		})
	}
	return merged, nil
}

// CreatePRParams holds parameters for creating a pull request.
type CreatePRParams struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

// CreatePR creates a new pull request.
func CreatePR(ownerRepo string, params CreatePRParams) (*PRInfo, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	jsonBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PR params: %w", err)
	}
	var pr PRInfo
	err = client.Post(fmt.Sprintf("repos/%s/%s/pulls",
		url.PathEscape(owner), url.PathEscape(repo)), bytes.NewReader(jsonBytes), &pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}
	return &pr, nil
}

// Comment holds information about an issue or PR comment.
type Comment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"user"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// ListIssueComments lists comments on an issue or pull request.
// Returns up to 100 comments in chronological order. For issues with
// more than 100 comments, only the first 100 are returned.
func ListIssueComments(ownerRepo string, number int) ([]Comment, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var comments []Comment
	err = client.Get(fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), number), &comments)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments for issue #%d: %w", number, err)
	}
	return comments, nil
}

// CheckRun represents a GitHub Checks API check run.
type CheckRun struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`      // queued, in_progress, completed
	Conclusion  string `json:"conclusion"`  // success, failure, neutral, cancelled, skipped, timed_out, action_required
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	HTMLURL     string `json:"html_url"`
	ExternalID  string `json:"external_id"`
	App         struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"app"`
	CheckSuite struct {
		ID int64 `json:"id"`
	} `json:"check_suite"`
}

// GetBranchSHA returns the HEAD commit SHA of a remote branch.
// Branch names may contain slashes and must not be path-escaped.
func GetBranchSHA(ownerRepo, branch string) (string, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return "", err
	}
	client, err := RESTClient()
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	path := fmt.Sprintf("repos/%s/%s/branches/%s",
		url.PathEscape(owner), url.PathEscape(repo), branch)
	if err := client.Get(path, &b); err != nil {
		return "", fmt.Errorf("failed to get branch %s: %w", branch, err)
	}
	return b.Commit.SHA, nil
}

// parseNextLink extracts the URL of the next page from a GitHub Link header.
// Returns "" when there is no next page.
func parseNextLink(link string) string {
	for _, part := range strings.Split(link, ",") {
		part = strings.TrimSpace(part)
		i := strings.Index(part, ";")
		if i < 0 {
			continue
		}
		u := strings.Trim(strings.TrimSpace(part[:i]), "<>")
		rel := strings.TrimSpace(part[i+1:])
		if rel == `rel="next"` {
			return u
		}
	}
	return ""
}

// GetCheckRuns returns all check runs for a commit SHA, following pagination.
func GetCheckRuns(ownerRepo, sha string) ([]CheckRun, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var all []CheckRun
	path := fmt.Sprintf("repos/%s/%s/commits/%s/check-runs?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	for path != "" {
		var response struct {
			CheckRuns []CheckRun `json:"check_runs"`
		}
		resp, reqErr := client.Request("GET", path, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("failed to get check runs for %s: %w", sha, reqErr)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if jsonErr := json.Unmarshal(body, &response); jsonErr != nil {
			return nil, jsonErr
		}
		all = append(all, response.CheckRuns...)
		path = parseNextLink(resp.Header.Get("Link"))
	}
	return all, nil
}

// WorkflowRun represents a GitHub Actions workflow run.
type WorkflowRun struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`     // queued, in_progress, completed
	Conclusion   string `json:"conclusion"` // success, failure, neutral, cancelled, skipped, timed_out, action_required, startup_failure
	HTMLURL      string `json:"html_url"`
	HeadSHA      string `json:"head_sha"`
	Event        string `json:"event"`
	RunNumber    int    `json:"run_number"`
	CreatedAt    string `json:"created_at"`
	CheckSuiteID int64  `json:"check_suite_id"`
}

// WorkflowJob represents a job within a GitHub Actions workflow run.
type WorkflowJob struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	HTMLURL     string `json:"html_url"`
}

// ListWorkflowRunsForSHA returns the workflow runs associated with a commit SHA, following pagination.
func ListWorkflowRunsForSHA(ownerRepo, sha string) ([]WorkflowRun, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var all []WorkflowRun
	path := fmt.Sprintf("repos/%s/%s/actions/runs?head_sha=%s&per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(sha))
	for path != "" {
		var response struct {
			WorkflowRuns []WorkflowRun `json:"workflow_runs"`
		}
		resp, reqErr := client.Request("GET", path, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("failed to get workflow runs for %s: %w", sha, reqErr)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if jsonErr := json.Unmarshal(body, &response); jsonErr != nil {
			return nil, jsonErr
		}
		all = append(all, response.WorkflowRuns...)
		path = parseNextLink(resp.Header.Get("Link"))
	}
	return all, nil
}

// ListWorkflowRunJobs returns the jobs for a workflow run, following pagination.
func ListWorkflowRunJobs(ownerRepo string, runID int64) ([]WorkflowJob, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return nil, err
	}
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var all []WorkflowJob
	path := fmt.Sprintf("repos/%s/%s/actions/runs/%d/jobs?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), runID)
	for path != "" {
		var response struct {
			Jobs []WorkflowJob `json:"jobs"`
		}
		resp, reqErr := client.Request("GET", path, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("failed to get jobs for run %d: %w", runID, reqErr)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if jsonErr := json.Unmarshal(body, &response); jsonErr != nil {
			return nil, jsonErr
		}
		all = append(all, response.Jobs...)
		path = parseNextLink(resp.Header.Get("Link"))
	}
	return all, nil
}

// GetJobLogs fetches the plain-text log for a GitHub Actions job.
// The API returns a redirect to the actual log content.
func GetJobLogs(ownerRepo string, jobID int64) (string, error) {
	owner, repo, err := splitOwnerRepo(ownerRepo)
	if err != nil {
		return "", err
	}
	httpClient, err := api.DefaultHTTPClient()
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP client: %w", err)
	}
	logURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%d/logs",
		url.PathEscape(owner), url.PathEscape(repo), jobID)
	req, err := http.NewRequest("GET", logURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch logs for job %d: %w", jobID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no logs found for job %d (logs may have expired)", jobID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return "", fmt.Errorf("unexpected status %d fetching logs for job %d: %s", resp.StatusCode, jobID, strings.TrimSpace(string(snippet)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read logs for job %d: %w", jobID, err)
	}
	return string(body), nil
}

// splitOwnerRepo splits "owner/repo" into its two components.
// Returns an error if the input is not in "owner/repo" format.
func splitOwnerRepo(ownerRepo string) (string, string, error) {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid owner/repo format: %q", ownerRepo)
	}
	return parts[0], parts[1], nil
}
