package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	gogh "github.com/cli/go-gh/v2"
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

// IsInstalled checks if gh CLI is available on PATH.
func IsInstalled() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// IsAuthenticated checks if gh CLI is authenticated.
func IsAuthenticated() bool {
	_, _, err := gogh.Exec("auth", "status")
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
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
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
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
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

// ExecInteractive runs a gh command with stdin/stdout/stderr connected to the terminal.
func ExecInteractive(args ...string) error {
	_, _, err := gogh.Exec(args...)
	return err
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

func splitOwnerRepo(ownerRepo string) [2]string {
	parts := strings.SplitN(ownerRepo, "/", 2)
	var result [2]string
	for i, part := range parts {
		result[i] = part
	}
	return result
}
