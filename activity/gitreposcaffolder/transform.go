package gitreposcaffolder

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	ReplacementModeSequential = "sequential"
	ReplacementModeSinglePass = "singlePass"

	defaultMaxTextBytes = 5 * 1024 * 1024
	binarySniffBytes    = 8 * 1024
)

// DefaultBinaryExtensions are never token-replaced; their bytes are copied verbatim.
var DefaultBinaryExtensions = []string{
	".jar", ".war", ".ear", ".zip", ".gz", ".tgz", ".bz2", ".7z", ".xz", ".tar",
	".class", ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp", ".svgz",
	".pdf", ".so", ".dll", ".exe", ".bin", ".dylib", ".keystore", ".jks",
	".p12", ".pfx", ".woff", ".woff2", ".ttf", ".eot", ".otf",
}

// FileEntry is one file of a template or of the generated output.
type FileEntry struct {
	Path       string
	Data       []byte
	Executable bool
}

type TransformOptions struct {
	Replacements     map[string]string
	Mode             string
	RenamePaths      bool
	ExcludePatterns  []string
	BinaryExtensions []string
	MaxTextBytes     int64
}

type TransformResult struct {
	Files         []FileEntry
	FilesModified int
	PathsRenamed  int
	FilesExcluded int
}

type pair struct{ old, new string }

// orderedPairs sorts tokens longest-first (then lexically) so a longer token
// such as "SystemName_ObjectName" wins over its prefix "SystemName".
func orderedPairs(m map[string]string, forPaths bool) []pair {
	out := make([]pair, 0, len(m))
	for k, v := range m {
		if k == "" {
			continue
		}
		if forPaths && (strings.Contains(k, "/") || strings.Contains(k, "\\")) {
			continue
		}
		out = append(out, pair{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].old) != len(out[j].old) {
			return len(out[i].old) > len(out[j].old)
		}
		return out[i].old < out[j].old
	})
	return out
}

// replacer applies an ordered token set in either sequential (each token applied
// to the output of the previous one) or single-pass (no cascading) mode.
type replacer struct {
	pairs  []pair
	single *strings.Replacer
}

func newReplacer(pairs []pair, mode string) *replacer {
	r := &replacer{pairs: pairs}
	if mode == ReplacementModeSinglePass && len(pairs) > 0 {
		args := make([]string, 0, len(pairs)*2)
		for _, p := range pairs {
			args = append(args, p.old, p.new)
		}
		r.single = strings.NewReplacer(args...)
	}
	return r
}

func (r *replacer) bytes(in []byte) []byte {
	if len(r.pairs) == 0 {
		return in
	}
	if r.single != nil {
		return []byte(r.single.Replace(string(in)))
	}
	out := in
	for _, p := range r.pairs {
		if bytes.Contains(out, []byte(p.old)) {
			out = bytes.ReplaceAll(out, []byte(p.old), []byte(p.new))
		}
	}
	return out
}

func (r *replacer) string(in string) string {
	return string(r.bytes([]byte(in)))
}

func isBinary(p string, data []byte, binExt map[string]bool, maxText int64) bool {
	if binExt[strings.ToLower(path.Ext(p))] {
		return true
	}
	if int64(len(data)) > maxText {
		return true
	}
	sniff := data
	if len(sniff) > binarySniffBytes {
		sniff = sniff[:binarySniffBytes]
	}
	return bytes.IndexByte(sniff, 0) >= 0
}

// MatchesExclude reports whether rel (slash-separated, relative to the template
// root) matches any pattern. Supported forms:
//
//	"name"       basename glob at any depth (e.g. ".DS_Store", "*.bak")
//	"dir/"       a directory (and everything below it) at any depth
//	"a/b/*.x"    glob against the full relative path
//	"**/x/*.y"   glob against any trailing part of the path
func MatchesExclude(rel string, patterns []string) bool {
	segs := strings.Split(rel, "/")
	for _, raw := range patterns {
		p := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		p = strings.TrimPrefix(p, "./")
		if p == "" {
			continue
		}
		switch {
		case strings.HasSuffix(p, "/"):
			dir := strings.TrimSuffix(strings.TrimPrefix(p, "**/"), "/")
			for i := 0; i < len(segs)-1; i++ {
				if ok, _ := path.Match(dir, strings.Join(segs[:i+1], "/")); ok {
					return true
				}
				if !strings.Contains(dir, "/") {
					if ok, _ := path.Match(dir, segs[i]); ok {
						return true
					}
				}
			}
		case strings.HasPrefix(p, "**/"):
			rest := strings.TrimPrefix(p, "**/")
			for i := range segs {
				if ok, _ := path.Match(rest, strings.Join(segs[i:], "/")); ok {
					return true
				}
			}
		case !strings.Contains(p, "/"):
			if ok, _ := path.Match(p, segs[len(segs)-1]); ok {
				return true
			}
		default:
			if ok, _ := path.Match(p, rel); ok {
				return true
			}
		}
	}
	return false
}

// Transform excludes, token-replaces and renames the template files. Paths in
// files must be relative to the template root.
func Transform(files []FileEntry, opts TransformOptions) (*TransformResult, error) {
	if opts.Mode == "" {
		opts.Mode = ReplacementModeSequential
	}
	if opts.Mode != ReplacementModeSequential && opts.Mode != ReplacementModeSinglePass {
		return nil, fmt.Errorf("unknown replacementMode %q (use %q or %q)", opts.Mode, ReplacementModeSequential, ReplacementModeSinglePass)
	}
	if opts.MaxTextBytes <= 0 {
		opts.MaxTextBytes = defaultMaxTextBytes
	}
	binExt := map[string]bool{}
	for _, e := range append(append([]string{}, DefaultBinaryExtensions...), opts.BinaryExtensions...) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		binExt[e] = true
	}

	content := newReplacer(orderedPairs(opts.Replacements, false), opts.Mode)
	names := newReplacer(orderedPairs(opts.Replacements, true), opts.Mode)

	res := &TransformResult{Files: make([]FileEntry, 0, len(files))}
	seen := make(map[string]string, len(files))
	for _, f := range files {
		if MatchesExclude(f.Path, opts.ExcludePatterns) {
			res.FilesExcluded++
			continue
		}
		out := FileEntry{Path: f.Path, Data: f.Data, Executable: f.Executable}

		if !isBinary(f.Path, f.Data, binExt, opts.MaxTextBytes) {
			if replaced := content.bytes(f.Data); !bytes.Equal(replaced, f.Data) {
				out.Data = replaced
				res.FilesModified++
			}
		}

		if opts.RenamePaths {
			// Rename each segment on its own so a token never spans a "/".
			segs := strings.Split(f.Path, "/")
			for i, s := range segs {
				segs[i] = names.string(s)
			}
			if renamed := strings.Join(segs, "/"); renamed != f.Path {
				for _, s := range strings.Split(renamed, "/") {
					if s == "" || s == "." || s == ".." {
						return nil, fmt.Errorf("renaming %q produced an invalid path %q (a replacement value is empty or contains a path element)", f.Path, renamed)
					}
				}
				out.Path = renamed
				res.PathsRenamed++
			}
		}

		if prev, dup := seen[out.Path]; dup {
			return nil, fmt.Errorf("path collision: %q and %q both map to %q after renaming", prev, f.Path, out.Path)
		}
		seen[out.Path] = f.Path
		res.Files = append(res.Files, out)
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, nil
}

// cleanRelPath normalises a relative path and rejects absolute or escaping paths.
func cleanRelPath(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be relative", p)
	}
	for _, s := range strings.Split(p, "/") {
		if s == ".." {
			return "", fmt.Errorf("path %q must not contain ..", p)
		}
	}
	return strings.Trim(path.Clean("/"+p), "/"), nil
}

// placeFiles moves the transformed template under prefix and adds extra files
// (an extra file replaces a template file at the same path).
func placeFiles(res *TransformResult, prefix string, extra []FileEntry) error {
	prefix, err := cleanRelPath(prefix)
	if err != nil {
		return fmt.Errorf("outputPrefix: %w", err)
	}
	index := make(map[string]int, len(res.Files)+len(extra))
	for i := range res.Files {
		res.Files[i].Path = path.Join(prefix, res.Files[i].Path)
		index[res.Files[i].Path] = i
	}
	for _, f := range extra {
		p, err := cleanRelPath(f.Path)
		if err != nil || p == "" {
			return fmt.Errorf("extraFiles: invalid path %q", f.Path)
		}
		f.Path = p
		if i, ok := index[p]; ok {
			res.Files[i] = f
			continue
		}
		index[p] = len(res.Files)
		res.Files = append(res.Files, f)
	}
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return nil
}
