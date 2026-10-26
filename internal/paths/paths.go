// Package paths resolves every location mcli reads or writes.
//
// Layout (defaults, all overridable through config or env MCLI_HOME):
//
//	~/.mcl-cli/            home      (env MCLI_HOME)
//	├─ mcli.toml           configuration (hand-editable, comments preserved)
//	├─ state.toml          machine state: accounts, instances, manifest pointer
//	├─ refresh.toml        long-lived refresh tokens, per account (0600)
//	├─ session.toml        short-lived Minecraft tokens + expiry (0600)
//	├─ last.toml           the previous launch, for `mcli launch --last`
//	├─ jdk/                java runtimes downloaded from the mirror
//	├─ cache/              version manifest cache
//	└─ logs/               mcli-*.log and game-*.log
//
//	~/.minecraft/          game data, identical to the official launcher
//	├─ libraries/          global, shared
//	├─ assets/             global, shared
//	└─ versions/{id}/      version json/jar/natives + per-instance game dirs
//
// The documents are split on purpose:
//   - mcli.toml is meant to be edited by hand; mcli only ever patches one key
//     in it, so comments and ordering survive.
//   - state.toml is machine-owned, holds no secrets and is therefore portable:
//     accounts (identity), instances and the manifest pointer live there.
//   - refresh.toml and session.toml hold the credentials, in two files because
//     a Minecraft token expires every few hours and is rewritten on every
//     refresh: keeping the long-lived refresh token out of that churn means the
//     file that matters is written once at login, not hourly.
//   - last.toml is tiny and separate so reading the previous launch arguments
//     (`launch --last`) does not require touching anything else.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// File names inside the mcli home.
const (
	ConfigFile  = "mcli.toml"
	StateFile   = "state.toml"
	RefreshFile = "refresh.toml"
	SessionFile = "session.toml"
	LastFile    = "last.toml"
)

// Paths holds every resolved directory for one mcli home.
type Paths struct {
	// Home is the .mcl-cli directory.
	Home string
}

// Resolve figures out the mcli home directory: $MCLI_HOME, else ~/.mcl-cli.
func Resolve() (Paths, error) {
	home := os.Getenv("MCLI_HOME")
	if home == "" {
		u, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("locate home directory: %w", err)
		}
		home = filepath.Join(u, ".mcl-cli")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, err
	}
	return Paths{Home: abs}, nil
}

// Config is the configuration file (TOML, hand-editable).
func (p Paths) Config() string { return filepath.Join(p.Home, ConfigFile) }

// State is the machine-owned state file (TOML, 0600).
func (p Paths) State() string { return filepath.Join(p.Home, StateFile) }

// Refresh is the file holding long-lived refresh tokens (TOML, 0600).
func (p Paths) Refresh() string { return filepath.Join(p.Home, RefreshFile) }

// Session is the file holding short-lived Minecraft tokens (TOML, 0600).
func (p Paths) Session() string { return filepath.Join(p.Home, SessionFile) }

// Last is the previous-launch file (TOML, 0600).
func (p Paths) Last() string { return filepath.Join(p.Home, LastFile) }

// JDKDir holds java runtimes installed by mcli.
func (p Paths) JDKDir() string { return filepath.Join(p.Home, "jdk") }

// CacheDir holds the version manifest and other cached metadata.
func (p Paths) CacheDir() string { return filepath.Join(p.Home, "cache") }

// ManifestFile is the cached version manifest (raw JSON as downloaded).
func (p Paths) ManifestFile() string {
	return filepath.Join(p.CacheDir(), "version_manifest_v2.json")
}

// LogsDir holds mcli and game log files.
func (p Paths) LogsDir() string { return filepath.Join(p.Home, "logs") }

// LogFile is today's log file.
func (p Paths) LogFile() string {
	return filepath.Join(p.LogsDir(), "mcli-"+time.Now().Format("20060102")+".log")
}

// Ensure creates the home directory tree (0755).
func (p Paths) Ensure() error {
	for _, d := range []string{p.Home, p.JDKDir(), p.CacheDir(), p.LogsDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}
