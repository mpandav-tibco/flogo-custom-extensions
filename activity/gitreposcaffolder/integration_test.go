//go:build integration

// Live test against GitHub. Run with:
//
//	GITHUB_TOKEN=... GITHUB_OWNER=... go test -tags integration -run Live -v ./...
package gitreposcaffolder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/project-flogo/core/support/log"
)

const liveTemplate = "KAFKATopic_To_REST_SERVICE"

// sampleTemplate is a small BWCE-style project using the token conventions of a real template library.
func sampleTemplate() map[string]string {
	m := "templates/" + liveTemplate + "/"
	return map[string]string{
		m + liveTemplate + ".module/META-INF/MANIFEST.MF":                    "Manifest-Version: 1.0\nBundle-Name: " + liveTemplate + ".module\nBundle-SymbolicName: " + liveTemplate + ".module\n",
		m + liveTemplate + ".module/META-INF/default.substvar":               "<globalVariable><name>InterfaceID</name><value>BWCEInterfaceID</value></globalVariable>\n<globalVariable><name>Repo</name><value>BWCERepoName</value></globalVariable>\n<globalVariable><name>Exceptions</name><value>D:\\BWCE\\Framework\\ExceptionLookup.csv</value></globalVariable>\n",
		m + liveTemplate + ".module/Processes/SystemName_ObjectName_Sub.bwp": "<process name=\"SystemName_ObjectName_Sub\">\n  <topic>SystemName.Inbound</topic>\n  <cdm>SystemName_ObjectName_CDM_SUB</cdm>\n</process>\n",
		m + liveTemplate + ".module/Resources/logo.png":                      "\x89PNG\r\n\x1a\n\x00ObjectName-binary",
		m + liveTemplate + ".module/.DS_Store":                               "\x00\x00\x00\x01Bud1",
		m + liveTemplate + ".parent/pom.xml":                                 "<project><artifactId>" + liveTemplate + ".parent</artifactId><module>../" + liveTemplate + ".module</module></project>\n",
		m + "deployment.yaml":                                                "name: BWCERepoName\nservice: BWCEServiceName\nsource: BWCESourceSystem\ntarget: BWCETargetSystem\n",
		m + "catalog-info.yaml":                                              "kind: Component # tooling metadata, excluded\n",
		"templates/README.md":                                                "Test template library for the Git Repo Scaffolder activity.\n",
	}
}

// replacementsFor mirrors how a flow would build the token map from a spec.
func replacementsFor(source, object, target, itype string) map[string]interface{} {
	repo := fmt.Sprintf("%s-%s-%s-%s", source, object, target, itype)
	r := map[string]interface{}{
		"BWCEInterfaceID":                       "IKD-001",
		"BWCERepoName":                          repo,
		"BWCEServiceName":                       strings.ReplaceAll(repo, "-", ""),
		"BWCESourceSystem":                      source,
		"BWCETargetSystem":                      target,
		"SystemName":                            source,
		"ObjectName":                            object,
		liveTemplate + ".module":                repo + ".module",
		liveTemplate + ".parent":                repo + ".parent",
		liveTemplate:                            repo,
		`D:\BWCE\Framework\ExceptionLookup.csv`: "/resources/addons/exceptionLookup/ExceptionLookup.csv",
	}
	if itype == "pub" {
		r["SystemName.Inbound"] = "SystemName.Outbound"
		r["SystemName_ObjectName_CDM_SUB"] = "SystemName_ObjectName_cdm_PUB"
		r["SystemName_ObjectName_Sub"] = "SystemName_ObjectName_Pub"
	}
	return r
}

func liveEnv(t *testing.T) (token, owner string) {
	token, owner = os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_OWNER")
	if token == "" || owner == "" {
		t.Skip("GITHUB_TOKEN and GITHUB_OWNER are required")
	}
	return
}

func runInputs(t *testing.T, values map[string]interface{}) *Output {
	t.Helper()
	in := newInput()
	if err := in.FromMap(values); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out, err := run(context.Background(), in, log.RootLogger())
	if err != nil {
		t.Fatal(err)
	}
	out.DurationMs = int(time.Since(start).Milliseconds())
	b, _ := json.MarshalIndent(out, "", "  ")
	t.Logf("output: %s", b)
	return out
}

func ghFile(t *testing.T, token, owner, repo, p string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+owner+"/"+repo+"/contents/"+p+"?ref=main", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("User-Agent", "flogo-git-repo-scaffolder-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestLiveScaffold(t *testing.T) {
	token, owner := liveEnv(t)
	tplRepo := "flogo-scaffold-test-templates"
	sharedRepo := "flogo-scaffold-test-shared"

	// 1. publish the sample template library (the activity copies a local repo into GitHub)
	local := newBareRepo(t, sampleTemplate())
	runInputs(t, map[string]interface{}{
		"authToken":          token,
		"templateRepoURL":    local,
		"targetMode":         TargetModeNewRepo,
		"targetRepoURL":      "https://github.com/" + owner + "/" + tplRepo,
		"failIfRepoExists":   false,
		"newRepoDescription": "Sample template library for the Flogo Git Repo Scaffolder activity",
		"commitMessage":      "Publish sample templates",
	})

	excludes := []interface{}{".DS_Store", "catalog-info.yaml"}

	// 2. subfolder mode into a shared repository
	if _, err := (githubAPI{base: "https://api.github.com", token: token}).EnsureRepo(context.Background(),
		RepoRef{Host: "github.com", Owner: owner, Name: sharedRepo}, true, "Shared repository for scaffolded integrations", false); err != nil {
		t.Fatal(err)
	}
	folder := "s4hana-article-kafka-pub"
	out := runInputs(t, map[string]interface{}{
		"authToken":       token,
		"templateRepoURL": "https://github.com/" + owner + "/" + tplRepo,
		"templatePath":    "templates/" + liveTemplate,
		"replacements":    replacementsFor("s4hana", "article", "kafka", "pub"),
		"excludePatterns": excludes,
		"targetMode":      TargetModeSubfolder,
		"targetRepoURL":   "https://github.com/" + owner + "/" + sharedRepo,
		"targetFolder":    folder,
		"commitMessage":   "Generate " + folder,
	})
	if out.FilesTotal != 6 || out.FilesExcluded != 2 || out.CommitSHA == "" {
		t.Fatalf("unexpected counts %+v", out)
	}
	// test repos made in earlier runs may still carry the account's default branch name
	api := githubAPI{base: "https://api.github.com", token: token}
	for _, r := range []string{tplRepo, sharedRepo} {
		if err := api.SetDefaultBranch(context.Background(), RepoRef{Host: "github.com", Owner: owner, Name: r}, "main"); err != nil {
			t.Fatal(err)
		}
	}
	code, body := ghFile(t, token, owner, sharedRepo, folder+"/s4hana-article-kafka-pub.module/Processes/s4hana_article_Pub.bwp")
	if code != 200 || !strings.Contains(body, "s4hana.Outbound") || !strings.Contains(body, "s4hana_article_cdm_PUB") {
		t.Fatalf("renamed process file: HTTP %d %q", code, body)
	}
	code, body = ghFile(t, token, owner, sharedRepo, folder+"/s4hana-article-kafka-pub.module/META-INF/default.substvar")
	if code != 200 || !strings.Contains(body, "IKD-001") || !strings.Contains(body, "/resources/addons/exceptionLookup/ExceptionLookup.csv") {
		t.Fatalf("substvar: HTTP %d %q", code, body)
	}
	if code, _ = ghFile(t, token, owner, sharedRepo, folder+"/catalog-info.yaml"); code != 404 {
		t.Fatalf("excluded file was published (HTTP %d)", code)
	}

	// identical re-run must not create a commit
	again := runInputs(t, map[string]interface{}{
		"authToken": token, "templateRepoURL": "https://github.com/" + owner + "/" + tplRepo,
		"templatePath": "templates/" + liveTemplate, "replacements": replacementsFor("s4hana", "article", "kafka", "pub"),
		"excludePatterns": excludes, "targetMode": TargetModeSubfolder,
		"targetRepoURL": "https://github.com/" + owner + "/" + sharedRepo, "targetFolder": folder,
	})
	if again.Pushed || again.CommitSHA != out.CommitSHA {
		t.Fatalf("re-run should be a no-op: %+v", again)
	}

	// 3. newRepo mode
	newRepo := fmt.Sprintf("wms-order-sap-sub-%d", time.Now().Unix())
	nr := runInputs(t, map[string]interface{}{
		"authToken":       token,
		"templateRepoURL": "https://github.com/" + owner + "/" + tplRepo,
		"templatePath":    "templates/" + liveTemplate,
		"replacements":    replacementsFor("wms", "order", "sap", "sub"),
		"excludePatterns": excludes,
		"targetMode":      TargetModeNewRepo,
		"targetRepoURL":   "https://github.com/" + owner + "/" + newRepo,
		"commitMessage":   "Generate " + newRepo,
	})
	if !nr.RepoCreated || !nr.Pushed {
		t.Fatalf("newRepo: %+v", nr)
	}
	code, body = ghFile(t, token, owner, newRepo, "wms-order-sap-sub.module/Processes/wms_order_Sub.bwp")
	if code != 200 || !strings.Contains(body, "wms.Inbound") {
		t.Fatalf("newRepo process file: HTTP %d %q", code, body)
	}
	t.Logf("created %s", nr.RepoURL)
}
