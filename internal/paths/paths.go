// Package paths resolves every location mcli reads or writes.
//
// Layout (defaults, all overridable through config or env):
//
//	~/.mcl-cli/            home      (env MCLI_HOME)
//	├─ mcli.db             config + accounts + instances (JSON)
//	├─ jdk/                java runtimes downloaded from the mirror
//	├─ cache/              version manifest cache
//	└─ logs/mcli-*.log     full logs
//
//	~/.minecraft/          game data, identical to the official launcher
//	├─ libraries/          global, shared
//	├─ assets/             global, shared
//	└─ versions/{id}/      version json/jar/natives + per-instance game dirs
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// DB is the config/account/instance database file.
func (p Paths) DB() string { return filepath.Join(p.Home, "mcli.db") }

// JDKDir holds java runtimes installed by mcli.
func (p Paths) JDKDir() string { return filepath.Join(p.Home, "jdk") }

// CacheDir holds the version manifest and other cached metadata.
func (p Paths) CacheDir() string { return filepath.Join(p.Home, "cache") }

// LogsDir holds mcli log files.
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
