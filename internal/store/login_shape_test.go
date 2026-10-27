package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The sequence a login runs: set the salt, write the encrypted token, save. It has to
// survive a home that already holds an older refresh.toml — replacing it is the whole
// point, and "the file it wrote is one it cannot read" is the failure this pins.
func TestSaveTokensReplacesAnOlderRefreshFile(t *testing.T) {
	dir := t.TempDir()
	files := Files{
		Config:  filepath.Join(dir, "mcli.toml"),
		State:   filepath.Join(dir, "state.toml"),
		Refresh: filepath.Join(dir, "refresh.toml"),
		Session: filepath.Join(dir, "session.toml"),
		Last:    filepath.Join(dir, "last.toml"),
	}
	if err := os.WriteFile(files.Refresh, []byte("[tokens]\ntoken_salt = \"old\"\nrefresh_token_enc = \"older\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.UnknownKeys) == 0 {
		t.Fatal("the fixture did not look like an older file")
	}

	uuid := "7be23c946df1498e91fdc31319e7ba48"
	acc := &Account{UUID: uuid, Name: "Gaoxj", AddedAt: time.Now()}
	if err := st.SetAccountSalt(acc); err != nil {
		t.Fatal(err)
	}
	tok, sess := st.Tokens(uuid)
	tok.RefreshEnc = "enc-refresh"
	sess.MCTokenEnc = "enc-mc"
	sess.MCTokenExp = time.Now().Add(time.Hour)
	st.Data.Accounts[uuid] = acc
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTokens(); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(files.Refresh)
	t.Logf("refresh.toml after the save:\n%s", raw)
	if string(raw) == "[tokens]\ntoken_salt = \"old\"\nrefresh_token_enc = \"older\"\n" {
		t.Fatal("the older file survived the save")
	}

	again, err := Open(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.UnknownKeys) != 0 {
		t.Errorf("the file just written is not readable: %v", again.UnknownKeys)
	}
	rec, _ := again.Tokens(uuid)
	if rec.RefreshEnc != "enc-refresh" || rec.Salt == "" {
		t.Errorf("credentials did not survive the round trip: %+v", rec)
	}
}
