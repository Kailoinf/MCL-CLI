// Package store owns mcli.db: the single JSON file holding the config table,
// accounts, instances and the version-manifest cache.
//
// It is deliberately not SQLite. The requirement is "a config table in
// mcli.db"; a small atomic JSON document does that with zero third-party
// dependencies, which matches this project's minimal-dependency policy.
package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/config"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/secret"
)

// DBVersion is bumped when the on-disk shape changes incompatibly.
const DBVersion = 1

// Account is one Microsoft/Minecraft login.
type Account struct {
	Name       string    `json:"name"`  // Minecraft profile name at login time
	UUID       string    `json:"uuid"`  // Minecraft profile UUID (no dashes)
	XUID       string    `json:"xuid"`  // Xbox user hash, passed to the game as --xuid
	Label      string    `json:"label"` // user-facing handle, e.g. "ms:kailo"
	RefreshEnc string    `json:"refresh_token_enc"`
	MCTokenEnc string    `json:"mc_token_enc"`
	MCTokenExp time.Time `json:"mc_token_expires"`
	Active     bool      `json:"active"`
	AddedAt    time.Time `json:"added_at"`
	LastLogin  time.Time `json:"last_login"`
}

// Instance is a named, isolated game profile bound to one Minecraft version.
type Instance struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"` // Minecraft version id, e.g. 1.21.4
	CreatedAt  time.Time `json:"created_at"`
	LastPlayed time.Time `json:"last_played,omitempty"`
	Java       string    `json:"java,omitempty"` // explicit java binary, empty = auto
	Xmx        string    `json:"xmx,omitempty"`  // e.g. "4G", empty = auto
	Xms        string    `json:"xms,omitempty"`  //
	JvmArgs    []string  `json:"jvm_args,omitempty"`
	GameArgs   []string  `json:"game_args,omitempty"`
}

// LastLaunch remembers the previous `launch` so `--last` can replay it.
type LastLaunch struct {
	Instance string    `json:"instance"`
	Version  string    `json:"version"`
	Java     string    `json:"java"`
	Xmx      string    `json:"xmx"`
	Xms      string    `json:"xms"`
	JvmArgs  []string  `json:"jvm_args,omitempty"`
	GameArgs []string  `json:"game_args,omitempty"`
	GameDir  string    `json:"game_dir"`
	At       time.Time `json:"at"`
}

// ManifestCache stores the raw version manifest plus its fetch time.
type ManifestCache struct {
	Source    string          `json:"source"` // "bmclapi" or "mojang"
	URL       string          `json:"url"`
	FetchedAt time.Time       `json:"fetched_at"`
	Raw       json.RawMessage `json:"raw"`
}

// Data is the whole mcli.db document.
type Data struct {
	DBVersion int                  `json:"db_version"`
	Salt      string               `json:"salt"` // base64, key-derivation salt
	Config    config.Config        `json:"config"`
	Accounts  map[string]*Account  `json:"accounts"`
	Instances map[string]*Instance `json:"instances"`
	Last      *LastLaunch          `json:"last_launch,omitempty"`
	Manifest  *ManifestCache       `json:"manifest_cache,omitempty"`
}

// DB is an open database handle.
type DB struct {
	Path string
	Data *Data

	salt []byte
	key  []byte
}

// Open loads mcli.db, creating it with defaults when absent.
func Open(path string) (*DB, error) {
	db := &DB{Path: path}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		d := &Data{}
		if err := json.Unmarshal(raw, d); err != nil {
			return nil, fmt.Errorf("%s", i18n.T("err.db.parse", path, err))
		}
		db.Data = d
	case os.IsNotExist(err):
		db.Data = &Data{DBVersion: DBVersion, Config: config.Default()}
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.db.read", err))
	}

	d := db.Data
	d.Config.FillMissing()
	if d.Accounts == nil {
		d.Accounts = map[string]*Account{}
	}
	if d.Instances == nil {
		d.Instances = map[string]*Instance{}
	}
	if d.DBVersion == 0 {
		d.DBVersion = DBVersion
	}
	if d.Salt == "" {
		s, err := secret.NewSalt()
		if err != nil {
			return nil, err
		}
		d.Salt = base64.StdEncoding.EncodeToString(s)
	}
	if db.salt, err = base64.StdEncoding.DecodeString(d.Salt); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err.db.salt", err))
	}
	return db, nil
}

// Key lazily derives (and caches) the AES key for this machine + db salt.
func (db *DB) Key() ([]byte, error) {
	if db.key != nil {
		return db.key, nil
	}
	k, err := secret.DeriveKey(secret.MachineIdentity(), db.salt)
	if err != nil {
		return nil, err
	}
	db.key = k
	return k, nil
}

// Save writes mcli.db atomically with 0600 permissions.
func (db *DB) Save() error {
	if err := db.Data.Config.Validate(); err != nil {
		return err
	}
	db.Data.DBVersion = DBVersion
	blob, err := json.MarshalIndent(db.Data, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	if err := os.MkdirAll(filepath.Dir(db.Path), 0o755); err != nil {
		return err
	}
	tmp := db.Path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.write", err))
	}
	if err := os.Rename(tmp, db.Path); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.commit", err))
	}
	return nil
}

// Cfg exposes the config table.
func (db *DB) Cfg() *config.Config { return &db.Data.Config }

// ---------------------------------------------------------------------------
// accounts

// ActiveAccount returns the currently selected account, if any.
func (db *DB) ActiveAccount() *Account {
	for _, a := range db.Data.Accounts {
		if a.Active {
			return a
		}
	}
	return nil
}

// SetActive makes one account the active one and clears the flag elsewhere.
func (db *DB) SetActive(uuid string) {
	for k, a := range db.Data.Accounts {
		a.Active = k == uuid
	}
}

// AccountNames returns account keys sorted by name.
func (db *DB) AccountNames() []string { return sortedKeys(db.Data.Accounts) }

// ---------------------------------------------------------------------------
// instances

// GetInstance looks up an instance by name.
func (db *DB) GetInstance(name string) (*Instance, bool) {
	i, ok := db.Data.Instances[name]
	return i, ok
}

// InstanceNames returns instance names, most recently played first, then
// alphabetical.
func (db *DB) InstanceNames() []string {
	names := sortedKeys(db.Data.Instances)
	sort.SliceStable(names, func(i, j int) bool {
		a, b := db.Data.Instances[names[i]], db.Data.Instances[names[j]]
		if !a.LastPlayed.Equal(b.LastPlayed) {
			return a.LastPlayed.After(b.LastPlayed)
		}
		return names[i] < names[j]
	})
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
