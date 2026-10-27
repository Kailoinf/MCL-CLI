package store

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/toml"
)

// What the writer does with a record that has no account key. This is the shape that
// reached a user: the salt was stored under an empty key, and the TOML writer turned
// the nameless record into exactly the flat section the reader calls unknown.
func TestANamelessRecordIsWhereAFlatSectionCameFrom(t *testing.T) {
	blob, err := toml.Marshal(refreshFile{Tokens: map[string]*TokenRecord{
		"": {Salt: "salt-under-no-name", RefreshEnc: "refresh"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(blob)
	t.Logf("a nameless record marshals to:\n%s", text)
	if !regexp.MustCompile(`(?m)^\[tokens\]\s*$`).MatchString(text) {
		t.Skip("the writer no longer produces a flat section, which is what we want; " +
			"this test documents where the reported file came from")
	}
	if !strings.Contains(text, "token_salt") {
		t.Errorf("expected the salt to be in the flat section:\n%s", text)
	}
}
