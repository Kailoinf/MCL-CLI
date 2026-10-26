// Package store owns mcli's five TOML documents:
//
//	mcli.toml     configuration only. Hand-editable; `mcli config set` patches a
//	              single key in place so comments and ordering survive.
//	state.toml    machine state: database version, accounts (identity only, no
//	              secrets, so the file is portable), instances, manifest-cache
//	              pointer.
//	refresh.toml  long-lived credentials: one refresh token per account, with the
//	              salt its encryption key was derived from (0600).
//	session.toml  short-lived credentials: the Minecraft access token and its
//	              expiry (0600). Separate from refresh.toml because a Minecraft
//	              token is refreshed every few hours — the file that holds the
//	              long-lived secret is then written once at login instead of on
//	              every refresh.
//	last.toml     the previous launch, for `mcli launch --last`. Separate so that
//	              replaying flags never has to read the configuration or state.
//
// All of them are parsed by internal/toml, so no third-party dependency is
// involved and the file name always matches the format. The cached version
// manifest body is not in any of them: it is a raw JSON file under cache/, which
// keeps the TOML documents small and readable.
package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/config"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/secret"
	"github.com/Kailoinf/MCL-CLI/internal/toml"
)

// DBVersion is bumped when the on-disk shape changes incompatibly: 2 moved the
// JSON database into TOML, 3 split the credentials out into refresh.toml and
// session.toml.
const DBVersion = 3

// ConfigSection is the TOML table holding the config table in mcli.toml.
const ConfigSection = "config"

// Account is the identity half of one Microsoft account: what a launcher needs
// to show a list, with no token material in it. Tokens live in the TokenRecord
// (refresh) and SessionRecord (Minecraft) that are keyed by the same UUID, in
// their own files.
type Account struct {
	Name      string    `toml:"name" json:"name"`   // Minecraft profile name at login time
	UUID      string    `toml:"uuid" json:"uuid"`   // Minecraft profile UUID (no dashes)
	XUID      string    `toml:"xuid" json:"xuid"`   // Xbox user hash, passed to the game as --xuid
	Label     string    `toml:"label" json:"label"` // user-facing handle, e.g. "ms:kailo"
	Active    bool      `toml:"active" json:"active"`
	AddedAt   time.Time `toml:"added_at" json:"added_at"`
	LastLogin time.Time `toml:"last_login" json:"last_login"`
}

// TokenRecord is the long-lived credential of an account: the refresh token plus
// the per-account salt the AES key is derived from. It lives in refresh.toml and
// is written at login, not on every token refresh.
type TokenRecord struct {
	Salt       string `toml:"token_salt"`
	RefreshEnc string `toml:"refresh_token_enc"`
}

// SessionRecord is the short-lived credential: the Minecraft access token and
// when it expires. It lives in session.toml and is rewritten on every refresh.
type SessionRecord struct {
	MCTokenEnc string    `toml:"mc_token_enc"`
	MCTokenExp time.Time `toml:"mc_token_expires"`
}

// Instance is a named, isolated game profile bound to one Minecraft version.
type Instance struct {
	Name       string    `toml:"name" json:"name"`
	Version    string    `toml:"version" json:"version"` // Minecraft version id, e.g. 1.21.4
	CreatedAt  time.Time `toml:"created_at" json:"created_at"`
	LastPlayed time.Time `toml:"last_played" json:"last_played"`
	Java       string    `toml:"java,omitempty" json:"java,omitempty"` // explicit java binary, empty = auto
	Xmx        string    `toml:"xmx,omitempty" json:"xmx,omitempty"`   // e.g. "4G", empty = auto
	Xms        string    `toml:"xms,omitempty" json:"xms,omitempty"`   //
	JvmArgs    []string  `toml:"jvm_args,omitempty" json:"jvm_args,omitempty"`
	GameArgs   []string  `toml:"game_args,omitempty" json:"game_args,omitempty"`
}

// LastLaunch remembers the previous `launch` so `--last` can replay it. It is
// stored in its own small file (last.toml) and holds exactly what a replay needs.
type LastLaunch struct {
	Instance string    `toml:"instance" json:"instance"`
	Version  string    `toml:"version" json:"version"`
	Java     string    `toml:"java,omitempty" json:"java,omitempty"`
	Xmx      string    `toml:"xmx,omitempty" json:"xmx,omitempty"`
	Xms      string    `toml:"xms,omitempty" json:"xms,omitempty"`
	Width    int       `toml:"width,omitempty" json:"width,omitempty"`
	Height   int       `toml:"height,omitempty" json:"height,omitempty"`
	JvmArgs  []string  `toml:"jvm_args,omitempty" json:"jvm_args,omitempty"`
	GameArgs []string  `toml:"game_args,omitempty" json:"game_args,omitempty"`
	GameDir  string    `toml:"game_dir,omitempty" json:"game_dir,omitempty"`
	At       time.Time `toml:"at" json:"at"`
}

// ManifestCache points at the cached version manifest body.
type ManifestCache struct {
	Source    string    `toml:"source" json:"source"` // "bmclapi" or "mojang"
	FetchedAt time.Time `toml:"fetched_at" json:"fetched_at"`
	File      string    `toml:"file" json:"file"` // absolute path of the cached JSON
}

// stateFile is the on-disk shape of state.toml.
type stateFile struct {
	DBVersion int                  `toml:"db_version"`
	Accounts  map[string]*Account  `toml:"accounts"`
	Instances map[string]*Instance `toml:"instances"`
	Manifest  *ManifestCache       `toml:"manifest_cache,omitempty"`
}

// stateFileV2 is the pre-split shape, where every account carried its own
// encrypted tokens. Read once, migrated, never written again.
type stateFileV2 struct {
	DBVersion int                       `toml:"db_version"`
	Accounts  map[string]*legacyAccount `toml:"accounts"`
	Instances map[string]*Instance      `toml:"instances"`
	Manifest  *ManifestCache            `toml:"manifest_cache,omitempty"`
}

// legacyAccount is the account record used before the credential split (and by
// the pre-TOML JSON database): identity and tokens in one place.
type legacyAccount struct {
	Name       string    `toml:"name" json:"name"`
	UUID       string    `toml:"uuid" json:"uuid"`
	XUID       string    `toml:"xuid" json:"xuid"`
	Label      string    `toml:"label" json:"label"`
	TokenSalt  string    `toml:"token_salt" json:"token_salt"`
	RefreshEnc string    `toml:"refresh_token_enc" json:"refresh_token_enc"`
	MCTokenEnc string    `toml:"mc_token_enc" json:"mc_token_enc"`
	MCTokenExp time.Time `toml:"mc_token_expires" json:"mc_token_expires"`
	Active     bool      `toml:"active" json:"active"`
	AddedAt    time.Time `toml:"added_at" json:"added_at"`
	LastLogin  time.Time `toml:"last_login" json:"last_login"`
}

// refreshFile is the on-disk shape of refresh.toml.
type refreshFile struct {
	Tokens map[string]*TokenRecord `toml:"tokens"`
}

// sessionFile is the on-disk shape of session.toml.
type sessionFile struct {
	Sessions map[string]*SessionRecord `toml:"sessions"`
}

// configFile is the on-disk shape of mcli.toml.
type configFile struct {
	Config config.Config `toml:"config"`
}

// Data is the merged view of the config, state, refresh and session documents.
// The previous launch is not part of it — see DB.Last / DB.SaveLast.
type Data struct {
	DBVersion int
	Config    config.Config
	Accounts  map[string]*Account
	// Refresh and Session are keyed by account UUID.
	Refresh   map[string]*TokenRecord
	Session   map[string]*SessionRecord
	Instances map[string]*Instance
	Manifest  *ManifestCache
}

// Files names the documents of one mcli home.
type Files struct {
	Config  string
	State   string
	Refresh string
	Session string
	Last    string
}

// DB is an open set of documents.
type DB struct {
	// Path is mcli.toml, StatePath state.toml, RefreshPath refresh.toml,
	// SessionPath session.toml, LastPath last.toml.
	Path        string
	StatePath   string
	RefreshPath string
	SessionPath string
	LastPath    string

	Data *Data

	// UnknownKeys lists keys found on disk that mcli does not know, so the CLI
	// can warn about typos instead of ignoring them silently.
	UnknownKeys []string

	last          *LastLaunch
	lastLoaded    bool
	migrated      string
	migratedSplit bool
	keys          map[string][]byte
}

// Open loads every document of one mcli home, creating the missing ones and
// migrating what older versions wrote (a legacy JSON mcli.db, and state.toml
// files whose accounts still carried their tokens).
func Open(f Files) (*DB, error) {
	configPath, statePath, lastPath := f.Config, f.State, f.Last
	db := &DB{
		Path:        configPath,
		StatePath:   statePath,
		RefreshPath: f.Refresh,
		SessionPath: f.Session,
		LastPath:    lastPath,
		keys:        map[string][]byte{},
		Data: &Data{
			Config:    config.Default(),
			Accounts:  map[string]*Account{},
			Refresh:   map[string]*TokenRecord{},
			Session:   map[string]*SessionRecord{},
			Instances: map[string]*Instance{},
		},
	}

	// --- configuration ---------------------------------------------------
	cfgRaw, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		file := configFile{Config: config.Default()}
		unknown, err := toml.UnmarshalWithReport(cfgRaw, &file)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("err.db.parse", configPath, err))
		}
		db.UnknownKeys = append(db.UnknownKeys, labelLeft(configPath, unknown)...)
		db.Data.Config = file.Config
	case os.IsNotExist(err):
		if legacy, legacyErr := os.ReadFile(legacyPathFor(statePath)); legacyErr == nil {
			if err := db.importLegacy(legacy, statePath); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.db.read", statePath, err))
	}

	// --- state -----------------------------------------------------------
	stateRaw, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		file := stateFile{}
		unknown, err := toml.UnmarshalWithReport(stateRaw, &file)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("err.db.parse", statePath, err))
		}
		if carriesInlineTokens(stateRaw) {
			// Pre-split state.toml: the accounts still hold their encrypted
			// tokens. Re-read them in the old shape and move the credentials
			// into refresh.toml / session.toml.
			old := stateFileV2{}
			if _, err := toml.UnmarshalWithReport(stateRaw, &old); err != nil {
				return nil, fmt.Errorf("%s", i18n.T("err.db.parse", statePath, err))
			}
			db.splitLegacyAccounts(old)
		} else {
			db.UnknownKeys = append(db.UnknownKeys, labelLeft(statePath, unknown)...)
		}
		db.Data.DBVersion = file.DBVersion
		if file.Accounts != nil {
			db.Data.Accounts = file.Accounts
		}
		if file.Instances != nil {
			db.Data.Instances = file.Instances
		}
		db.Data.Manifest = file.Manifest
	case os.IsNotExist(err):
		// fresh home
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.db.read", statePath, err))
	}

	// --- credentials -----------------------------------------------------
	if err := db.loadTokens(); err != nil {
		return nil, err
	}

	d := db.Data
	d.Config.FillMissing()
	if d.Instances == nil {
		d.Instances = map[string]*Instance{}
	}
	if d.Accounts == nil {
		d.Accounts = map[string]*Account{}
	}
	if d.Refresh == nil {
		d.Refresh = map[string]*TokenRecord{}
	}
	if d.Session == nil {
		d.Session = map[string]*SessionRecord{}
	}
	if d.DBVersion == 0 {
		d.DBVersion = DBVersion
	}
	// A migrated home always ends up at the current shape, whatever version the
	// file it came from claimed.
	if db.migrated != "" || db.migratedSplit {
		d.DBVersion = DBVersion
	}
	// A legacy database's salt is handed to each imported account by
	// importLegacy, so their tokens stay decryptable after the migration.
	if db.migrated != "" || db.migratedSplit {
		// write the documents straight away so the old shape can be retired
		if err := db.SaveAll(); err != nil {
			return nil, err
		}
		if lt := db.last; lt != nil {
			if err := db.SaveLast(lt); err != nil {
				return nil, err
			}
		}
	}
	return db, nil
}

// loadTokens reads refresh.toml and session.toml. Missing files are the normal
// case before the first login.
func (db *DB) loadTokens() error {
	if db.RefreshPath != "" {
		if raw, err := os.ReadFile(db.RefreshPath); err == nil {
			file := refreshFile{}
			unknown, err := toml.UnmarshalWithReport(raw, &file)
			if err != nil {
				return fmt.Errorf("%s", i18n.T("err.db.parse", db.RefreshPath, err))
			}
			db.UnknownKeys = append(db.UnknownKeys, labelLeft(db.RefreshPath, unknown)...)
			if file.Tokens != nil {
				db.Data.Refresh = file.Tokens
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s", i18n.T("err.db.read", db.RefreshPath, err))
		}
	}
	if db.SessionPath != "" {
		if raw, err := os.ReadFile(db.SessionPath); err == nil {
			file := sessionFile{}
			unknown, err := toml.UnmarshalWithReport(raw, &file)
			if err != nil {
				return fmt.Errorf("%s", i18n.T("err.db.parse", db.SessionPath, err))
			}
			db.UnknownKeys = append(db.UnknownKeys, labelLeft(db.SessionPath, unknown)...)
			if file.Sessions != nil {
				db.Data.Session = file.Sessions
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s", i18n.T("err.db.read", db.SessionPath, err))
		}
	}
	return nil
}

// carriesInlineTokens reports whether a state.toml body still stores credentials
// inside its account tables.
func carriesInlineTokens(raw []byte) bool {
	body := string(raw)
	return strings.Contains(body, "refresh_token_enc") || strings.Contains(body, "mc_token_enc")
}

// splitLegacyAccounts moves the credentials of a pre-split state.toml into the
// two credential documents, keeping the identity (and therefore the logins) as
// they were.
func (db *DB) splitLegacyAccounts(old stateFileV2) {
	for uuid, acc := range old.Accounts {
		if acc == nil {
			continue
		}
		db.Data.Accounts[uuid] = &Account{
			Name:      acc.Name,
			UUID:      acc.UUID,
			XUID:      acc.XUID,
			Label:     acc.Label,
			Active:    acc.Active,
			AddedAt:   acc.AddedAt,
			LastLogin: acc.LastLogin,
		}
		if acc.RefreshEnc != "" || acc.TokenSalt != "" {
			db.Data.Refresh[uuid] = &TokenRecord{Salt: acc.TokenSalt, RefreshEnc: acc.RefreshEnc}
		}
		if acc.MCTokenEnc != "" {
			db.Data.Session[uuid] = &SessionRecord{MCTokenEnc: acc.MCTokenEnc, MCTokenExp: acc.MCTokenExp}
		}
	}
	if len(old.Instances) > 0 {
		db.Data.Instances = old.Instances
	}
	if old.Manifest != nil {
		db.Data.Manifest = old.Manifest
	}
	if old.DBVersion != 0 {
		db.Data.DBVersion = old.DBVersion
	}
	db.migratedSplit = true
}

// legacyDoc mirrors the pre-TOML JSON layout. It is decoded with the TOML
// decoder (DecodeMap) so no second set of struct tags is needed.
type legacyDoc struct {
	DBVersion int                       `toml:"db_version"`
	Salt      string                    `toml:"salt"`
	Config    config.Config             `toml:"config"`
	Accounts  map[string]*legacyAccount `toml:"accounts"`
	Instances map[string]*Instance      `toml:"instances"`
	Last      *LastLaunch               `toml:"last_launch"`
}

// importLegacy reads a pre-TOML JSON mcli.db into the new documents: the config
// goes to mcli.toml, everything else to state.toml (and last.toml), and the
// cached manifest body moves into cache/ as raw JSON.
func (db *DB) importLegacy(raw []byte, statePath string) error {
	generic := map[string]interface{}{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.parse", legacyPathFor(statePath), err))
	}
	old := &legacyDoc{}
	if _, err := toml.DecodeMap(generic, old); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.parse", legacyPathFor(statePath), err))
	}
	if old.DBVersion != 0 {
		db.Data.DBVersion = old.DBVersion
	}
	db.Data.Config = old.Config
	if old.Accounts != nil {
		for uuid, acc := range old.Accounts {
			if acc == nil {
				continue
			}
			salt := acc.TokenSalt
			if salt == "" {
				salt = old.Salt // the pre-TOML format kept one salt for everyone
			}
			db.Data.Accounts[uuid] = &Account{
				Name:      acc.Name,
				UUID:      acc.UUID,
				XUID:      acc.XUID,
				Label:     acc.Label,
				Active:    acc.Active,
				AddedAt:   acc.AddedAt,
				LastLogin: acc.LastLogin,
			}
			if acc.RefreshEnc != "" || salt != "" {
				db.Data.Refresh[uuid] = &TokenRecord{Salt: salt, RefreshEnc: acc.RefreshEnc}
			}
			if acc.MCTokenEnc != "" {
				db.Data.Session[uuid] = &SessionRecord{MCTokenEnc: acc.MCTokenEnc, MCTokenExp: acc.MCTokenExp}
			}
		}
	}
	if old.Instances != nil {
		db.Data.Instances = old.Instances
	}
	if old.Last != nil {
		db.last = old.Last
		db.lastLoaded = true
	}
	if mc, ok := generic["manifest_cache"].(map[string]interface{}); ok {
		cache := &ManifestCache{File: manifestPathFor(statePath)}
		if s, ok := mc["source"].(string); ok {
			cache.Source = s
		}
		if ts, ok := mc["fetched_at"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				cache.FetchedAt = parsed
			}
		}
		if body := mc["raw"]; body != nil {
			if blob, err := json.Marshal(body); err == nil {
				if err := os.MkdirAll(filepath.Dir(cache.File), 0o755); err == nil {
					_ = os.WriteFile(cache.File, blob, 0o644)
				}
			}
		}
		db.Data.Manifest = cache
	}
	db.migrated = legacyPathFor(statePath)
	return nil
}

func legacyPathFor(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "mcli.db")
}

func manifestPathFor(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "cache", "version_manifest_v2.json")
}

func labelLeft(path string, keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, filepath.Base(path)+":"+k)
	}
	return out
}

// ---------------------------------------------------------------------------
// account credentials
//
// One salt per account, stored in refresh.toml next to the refresh token it
// protects. The key is PBKDF2(machine identity, account salt): the credential
// files alone are useless on another machine, while config, state (identity and
// instances) and last-launch stay fully portable.

// Tokens returns an account's credential records, creating empty ones when the
// account has never stored a token.
func (db *DB) Tokens(uuid string) (*TokenRecord, *SessionRecord) {
	if db.Data.Refresh == nil {
		db.Data.Refresh = map[string]*TokenRecord{}
	}
	if db.Data.Session == nil {
		db.Data.Session = map[string]*SessionRecord{}
	}
	rec, ok := db.Data.Refresh[uuid]
	if !ok || rec == nil {
		rec = &TokenRecord{}
		db.Data.Refresh[uuid] = rec
	}
	sess, ok := db.Data.Session[uuid]
	if !ok || sess == nil {
		sess = &SessionRecord{}
		db.Data.Session[uuid] = sess
	}
	return rec, sess
}

// SetRefreshToken stores one account's long-lived credential material.
func (db *DB) SetRefreshToken(uuid, salt, enc string) {
	rec, _ := db.Tokens(uuid)
	rec.Salt = salt
	rec.RefreshEnc = enc
}

// SetSessionToken stores one account's short-lived Minecraft token.
func (db *DB) SetSessionToken(uuid, enc string, exp time.Time) {
	_, sess := db.Tokens(uuid)
	sess.MCTokenEnc = enc
	sess.MCTokenExp = exp
}

// DropAccount removes an account and every trace of its credentials.
func (db *DB) DropAccount(uuid string) {
	delete(db.Data.Accounts, uuid)
	delete(db.Data.Refresh, uuid)
	delete(db.Data.Session, uuid)
	delete(db.keys, uuid)
}

// SetAccountSalt gives an account a fresh salt when it has none (first login).
func (db *DB) SetAccountSalt(acc *Account) error {
	rec, _ := db.Tokens(acc.UUID)
	if rec.Salt != "" {
		return nil
	}
	salt, err := secret.NewSalt()
	if err != nil {
		return err
	}
	rec.Salt = base64.StdEncoding.EncodeToString(salt)
	delete(db.keys, acc.UUID)
	return nil
}

// AccountKey derives (and caches) the AES key protecting one account's tokens.
func (db *DB) AccountKey(acc *Account) ([]byte, error) {
	if acc == nil {
		return nil, fmt.Errorf("%s", i18n.T("err.secret.nosalt"))
	}
	if k, ok := db.keys[acc.UUID]; ok {
		return k, nil
	}
	rec := db.Data.Refresh[acc.UUID]
	if rec == nil {
		return nil, fmt.Errorf("%s", i18n.T("err.secret.nosalt"))
	}
	salt, err := base64.StdEncoding.DecodeString(rec.Salt)
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("%s", i18n.T("err.secret.nosalt"))
	}
	k, err := secret.DeriveKey(secret.MachineIdentity(), salt)
	if err != nil {
		return nil, err
	}
	db.keys[acc.UUID] = k
	return k, nil
}

// MigratedFrom is the legacy file that was imported, if any.
func (db *DB) MigratedFrom() string { return db.migrated }

// Save writes state.toml atomically with 0600 permissions. Credentials are not
// part of it: use SaveRefresh / SaveSession for those.
func (db *DB) Save() error {
	if err := db.Data.Config.Validate(); err != nil {
		return err
	}
	state := stateFile{
		DBVersion: DBVersion,
		Accounts:  db.Data.Accounts,
		Instances: db.Data.Instances,
		Manifest:  db.Data.Manifest,
	}
	blob, err := toml.Marshal(state)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.marshal", err))
	}
	// The credential documents are created lazily by their own savers, but a
	// state write should not leave the home looking half-built.
	if _, err := os.Stat(db.RefreshPath); os.IsNotExist(err) && db.RefreshPath != "" {
		if err := db.SaveRefresh(); err != nil {
			return err
		}
	}
	return writeFileAtomic(db.StatePath, append([]byte(stateHeader()), blob...))
}

// SaveRefresh writes refresh.toml (0600): the long-lived credentials.
func (db *DB) SaveRefresh() error {
	if db.RefreshPath == "" {
		return nil
	}
	// Accounts that never stored a token are simply absent from the file: an
	// empty entry would only be noise a reader has to explain.
	tokens := map[string]*TokenRecord{}
	for uuid, rec := range db.Data.Refresh {
		if rec == nil || (rec.RefreshEnc == "" && rec.Salt == "") {
			continue
		}
		tokens[uuid] = rec
	}
	blob, err := toml.Marshal(refreshFile{Tokens: tokens})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.marshal", err))
	}
	return writeFileAtomic(db.RefreshPath, append([]byte(refreshHeader()), bytes.TrimLeft(blob, "\n")...))
}

// SaveSession writes session.toml (0600): the short-lived credentials. This is
// the file that gets rewritten whenever a token is refreshed.
func (db *DB) SaveSession() error {
	if db.SessionPath == "" {
		return nil
	}
	sessions := map[string]*SessionRecord{}
	for uuid, rec := range db.Data.Session {
		if rec == nil || rec.MCTokenEnc == "" {
			continue
		}
		sessions[uuid] = rec
	}
	blob, err := toml.Marshal(sessionFile{Sessions: sessions})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.marshal", err))
	}
	return writeFileAtomic(db.SessionPath, append([]byte(sessionHeader()), bytes.TrimLeft(blob, "\n")...))
}

// SaveTokens writes both credential documents.
func (db *DB) SaveTokens() error {
	if err := db.SaveRefresh(); err != nil {
		return err
	}
	return db.SaveSession()
}

// SaveConfig regenerates mcli.toml from scratch (comments included).
func (db *DB) SaveConfig() error {
	if err := db.Data.Config.Validate(); err != nil {
		return err
	}
	file := configFile{Config: db.Data.Config}
	blob, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.marshal", err))
	}
	return writeFileAtomic(db.Path, append([]byte(configHeader()), blob...))
}

// SaveConfigValue persists exactly one config key by patching the existing file,
// so the user's comments, ordering and unrelated tables survive. It falls back
// to a full rewrite only when the key is not present in mcli.toml.
func (db *DB) SaveConfigValue(key string) error {
	rawValue, err := db.Data.Config.RawValue(key)
	if err != nil {
		return err
	}
	if current, readErr := os.ReadFile(db.Path); readErr == nil {
		patched, ok, err := toml.SetKeyInSection(current, ConfigSection, key, rawValue)
		if err == nil && ok {
			return writeFileAtomic(db.Path, patched)
		}
	}
	return db.SaveConfig()
}

// SaveAll writes every generated document.
func (db *DB) SaveAll() error {
	if err := db.SaveConfig(); err != nil {
		return err
	}
	if err := db.Save(); err != nil {
		return err
	}
	return db.SaveTokens()
}

// ---------------------------------------------------------------------------
// last launch (its own tiny document)

// Last returns the previous launch, reading last.toml on first use.
func (db *DB) Last() (*LastLaunch, error) {
	if db.lastLoaded {
		return db.last, nil
	}
	db.lastLoaded = true
	raw, err := os.ReadFile(db.LastPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s", i18n.T("err.db.read", db.LastPath, err))
	}
	lt := &LastLaunch{}
	if _, err := toml.UnmarshalWithReport(raw, lt); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err.db.parse", db.LastPath, err))
	}
	if lt.At.IsZero() && lt.Instance == "" && lt.Version == "" {
		return nil, nil
	}
	db.last = lt
	return lt, nil
}

// SaveLast writes only last.toml — no configuration or state is touched.
func (db *DB) SaveLast(lt *LastLaunch) error {
	blob, err := toml.Marshal(lt)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.marshal", err))
	}
	header := "# the previous `mcli launch`, replayed by `mcli launch --last`.\n" +
		"# Small on purpose: replaying flags must not read mcli.toml or state.toml.\n\n"
	if err := writeFileAtomic(db.LastPath, append([]byte(header), blob...)); err != nil {
		return err
	}
	db.last = lt
	db.lastLoaded = true
	return nil
}

// ---------------------------------------------------------------------------
// helpers

func writeFileAtomic(path string, blob []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.write", path, err))
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%s", i18n.T("err.db.commit", path, err))
	}
	return nil
}

func configHeader() string {
	return "# mcli configuration — hand-editable, TOML.\n" +
		"# `mcli config set <key> <value>` rewrites a single line, so your comments,\n" +
		"# ordering and extra keys are kept. `mcli config list` names every key.\n\n"
}

func stateHeader() string {
	return "# mcli machine state — generated file (TOML).\n" +
		"# Accounts here are identity only; their tokens live in refresh.toml\n" +
		"# (long-lived) and session.toml (short-lived), both encrypted with a key\n" +
		"# derived from THIS machine. This file therefore carries no secrets and is\n" +
		"# portable; configuration lives in mcli.toml.\n\n"
}

func refreshHeader() string {
	return "# mcli refresh tokens — one entry per account, encrypted with a key\n" +
		"# derived from this machine plus that account's token_salt. Long-lived: this\n" +
		"# file is written when you log in, not when tokens are refreshed.\n" +
		"# Back it up like a password.\n\n"
}

func sessionHeader() string {
	return "# mcli session tokens — the short-lived Minecraft access tokens, with\n" +
		"# their expiry. Rewritten whenever a token is refreshed; safe to delete\n" +
		"# (the refresh token in refresh.toml will mint a new one).\n\n"
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
