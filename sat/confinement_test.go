package sat

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGophersatConfinedToSatPackage enforces the dependency rule recorded in
// CLAUDE.md: gophersat is the main module's sole third-party dependency and must be
// imported only by this package, so the rest of the library stays dependency-free
// (and the js/wasm build stays minimal). It parses the imports of every .go file in
// the module and fails if any file outside sat/ imports gophersat.
func TestGophersatConfinedToSatPackage(t *testing.T) {
	root := moduleRoot(t)
	satDir := filepath.Join(root, "sat") + string(os.PathSeparator)
	const dep = "github.com/crillab/gophersat"

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist":
				return filepath.SkipDir
			}
			// Skip nested modules (e.g. bench/) — they have their own dependency graph.
			if path != root {
				if _, e := os.Stat(filepath.Join(path, "go.mod")); e == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, e := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if e != nil {
			return nil // ignore unparseable files
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if (p == dep || strings.HasPrefix(p, dep+"/")) && !strings.HasPrefix(path, satDir) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s imports %q — gophersat must be confined to the sat/ package", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// moduleRoot walks up from the test's working directory to the directory holding
// go.mod (the module root).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, e := os.Stat(filepath.Join(dir, "go.mod")); e == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above working directory")
		}
		dir = parent
	}
}
