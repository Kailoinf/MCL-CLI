package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "mcli.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestOpenCreatesDefaults(t *testing.T) {
	db := openTemp(t)
	if db.Data.DBVersion != DBVersion {
		t.Errorf("db version = %d", db.Data.DBVersion)
	}
	if db.Data.Salt == "" {
		t.Error("a fresh db must have a salt")
	}
	if db.Cfg().BmclapiEndpoint == "" {
		t.Error("config table not initialised")
	}
	if db.Data.Accounts == nil || db.Data.Instances == nil {
		t.Error("maps must be initialised to avoid nil map writes")
	}
}

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcli.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Cfg().Set("manifest_ttl_hours", "9"); err != nil {
		t.Fatal(err)
	}
	db.Data.Instances["alpha"] = &Instance{Name: "alpha", Version: "1.21.4", CreatedAt: time.Now()}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err == nil {
		if st.Mode().Perm() != 0o600 {
			t.Errorf("mcli.db permissions = %o, want 600", st.Mode().Perm())
		}
	} else {
		t.Fatalf("mcli.db missing: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := again.Cfg().Get("manifest_ttl_hours"); v != "9" {
		t.Errorf("config not persisted: %q", v)
	}
	if _, ok := again.GetInstance("alpha"); !ok {
		t.Error("instance not persisted")
	}
	if again.Data.Salt != db.Data.Salt {
		t.Error("salt must be stable across reloads")
	}
	if string(again.mustKey(t)) != string(db.mustKey(t)) {
		t.Error("derived key must be stable across reloads")
	}
}

func (db *DB) mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := db.Key()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestActiveAccountSwitching(t *testing.T) {
	db := openTemp(t)
	db.Data.Accounts["a"] = &Account{UUID: "a", Name: "One", Active: true}
	db.Data.Accounts["b"] = &Account{UUID: "b", Name: "Two"}
	if got := db.ActiveAccount(); got == nil || got.Name != "One" {
		t.Fatalf("active account = %+v", got)
	}
	db.SetActive("b")
	if got := db.ActiveAccount(); got == nil || got.Name != "Two" {
		t.Fatalf("after switching: %+v", got)
	}
	if db.Data.Accounts["a"].Active {
		t.Error("old account must be deactivated")
	}
	if names := db.AccountNames(); len(names) != 2 || names[0] != "a" {
		t.Errorf("account names = %v", names)
	}
}

func TestInstanceOrderingPrefersRecentlyPlayed(t *testing.T) {
	db := openTemp(t)
	now := time.Now()
	db.Data.Instances["old"] = &Instance{Name: "old", Version: "1.19", LastPlayed: now.Add(-48 * time.Hour)}
	db.Data.Instances["new"] = &Instance{Name: "new", Version: "1.21.4", LastPlayed: now}
	db.Data.Instances["never"] = &Instance{Name: "never", Version: "1.7.10"}
	names := db.InstanceNames()
	if len(names) != 3 || names[0] != "new" {
		t.Fatalf("instance order = %v", names)
	}
	if names[2] != "never" {
		t.Errorf("never-played instance should sort last: %v", names)
	}
}

func TestSaveRejectsInvalidConfig(t *testing.T) {
	db := openTemp(t)
	db.Cfg().HTTPTimeout = 99999
	if err := db.Save(); err == nil {
		t.Error("invalid config must not be written")
	}
}
