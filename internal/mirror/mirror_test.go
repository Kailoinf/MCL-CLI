package mirror

import (
	"strings"
	"testing"
)

func TestMapURL(t *testing.T) {
	m := Mapper{Endpoint: "https://bmclapi2.bangbang93.com/", Fallback: true}
	cases := []struct{ in, want string }{
		{"https://piston-meta.mojang.com/v1/packages/abc/19.json", "https://bmclapi2.bangbang93.com/v1/packages/abc/19.json"},
		{"https://libraries.minecraft.net/com/google/guava/guava/33.0-jre/guava-33.0-jre.jar", "https://bmclapi2.bangbang93.com/maven/com/google/guava/guava/33.0-jre/guava-33.0-jre.jar"},
		{"https://resources.download.minecraft.net/ab/abcdef", "https://bmclapi2.bangbang93.com/assets/ab/abcdef"},
		{"https://piston-data.mojang.com/v1/objects/deadbeef/client.jar", "https://bmclapi2.bangbang93.com/v1/objects/deadbeef/client.jar"},
	}
	for _, c := range cases {
		got, ok := m.MapURL(c.in)
		if !ok || got != c.want {
			t.Errorf("MapURL(%s) = %q ok=%v want %q", c.in, got, ok, c.want)
		}
	}
	if _, ok := m.MapURL("https://example.com/whatever.jar"); ok {
		t.Error("unknown hosts must not be rewritten")
	}
	if _, ok := m.MapURL("not a url"); ok {
		t.Error("garbage must not be rewritten")
	}
}

func TestCandidatesOrderAndFallback(t *testing.T) {
	official := "https://libraries.minecraft.net/a/b/c/1/c-1.jar"

	withMirror := Mapper{Endpoint: "https://mirror.example", Fallback: true}
	cands := withMirror.Candidates(official)
	if len(cands) != 2 {
		t.Fatalf("want mirror + official, got %d", len(cands))
	}
	if cands[0].Source != "bmclapi" || !strings.HasPrefix(cands[0].URL, "https://mirror.example") {
		t.Errorf("mirror must come first: %+v", cands[0])
	}
	if cands[1].Source != "mojang" || cands[1].URL != official {
		t.Errorf("official must be the fallback: %+v", cands[1])
	}

	noFallback := Mapper{Endpoint: "https://mirror.example", Fallback: false}
	if got := noFallback.Candidates(official); len(got) != 1 {
		t.Errorf("fallback disabled should leave one candidate, got %d", len(got))
	}

	noMirror := Mapper{}
	got := noMirror.Candidates(official)
	if len(got) != 1 || got[0].Source != "mojang" || got[0].URL != official {
		t.Errorf("mirror disabled must yield the official url: %+v", got)
	}
}

func TestVersionAndClientCandidates(t *testing.T) {
	m := Mapper{Endpoint: "https://bmclapi2.bangbang93.com", Fallback: true}
	v := m.VersionJSON("1.21.4", "https://piston-meta.mojang.com/x/1.21.4.json")
	if len(v) != 2 || v[0].URL != "https://bmclapi2.bangbang93.com/version/1.21.4/json" {
		t.Errorf("version json mapping wrong: %+v", v)
	}
	c := m.ClientJar("1.21.4", "https://piston-data.mojang.com/v1/objects/x/client.jar")
	if len(c) != 2 || c[0].URL != "https://bmclapi2.bangbang93.com/version/1.21.4/client" {
		t.Errorf("client jar mapping wrong: %+v", c)
	}
}
