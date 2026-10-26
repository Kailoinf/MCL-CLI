package main

import (
	"fmt"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/fabric"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// Mod loaders are a *choice*, not another positional: the command's positional
// argument stays the one thing it acts on (the instance name), and everything that
// modifies how it is created is a flag with a closed set of values. That is what
// GNU long options and clap's ValueEnum both describe, and what other launchers do
// — CMCL expresses the loader as `version --fabric`, Fabric's own installer as
// `-loader <version>`.
//
// Vanilla is a value of the same choice rather than the absence of the flag, so
// `--loader vanilla` can undo a loader: same flag, same place, no second syntax to
// remember.
type modLoader string

const (
	loaderVanilla modLoader = "vanilla"
	loaderFabric  modLoader = "fabric"
)

// modLoaders is the closed set, in the order the help lists it.
var modLoaders = []modLoader{loaderVanilla, loaderFabric}

// parseLoader validates the choice. An unknown value is an error that names the
// alternatives — never a silent fallback to vanilla.
func parseLoader(value string) (modLoader, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return loaderVanilla, nil
	}
	for _, l := range modLoaders {
		if string(l) == v {
			return l, nil
		}
	}
	return "", fmt.Errorf("%s", i18n.T("loader.bad", value, loaderChoices()))
}

// loaderChoices renders the choice list for messages and help.
func loaderChoices() string {
	names := make([]string, 0, len(modLoaders))
	for _, l := range modLoaders {
		names = append(names, string(l))
	}
	return strings.Join(names, ", ")
}

// applyLoader turns a game version plus a loader choice into the version id an
// instance should run, installing the loader profile when there is one to install.
// It returns an empty id when the choice is vanilla — the caller then keeps the
// game version as it is.
//
// loaderVersion pins a specific loader build; empty means the newest stable one.
func (a *App) applyLoader(gameVersion string, loader modLoader, loaderVersion string) (string, error) {
	switch loader {
	case loaderVanilla:
		return "", nil
	case loaderFabric:
		if gameVersion == "" {
			return "", fmt.Errorf("%s", i18n.T("loader.needs_game"))
		}
		if isLatest(loaderVersion) || loaderVersion == "" {
			entry, err := fabric.Latest(a.Policy(), gameVersion)
			if err != nil {
				return "", fmt.Errorf("%s", i18n.T("fab.no_loaders", gameVersion, err))
			}
			a.Log.Infof("%s", i18n.T("fab.latest_resolved", gameVersion, entry.Loader.Version))
			loaderVersion = entry.Loader.Version
		}
		a.Log.Printf("%s", i18n.T("fab.installing", gameVersion, loaderVersion))
		id, err := fabric.Install(a.Policy(), a.VersionsDir(), gameVersion, loaderVersion, a.Log)
		if err != nil {
			return "", err
		}
		a.Log.Printf("%s", i18n.T("fab.installed", id, id))
		return id, nil
	}
	return "", fmt.Errorf("%s", i18n.T("loader.bad", string(loader), loaderChoices()))
}

// baseGameVersion returns the game version a version id ultimately runs: a modded
// profile inherits from it, or was built for it when another launcher wrote the
// profile. `instance set --loader vanilla` uses this to go back.
func (a *App) baseGameVersion(versionID string) (string, error) {
	if versionID == "" {
		return "", nil
	}
	v, err := a.Loader(true).Lookup(versionID, "")
	if err != nil {
		return versionID, err
	}
	return v.GameVersion(), nil
}

// loaderOfVersionID reports the loader a version id belongs to, so `instance show`
// and `instance set` can talk about it. It is a guess by naming, and the only place
// mcli accepts one: the id is what Fabric itself calls its profile.
func loaderOfVersionID(versionID string) modLoader {
	if strings.HasPrefix(versionID, "fabric-loader-") {
		return loaderFabric
	}
	return loaderVanilla
}

// ensureLoaderTarget is the shared body of `create --loader` and
// `instance set --loader`: it works out which version id the instance should point
// at, given what it points at today.
func (a *App) ensureLoaderTarget(current versionRef, loader modLoader, loaderVersion string) (string, error) {
	switch loader {
	case loaderVanilla:
		// going back from a profile means running the game version it inherits
		if loaderOfVersionID(current.id) == loaderVanilla {
			return "", nil
		}
		game, err := a.baseGameVersion(current.id)
		if err != nil {
			return "", err
		}
		a.Log.Printf("%s", i18n.T("loader.back_to_vanilla", game))
		return game, nil
	case loaderFabric:
		game := current.game
		if game == "" {
			var err error
			game, err = a.baseGameVersion(current.id)
			if err != nil {
				return "", err
			}
		}
		return a.applyLoader(game, loaderFabric, loaderVersion)
	}
	return "", fmt.Errorf("%s", i18n.T("loader.bad", string(loader), loaderChoices()))
}

// versionRef is what a caller knows about the version an instance runs today.
type versionRef struct {
	id   string
	game string
}

// isLatest accepts the words a person actually types for "the newest one", and the
// empty string: `--loader fabric` without `--loader-version` means "newest stable",
// which is what everyone expects and what the flag documents.
func isLatest(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "latest", "stable", "newest", "auto":
		return true
	}
	return false
}

// loaderDiffers reports whether two loader versions name different builds. The
// comparison is on the version itself, not on the profile id it was written into,
// and a build suffix ("0.20.1+build.3") is not a difference: a pack that names a
// loader the instance does not have byte-for-byte is worth a word, not a refusal.
func loaderDiffers(a, b string) bool {
	base := func(s string) string { return strings.SplitN(strings.TrimSpace(s), "+", 2)[0] }
	return base(a) != base(b)
}
