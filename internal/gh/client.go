package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// IsAuthenticated checks if we can make authenticated GitHub API calls.
func IsAuthenticated() bool {
	client, err := RESTClient()
	if err != nil {
		return false
	}
	var result interface{}
	err = client.Get("user", &result)
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
		Ref  string `json:"ref"`
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
	return client.Delete(fmt.Sprintf("repos/%s/%s/git/refs/heads/%s",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch)), nil)
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
// Returns up to 100 comments. For issues with more comments, the most
// recent 100 are returned.
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

// splitOwnerRepo splits "owner/repo" into its two components.
// Returns an error if the input is not in "owner/repo" format.
func splitOwnerRepo(ownerRepo string) (string, string, error) {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid owner/repo format: %q", ownerRepo)
	}
	return parts[0], parts[1], nil
}
