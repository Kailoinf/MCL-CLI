package main

import (
	"fmt"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/fabric"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// cmdFabric installs and lists Fabric loader builds. mcli does not ship a loader
// of its own: it asks Fabric's version service for the profile JSON (which is a
// version JSON with inheritsFrom) and writes it where the version loader looks.
func (a *App) cmdFabric(args []string) int {
	if len(args) == 0 || args[0] == "help" || strings.HasPrefix(args[0], "-") {
		a.Log.Printf("%s", i18n.T("cli.cmd.fabric.usage"))
		return 0
	}
	switch args[0] {
	case "list", "versions", "ls":
		return a.fabricList(args[1:])
	case "install", "add":
		return a.fabricInstall(args[1:])
	}
	return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", "fabric "+args[0])))
}

// fabricList prints the loader builds published for one game version.
func (a *App) fabricList(args []string) int {
	f, err := parseFlags("fabric list", args, map[string]flagSpec{
		"limit": {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.fabric.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "fabric list", "<game version>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "fabric list")
	}
	game, err := a.resolveVersion(f.args[0])
	if err != nil {
		return a.fatal(err)
	}
	limit := 20
	if f.has("limit") {
		limit, err = f.int("limit", limit)
		if err != nil {
			return a.fatal(err)
		}
	}
	entries, err := fabric.Loaders(a.Policy(), game)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("fab.no_loaders", game, err)))
	}
	a.Log.Printf("%s", i18n.T("fab.list_header",
		padRight("LOADER", 14), padRight("STABLE", 8), padRight("INTERMEDIARY", 14), "MIN JAVA"))
	shown := 0
	for _, e := range entries {
		if shown == limit {
			break
		}
		stable := "-"
		if e.Loader.Stable {
			stable = i18n.T("fab.stable_yes")
		}
		a.Log.Printf("%s", i18n.T("fab.list_row",
			padRight(e.Loader.Version, 14), padRight(stable, 8),
			padRight(e.Intermediary.Version, 14), fmt.Sprintf("%d", e.LauncherMeta.MinJavaVersion)))
		shown++
	}
	a.Log.Printf("%s", i18n.T("fab.list_hint", shown, len(entries), game, game))
	return 0
}

// fabricInstall fetches one loader profile and puts it in place.
func (a *App) fabricInstall(args []string) int {
	f, err := parseFlags("fabric install", args, map[string]flagSpec{
		"loader": {takesValue: true},
		"yes":    {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.fabric.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "fabric install", "<game version>")
	}
	if len(f.args) > 2 {
		return a.usageError("cli.unexpected_arg", f.args[2], "fabric install")
	}
	game, err := a.resolveVersion(f.args[0])
	if err != nil {
		return a.fatal(err)
	}

	loader := f.str("loader", "")
	if loader == "" && len(f.args) == 2 {
		loader = f.args[1]
	}
	if loader == "" || isLatest(loader) {
		entry, err := fabric.Latest(a.Policy(), game)
		if err != nil {
			return a.fatal(fmt.Errorf("%s", i18n.T("fab.no_loaders", game, err)))
		}
		a.Log.Infof("%s", i18n.T("fab.latest_resolved", game, entry.Loader.Version))
		loader = entry.Loader.Version
	}

	a.Log.Printf("%s", i18n.T("fab.installing", game, loader))
	id, err := fabric.Install(a.Policy(), a.VersionsDir(), game, loader, a.Log)
	if err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("fab.installed", id, id))
	a.Log.Printf("%s", i18n.T("fab.next_hint", id))
	return 0
}

// isLatest accepts the words a person actually types for "the newest one".
func isLatest(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "latest", "stable", "newest", "auto":
		return true
	}
	return false
}
