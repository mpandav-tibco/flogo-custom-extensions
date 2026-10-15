package gitreposcaffolder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub is a minimal in-memory Git Data API: one repo, one branch, flat
// snapshots of path -> content per commit.
type fakeGitHub struct {
	mu        sync.Mutex
	empty     bool
	commits   map[string]map[string]string // commit sha -> files
	trees     map[string]map[string]string // tree sha -> files (root trees)
	treeOf    map[string]string            // commit -> tree
	head      string
	n         int
	sentPaths []string // paths sent in the last POST /git/trees
	writes    int      // POST/PATCH calls
	raceOnce  bool
}

func newFakeGitHub(files map[string]string) *fakeGitHub {
	f := &fakeGitHub{commits: map[string]map[string]string{}, trees: map[string]map[string]string{}, treeOf: map[string]string{}}
	if files == nil {
		f.empty = true
		return f
	}
	f.head = f.commit(files)
	return f
}

func (f *fakeGitHub) commit(files map[string]string) string {
	f.n++
	c, t := fmt.Sprintf("c%03d", f.n), f.treeID(files)
	f.commits[c], f.treeOf[c] = files, t
	return c
}

func (f *fakeGitHub) treeID(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k, v := range files {
		keys = append(keys, k+"="+blobSHA([]byte(v)))
	}
	sortStrings(keys)
	id := blobSHA([]byte(strings.Join(keys, "\n")))
	f.trees[id] = files
	return id
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// subtree id encodes "<root tree>|<folder>" so listFolder can walk into it.
func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/repos/o/r")
	reply := func(status int, v interface{}) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	var body map[string]interface{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch {
	case r.Method == http.MethodGet && p == "":
		size := 1
		if f.empty {
			size = 0
		}
		reply(200, map[string]interface{}{"size": size})
	case r.Method == http.MethodGet && p == "/git/ref/heads/main":
		if f.empty {
			reply(409, map[string]string{"message": "Git Repository is empty."})
			return
		}
		reply(200, map[string]interface{}{"object": map[string]string{"sha": f.head}})
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/git/commits/"):
		c := strings.TrimPrefix(p, "/git/commits/")
		reply(200, map[string]interface{}{"tree": map[string]string{"sha": f.treeOf[c]}})
	case r.Method == http.MethodGet && strings.HasPrefix(p, "/git/trees/"):
		id := strings.TrimPrefix(p, "/git/trees/")
		root, folder, _ := strings.Cut(id, "|")
		files := f.trees[root]
		var entries []map[string]string
		seen := map[string]bool{}
		prefix := ""
		if folder != "" {
			prefix = folder + "/"
		}
		for path, content := range files {
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			rel := strings.TrimPrefix(path, prefix)
			if r.URL.Query().Get("recursive") == "1" {
				entries = append(entries, map[string]string{"path": rel, "type": "blob", "mode": "100644", "sha": blobSHA([]byte(content))})
				continue
			}
			first, _, isDir := strings.Cut(rel, "/")
			if seen[first] {
				continue
			}
			seen[first] = true
			if isDir {
				entries = append(entries, map[string]string{"path": first, "type": "tree", "sha": root + "|" + prefix + first})
			} else {
				entries = append(entries, map[string]string{"path": first, "type": "blob", "sha": blobSHA([]byte(content))})
			}
		}
		reply(200, map[string]interface{}{"tree": entries, "truncated": false})
	case r.Method == http.MethodPost && p == "/git/trees":
		f.writes++
		base := f.trees[body["base_tree"].(string)]
		next := map[string]string{}
		for k, v := range base {
			next[k] = v
		}
		f.sentPaths = nil
		for _, raw := range body["tree"].([]interface{}) {
			e := raw.(map[string]interface{})
			path := e["path"].(string)
			f.sentPaths = append(f.sentPaths, path)
			if e["sha"] == nil && e["content"] == nil {
				delete(next, path)
			} else if c, ok := e["content"].(string); ok {
				next[path] = c
			}
		}
		reply(201, map[string]string{"sha": f.treeID(next)})
	case r.Method == http.MethodPost && p == "/git/commits":
		f.writes++
		c := f.commit(f.trees[body["tree"].(string)])
		reply(201, map[string]string{"sha": c})
	case r.Method == http.MethodPatch && p == "/git/refs/heads/main":
		f.writes++
		if f.raceOnce {
			f.raceOnce = false
			files := map[string]string{}
			for k, v := range f.commits[f.head] {
				files[k] = v
			}
			files["other/concurrent.txt"] = "x"
			f.head = f.commit(files)
			reply(422, map[string]string{"message": "Update is not a fast forward"})
			return
		}
		f.head = body["sha"].(string)
		reply(200, map[string]interface{}{})
	default:
		reply(404, map[string]string{"message": "not found: " + r.Method + " " + p})
	}
}

func apiFixture(t *testing.T, files map[string]string) (*fakeGitHub, githubAPI) {
	fake := newFakeGitHub(files)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return fake, githubAPI{base: srv.URL, token: "t"}
}

var apiRepo = RepoRef{Host: "github.com", Owner: "o", Name: "r"}

func apiOpts(folder string) WriteOptions {
	return WriteOptions{Branch: "main", Folder: folder, CleanFolder: true, Message: "m", AuthorName: "a", AuthorEmail: "a@a", Retries: 3}
}

func TestAPIWriteSendsOnlyChangedFiles(t *testing.T) {
	fake, api := apiFixture(t, map[string]string{
		"keep/other.txt": "other integration",
		"int1/same.txt":  "same",
		"int1/old.txt":   "to be removed",
		"int1/edit.txt":  "v1",
	})
	files := []FileEntry{
		{Path: "same.txt", Data: []byte("same")},
		{Path: "edit.txt", Data: []byte("v2")},
		{Path: "sub/new.txt", Data: []byte("new")},
	}
	res, err := api.WriteFilesAPI(context.Background(), apiRepo, files, apiOpts("int1"))
	if err != nil || !res.Pushed {
		t.Fatalf("write: %v %+v", err, res)
	}
	sortStrings(fake.sentPaths)
	if got := strings.Join(fake.sentPaths, ","); got != "int1/edit.txt,int1/old.txt,int1/sub/new.txt" {
		t.Fatalf("sent %s; unchanged files must not be sent", got)
	}
	head := fake.commits[fake.head]
	if head["keep/other.txt"] != "other integration" || head["int1/edit.txt"] != "v2" || head["int1/sub/new.txt"] != "new" {
		t.Fatalf("result %v", head)
	}
	if _, stale := head["int1/old.txt"]; stale {
		t.Fatal("old.txt should be deleted")
	}

	// identical re-run: read-only, no commit
	writes := fake.writes
	res2, err := api.WriteFilesAPI(context.Background(), apiRepo, files, apiOpts("int1"))
	if err != nil || res2.Pushed || res2.CommitSHA != res.CommitSHA || fake.writes != writes {
		t.Fatalf("re-run should not write: %v %+v writes=%d->%d", err, res2, writes, fake.writes)
	}
}

func TestAPIWriteNewFolder(t *testing.T) {
	fake, api := apiFixture(t, map[string]string{"README.md": "shared"})
	res, err := api.WriteFilesAPI(context.Background(), apiRepo, []FileEntry{{Path: "a.txt", Data: []byte("1")}}, apiOpts("brand-new"))
	if err != nil || !res.Pushed || fake.commits[fake.head]["brand-new/a.txt"] != "1" || fake.commits[fake.head]["README.md"] != "shared" {
		t.Fatalf("%v %+v %v", err, res, fake.commits[fake.head])
	}
}

func TestAPIWriteEmptyRepoIsNotApplicable(t *testing.T) {
	_, api := apiFixture(t, nil)
	_, err := api.WriteFilesAPI(context.Background(), apiRepo, []FileEntry{{Path: "a", Data: []byte("1")}}, apiOpts("f"))
	if !errors.Is(err, errAPINotApplicable) {
		t.Fatalf("want errAPINotApplicable, got %v", err)
	}
}

func TestAPIWriteRetriesLostRace(t *testing.T) {
	fake, api := apiFixture(t, map[string]string{"README.md": "shared"})
	fake.raceOnce = true
	res, err := api.WriteFilesAPI(context.Background(), apiRepo, []FileEntry{{Path: "a.txt", Data: []byte("1")}}, apiOpts("mine"))
	if err != nil || !res.Pushed {
		t.Fatalf("expected success after retry: %v", err)
	}
	head := fake.commits[fake.head]
	if head["other/concurrent.txt"] != "x" || head["mine/a.txt"] != "1" {
		t.Fatalf("both writers' files expected: %v", head)
	}
}

func TestUseAPIDecision(t *testing.T) {
	in := newInput()
	gh := RepoRef{Host: "github.com"}
	gl := RepoRef{Host: "gitlab.example.com"}
	if !useAPI(in, gh) || useAPI(in, gl) {
		t.Fatal("auto: api for github.com only by default")
	}
	in.APIBaseURL = "https://ghe.example.com/api/v3"
	if !useAPI(in, RepoRef{Host: "ghe.example.com"}) {
		t.Fatal("auto: api when an Enterprise API base is configured")
	}
	in.WriteStrategy = WriteStrategyGit
	if useAPI(in, gh) {
		t.Fatal("git strategy must not use the API")
	}
}
