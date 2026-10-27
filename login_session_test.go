package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/auth"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// A fresh login has no account yet. The record the salt is derived for therefore has
// to be the account that is about to be stored — if the salt is created before the
// account has its UUID, it lands on nobody, and the next run says "this account has no
// token_salt" right after a login that reported success.
func TestLoginPutsTheSaltOnTheStoredAccount(t *testing.T) {
	t.Setenv("MCLI_HOME", t.TempDir())
	t.Setenv("MCLI_MINECRAFT", filepath.Join(t.TempDir(), "mc"))
	a, err := newApp(newFlags())
	if err != nil {
		t.Fatal(err)
	}

	const id = "7be23c946df1498e91fdc31319e7ba48"
	sess := &auth.Session{
		MS:      auth.MSTokens{RefreshToken: "the-refresh-token"},
		MC:      auth.MCTokens{AccessToken: "the-access-token"},
		Profile: auth.Profile{ID: id, Name: "Gaoxj"},
		XUID:    "2535412345678901",
	}
	if err := a.storeSession(&store.Account{}, sess, ""); err != nil {
		t.Fatal(err)
	}

	rec, _ := a.Store.Tokens(id)
	if rec.Salt == "" {
		t.Fatalf("the stored account has no salt: %+v", rec)
	}
	if rec.RefreshEnc == "" {
		t.Fatalf("the refresh token was not stored: %+v", rec)
	}

	raw, err := os.ReadFile(a.Store.RefreshPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("refresh.toml:\n%s", raw)
	if strings.Contains(string(raw), "\n[tokens]\n") {
		t.Errorf("a flat [tokens] section was written:\n%s", raw)
	}

	// and the file must be one this build can read back
	again, err := store.Open(store.Files{
		Config:  a.Store.Path,
		State:   a.Store.StatePath,
		Refresh: a.Store.RefreshPath,
		Session: a.Store.SessionPath,
		Last:    a.Store.LastPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.UnknownKeys) != 0 {
		t.Errorf("mcli cannot read the file it just wrote: %v", again.UnknownKeys)
	}
	if r, _ := again.Tokens(id); r.Salt == "" {
		t.Errorf("the salt did not survive the round trip: %+v", r)
	}
}
