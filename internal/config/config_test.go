package config

import (
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.DownloadRetryTimes != 3 || c.HTTPTimeout != 30 {
		t.Errorf("unexpected download defaults: %+v", c)
	}
	if c.BmclapiEndpoint != "https://bmclapi2.bangbang93.com" {
		t.Errorf("unexpected mirror default: %s", c.BmclapiEndpoint)
	}
	if c.MinecraftDir == "" {
		t.Error("minecraft_dir must default to a real path")
	}
	if c.Language != "auto" {
		t.Errorf("language default = %q", c.Language)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("defaults must validate: %v", err)
	}
}

func TestGetSetReset(t *testing.T) {
	i18n.Set("en")
	c := Default()
	if _, err := c.Get("manifest_ttl_hours"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("manifest_ttl_hours", "12"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("manifest_ttl_hours"); got != "12" {
		t.Errorf("after set: %q", got)
	}
	if err := c.Set("mirror_fallback", "no"); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Get("mirror_fallback"); v != "false" {
		t.Errorf("bool parse failed: %q", v)
	}
	if err := c.Set("http_timeout", "abc"); err == nil {
		t.Error("non-numeric value must be rejected")
	}
	if err := c.Set("http_timeout", "1"); err == nil {
		t.Error("out-of-range value must be rejected")
	}
	if err := c.Set("nope", "1"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown key error = %v", err)
	}
	got, err := c.Reset("manifest_ttl_hours")
	if err != nil {
		t.Fatal(err)
	}
	if got != "6" {
		t.Errorf("reset = %q want 6", got)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	c := Default()
	c.BmclapiEndpoint = "bmclapi2.bangbang93.com"
	if err := c.Validate(); err == nil {
		t.Error("endpoint without scheme must fail")
	}
	c = Default()
	c.VersionsDir = "a/b"
	if err := c.Validate(); err == nil {
		t.Error("versions_dir with separators must fail")
	}
	c = Default()
	c.LogLevelTerminal = "loud"
	if err := c.Validate(); err == nil {
		t.Error("unknown log level must fail")
	}
	c = Default()
	c.Language = "klingon"
	if err := c.Validate(); err == nil {
		t.Error("unknown language must fail")
	}
}

func TestKeysAreUniqueAndSorted(t *testing.T) {
	keys := Keys()
	seen := map[string]bool{}
	for i, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key %q", k)
		}
		seen[k] = true
		if i > 0 && keys[i-1] > k {
			t.Errorf("keys not sorted: %q before %q", keys[i-1], k)
		}
		if Describe(k) == "" {
			t.Errorf("key %q has no description", k)
		}
	}
}

func TestFillMissingRestoresDefaults(t *testing.T) {
	c := Config{} // as if unmarshalled from an older mcli.toml
	c.FillMissing()
	if c.HTTPTimeout != 30 || c.BmclapiEndpoint == "" || c.VersionsDir != "versions" {
		t.Errorf("FillMissing did not restore defaults: %+v", c)
	}
	// an intentionally empty string stays empty
	c2 := Config{JavaPath: "", MinecraftDir: ""}
	c2.FillMissing()
	if c2.JavaPath != "" {
		t.Errorf("java_path must stay blank, got %q", c2.JavaPath)
	}
}
