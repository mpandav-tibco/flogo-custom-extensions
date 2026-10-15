package gitreposcaffolder

import (
	"strings"
	"testing"
)

func names(files []FileEntry) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func find(t *testing.T, files []FileEntry, p string) FileEntry {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f
		}
	}
	t.Fatalf("file %q not in output %v", p, names(files))
	return FileEntry{}
}

// Mirrors the cascading, longest-first behaviour of the customer's Python generator
// (pub rewrite "SystemName.Inbound" -> "SystemName.Outbound" is later hit by "SystemName").
func TestSequentialMatchesPythonCascade(t *testing.T) {
	files := []FileEntry{
		{Path: "Tpl_X.module/META-INF/MANIFEST.MF", Data: []byte("Bundle-Name: Tpl_X.module\nqueue=SystemName.Inbound\nobj=SystemName_ObjectName_CDM_SUB\n")},
	}
	res, err := Transform(files, TransformOptions{
		Replacements: map[string]string{
			"Tpl_X.module":                  "s4hana-article-kafka-pub.module",
			"Tpl_X":                         "s4hana-article-kafka-pub",
			"SystemName":                    "s4hana",
			"ObjectName":                    "article",
			"SystemName.Inbound":            "SystemName.Outbound",
			"SystemName_ObjectName_CDM_SUB": "SystemName_ObjectName_cdm_PUB",
		},
		RenamePaths: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := find(t, res.Files, "s4hana-article-kafka-pub.module/META-INF/MANIFEST.MF")
	want := "Bundle-Name: s4hana-article-kafka-pub.module\nqueue=s4hana.Outbound\nobj=s4hana_article_cdm_PUB\n"
	if string(f.Data) != want {
		t.Fatalf("content:\n got %q\nwant %q", f.Data, want)
	}
	if res.FilesModified != 1 || res.PathsRenamed != 1 {
		t.Fatalf("counts: modified=%d renamed=%d", res.FilesModified, res.PathsRenamed)
	}
}

func TestSinglePassDoesNotCascade(t *testing.T) {
	res, err := Transform([]FileEntry{{Path: "a.txt", Data: []byte("A B")}}, TransformOptions{
		Replacements: map[string]string{"A": "B", "B": "C"},
		Mode:         ReplacementModeSinglePass,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Files[0].Data); got != "B C" {
		t.Fatalf("got %q, want %q", got, "B C")
	}
}

func TestLongestTokenWins(t *testing.T) {
	res, _ := Transform([]FileEntry{{Path: "a.txt", Data: []byte("SystemNameX SystemName")}}, TransformOptions{
		Replacements: map[string]string{"SystemName": "s", "SystemNameX": "long"},
	})
	if got := string(res.Files[0].Data); got != "long s" {
		t.Fatalf("got %q", got)
	}
}

func TestBinaryFilesAreCopiedVerbatim(t *testing.T) {
	png := []byte("\x89PNG ObjectName")
	nul := []byte("ObjectName\x00ObjectName")
	res, err := Transform([]FileEntry{
		{Path: "img/ObjectName.png", Data: png},
		{Path: "data.dat", Data: nul},
		{Path: "lib.custom", Data: []byte("ObjectName")},
	}, TransformOptions{
		Replacements:     map[string]string{"ObjectName": "article"},
		BinaryExtensions: []string{"custom"},
		RenamePaths:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(find(t, res.Files, "img/article.png").Data) != string(png) {
		t.Fatal("png content changed")
	}
	if string(find(t, res.Files, "data.dat").Data) != string(nul) {
		t.Fatal("NUL-containing file changed")
	}
	if string(find(t, res.Files, "lib.custom").Data) != "ObjectName" {
		t.Fatal("custom binary extension was replaced")
	}
	if res.FilesModified != 0 {
		t.Fatalf("modified=%d, want 0", res.FilesModified)
	}
}

func TestMaxTextBytesTreatsLargeFilesAsBinary(t *testing.T) {
	big := []byte(strings.Repeat("ObjectName ", 10))
	res, _ := Transform([]FileEntry{{Path: "big.txt", Data: big}}, TransformOptions{
		Replacements: map[string]string{"ObjectName": "x"},
		MaxTextBytes: 20,
	})
	if string(res.Files[0].Data) != string(big) {
		t.Fatal("file over maxTextBytes was modified")
	}
}

func TestExcludePatterns(t *testing.T) {
	cases := []struct {
		path    string
		pattern string
		want    bool
	}{
		{".DS_Store", ".DS_Store", true},
		{"a/b/.DS_Store", ".DS_Store", true},
		{"a/b/x.bak", "*.bak", true},
		{"catalog-info.yaml", "catalog-info.yaml", true},
		{"a/__pycache__/m.pyc", "__pycache__/", true},
		{"docs/a/b.md", "docs/", true},
		{"src/docs/b.md", "docs/", true},
		{"docsx/b.md", "docs/", false},
		{"a/b/c.yml", "a/b/*.yml", true},
		{"z/a/b/c.yml", "a/b/*.yml", false},
		{"z/a/b/c.yml", "**/a/b/*.yml", true},
		{"src/main.go", "*.bak", false},
	}
	for _, c := range cases {
		if got := MatchesExclude(c.path, []string{c.pattern}); got != c.want {
			t.Errorf("MatchesExclude(%q, %q) = %t, want %t", c.path, c.pattern, got, c.want)
		}
	}
}

func TestExcludedFilesAreCounted(t *testing.T) {
	res, _ := Transform([]FileEntry{
		{Path: "keep.txt", Data: []byte("x")},
		{Path: ".DS_Store", Data: []byte("x")},
		{Path: "skeleton/catalog-info.yaml", Data: []byte("x")},
	}, TransformOptions{ExcludePatterns: []string{".DS_Store", "catalog-info.yaml"}})
	if res.FilesExcluded != 2 || len(res.Files) != 1 {
		t.Fatalf("excluded=%d files=%v", res.FilesExcluded, names(res.Files))
	}
}

func TestRenameCollisionIsAnError(t *testing.T) {
	_, err := Transform([]FileEntry{
		{Path: "A.txt", Data: []byte("1")},
		{Path: "B.txt", Data: []byte("2")},
	}, TransformOptions{Replacements: map[string]string{"A": "C", "B": "C"}, RenamePaths: true})
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("want collision error, got %v", err)
	}
}

func TestRenameRejectsTraversal(t *testing.T) {
	_, err := Transform([]FileEntry{{Path: "dir/NAME.txt", Data: []byte("1")}}, TransformOptions{
		Replacements: map[string]string{"NAME": "../../etc"}, RenamePaths: true,
	})
	if err == nil {
		t.Fatal("want error for a replacement that escapes the template folder")
	}
}

func TestPathTokensWithSlashesAreContentOnly(t *testing.T) {
	res, _ := Transform([]FileEntry{{Path: "D_x.txt", Data: []byte(`D:\BWCE\x.csv`)}}, TransformOptions{
		Replacements: map[string]string{`D:\BWCE\x.csv`: "/res/x.csv"}, RenamePaths: true,
	})
	if res.Files[0].Path != "D_x.txt" || string(res.Files[0].Data) != "/res/x.csv" {
		t.Fatalf("got path=%q data=%q", res.Files[0].Path, res.Files[0].Data)
	}
}

func TestRenameDisabled(t *testing.T) {
	res, _ := Transform([]FileEntry{{Path: "ObjectName.txt", Data: []byte("ObjectName")}}, TransformOptions{
		Replacements: map[string]string{"ObjectName": "article"}, RenamePaths: false,
	})
	if res.Files[0].Path != "ObjectName.txt" || string(res.Files[0].Data) != "article" {
		t.Fatalf("got %q %q", res.Files[0].Path, res.Files[0].Data)
	}
}

func TestUnknownModeIsAnError(t *testing.T) {
	if _, err := Transform(nil, TransformOptions{Mode: "regex"}); err == nil {
		t.Fatal("want error")
	}
}

func TestParseRepoURL(t *testing.T) {
	r, err := ParseRepoURL("https://github.com/acme/shared-repo.git")
	if err != nil || r.Host != "github.com" || r.Owner != "acme" || r.Name != "shared-repo" {
		t.Fatalf("got %+v %v", r, err)
	}
	if r.WebURL() != "https://github.com/acme/shared-repo" {
		t.Fatalf("web url %q", r.WebURL())
	}
	for _, bad := range []string{"", "git@github.com:a/b.git", "https://github.com/onlyowner", "https://github.com/a/b/c"} {
		if _, err := ParseRepoURL(bad); err == nil {
			t.Errorf("ParseRepoURL(%q) should fail", bad)
		}
	}
}

func TestInputDefaultsAndCoercion(t *testing.T) {
	in := newInput()
	err := in.FromMap(map[string]interface{}{
		"templateRepoURL": "https://github.com/a/t",
		"replacements":    map[string]interface{}{"ObjectName": "article", "Empty": "", "Nil": nil, "Num": 7},
		"excludePatterns": []interface{}{".DS_Store", ""},
		"dryRun":          "true",
		"pushRetries":     "5",
		"targetBranch":    "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.TargetBranch != "main" || in.TargetMode != TargetModeSubfolder || !in.RenamePaths || in.PushRetries != 5 || !in.DryRun {
		t.Fatalf("defaults/coercion wrong: %+v", in)
	}
	if in.Replacements["Num"] != "7" || in.Replacements["Empty"] != "" {
		t.Fatalf("replacements: %v", in.Replacements)
	}
	if _, ok := in.Replacements["Nil"]; ok {
		t.Fatal("nil replacement value should be dropped")
	}
	if len(in.ExcludePatterns) != 1 {
		t.Fatalf("excludePatterns: %v", in.ExcludePatterns)
	}
}

func TestReplacementListMergesAndOverrides(t *testing.T) {
	in := newInput()
	err := in.FromMap(map[string]interface{}{
		"replacements": map[string]interface{}{"ObjectName": "article", "SystemName": "old"},
		"replacementList": []interface{}{
			map[string]interface{}{"token": "DB_To_KafkaTopic.module", "value": "s4hana-article-kafka-pub.module"},
			map[string]interface{}{"token": "SystemName", "value": "s4hana"},
			map[string]interface{}{"token": "", "value": "ignored"},
			map[string]interface{}{"token": "Nil", "value": nil},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"ObjectName": "article", "SystemName": "s4hana", "DB_To_KafkaTopic.module": "s4hana-article-kafka-pub.module"}
	if len(in.Replacements) != len(want) {
		t.Fatalf("got %v", in.Replacements)
	}
	for k, v := range want {
		if in.Replacements[k] != v {
			t.Fatalf("%s: got %q want %q", k, in.Replacements[k], v)
		}
	}
}

func TestValidateGuardsSharedRepoRoot(t *testing.T) {
	in := newInput()
	in.TemplateRepoURL = "https://github.com/a/t"
	in.TargetRepoURL = "https://github.com/a/shared"
	in.TargetFolder = "/"
	if _, _, err := validate(in); err == nil {
		t.Fatal("subfolder mode with an empty folder must be rejected")
	}
	in.TargetMode = TargetModeNewRepo
	if _, folder, err := validate(in); err != nil || folder != "" {
		t.Fatalf("newRepo at root should be allowed: folder=%q err=%v", folder, err)
	}
}

func TestPlaceFilesPrefixAndExtras(t *testing.T) {
	res := &TransformResult{Files: []FileEntry{{Path: "a.txt", Data: []byte("a")}, {Path: "m/b.txt", Data: []byte("b")}}}
	extra := []FileEntry{
		{Path: "configs/generation.properties", Data: []byte("k=v")},
		{Path: "code/repo/a.txt", Data: []byte("override")},
	}
	if err := placeFiles(res, "code/repo/", extra); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Path] = string(f.Data)
	}
	want := map[string]string{"code/repo/a.txt": "override", "code/repo/m/b.txt": "b", "configs/generation.properties": "k=v"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: got %q want %q", k, got[k], v)
		}
	}
}

func TestPlaceFilesRejectsEscapingPaths(t *testing.T) {
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../b"} {
		if err := placeFiles(&TransformResult{}, "", []FileEntry{{Path: bad, Data: []byte("x")}}); err == nil {
			t.Errorf("extra file %q should be rejected", bad)
		}
		if err := placeFiles(&TransformResult{}, bad, nil); err == nil {
			t.Errorf("prefix %q should be rejected", bad)
		}
	}
}

func TestExtraFilesInput(t *testing.T) {
	in := newInput()
	err := in.FromMap(map[string]interface{}{
		"outputPrefix": "code/r",
		"extraFiles": []interface{}{
			map[string]interface{}{"path": "configs/a.properties", "content": "x=1"},
			map[string]interface{}{"path": "", "content": "skipped"},
		},
	})
	extra, _ := toExtraFiles(in.ExtraFiles)
	if err != nil || in.OutputPrefix != "code/r" || len(extra) != 1 || string(extra[0].Data) != "x=1" {
		t.Fatalf("%v %+v", err, extra)
	}
}
