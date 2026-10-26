package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCatalogsInSync is the guard that keeps translations honest: every key in
// English must exist in every other language and vice versa.
func TestCatalogsInSync(t *testing.T) {
	for lang, cat := range catalogs {
		for k := range en {
			if _, ok := cat[k]; !ok {
				t.Errorf("language %s is missing key %q", lang, k)
			}
		}
		for k := range cat {
			if _, ok := en[k]; !ok {
				t.Errorf("language %s has key %q that English does not define", lang, k)
			}
		}
	}
}

// TestNoEmptyTranslations catches half-finished catalog entries.
func TestNoEmptyTranslations(t *testing.T) {
	for lang, cat := range catalogs {
		for k, v := range cat {
			if v == "" {
				t.Errorf("language %s: key %q is empty", lang, k)
			}
		}
	}
}

// TestFormatVerbsMatch ensures a translation uses the same printf verbs as the
// English source, so runtime formatting never breaks.
func TestFormatVerbsMatch(t *testing.T) {
	for lang, cat := range catalogs {
		if lang == EN {
			continue
		}
		for k, v := range cat {
			if got, want := verbs(v), verbs(en[k]); got != want {
				t.Errorf("language %s: key %q has verbs %q, English has %q", lang, k, got, want)
			}
		}
	}
}

func verbs(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		out = append(out, '%')
		for i+1 < len(s) {
			i++
			c := s[i]
			if c == 'v' || c == 'd' || c == 's' || c == 'q' || c == 'f' || c == 'x' {
				out = append(out, c)
				break
			}
			out = append(out, c)
		}
	}
	return string(out)
}

// TestSet switches languages and checks fallback behaviour.
func TestSet(t *testing.T) {
	defer Set("en")
	if err := Set("zh"); err != nil {
		t.Fatal(err)
	}
	if got := T("common.yes"); got != "是" {
		t.Errorf("zh T(common.yes) = %q", got)
	}
	if err := Set("en"); err != nil {
		t.Fatal(err)
	}
	if got := T("common.yes"); got != "yes" {
		t.Errorf("en T(common.yes) = %q", got)
	}
	if err := Set("kl"); err == nil {
		t.Error("Set(kl) should fail")
	}
	if got := T("does.not.exist"); got != "<!does.not.exist!>" {
		t.Errorf("missing key should be visible, got %q", got)
	}
}

// TestFormatting checks that arguments are applied.
func TestFormatting(t *testing.T) {
	Set("en")
	got := T("dl.size_mismatch", 10, 20)
	want := "size mismatch: want 10, got 20"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// TestSourceKeysExist scans the whole repository for i18n.T("…") call sites and
// fails when a key is missing from the catalogs. This is what keeps the
// bilingual surface honest as the CLI grows.
func TestSourceKeysExist(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("cannot locate repository root: %v", err)
	}
	re := regexp.MustCompile(`i18n\.T\("([^"]+)"`)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "i18n" {
			return nil // the catalog itself mentions example keys
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			if !Has(m[1]) {
				t.Errorf("%s uses i18n key %q which is not defined", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
