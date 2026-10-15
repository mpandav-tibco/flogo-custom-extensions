package gitreposcaffolder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-billy/v5/util"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

var errPushConflict = errors.New("remote branch moved while pushing")

// beforePushHook lets tests simulate a concurrent writer.
var beforePushHook = func() {}

func basicAuth(token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	// GitHub/GitLab/Bitbucket accept a token as the password with any non-empty user.
	return &githttp.BasicAuth{Username: "x-access-token", Password: token}
}

// resolveRef lists the remote and returns the full ref name and commit hash for
// ref ("" = the remote's default branch). ok=false means the remote is empty.
func resolveRef(ctx context.Context, url, ref string, auth transport.AuthMethod) (plumbing.ReferenceName, plumbing.Hash, bool, error) {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := rem.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			return "", plumbing.ZeroHash, false, nil
		}
		return "", plumbing.ZeroHash, false, err
	}
	byName := make(map[plumbing.ReferenceName]*plumbing.Reference, len(refs))
	for _, r := range refs {
		byName[r.Name()] = r
	}
	if len(byName) == 0 {
		return "", plumbing.ZeroHash, false, nil
	}

	var candidates []plumbing.ReferenceName
	if ref == "" {
		head, ok := byName[plumbing.HEAD]
		if !ok {
			return "", plumbing.ZeroHash, false, fmt.Errorf("remote has no HEAD; set the ref explicitly")
		}
		if head.Type() == plumbing.SymbolicReference {
			candidates = []plumbing.ReferenceName{head.Target()}
		} else {
			for n, r := range byName {
				if n.IsBranch() && r.Hash() == head.Hash() {
					candidates = append(candidates, n)
				}
			}
		}
	} else {
		candidates = []plumbing.ReferenceName{
			plumbing.ReferenceName(ref),
			plumbing.NewBranchReferenceName(ref),
			plumbing.NewTagReferenceName(ref),
		}
	}
	for _, c := range candidates {
		r, ok := byName[c]
		if !ok {
			continue
		}
		h := r.Hash()
		// Annotated tags advertise a peeled entry pointing at the commit.
		if peeled, ok := byName[plumbing.ReferenceName(string(c)+"^{}")]; ok {
			h = peeled.Hash()
		}
		return c, h, true, nil
	}
	return "", plumbing.ZeroHash, true, fmt.Errorf("ref %q not found on remote", ref)
}

type templateSnapshot struct {
	commit string
	files  []FileEntry
}

const maxCachedTemplates = 8

var templateCache = struct {
	sync.Mutex
	byKey map[string]*templateSnapshot
}{byKey: map[string]*templateSnapshot{}}

// FetchTemplate returns every file under subPath (paths relative to subPath) at
// the current commit of ref. Snapshots are cached per resolved commit, so a
// repeat call costs one ref listing.
func FetchTemplate(ctx context.Context, url, ref, subPath string, auth transport.AuthMethod) ([]FileEntry, string, error) {
	refName, hash, nonEmpty, err := resolveRef(ctx, url, ref, auth)
	if err != nil {
		return nil, "", err
	}
	if !nonEmpty {
		return nil, "", fmt.Errorf("template repository is empty")
	}

	key := url + "#" + string(refName)
	templateCache.Lock()
	snap := templateCache.byKey[key]
	templateCache.Unlock()

	if snap == nil || snap.commit != hash.String() {
		snap, err = loadSnapshot(ctx, url, refName, auth)
		if err != nil {
			return nil, "", err
		}
		templateCache.Lock()
		if _, ok := templateCache.byKey[key]; !ok && len(templateCache.byKey) >= maxCachedTemplates {
			for k := range templateCache.byKey {
				delete(templateCache.byKey, k)
				break
			}
		}
		templateCache.byKey[key] = snap
		templateCache.Unlock()
	}

	prefix := strings.Trim(path.Clean("/"+strings.ReplaceAll(subPath, "\\", "/")), "/")
	var out []FileEntry
	for _, f := range snap.files {
		rel := f.Path
		if prefix != "" {
			if !strings.HasPrefix(rel, prefix+"/") {
				continue
			}
			rel = strings.TrimPrefix(rel, prefix+"/")
		}
		out = append(out, FileEntry{Path: rel, Data: f.Data, Executable: f.Executable})
	}
	if len(out) == 0 {
		return nil, snap.commit, fmt.Errorf("template path %q has no files at %s", subPath, short(snap.commit))
	}
	return out, snap.commit, nil
}

func loadSnapshot(ctx context.Context, url string, refName plumbing.ReferenceName, auth transport.AuthMethod) (*templateSnapshot, error) {
	repo, err := git.CloneContext(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL:           url,
		Auth:          auth,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err != nil {
		return nil, fmt.Errorf("clone template: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	snap := &templateSnapshot{commit: head.Hash().String()}
	err = tree.Files().ForEach(func(f *object.File) error {
		if f.Mode == filemode.Submodule || f.Mode == filemode.Symlink {
			return nil
		}
		r, err := f.Reader()
		if err != nil {
			return err
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		snap.files = append(snap.files, FileEntry{Path: f.Name, Data: data, Executable: f.Mode == filemode.Executable})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// per-target lock so concurrent flows in one engine don't race each other on the same branch
var targetLocks sync.Map

func lockTarget(key string) func() {
	m, _ := targetLocks.LoadOrStore(key, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type WriteOptions struct {
	URL         string
	Branch      string
	Folder      string
	CleanFolder bool
	Message     string
	AuthorName  string
	AuthorEmail string
	Auth        transport.AuthMethod
	Retries     int
}

type WriteResult struct {
	CommitSHA string
	Pushed    bool
}

// WriteFiles writes files under opts.Folder on opts.Branch as one commit and
// pushes it, re-reading the branch and retrying if someone else pushed first.
func WriteFiles(ctx context.Context, files []FileEntry, opts WriteOptions) (*WriteResult, error) {
	unlock := lockTarget(opts.URL + "#" + opts.Branch)
	defer unlock()

	if opts.Retries < 1 {
		opts.Retries = 1
	}
	var lastErr error
	for attempt := 0; attempt < opts.Retries; attempt++ {
		res, err := writeOnce(ctx, files, opts)
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

func writeOnce(ctx context.Context, files []FileEntry, opts WriteOptions) (*WriteResult, error) {
	branchRef := plumbing.NewBranchReferenceName(opts.Branch)
	_, _, nonEmpty, err := resolveRef(ctx, opts.URL, "", opts.Auth)
	if err != nil {
		return nil, fmt.Errorf("read target repository: %w", err)
	}

	var repo *git.Repository
	if nonEmpty {
		repo, err = git.CloneContext(ctx, memory.NewStorage(), memfs.New(), &git.CloneOptions{
			URL:           opts.URL,
			Auth:          opts.Auth,
			ReferenceName: branchRef,
			SingleBranch:  true,
			Depth:         1,
			Tags:          git.NoTags,
		})
		if err != nil {
			if errors.Is(err, plumbing.ErrReferenceNotFound) || strings.Contains(err.Error(), "couldn't find remote ref") {
				return nil, fmt.Errorf("branch %q does not exist in the target repository", opts.Branch)
			}
			return nil, fmt.Errorf("clone target: %w", err)
		}
	} else {
		repo, err = git.Init(memory.NewStorage(), memfs.New())
		if err != nil {
			return nil, err
		}
		if _, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{opts.URL}}); err != nil {
			return nil, err
		}
		if err = repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef)); err != nil {
			return nil, err
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	base := plumbing.ZeroHash
	if head, err := repo.Head(); err == nil {
		base = head.Hash()
	}
	fs := wt.Filesystem
	folder := strings.Trim(path.Clean("/"+opts.Folder), "/")

	if opts.CleanFolder {
		if err := cleanFolder(fs, folder); err != nil {
			return nil, fmt.Errorf("clean target folder: %w", err)
		}
	}
	for _, f := range files {
		perm := os.FileMode(0o644)
		if f.Executable {
			perm = 0o755
		}
		if err := util.WriteFile(fs, path.Join(folder, f.Path), f.Data, perm); err != nil {
			return nil, fmt.Errorf("write %s: %w", f.Path, err)
		}
	}

	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return nil, fmt.Errorf("stage changes: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	if status.IsClean() {
		head, err := repo.Head()
		if err != nil {
			return nil, err
		}
		return &WriteResult{CommitSHA: head.Hash().String(), Pushed: false}, nil
	}

	sig := &object.Signature{Name: opts.AuthorName, Email: opts.AuthorEmail, When: nowFunc()}
	hash, err := wt.Commit(opts.Message, &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	spec := config.RefSpec(string(branchRef) + ":" + string(branchRef))
	beforePushHook()
	err = repo.PushContext(ctx, &git.PushOptions{RemoteName: "origin", Auth: opts.Auth, RefSpecs: []config.RefSpec{spec}})
	if err != nil {
		// A shallow clone can surface a lost race as "object not found", so compare the remote tip instead of the error text.
		if errors.Is(err, git.ErrNonFastForwardUpdate) || branchMoved(ctx, opts, base) {
			return nil, fmt.Errorf("%w: %v", errPushConflict, err)
		}
		return nil, fmt.Errorf("push: %w", err)
	}
	return &WriteResult{CommitSHA: hash.String(), Pushed: true}, nil
}

func branchMoved(ctx context.Context, opts WriteOptions, base plumbing.Hash) bool {
	_, tip, _, err := resolveRef(ctx, opts.URL, opts.Branch, opts.Auth)
	if err != nil {
		return false
	}
	return tip != base
}

// cleanFolder removes the folder's current contents ("" = the whole worktree).
func cleanFolder(fs billy.Filesystem, folder string) error {
	if folder != "" {
		return util.RemoveAll(fs, folder)
	}
	entries, err := fs.ReadDir("/")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := util.RemoveAll(fs, e.Name()); err != nil {
			return err
		}
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
