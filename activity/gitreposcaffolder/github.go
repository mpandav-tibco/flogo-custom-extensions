package gitreposcaffolder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var errRepoExists = errors.New("repository already exists")

var httpClient = &http.Client{}

// RepoRef identifies a hosted repository parsed from its clone URL.
type RepoRef struct {
	Host  string
	Owner string
	Name  string
}

func (r RepoRef) WebURL() string { return "https://" + r.Host + "/" + r.Owner + "/" + r.Name }

// ParseRepoURL accepts https://host/owner/name(.git).
func ParseRepoURL(raw string) (RepoRef, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return RepoRef{}, fmt.Errorf("repository URL %q must look like https://host/owner/name", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return RepoRef{}, fmt.Errorf("repository URL %q must look like https://host/owner/name", raw)
	}
	return RepoRef{Host: u.Host, Owner: parts[0], Name: strings.TrimSuffix(parts[1], ".git")}, nil
}

type githubAPI struct {
	base  string
	token string
}

func (g githubAPI) call(ctx context.Context, method, p string, body interface{}) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(g.base, "/")+p, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "flogo-git-repo-scaffolder")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

func apiMessage(status int, data []byte) string {
	var m struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(data, &m)
	msg := m.Message
	for _, e := range m.Errors {
		if e.Message != "" {
			msg += "; " + e.Message
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, msg)
}

// EnsureRepo creates owner/name (under the user or the organisation) if it does
// not exist. It returns created=false when the repository was already there.
func (g githubAPI) EnsureRepo(ctx context.Context, repo RepoRef, private bool, description string, failIfExists bool) (bool, error) {
	status, data, err := g.call(ctx, http.MethodGet, "/repos/"+url.PathEscape(repo.Owner)+"/"+url.PathEscape(repo.Name), nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		if failIfExists {
			return false, fmt.Errorf("%w: %s/%s", errRepoExists, repo.Owner, repo.Name)
		}
		return false, nil
	case http.StatusNotFound:
	default:
		return false, fmt.Errorf("check repository: %s", apiMessage(status, data))
	}

	status, data, err = g.call(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("identify token owner: %s", apiMessage(status, data))
	}
	var user struct {
		Login string `json:"login"`
	}
	_ = json.Unmarshal(data, &user)

	createPath := "/orgs/" + url.PathEscape(repo.Owner) + "/repos"
	if strings.EqualFold(user.Login, repo.Owner) {
		createPath = "/user/repos"
	}
	body := map[string]interface{}{
		"name":        repo.Name,
		"private":     private,
		"description": description,
		"auto_init":   false,
	}
	status, data, err = g.call(ctx, http.MethodPost, createPath, body)
	if err != nil {
		return false, err
	}
	if status == http.StatusUnprocessableEntity && strings.Contains(strings.ToLower(string(data)), "already exists") {
		if failIfExists {
			return false, fmt.Errorf("%w: %s/%s", errRepoExists, repo.Owner, repo.Name)
		}
		return false, nil
	}
	if status != http.StatusCreated {
		return false, fmt.Errorf("create repository: %s", apiMessage(status, data))
	}
	return true, nil
}

// SetDefaultBranch is needed after the first push: an API-created empty repository
// keeps the account's configured default name, which may differ from the pushed branch.
func (g githubAPI) SetDefaultBranch(ctx context.Context, repo RepoRef, branch string) error {
	status, data, err := g.call(ctx, http.MethodPatch, "/repos/"+url.PathEscape(repo.Owner)+"/"+url.PathEscape(repo.Name),
		map[string]interface{}{"default_branch": branch})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("set default branch: %s", apiMessage(status, data))
	}
	return nil
}
