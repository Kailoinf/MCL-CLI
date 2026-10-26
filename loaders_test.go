package main

import (
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// The loader is a choice: the interesting cases are the ones that must be refused.
func TestParseLoaderIsAClosedChoice(t *testing.T) {
	i18n.Set("en")
	cases := []struct {
		in   string
		want modLoader
		bad  bool
	}{
		{"", loaderVanilla, false},
		{"vanilla", loaderVanilla, false},
		{"Vanilla", loaderVanilla, false},
		{"  vanilla  ", loaderVanilla, false},
		{"fabric", loaderFabric, false},
		{"FABRIC", loaderFabric, false},
		{"forge", "", true},
		{"quilt", "", true},
		{"neoforge", "", true},
		{"fabric-loader-0.20.1-26.3", "", true}, // a version id is not a loader
	}
	for _, c := range cases {
		got, err := parseLoader(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("parseLoader(%q) accepted an unknown loader", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLoader(%q) failed: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseLoader(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// the error has to name the alternatives, not just refuse
	_, err := parseLoader("forge")
	if err == nil || !strings.Contains(err.Error(), "vanilla") || !strings.Contains(err.Error(), "fabric") {
		t.Errorf("the error must list the choices: %v", err)
	}
}

// An empty --loader-version is the documented default, so it has to mean "newest".
func TestIsLatestUnderstandsEmptyAndWords(t *testing.T) {
	for _, v := range []string{"", "  ", "latest", "Latest", "stable", "newest", "auto"} {
		if !isLatest(v) {
			t.Errorf("isLatest(%q) must be true", v)
		}
	}
	for _, v := range []string{"0.20.1", "1.21.4", "fabric"} {
		if isLatest(v) {
			t.Errorf("isLatest(%q) must be false", v)
		}
	}
}

func TestLoaderChoicesMatchTheConstants(t *testing.T) {
	choices := loaderChoices()
	for _, l := range modLoaders {
		if !strings.Contains(choices, string(l)) {
			t.Errorf("%q missing from %q", l, choices)
		}
	}
}

func TestLoaderOfVersionID(t *testing.T) {
	if loaderOfVersionID("fabric-loader-0.20.1-26.3") != loaderFabric {
		t.Error("a Fabric profile id must be reported as Fabric")
	}
	for _, id := range []string{"1.21.4", "26.3-Fabric 0.20.1", "", "rd-132211"} {
		if loaderOfVersionID(id) != loaderVanilla {
			t.Errorf("%q must be reported as vanilla", id)
		}
	}
}
