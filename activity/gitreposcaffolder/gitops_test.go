package gitreposcaffolder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

var testSig = &object.Signature{Name: "t", Email: "t@t", When: time.Unix(0, 0)}

// newBareRepo creates a bare repo seeded (on branch main) with files, or empty when files is nil.
func newBareRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	}); err != nil {
		t.Fatal(err)
	}
	if files != nil {
		commitFiles(t, dir, files)
	}
	return "file://" + dir
}

func commitFiles(t *testing.T, bare string, files map[string]string) {
	t.Helper()
	work := t.TempDir()
	repo, err := git.PlainClone(work, false, &git.CloneOptions{URL: bare})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		_ = os.RemoveAll(work)
		repo, err = git.PlainInitWithOptions(work, &git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		})
		if err == nil {
			_, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{bare}})
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	for p, c := range files {
		full := filepath.Join(work, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: testSig}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{"refs/heads/main:refs/heads/main"}}); err != nil {
		t.Fatal(err)
	}
}

// readRemote returns the files on branch main of a bare repo.
func readRemote(t *testing.T, url string) map[string]string {
	t.Helper()
	repo, err := git.PlainOpen(url[len("file://"):])
	if err != nil {
		t.Fatal(err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := repo.CommitObject(ref.Hash())
	tree, _ := c.Tree()
	out := map[string]string{}
	_ = tree.Files().ForEach(func(f *object.File) error {
		r, _ := f.Reader()
		b, _ := io.ReadAll(r)
		r.Close()
		out[f.Name] = string(b)
		return nil
	})
	return out
}

func writeOpts(url, folder string) WriteOptions {
	return WriteOptions{URL: url, Branch: "main", Folder: folder, CleanFolder: true, Message: "scaffold",
		AuthorName: "a", AuthorEmail: "a@a", Retries: 3}
}

func TestFetchTemplateSubPathAndCache(t *testing.T) {
	url := newBareRepo(t, map[string]string{
		"templates/T1/a.txt":     "one",
		"templates/T1/sub/b.txt": "two",
		"templates/T2/c.txt":     "three",
	})
	ctx := context.Background()
	files, commit, err := FetchTemplate(ctx, url, "", "templates/T1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || commit == "" {
		t.Fatalf("files=%v commit=%q", names(files), commit)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = string(f.Data)
	}
	if got["a.txt"] != "one" || got["sub/b.txt"] != "two" {
		t.Fatalf("unexpected content %v", got)
	}

	// cached snapshot is reused for the same commit and refreshed after a new one
	if _, again, _ := FetchTemplate(ctx, url, "main", "templates/T1", nil); again != commit {
		t.Fatalf("same commit expected, got %s vs %s", again, commit)
	}
	commitFiles(t, url[len("file://"):], map[string]string{"templates/T1/a.txt": "ONE"})
	files, newCommit, err := FetchTemplate(ctx, url, "", "templates/T1", nil)
	if err != nil || newCommit == commit {
		t.Fatalf("expected a refreshed snapshot: %v %s", err, newCommit)
	}
	for _, f := range files {
		if f.Path == "a.txt" && string(f.Data) != "ONE" {
			t.Fatalf("stale cache: %q", f.Data)
		}
	}

	if _, _, err := FetchTemplate(ctx, url, "", "templates/missing", nil); err == nil {
		t.Fatal("missing template path should fail")
	}
	if _, _, err := FetchTemplate(ctx, url, "no-such-branch", "", nil); err == nil {
		t.Fatal("missing ref should fail")
	}
}

func TestWriteSubfolderPreservesOtherFolders(t *testing.T) {
	url := newBareRepo(t, map[string]string{"other-integration/keep.txt": "keep", "README.md": "shared"})
	ctx := context.Background()

	res, err := WriteFiles(ctx, []FileEntry{{Path: "x.txt", Data: []byte("1")}, {Path: "d/y.txt", Data: []byte("2")}}, writeOpts(url, "new-int"))
	if err != nil || !res.Pushed {
		t.Fatalf("write: %v %+v", err, res)
	}
	got := readRemote(t, url)
	if got["other-integration/keep.txt"] != "keep" || got["README.md"] != "shared" || got["new-int/x.txt"] != "1" || got["new-int/d/y.txt"] != "2" {
		t.Fatalf("remote content wrong: %v", got)
	}

	// identical re-run is a no-op
	res2, err := WriteFiles(ctx, []FileEntry{{Path: "x.txt", Data: []byte("1")}, {Path: "d/y.txt", Data: []byte("2")}}, writeOpts(url, "new-int"))
	if err != nil || res2.Pushed || res2.CommitSHA != res.CommitSHA {
		t.Fatalf("re-run should not push: %v %+v", err, res2)
	}

	// clean mode removes files no longer produced by the template
	if _, err := WriteFiles(ctx, []FileEntry{{Path: "x.txt", Data: []byte("1b")}}, writeOpts(url, "new-int")); err != nil {
		t.Fatal(err)
	}
	got = readRemote(t, url)
	if _, stale := got["new-int/d/y.txt"]; stale || got["new-int/x.txt"] != "1b" || got["other-integration/keep.txt"] != "keep" {
		t.Fatalf("clean folder wrong: %v", got)
	}
}

func TestWriteIntoEmptyRepo(t *testing.T) {
	url := newBareRepo(t, nil)
	res, err := WriteFiles(context.Background(), []FileEntry{{Path: "a.txt", Data: []byte("hi")}, {Path: "run.sh", Data: []byte("#!/bin/sh"), Executable: true}}, writeOpts(url, ""))
	if err != nil || !res.Pushed {
		t.Fatalf("write: %v %+v", err, res)
	}
	got := readRemote(t, url)
	if got["a.txt"] != "hi" || got["run.sh"] != "#!/bin/sh" {
		t.Fatalf("remote: %v", got)
	}
}

func TestWriteMissingBranchFails(t *testing.T) {
	url := newBareRepo(t, map[string]string{"a": "1"})
	o := writeOpts(url, "f")
	o.Branch = "develop"
	if _, err := WriteFiles(context.Background(), []FileEntry{{Path: "x", Data: []byte("1")}}, o); err == nil {
		t.Fatal("expected an error for a missing branch on a non-empty repo")
	}
}

func TestPushConflictIsRetried(t *testing.T) {
	url := newBareRepo(t, map[string]string{"a": "1"})
	bare := url[len("file://"):]

	// Simulate another writer landing a commit between our clone and our push, once.
	orig := beforePushHook
	defer func() { beforePushHook = orig }()
	fired := false
	writes := 0
	beforePushHook = func() {
		if !fired {
			fired = true
			writes++
			commitFiles(t, bare, map[string]string{fmt.Sprintf("concurrent/z%d.txt", writes): "z"})
		}
	}

	res, err := WriteFiles(context.Background(), []FileEntry{{Path: "x.txt", Data: []byte("1")}}, writeOpts(url, "mine"))
	if err != nil || !res.Pushed {
		t.Fatalf("expected success after retry: %v", err)
	}
	got := readRemote(t, url)
	if got["concurrent/z1.txt"] != "z" || got["mine/x.txt"] != "1" {
		t.Fatalf("both writers' files expected: %v", got)
	}

	// with a single attempt the conflict surfaces as errPushConflict
	fired = false
	o := writeOpts(url, "mine2")
	o.Retries = 1
	if _, err := WriteFiles(context.Background(), []FileEntry{{Path: "x.txt", Data: []byte("1")}}, o); !errors.Is(err, errPushConflict) {
		t.Fatalf("want errPushConflict, got %v", err)
	}
}

func TestCleanFolderRoot(t *testing.T) {
	url := newBareRepo(t, map[string]string{"old.txt": "o", "dir/old2.txt": "o"})
	if _, err := WriteFiles(context.Background(), []FileEntry{{Path: "new.txt", Data: []byte("n")}}, writeOpts(url, "")); err != nil {
		t.Fatal(err)
	}
	got := readRemote(t, url)
	if len(got) != 1 || got["new.txt"] != "n" {
		t.Fatalf("root clean should leave only new files: %v", got)
	}
}
