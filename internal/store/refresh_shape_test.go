package store

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// refresh.toml written by this build must come out in the shape this build reads:
// one table per account. Writing the flat shape would make mcli warn about a file
// it had just written itself, which is how a user ran into it.
func TestRefreshFileIsWrittenPerAccount(t *testing.T) {
	st := openTemp(t)
	uuid := "0123456789abcdef0123456789abcdef"
	if err := st.SetAccountSalt(&Account{UUID: uuid, Name: "Kailo"}); err != nil {
		t.Fatal(err)
	}
	if _, _ = st.Tokens(uuid); false {
		t.Fatal("unreachable")
	}
	if err := st.SaveTokens(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(st.RefreshPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	t.Logf("refresh.toml written by this build:\n%s", text)
	if !strings.Contains(text, "[tokens."+uuid+"]") {
		t.Errorf("the file has no per-account table:\n%s", text)
	}
	// a bare [tokens] section is the older, single-account layout: the reader warns
	// about its keys, so it must never be what this build writes
	flat := regexp.MustCompile(`(?m)^\[tokens\]\s*$`)
	if flat.MatchString(text) {
		t.Errorf("the file has a flat [tokens] section:\n%s", text)
	}
	// and the reloaded store must accept it without unknowns
	again := reopen(t, st)
	if len(again.UnknownKeys) != 0 {
		t.Errorf("mcli flagged its own file: %v", again.UnknownKeys)
	}
	if again.CredentialKeysUnreadable {
		t.Error("mcli called its own file unreadable")
	}
}
