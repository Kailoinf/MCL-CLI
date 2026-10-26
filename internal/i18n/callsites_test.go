package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCallSiteVerbsMatch counts the arguments of every i18n.T("key", …) call in the
// repository and compares them with that key's verbs. TestFormatVerbsMatch only
// keeps the catalogs in step — nothing noticed a *call site* passing the wrong
// number of arguments until the CLI printed %!s(MISSING) at run time.
func TestCallSiteVerbsMatch(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "docs", "skill":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "T" || len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || call.Ellipsis.IsValid() {
				return true
			}
			key := strings.Trim(lit.Value, `"`)
			text, known := en[key]
			if !known {
				return true // TestSourceKeysExist reports unknown keys
			}
			// verbs() renders each verb as "%" plus whatever flags it carries:
			// count the '%' it emits, not the bytes.
			want := strings.Count(verbs(text), "%")
			if got := len(call.Args) - 1; got != want {
				t.Errorf("%s: i18n.T(%q) passes %d argument(s); the translation has %d verb(s)",
					rel, key, got, want)
			}
			checked++
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if checked < 200 {
		t.Fatalf("only %d call sites checked — the walk is not finding the CLI sources", checked)
	}
}
