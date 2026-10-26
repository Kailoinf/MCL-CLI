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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/config"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/secret"
	"github.com/Kailoinf/MCL-CLI/internal/toml"
)

// StateVersion is the shape of state.toml this build writes. There is no
// migration path and none is wanted: mcli has no users on an older shape, so the
// next save simply writes the current one. A file written by a *newer* mcli keeps
// its number, and the CLI warns about it instead of garbling it.
const StateVersion = 1

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

// Server is a server-side install: one version's server jar, a directory of its
// own, and the memory and port it runs with. It has no account and no assets.
type Server struct {
	Name    string   `toml:"name" json:"name"`
	Version string   `toml:"version" json:"version"`
	Dir     string   `toml:"dir,omitempty" json:"dir,omitempty"` // empty = <minecraft>/servers/<name>
	Java    string   `toml:"java,omitempty" json:"java,omitempty"`
	Xmx     string   `toml:"xmx,omitempty" json:"xmx,omitempty"`
	Xms     string   `toml:"xms,omitempty" json:"xms,omitempty"`
	Port    int      `toml:"port,omitempty" json:"port,omitempty"`
	JvmArgs []string `toml:"jvm_args,omitempty" json:"jvm_args,omitempty"`
	Eula    bool     `toml:"eula" json:"eula"`
	// Pid is the running server's process id, 0 when it is not up. `server stop`
	// uses it, and it is cleared as soon as the process is gone.
	Pid       int       `toml:"pid,omitempty" json:"pid,omitempty"`
	CreatedAt time.Time `toml:"created_at" json:"created_at"`
	StartedAt time.Time `toml:"last_started,omitempty" json:"last_started,omitempty"`
}

// ManifestCache points at the cached version manifest body.
type ManifestCache struct {
	Source    string    `toml:"source" json:"source"` // "bmclapi" or "mojang"
	FetchedAt time.Time `toml:"fetched_at" json:"fetched_at"`
	File      string    `toml:"file" json:"file"` // absolute path of the cached JSON
}

// stateFile is the on-disk shape of state.toml. It holds identity only: every
// credential lives in refresh.toml / session.toml.
type stateFile struct {
	StateVersion int                  `toml:"state_version"`
	Accounts     map[string]*Account  `toml:"accounts"`
	Instances    map[string]*Instance `toml:"instances"`
	Servers      map[string]*Server   `toml:"servers,omitempty"`
	Manifest     *ManifestCache       `toml:"manifest_cache,omitempty"`
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
// The previous launch is not part of it — see Store.Last / Store.SaveLast.
type Data struct {
	StateVersion int
	Config       config.Config
	Accounts     map[string]*Account
	// Refresh and Session are keyed by account UUID.
	Refresh   map[string]*TokenRecord
	Session   map[string]*SessionRecord
	Instances map[string]*Instance
	Servers   map[string]*Server
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

// Store is an open set of documents.
type Store struct {
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

	last       *LastLaunch
	lastLoaded bool
	keys       map[string][]byte
}

// Open loads every document of one mcli home, creating the missing ones. There is
// exactly one shape on disk; nothing older is read.
func Open(f Files) (*Store, error) {
	configPath, statePath, lastPath := f.Config, f.State, f.Last
	s := &Store{
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
			Servers:   map[string]*Server{},
		},
	}

	// --- configuration ---------------------------------------------------
	cfgRaw, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		file := configFile{Config: config.Default()}
		unknown, err := toml.UnmarshalWithReport(cfgRaw, &file)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("err.state.parse", configPath, err))
		}
		s.UnknownKeys = append(s.UnknownKeys, labelLeft(configPath, unknown)...)
		s.Data.Config = file.Config
	case os.IsNotExist(err):
		// a fresh home: the defaults are already in place
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.state.read", configPath, err))
	}

	// --- state -----------------------------------------------------------
	stateRaw, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		file := stateFile{}
		unknown, err := toml.UnmarshalWithReport(stateRaw, &file)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("err.state.parse", statePath, err))
		}
		s.UnknownKeys = append(s.UnknownKeys, labelLeft(statePath, unknown)...)
		s.Data.StateVersion = file.StateVersion
		if file.Accounts != nil {
			s.Data.Accounts = file.Accounts
		}
		if file.Instances != nil {
			s.Data.Instances = file.Instances
		}
		if file.Servers != nil {
			s.Data.Servers = file.Servers
		}
		s.Data.Manifest = file.Manifest
	case os.IsNotExist(err):
		// a fresh home
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.state.read", statePath, err))
	}

	// --- credentials -----------------------------------------------------
	if err := s.loadTokens(); err != nil {
		return nil, err
	}

	d := s.Data
	d.Config.FillMissing()
	if d.Instances == nil {
		d.Instances = map[string]*Instance{}
	}
	if d.Servers == nil {
		d.Servers = map[string]*Server{}
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
	// A file this build does not know the shape of keeps its number, so the CLI
	// can warn about it instead of garbling it. Anything else is this build's.
	if d.StateVersion == 0 {
		d.StateVersion = StateVersion
	}
	return s, nil
}

// loadTokens reads refresh.toml and session.toml. Missing files are the normal
// case before the first login.
func (s *Store) loadTokens() error {
	if s.RefreshPath != "" {
		if raw, err := os.ReadFile(s.RefreshPath); err == nil {
			file := refreshFile{}
			unknown, err := toml.UnmarshalWithReport(raw, &file)
			if err != nil {
				return fmt.Errorf("%s", i18n.T("err.state.parse", s.RefreshPath, err))
			}
			s.UnknownKeys = append(s.UnknownKeys, labelLeft(s.RefreshPath, unknown)...)
			if file.Tokens != nil {
				s.Data.Refresh = file.Tokens
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s", i18n.T("err.state.read", s.RefreshPath, err))
		}
	}
	if s.SessionPath != "" {
		if raw, err := os.ReadFile(s.SessionPath); err == nil {
			file := sessionFile{}
			unknown, err := toml.UnmarshalWithReport(raw, &file)
			if err != nil {
				return fmt.Errorf("%s", i18n.T("err.state.parse", s.SessionPath, err))
			}
			s.UnknownKeys = append(s.UnknownKeys, labelLeft(s.SessionPath, unknown)...)
			if file.Sessions != nil {
				s.Data.Session = file.Sessions
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s", i18n.T("err.state.read", s.SessionPath, err))
		}
	}
	return nil
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
func (s *Store) Tokens(uuid string) (*TokenRecord, *SessionRecord) {
	if s.Data.Refresh == nil {
		s.Data.Refresh = map[string]*TokenRecord{}
	}
	if s.Data.Session == nil {
		s.Data.Session = map[string]*SessionRecord{}
	}
	rec, ok := s.Data.Refresh[uuid]
	if !ok || rec == nil {
		rec = &TokenRecord{}
		s.Data.Refresh[uuid] = rec
	}
	sess, ok := s.Data.Session[uuid]
	if !ok || sess == nil {
		sess = &SessionRecord{}
		s.Data.Session[uuid] = sess
	}
	return rec, sess
}

// SetRefreshToken stores one account's long-lived credential material.
func (s *Store) SetRefreshToken(uuid, salt, enc string) {
	rec, _ := s.Tokens(uuid)
	rec.Salt = salt
	rec.RefreshEnc = enc
}

// SetSessionToken stores one account's short-lived Minecraft token.
func (s *Store) SetSessionToken(uuid, enc string, exp time.Time) {
	_, sess := s.Tokens(uuid)
	sess.MCTokenEnc = enc
	sess.MCTokenExp = exp
}

// DropAccount removes an account and every trace of its credentials.
func (s *Store) DropAccount(uuid string) {
	delete(s.Data.Accounts, uuid)
	delete(s.Data.Refresh, uuid)
	delete(s.Data.Session, uuid)
	delete(s.keys, uuid)
}

// SetAccountSalt gives an account a fresh salt when it has none (first login).
func (s *Store) SetAccountSalt(acc *Account) error {
	rec, _ := s.Tokens(acc.UUID)
	if rec.Salt != "" {
		return nil
	}
	salt, err := secret.NewSalt()
	if err != nil {
		return err
	}
	rec.Salt = base64.StdEncoding.EncodeToString(salt)
	delete(s.keys, acc.UUID)
	return nil
}

// AccountKey derives (and caches) the AES key protecting one account's tokens.
func (s *Store) AccountKey(acc *Account) ([]byte, error) {
	if acc == nil {
		return nil, fmt.Errorf("%s", i18n.T("err.secret.nosalt"))
	}
	if k, ok := s.keys[acc.UUID]; ok {
		return k, nil
	}
	rec := s.Data.Refresh[acc.UUID]
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
	s.keys[acc.UUID] = k
	return k, nil
}

// Save writes state.toml atomically with 0600 permissions. Credentials are not
// part of it: use SaveRefresh / SaveSession for those.
func (s *Store) Save() error {
	if err := s.Data.Config.Validate(); err != nil {
		return err
	}
	state := stateFile{
		StateVersion: StateVersion,
		Accounts:     s.Data.Accounts,
		Instances:    s.Data.Instances,
		Servers:      s.Data.Servers,
		Manifest:     s.Data.Manifest,
	}
	blob, err := toml.Marshal(state)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.marshal", err))
	}
	// The credential documents are created lazily by their own savers, but a
	// state write should not leave the home looking half-built.
	if _, err := os.Stat(s.RefreshPath); os.IsNotExist(err) && s.RefreshPath != "" {
		if err := s.SaveRefresh(); err != nil {
			return err
		}
	}
	return writeFileAtomic(s.StatePath, append([]byte(stateHeader()), blob...))
}

// SaveRefresh writes refresh.toml (0600): the long-lived credentials.
func (s *Store) SaveRefresh() error {
	if s.RefreshPath == "" {
		return nil
	}
	// Accounts that never stored a token are simply absent from the file: an
	// empty entry would only be noise a reader has to explain.
	tokens := map[string]*TokenRecord{}
	for uuid, rec := range s.Data.Refresh {
		if rec == nil || (rec.RefreshEnc == "" && rec.Salt == "") {
			continue
		}
		tokens[uuid] = rec
	}
	blob, err := toml.Marshal(refreshFile{Tokens: tokens})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.marshal", err))
	}
	return writeFileAtomic(s.RefreshPath, append([]byte(refreshHeader()), bytes.TrimLeft(blob, "\n")...))
}

// SaveSession writes session.toml (0600): the short-lived credentials. This is
// the file that gets rewritten whenever a token is refreshed.
func (s *Store) SaveSession() error {
	if s.SessionPath == "" {
		return nil
	}
	sessions := map[string]*SessionRecord{}
	for uuid, rec := range s.Data.Session {
		if rec == nil || rec.MCTokenEnc == "" {
			continue
		}
		sessions[uuid] = rec
	}
	blob, err := toml.Marshal(sessionFile{Sessions: sessions})
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.marshal", err))
	}
	return writeFileAtomic(s.SessionPath, append([]byte(sessionHeader()), bytes.TrimLeft(blob, "\n")...))
}

// SaveTokens writes both credential documents.
func (s *Store) SaveTokens() error {
	if err := s.SaveRefresh(); err != nil {
		return err
	}
	return s.SaveSession()
}

// SaveConfig regenerates mcli.toml from scratch (comments included).
func (s *Store) SaveConfig() error {
	if err := s.Data.Config.Validate(); err != nil {
		return err
	}
	file := configFile{Config: s.Data.Config}
	blob, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.marshal", err))
	}
	return writeFileAtomic(s.Path, append([]byte(configHeader()), blob...))
}

// SaveConfigValue persists exactly one config key by patching the existing file,
// so the user's comments, ordering and unrelated tables survive. It falls back
// to a full rewrite only when the key is not present in mcli.toml.
func (s *Store) SaveConfigValue(key string) error {
	rawValue, err := s.Data.Config.RawValue(key)
	if err != nil {
		return err
	}
	if current, readErr := os.ReadFile(s.Path); readErr == nil {
		patched, ok, err := toml.SetKeyInSection(current, ConfigSection, key, rawValue)
		if err == nil && ok {
			return writeFileAtomic(s.Path, patched)
		}
	}
	return s.SaveConfig()
}

// SaveAll writes every generated document.
func (s *Store) SaveAll() error {
	if err := s.SaveConfig(); err != nil {
		return err
	}
	if err := s.Save(); err != nil {
		return err
	}
	return s.SaveTokens()
}

// ---------------------------------------------------------------------------
// last launch (its own tiny document)

// Last returns the previous launch, reading last.toml on first use.
func (s *Store) Last() (*LastLaunch, error) {
	if s.lastLoaded {
		return s.last, nil
	}
	s.lastLoaded = true
	raw, err := os.ReadFile(s.LastPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s", i18n.T("err.state.read", s.LastPath, err))
	}
	lt := &LastLaunch{}
	if _, err := toml.UnmarshalWithReport(raw, lt); err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err.state.parse", s.LastPath, err))
	}
	if lt.At.IsZero() && lt.Instance == "" && lt.Version == "" {
		return nil, nil
	}
	s.last = lt
	return lt, nil
}

// SaveLast writes only last.toml — no configuration or state is touched.
func (s *Store) SaveLast(lt *LastLaunch) error {
	blob, err := toml.Marshal(lt)
	if err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.marshal", err))
	}
	header := "# the previous `mcli launch`, replayed by `mcli launch --last`.\n" +
		"# Small on purpose: replaying flags must not read mcli.toml or state.toml.\n\n"
	if err := writeFileAtomic(s.LastPath, append([]byte(header), blob...)); err != nil {
		return err
	}
	s.last = lt
	s.lastLoaded = true
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
		return fmt.Errorf("%s", i18n.T("err.state.write", path, err))
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("%s", i18n.T("err.state.commit", path, err))
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
func (s *Store) Cfg() *config.Config { return &s.Data.Config }

// ---------------------------------------------------------------------------
// accounts

// ActiveAccount returns the currently selected account, if any.
func (s *Store) ActiveAccount() *Account {
	for _, a := range s.Data.Accounts {
		if a.Active {
			return a
		}
	}
	return nil
}

// SetActive makes one account the active one and clears the flag elsewhere.
func (s *Store) SetActive(uuid string) {
	for k, a := range s.Data.Accounts {
		a.Active = k == uuid
	}
}

// AccountNames returns account keys sorted by name.
func (s *Store) AccountNames() []string { return sortedKeys(s.Data.Accounts) }

// ---------------------------------------------------------------------------
// instances

// GetInstance looks up an instance by name.
func (s *Store) GetInstance(name string) (*Instance, bool) {
	i, ok := s.Data.Instances[name]
	return i, ok
}

// InstanceNames returns instance names, most recently played first, then
// alphabetical.
func (s *Store) InstanceNames() []string {
	names := sortedKeys(s.Data.Instances)
	sort.SliceStable(names, func(i, j int) bool {
		a, b := s.Data.Instances[names[i]], s.Data.Instances[names[j]]
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

// GetServer returns a registered server, if it exists.
func (s *Store) GetServer(name string) (*Server, bool) {
	srv, ok := s.Data.Servers[name]
	return srv, ok
}

// ServerNames lists registered servers, most recently started first.
func (s *Store) ServerNames() []string {
	names := sortedKeys(s.Data.Servers)
	sort.SliceStable(names, func(i, j int) bool {
		a, b := s.Data.Servers[names[i]], s.Data.Servers[names[j]]
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.After(b.StartedAt)
		}
		return names[i] < names[j]
	})
	return names
}
