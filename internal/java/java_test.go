package java

import (
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

func TestParseMajor(t *testing.T) {
	cases := map[string]int{
		"1.8.0_401":     8,
		"1.8.0":         8,
		"11.0.21":       11,
		"16.0.2":        16,
		"17.0.9":        17,
		"21.0.5+11":     21,
		"25":            25,
		"1.7.0_80":      7,
		"garbage-1.0.0": 0,
	}
	for in, want := range cases {
		if got := ParseMajor(in); got != want {
			t.Errorf("ParseMajor(%q) = %d want %d", in, got, want)
		}
	}
}

func TestRequiredMajorFromVersionID(t *testing.T) {
	log := logx.Discard()
	cases := map[string]int{
		"1.6.4":     8,
		"1.7.10":    8,
		"1.16.5":    8,
		"1.17":      16,
		"1.17.1":    16,
		"1.18.2":    17,
		"1.19.4":    17,
		"1.20.4":    17,
		"1.20.5":    21,
		"1.21.4":    21,
		"b1.7.3":    8,
		"a1.2.6":    8,
		"rd-132211": 8,
	}
	for id, want := range cases {
		if got := RequiredMajor(nil, id, log); got != want {
			t.Errorf("RequiredMajor(%q) = %d want %d", id, got, want)
		}
	}
	// the version JSON wins when present
	v := &mcv.Version{JavaVersion: &mcv.JavaVersion{MajorVersion: 25}}
	if got := RequiredMajor(v, "26.3", log); got != 25 {
		t.Errorf("javaVersion.majorVersion must win, got %d", got)
	}
	// unknown new-style ids fall back conservatively
	if got := RequiredMajor(nil, "27.1", log); got != 21 {
		t.Errorf("unknown id fallback = %d want 21", got)
	}
}

func TestComponentMapping(t *testing.T) {
	if MajorForComponent("java-runtime-delta") != 21 {
		t.Error("delta must map to 21")
	}
	if MajorForComponent("java-runtime-epsilon") != 25 {
		t.Error("epsilon must map to 25")
	}
	if MajorForComponent("nonsense") != 0 {
		t.Error("unknown components must map to 0")
	}
	if got := ComponentForMajor(21); got != "java-runtime-delta" {
		t.Errorf("ComponentForMajor(21) = %q", got)
	}
	if got := ComponentForMajor(8); got != "jre-legacy" {
		t.Errorf("ComponentForMajor(8) = %q", got)
	}
	// a requirement newer than anything published falls back to the newest
	if got := ComponentForMajor(99); got != "java-runtime-epsilon" {
		t.Errorf("ComponentForMajor(99) = %q", got)
	}
}

func TestPick(t *testing.T) {
	list := []Runtime{
		{Path: "/j8/java", Major: 8},
		{Path: "/j17/java", Major: 17},
		{Path: "/j21/java", Major: 21},
	}
	if got := Pick(list, 21); got == nil || got.Major != 21 {
		t.Errorf("exact match expected, got %+v", got)
	}
	if got := Pick(list, 16); got == nil || got.Major != 17 {
		t.Errorf("smallest newer runtime expected, got %+v", got)
	}
	if got := Pick(list, 8); got == nil || got.Major != 8 {
		t.Errorf("java 8 needed, got %+v", got)
	}
	if got := Pick(list, 25); got != nil {
		t.Errorf("nothing satisfies 25, got %+v", got)
	}
	if got := Pick(nil, 21); got != nil {
		t.Errorf("empty list must pick nothing, got %+v", got)
	}
}

func TestPlatformKey(t *testing.T) {
	if _, ok := PlatformKey(); !ok {
		t.Skip("platform not covered by Mojang's product manifest")
	}
}

func TestAdoptiumURLForKnownPlatform(t *testing.T) {
	url, err := AdoptiumBinaryURL(21)
	if err != nil {
		t.Skipf("no Adoptium mapping for this platform: %v", err)
	}
	if !strings.Contains(url, "/latest/21/ga/") {
		t.Errorf("major version missing from url: %q", url)
	}
	if !strings.HasSuffix(url, "/jdk/hotspot/normal/eclipse") {
		t.Errorf("unexpected adoptium url shape: %q", url)
	}
	if !strings.HasPrefix(url, "https://api.adoptium.net/v3/binary/") {
		t.Errorf("unexpected adoptium host: %q", url)
	}
}
