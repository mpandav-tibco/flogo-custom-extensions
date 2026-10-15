package gitreposcaffolder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/go-git/go-git/v5/plumbing"
)

// errAPINotApplicable means the Git Data API cannot do this write (e.g. the
// repository is still empty); the caller falls back to git transport.
var errAPINotApplicable = errors.New("git data API not applicable")

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	Sha  string `json:"sha"`
}

func blobSHA(data []byte) string {
	return plumbing.ComputeHash(plumbing.BlobObject, data).String()
}

func (g githubAPI) repoPath(repo RepoRef) string {
	return "/repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Name)
}

func (g githubAPI) getJSON(ctx context.Context, p string, into interface{}) (int, error) {
	status, data, err := g.call(ctx, http.MethodGet, p, nil)
	if err != nil {
		return 0, err
	}
	if status == http.StatusOK {
		return status, json.Unmarshal(data, into)
	}
	return status, fmt.Errorf("GET %s: %s", p, apiMessage(status, data))
}

func (g githubAPI) postJSON(ctx context.Context, method, p string, body, into interface{}) (int, error) {
	status, data, err := g.call(ctx, method, p, body)
	if err != nil {
		return 0, err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return status, json.Unmarshal(data, into)
	}
	return status, fmt.Errorf("%s %s: %s", method, p, apiMessage(status, data))
}

// listFolder returns path -> blob sha for every file below folder ("" = whole tree).
func (g githubAPI) listFolder(ctx context.Context, repo RepoRef, rootTree, folder string) (map[string]treeEntry, error) {
	sha := rootTree
	if folder != "" {
		for _, seg := range strings.Split(folder, "/") {
			var t struct {
				Tree []treeEntry `json:"tree"`
			}
			if _, err := g.getJSON(ctx, g.repoPath(repo)+"/git/trees/"+sha, &t); err != nil {
				return nil, err
			}
			next := ""
			for _, e := range t.Tree {
				if e.Path == seg && e.Type == "tree" {
					next = e.Sha
				}
			}
			if next == "" {
				return map[string]treeEntry{}, nil
			}
			sha = next
		}
	}
	var t struct {
		Tree      []treeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if _, err := g.getJSON(ctx, g.repoPath(repo)+"/git/trees/"+sha+"?recursive=1", &t); err != nil {
		return nil, err
	}
	if t.Truncated {
		return nil, fmt.Errorf("%w: folder listing truncated by the API", errAPINotApplicable)
	}
	out := make(map[string]treeEntry, len(t.Tree))
	for _, e := range t.Tree {
		if e.Type == "blob" {
			e.Path = path.Join(folder, e.Path)
			out[e.Path] = e
		}
	}
	return out, nil
}

// WriteFilesAPI commits files under opts.Folder through the GitHub Git Data API:
// nothing is cloned, unchanged files are not sent, and an identical result
// creates no commit. Cost does not grow with the size of the target repository.
func (g githubAPI) WriteFilesAPI(ctx context.Context, repo RepoRef, files []FileEntry, opts WriteOptions) (*WriteResult, error) {
	unlock := lockTarget(repo.WebURL() + "#" + opts.Branch)
	defer unlock()
	if opts.Retries < 1 {
		opts.Retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < opts.Retries; attempt++ {
		res, err := g.writeAPIOnce(ctx, repo, files, opts)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, errPushConflict) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w after %d attempt(s)", lastErr, opts.Retries)
}

func (g githubAPI) writeAPIOnce(ctx context.Context, repo RepoRef, files []FileEntry, opts WriteOptions) (*WriteResult, error) {
	rp := g.repoPath(repo)
	var ref struct {
		Object struct {
			Sha string `json:"sha"`
		} `json:"object"`
	}
	status, err := g.getJSON(ctx, rp+"/git/ref/heads/"+url.PathEscape(opts.Branch), &ref)
	switch {
	case status == http.StatusConflict:
		return nil, fmt.Errorf("%w: repository is empty", errAPINotApplicable)
	case status == http.StatusNotFound:
		var r struct {
			Size int `json:"size"`
		}
		if s, _ := g.getJSON(ctx, rp, &r); s == http.StatusOK && r.Size == 0 {
			return nil, fmt.Errorf("%w: repository is empty", errAPINotApplicable)
		}
		return nil, fmt.Errorf("branch %q does not exist in %s/%s", opts.Branch, repo.Owner, repo.Name)
	case err != nil:
		return nil, err
	}
	head := ref.Object.Sha

	var commit struct {
		Tree struct {
			Sha string `json:"sha"`
		} `json:"tree"`
	}
	if _, err := g.getJSON(ctx, rp+"/git/commits/"+head, &commit); err != nil {
		return nil, err
	}

	folder := strings.Trim(path.Clean("/"+opts.Folder), "/")
	existing, err := g.listFolder(ctx, repo, commit.Tree.Sha, folder)
	if err != nil {
		return nil, err
	}

	var entries []map[string]interface{}
	wanted := make(map[string]bool, len(files))
	for _, f := range files {
		p := path.Join(folder, f.Path)
		wanted[p] = true
		mode := "100644"
		if f.Executable {
			mode = "100755"
		}
		if e, ok := existing[p]; ok && e.Sha == blobSHA(f.Data) && e.Mode == mode {
			continue
		}
		entry := map[string]interface{}{"path": p, "mode": mode, "type": "blob"}
		if utf8.Valid(f.Data) {
			entry["content"] = string(f.Data)
		} else {
			var blob struct {
				Sha string `json:"sha"`
			}
			if _, err := g.postJSON(ctx, http.MethodPost, rp+"/git/blobs", map[string]string{
				"content": base64.StdEncoding.EncodeToString(f.Data), "encoding": "base64",
			}, &blob); err != nil {
				return nil, err
			}
			entry["sha"] = blob.Sha
		}
		entries = append(entries, entry)
	}
	if opts.CleanFolder {
		for p, e := range existing {
			if !wanted[p] {
				entries = append(entries, map[string]interface{}{"path": p, "mode": e.Mode, "type": "blob", "sha": nil})
			}
		}
	}
	if len(entries) == 0 {
		return &WriteResult{CommitSHA: head, Pushed: false}, nil
	}

	var tree struct {
		Sha string `json:"sha"`
	}
	if _, err := g.postJSON(ctx, http.MethodPost, rp+"/git/trees", map[string]interface{}{
		"base_tree": commit.Tree.Sha, "tree": entries,
	}, &tree); err != nil {
		return nil, err
	}
	if tree.Sha == commit.Tree.Sha {
		return &WriteResult{CommitSHA: head, Pushed: false}, nil
	}

	sig := map[string]string{"name": opts.AuthorName, "email": opts.AuthorEmail, "date": nowFunc().UTC().Format("2006-01-02T15:04:05Z")}
	var newCommit struct {
		Sha string `json:"sha"`
	}
	if _, err := g.postJSON(ctx, http.MethodPost, rp+"/git/commits", map[string]interface{}{
		"message": opts.Message, "tree": tree.Sha, "parents": []string{head}, "author": sig, "committer": sig,
	}, &newCommit); err != nil {
		return nil, err
	}

	beforePushHook()
	var updated struct{}
	status, err = g.postJSON(ctx, http.MethodPatch, rp+"/git/refs/heads/"+url.PathEscape(opts.Branch), map[string]interface{}{
		"sha": newCommit.Sha, "force": false,
	}, &updated)
	if err != nil {
		if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
			return nil, fmt.Errorf("%w: %v", errPushConflict, err)
		}
		return nil, err
	}
	return &WriteResult{CommitSHA: newCommit.Sha, Pushed: true}, nil
}
