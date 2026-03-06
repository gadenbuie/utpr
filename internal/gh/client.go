package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	client, err := RESTClient()
	if err != nil {
		return nil, err
	}
	var repo RepoInfo
	if err := client.Get(fmt.Sprintf("repos/%s", ownerRepo), &repo); err != nil {
		return nil, err
	}
	return &repo, nil
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
	client, err := RESTClient()
	if err != nil {
		return nil, err
	}
	var pr PRInfo
	if err := client.Get(fmt.Sprintf("repos/%s/pulls/%d", ownerRepo, number), &pr); err != nil {
		return nil, err
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
	User struct {
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
	client, err := RESTClient()
	if err != nil {
		return nil, err
	}
	var issue IssueInfo
	if err := client.Get(fmt.Sprintf("repos/%s/issues/%d", ownerRepo, number), &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// DeleteRemoteBranch deletes a branch from a remote repository.
func DeleteRemoteBranch(ownerRepo, branch string) error {
	client, err := RESTClient()
	if err != nil {
		return err
	}
	return client.Delete(fmt.Sprintf("repos/%s/git/refs/heads/%s", ownerRepo, branch), nil)
}

// AddIssueAssignee assigns a user to an issue.
func AddIssueAssignee(ownerRepo string, number int, login string) error {
	client, err := RESTClient()
	if err != nil {
		return err
	}
	bodyData := struct {
		Assignees []string `json:"assignees"`
	}{
		Assignees: []string{login},
	}
	jsonBytes, err := json.Marshal(bodyData)
	if err != nil {
		return err
	}
	var result interface{}
	return client.Post(fmt.Sprintf("repos/%s/issues/%d/assignees", ownerRepo, number), bytes.NewReader(jsonBytes), &result)
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

	client, err := GraphQLClient()
	if err != nil {
		return nil, err
	}

	// Build dynamic query with aliased fields
	var queryParts []string
	for _, num := range prNumbers {
		queryParts = append(queryParts,
			fmt.Sprintf(`pr%d: pullRequest(number:%d) { number title merged author { login } }`, num, num))
	}

	parts := splitOwnerRepo(ownerRepo)
	query := fmt.Sprintf(`query { repository(owner:"%s", name:"%s") { %s } }`,
		parts[0], parts[1], strings.Join(queryParts, "\n"))

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

	if err := client.Do(query, nil, &result); err != nil {
		return nil, err
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

// GetPRForBranch finds an open PR whose head matches the given branch.
// ownerRepo is "owner/repo", branch is just the branch name.
// Returns nil (not error) if no matching PR is found.
func GetPRForBranch(ownerRepo, branch string) (*PRInfo, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	parts := splitOwnerRepo(ownerRepo)
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/pulls?head=%s:%s&state=open&per_page=1",
		ownerRepo, parts[0], branch), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs for branch %s: %w", branch, err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// ListIssues lists issues for a repository, filtering out pull requests.
func ListIssues(ownerRepo, state string) ([]IssueInfo, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var all []IssueInfo
	err = client.Get(fmt.Sprintf("repos/%s/issues?state=%s&sort=updated&direction=desc&per_page=100",
		ownerRepo, state), &all)
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
func ListPRs(ownerRepo, state string) ([]PRInfo, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/pulls?state=%s&sort=updated&direction=desc&per_page=100",
		ownerRepo, state), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}
	return prs, nil
}

// SearchPRsByCommit finds pull requests associated with a given commit SHA.
func SearchPRsByCommit(ownerRepo, sha string) ([]PRInfo, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/commits/%s/pulls", ownerRepo, sha), &prs)
	if err != nil {
		return nil, fmt.Errorf("failed to search PRs by commit %s: %w", sha, err)
	}
	return prs, nil
}

// GetMergedPRForBranch finds a merged PR whose head matched the given branch.
// Returns nil (not error) if no matching merged PR is found.
func GetMergedPRForBranch(ownerRepo, branch string) (*PRInfo, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	parts := splitOwnerRepo(ownerRepo)
	var prs []PRInfo
	err = client.Get(fmt.Sprintf("repos/%s/pulls?head=%s:%s&state=closed&per_page=10",
		ownerRepo, parts[0], branch), &prs)
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
func SearchMergedPRs(ownerRepo string, limit int) ([]MergedPRInfo, error) {
	client, err := GraphQLClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL client: %w", err)
	}

	parts := splitOwnerRepo(ownerRepo)
	query := fmt.Sprintf(`query {
		repository(owner:"%s", name:"%s") {
			pullRequests(states:MERGED, first:%d, orderBy:{field:UPDATED_AT, direction:DESC}) {
				nodes {
					number
					title
					headRefName
					author { login }
				}
			}
		}
	}`, parts[0], parts[1], limit)

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

	if err := client.Do(query, nil, &result); err != nil {
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
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	jsonBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PR params: %w", err)
	}
	var pr PRInfo
	err = client.Post(fmt.Sprintf("repos/%s/pulls", ownerRepo), bytes.NewReader(jsonBytes), &pr)
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
func ListIssueComments(ownerRepo string, number int) ([]Comment, error) {
	client, err := RESTClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	var comments []Comment
	err = client.Get(fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", ownerRepo, number), &comments)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments for issue #%d: %w", number, err)
	}
	return comments, nil
}

func splitOwnerRepo(ownerRepo string) [2]string {
	parts := strings.SplitN(ownerRepo, "/", 2)
	var result [2]string
	for i, part := range parts {
		result[i] = part
	}
	return result
}
