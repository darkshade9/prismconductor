package githubauth

import (
	"context"

	gh "github.com/google/go-github/v62/github"
)

// User identifies the authenticated GitHub user. Surfaced in the UI header.
type User struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// FetchUser hits /user with the cached token. Used right after login to confirm.
func FetchUser(ctx context.Context, token string) (*User, error) {
	c := gh.NewClient(nil).WithAuthToken(token)
	u, _, err := c.Users.Get(ctx, "")
	if err != nil {
		return nil, err
	}
	return &User{
		Login:     u.GetLogin(),
		Name:      u.GetName(),
		AvatarURL: u.GetAvatarURL(),
	}, nil
}

// FetchRepos returns repos the authenticated user has access to (owner + collaborator).
// Used by the workspace-onboarding flow so the user picks from a list rather than typing a path.
func FetchRepos(ctx context.Context, token string) ([]*gh.Repository, error) {
	c := gh.NewClient(nil).WithAuthToken(token)
	opts := &gh.RepositoryListByAuthenticatedUserOptions{
		Affiliation: "owner,collaborator",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	var all []*gh.Repository
	for {
		page, resp, err := c.Repositories.ListByAuthenticatedUser(ctx, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}
