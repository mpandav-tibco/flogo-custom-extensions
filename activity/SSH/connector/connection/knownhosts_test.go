package connection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnownHostsPathStaysInTempDir(t *testing.T) {
	base := filepath.Join(os.TempDir(), "flogo-ssh-knownhosts")
	for _, name := range []string{"prod-ssh", "../../etc/passwd", "a/b", "..", "", `c:\x`} {
		p := knownHostsPath(name)
		if filepath.Dir(p) != base || strings.Contains(filepath.Base(p), "/") {
			t.Errorf("knownHostsPath(%q) = %q escapes %q", name, p, base)
		}
	}
}
