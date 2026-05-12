package parser

import (
	"testing"
)

func TestDefaultRegistry_BuiltinParsers(t *testing.T) {
	reg := DefaultRegistry()

	cases := []struct {
		name string
		exts []string
	}{
		{"json", []string{".json", ".flogo"}},
		{"xml", []string{".xml", ".bwp"}},
		{"yaml", []string{".yaml", ".yml"}},
		{"kv", []string{".conf", ".properties", ".env"}},
		{"lines", []string{".log", ".txt", ".out"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := reg.Get(tc.name)
			if !ok || p == nil {
				t.Fatalf("expected parser %q to be registered", tc.name)
			}
			for _, ext := range tc.exts {
				p2, gotName, ok2 := reg.Detect("test" + ext)
				if !ok2 || p2 == nil {
					t.Fatalf("expected to detect extension %q as parser %q", ext, tc.name)
				}
				if gotName != tc.name {
					t.Fatalf("extension %q: expected parser name %q, got %q", ext, tc.name, gotName)
				}
			}
		})
	}
}

func TestRegistry_Get_UnknownName(t *testing.T) {
	reg := DefaultRegistry()
	_, ok := reg.Get("unknown")
	if ok {
		t.Fatal("expected not-found for unknown parser name")
	}
}

func TestRegistry_Detect_UnknownExtension(t *testing.T) {
	reg := DefaultRegistry()
	_, _, ok := reg.Detect("file.unknown")
	if ok {
		t.Fatal("expected not-found for unknown extension")
	}
}

func TestRegistry_Detect_NoExtension(t *testing.T) {
	reg := DefaultRegistry()
	_, _, ok := reg.Detect("Makefile")
	if ok {
		t.Fatal("expected not-found for file with no extension")
	}
}

func TestRegistry_Detect_CaseInsensitive(t *testing.T) {
	reg := DefaultRegistry()
	cases := []string{"FILE.JSON", "file.JSON", "file.Json", "file.FLOGO"}
	for _, name := range cases {
		p, _, ok := reg.Detect(name)
		if !ok || p == nil {
			t.Fatalf("expected case-insensitive detection for %q", name)
		}
	}
}

func TestRegistry_Detect_YAMLExtensions(t *testing.T) {
	reg := DefaultRegistry()
	for _, name := range []string{"manifest.yaml", "values.yml", "config.YAML", "config.YML"} {
		_, gotName, ok := reg.Detect(name)
		if !ok || gotName != "yaml" {
			t.Fatalf("expected yaml parser for %q, got %q (ok=%v)", name, gotName, ok)
		}
	}
}

func TestRegistry_Register_CustomParser(t *testing.T) {
	reg := NewRegistry()
	reg.Register("custom", []string{".custom", ".cust"}, &JSONParser{}) // reuse JSON for test

	p, ok := reg.Get("custom")
	if !ok || p == nil {
		t.Fatal("expected custom parser to be registered")
	}

	_, name, ok2 := reg.Detect("file.custom")
	if !ok2 || name != "custom" {
		t.Fatalf("expected detection via .custom extension")
	}
}

func TestRegistry_MustGet_Panics(t *testing.T) {
	reg := NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from MustGet on unknown parser")
		}
	}()
	reg.MustGet("nonexistent")
}

func TestRegistry_MustGet_Succeeds(t *testing.T) {
	reg := DefaultRegistry()
	p := reg.MustGet("json")
	if p == nil {
		t.Fatal("expected non-nil parser from MustGet")
	}
}

func TestRegistry_Detect_KVExtensions(t *testing.T) {
	reg := DefaultRegistry()
	kvFiles := []string{"ems.conf", "app.properties", ".env", "server.ENV"}
	for _, f := range kvFiles {
		_, name, ok := reg.Detect(f)
		if !ok || name != "kv" {
			t.Fatalf("expected kv parser for %q, got %q (ok=%v)", f, name, ok)
		}
	}
}

func TestRegistry_Detect_LinesExtensions(t *testing.T) {
	reg := DefaultRegistry()
	lineFiles := []string{"app.log", "output.txt", "result.out"}
	for _, f := range lineFiles {
		_, name, ok := reg.Detect(f)
		if !ok || name != "lines" {
			t.Fatalf("expected lines parser for %q, got %q (ok=%v)", f, name, ok)
		}
	}
}

func TestRegistry_Detect_ExtractsByExtension_NotContent(t *testing.T) {
	reg := DefaultRegistry()
	// A .yaml file with JSON content → still detected as yaml parser
	_, name, ok := reg.Detect("kubernetes.yaml")
	if !ok || name != "yaml" {
		t.Fatalf("expected yaml detection based on extension, got %q", name)
	}
}

func TestRegistry_NewRegistry_IsEmpty(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("json")
	if ok {
		t.Fatal("new registry should be empty")
	}
	_, _, ok2 := reg.Detect("file.json")
	if ok2 {
		t.Fatal("new registry should detect nothing")
	}
}
