package store

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// openTemp opens a fresh mcli home inside a test temp dir.
func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(Files{
		Config:  filepath.Join(dir, "mcli.toml"),
		State:   filepath.Join(dir, "state.toml"),
		Refresh: filepath.Join(dir, "refresh.toml"),
		Session: filepath.Join(dir, "session.toml"),
		Last:    filepath.Join(dir, "last.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// reopen closes and re-opens the same home, as a second mcli run would.
func reopen(t *testing.T, st *Store) *Store {
	t.Helper()
	again, err := Open(Files{
		Config: st.Path, State: st.StatePath, Refresh: st.RefreshPath,
		Session: st.SessionPath, Last: st.LastPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return again
}

func TestOpenCreatesDefaults(t *testing.T) {
	i18n.Set("en")
	st := openTemp(t)
	if st.Data.StateVersion != StateVersion {
		t.Errorf("st version = %d", st.Data.StateVersion)
	}
	if st.Cfg().BmclapiEndpoint == "" || st.Cfg().ManifestTTLHours == 0 {
		t.Errorf("config table not initialised: %+v", st.Cfg())
	}
	if st.Data.Accounts == nil || st.Data.Instances == nil {
		t.Error("maps must be initialised to avoid nil map writes")
	}
	if _, err := st.Last(); err != nil {
		t.Errorf("a missing last.toml must not be an error: %v", err)
	}
	if lt, _ := st.Last(); lt != nil {
		t.Errorf("no previous launch expected, got %+v", lt)
	}
}

func TestSaveWritesThreeFilesWithSafePermissions(t *testing.T) {
	st := openTemp(t)
	st.Data.Instances["alpha"] = &Instance{Name: "alpha", Version: "1.21.4", CreatedAt: time.Now()}
	if err := st.SaveAll(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveLast(&LastLaunch{Instance: "alpha", Version: "1.21.4", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{st.Path, st.StatePath, st.LastPath} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s missing: %v", filepath.Base(path), err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions = %o, want 600", filepath.Base(path), st.Mode().Perm())
		}
	}
	// the file name must match the format inside
	cfgRaw, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgRaw), "[config]") {
		t.Errorf("mcli.toml has no [config] table:\n%s", cfgRaw)
	}
	if !strings.HasPrefix(string(cfgRaw), "#") {
		t.Error("mcli.toml should explain itself at the top")
	}
}

func TestConfigIsPersistedAndCommentsSurvive(t *testing.T) {
	st := openTemp(t)
	// simulate a hand-edited file
	handWritten := "# my note\n\n[config]\nmanifest_ttl_hours = 6 # keep me\nminecraft_dir = \"/srv/mc\"\n"
	if err := os.WriteFile(st.Path, []byte(handWritten), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := reopen(t, st)
	if v, _ := reopened.Cfg().Get("minecraft_dir"); v != "/srv/mc" {
		t.Errorf("hand-written value not read: %q", v)
	}
	if reopened.Cfg().ManifestTTLHours != 6 {
		t.Errorf("hand-written int not read: %d", reopened.Cfg().ManifestTTLHours)
	}
	if err := reopened.Cfg().Set("manifest_ttl_hours", "12"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SaveConfigValue("manifest_ttl_hours"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	if !strings.Contains(text, "manifest_ttl_hours = 12") {
		t.Errorf("value not patched:\n%s", text)
	}
	for _, keep := range []string{"# my note", "# keep me", `minecraft_dir = "/srv/mc"`} {
		if !strings.Contains(text, keep) {
			t.Errorf("comment lost (%q):\n%s", keep, text)
		}
	}
	// and it survives a round trip
	again := reopen(t, st)
	if again.Cfg().ManifestTTLHours != 12 {
		t.Errorf("patched value not readable: %d", again.Cfg().ManifestTTLHours)
	}
}

func TestStateAndConfigAreSeparate(t *testing.T) {
	st := openTemp(t)
	if err := st.SaveAll(); err != nil {
		t.Fatal(err)
	}
	cfgBefore, _ := os.ReadFile(st.Path)
	st.Data.Instances["beta"] = &Instance{Name: "beta", Version: "1.7.10", CreatedAt: time.Now()}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	cfgAfter, _ := os.ReadFile(st.Path)
	if string(cfgBefore) != string(cfgAfter) {
		t.Error("saving state must not touch mcli.toml")
	}
	stateRaw, _ := os.ReadFile(st.StatePath)
	if !strings.Contains(string(stateRaw), "[instances.beta]") {
		t.Errorf("instance not written to state.toml:\n%s", stateRaw)
	}
	if strings.Contains(string(stateRaw), "minecraft_dir") {
		t.Error("state.toml must not duplicate the configuration")
	}
}

func TestLastLaunchIsItsOwnSmallFile(t *testing.T) {
	st := openTemp(t)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	stateBefore, _ := os.ReadFile(st.StatePath)

	lt := &LastLaunch{
		Instance: "vanilla", Version: "1.21.4", Xmx: "4G", Xms: "1G",
		JvmArgs: []string{"-XX:+UseG1GC"}, Width: 1920, Height: 1080, At: time.Now(),
	}
	if err := st.SaveLast(lt); err != nil {
		t.Fatal(err)
	}
	stateAfter, _ := os.ReadFile(st.StatePath)
	if string(stateBefore) != string(stateAfter) {
		t.Error("SaveLast must not rewrite state.toml")
	}
	lastRaw, err := os.ReadFile(st.LastPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(lastRaw), "[instances") {
		t.Error("last.toml must contain nothing but the previous launch")
	}

	again := reopen(t, st)
	got, err := again.Last()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Instance != "vanilla" || got.Width != 1920 || len(got.JvmArgs) != 1 {
		t.Fatalf("replayed launch = %+v", got)
	}
}

func TestAccountTokenSaltLifecycle(t *testing.T) {
	i18n.Set("en")
	st := openTemp(t)
	acc := &Account{UUID: "abc", Name: "Kailo"}
	st.Data.Accounts[acc.UUID] = acc

	if _, err := st.AccountKey(acc); err == nil {
		t.Error("an account without a salt must not yield a key")
	}
	if err := st.SetAccountSalt(acc); err != nil {
		t.Fatal(err)
	}
	tok, _ := st.Tokens(acc.UUID)
	if _, err := base64.StdEncoding.DecodeString(tok.Salt); err != nil {
		t.Errorf("salt is not base64: %q", tok.Salt)
	}
	first, err := st.AccountKey(acc)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Errorf("key length = %d", len(first))
	}
	// stable, and cached: a second call returns the same bytes
	second, _ := st.AccountKey(acc)
	if string(first) != string(second) {
		t.Error("key derivation must be deterministic")
	}

	// different accounts must not share a key
	other := &Account{UUID: "def", Name: "Alt"}
	if err := st.SetAccountSalt(other); err != nil {
		t.Fatal(err)
	}
	otherTok, _ := st.Tokens(other.UUID)
	if otherTok.Salt == tok.Salt {
		t.Error("each account needs its own salt")
	}
	otherKey, _ := st.AccountKey(other)
	if string(otherKey) == string(first) {
		t.Error("different salts must derive different keys")
	}

	// and it must survive a reload (so stored tokens stay decryptable)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTokens(); err != nil {
		t.Fatal(err)
	}
	again := reopen(t, st)
	reloaded := again.Data.Accounts["abc"]
	if reloaded == nil {
		t.Fatalf("account not persisted: %+v", reloaded)
	}
	reloadedTok, _ := again.Tokens("abc")
	if reloadedTok.Salt != tok.Salt {
		t.Fatalf("salt not persisted: %+v", reloadedTok)
	}
	keyAgain, err := again.AccountKey(reloaded)
	if err != nil {
		t.Fatal(err)
	}
	if string(keyAgain) != string(first) {
		t.Error("key must be reproducible across runs")
	}
}

func TestActiveAccountSwitching(t *testing.T) {
	st := openTemp(t)
	st.Data.Accounts["a"] = &Account{UUID: "a", Name: "One", Active: true}
	st.Data.Accounts["b"] = &Account{UUID: "b", Name: "Two"}
	if got := st.ActiveAccount(); got == nil || got.Name != "One" {
		t.Fatalf("active account = %+v", got)
	}
	st.SetActive("b")
	if got := st.ActiveAccount(); got == nil || got.Name != "Two" {
		t.Fatalf("after switching: %+v", got)
	}
	if st.Data.Accounts["a"].Active {
		t.Error("old account must be deactivated")
	}
	if names := st.AccountNames(); len(names) != 2 || names[0] != "a" {
		t.Errorf("account names = %v", names)
	}
}

func TestInstanceOrderingPrefersRecentlyPlayed(t *testing.T) {
	st := openTemp(t)
	now := time.Now()
	st.Data.Instances["old"] = &Instance{Name: "old", Version: "1.19", LastPlayed: now.Add(-48 * time.Hour)}
	st.Data.Instances["new"] = &Instance{Name: "new", Version: "1.21.4", LastPlayed: now}
	st.Data.Instances["never"] = &Instance{Name: "never", Version: "1.7.10"}
	st.Data.Instances["dotted.name"] = &Instance{Name: "dotted.name", Version: "1.21.4"}
	names := st.InstanceNames()
	if len(names) != 4 || names[0] != "new" {
		t.Fatalf("instance order = %v", names)
	}
	if names[3] != "never" {
		t.Errorf("never-played instance should sort last: %v", names)
	}

	// dotted keys must round trip through the TOML writer
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	again := reopen(t, st)
	if _, ok := again.GetInstance("dotted.name"); !ok {
		t.Errorf("instance with a dot in its name was lost: %v", again.InstanceNames())
	}
}

func TestSaveRejectsInvalidConfig(t *testing.T) {
	st := openTemp(t)
	st.Cfg().HTTPTimeout = 99999
	if err := st.SaveConfig(); err == nil {
		t.Error("invalid config must not be written")
	}
}

// TestCredentialsLiveInTheirOwnFiles pins the split: identity in state.toml (so
// that file stays shareable), long-lived and short-lived tokens in two separate
// 0600 documents, and no token material anywhere else.
func TestCredentialsLiveInTheirOwnFiles(t *testing.T) {
	i18n.Set("en")
	st := openTemp(t)
	acc := &Account{UUID: "abc", Name: "Kailo", Label: "ms:Kailo", Active: true}
	st.Data.Accounts["abc"] = acc
	if err := st.SetAccountSalt(acc); err != nil {
		t.Fatal(err)
	}
	st.SetRefreshToken("abc", st.Data.Refresh["abc"].Salt, "sealed-refresh")
	st.SetSessionToken("abc", "sealed-session", time.Now().Add(24*time.Hour))
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTokens(); err != nil {
		t.Fatal(err)
	}

	state, err := os.ReadFile(st.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sealed-refresh", "sealed-session"} {
		if strings.Contains(string(state), secret) {
			t.Errorf("state.toml must not carry credentials, found %q:\n%s", secret, state)
		}
	}
	refresh, err := os.ReadFile(st.RefreshPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(refresh), "sealed-refresh") {
		t.Errorf("refresh.toml must hold the refresh token:\n%s", refresh)
	}
	if strings.Contains(string(refresh), "sealed-session") {
		t.Error("refresh.toml must not hold the short-lived token")
	}
	session, err := os.ReadFile(st.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(session), "sealed-session") {
		t.Errorf("session.toml must hold the Minecraft token:\n%s", session)
	}
	for _, path := range []string{st.RefreshPath, st.SessionPath} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s permissions = %o, want 600", filepath.Base(path), st.Mode().Perm())
		}
	}

	// and both halves survive a reload
	again := reopen(t, st)
	tok, sess := again.Tokens("abc")
	if tok.RefreshEnc != "sealed-refresh" || sess.MCTokenEnc != "sealed-session" {
		t.Fatalf("credentials lost across runs: %+v %+v", tok, sess)
	}
	if again.Data.Accounts["abc"] == nil || again.Data.Refresh["abc"] == nil {
		t.Fatal("account or token record missing after reload")
	}

	// logging out removes the account and its credentials everywhere
	again.DropAccount("abc")
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	if err := again.SaveTokens(); err != nil {
		t.Fatal(err)
	}
	final := reopen(t, again)
	if len(final.Data.Accounts) != 0 || len(final.Data.Refresh) != 0 || len(final.Data.Session) != 0 {
		t.Fatalf("logout left something behind: %+v %+v %+v",
			final.Data.Accounts, final.Data.Refresh, final.Data.Session)
	}
}

// TestPreSplitStateIsMigrated covers the v2 layout, where the tokens still sat
// inside the account records.

func TestUnknownKeysAreReportedNotSwallowed(t *testing.T) {
	dir := t.TempDir()
	cfg := "[config]\nretries_typo = 3\n"
	if err := os.WriteFile(filepath.Join(dir, "mcli.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(Files{
		Config:  filepath.Join(dir, "mcli.toml"),
		State:   filepath.Join(dir, "state.toml"),
		Refresh: filepath.Join(dir, "refresh.toml"),
		Session: filepath.Join(dir, "session.toml"),
		Last:    filepath.Join(dir, "last.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.UnknownKeys) == 0 {
		t.Fatal("a typo in mcli.toml must be reported")
	}
	if !strings.Contains(strings.Join(st.UnknownKeys, ","), "retries_typo") {
		t.Errorf("unknown keys = %v", st.UnknownKeys)
	}
}
