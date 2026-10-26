package main

import (
	"fmt"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// update_cmd.go moves an instance to another game version — in practice, to a
// newer one. Everything that is not the game version stays where it is: the game
// directory, the memory, the arguments, the mods, the worlds.
//
// The old version's files are left alone. They are shared: another instance can
// run the same version, and deleting a shared jar because one instance moved on is
// how a launcher breaks a setup it was not asked about.

func (a *App) cmdUpdate(args []string) int {
	f, err := parseFlags("update", args, map[string]flagSpec{
		"version":        {takesValue: true},
		"loader":         {takesValue: true},
		"loader-version": {takesValue: true},
		"dry-run":        {},
		"force":          {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.update.usage"))
		return 0
	}

	// Which instance: the named one, or the one that was launched last.
	name := ""
	if len(f.args) > 0 {
		name = f.args[0]
	} else if last, err := a.Store.Last(); err == nil && last != nil && last.Instance != "" {
		name = last.Instance
		a.Log.Printf("%s", i18n.T("launch.no_target_last", name))
	}
	if name == "" {
		return a.usageError("cli.missing_arg", "update", "<instance>")
	}
	inst, ok := a.Store.GetInstance(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", name)))
	}

	dryRun := f.boolean("dry-run")
	spec, err := a.instanceSpecOf(inst, "", "")
	if err != nil {
		// The version the instance points at may not be installed at all (a fresh
		// instance, or a share wiped by hand). That is exactly when `update` is
		// wanted, so it is not fatal — it only means we cannot say what it ran.
		a.Log.Warnf("%s", i18n.T("upd.unknown_current", name, err))
	}
	a.Log.Printf("%s", i18n.T("upd.current", name, inst.Version, spec.Game))

	// --- the target ------------------------------------------------------
	asked := f.str("version", "")
	if asked == "" {
		asked = "latest"
	}
	target, err := a.resolveVersion(asked)
	if err != nil {
		return a.fatal(err)
	}
	if !f.boolean("force") {
		if man, err := a.ManifestStore().Get(false); err != nil {
			a.Log.Warnf("%s", err)
		} else if _, ok := man.Find(target); !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("inst.version_unknown", target)))
		}
	}

	// --- which loader the new version runs -------------------------------
	loader := modLoader(spec.Loader)
	if loader == "" {
		loader = loaderVanilla
	}
	if l := f.str("loader", ""); l != "" {
		loader, err = parseLoader(l)
		if err != nil {
			return a.usageError("loader.bad", l, loaderChoices())
		}
	}
	switch loader {
	case loaderVanilla:
	case loaderFabric:
		a.Log.Printf("%s", i18n.T("upd.loader_kept", inst.Name, string(loader)))
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("upd.loader_unsupported", string(loader), inst.Name)))
	}

	id := target
	if loader == loaderFabric {
		// The loader belongs to the version: the instance moves onto a profile for
		// the new game version, built by the same loader it already had.
		id, err = a.ensureLoaderTarget(versionRef{id: inst.Version, game: target}, loader, f.str("loader-version", ""))
		if err != nil {
			return a.fatal(err)
		}
		if id == "" {
			id = target
		}
	}

	if id == inst.Version {
		a.Log.Printf("%s", i18n.T("upd.same", inst.Name, id))
	}

	// --- install it -------------------------------------------------------
	v, err := a.Loader(dryRun).Lookup(id, "")
	if err != nil {
		return a.fatal(err)
	}
	eff := v.Effective()
	res, err := a.Installer(dryRun).Ensure(eff)
	if err != nil {
		return a.fatal(err)
	}
	if res.AlreadyPresent {
		a.Log.Printf("%s", i18n.T("upd.nothing", id))
	} else {
		a.Log.Printf("%s", i18n.T("upd.downloaded", res.Downloaded, humanBytes(res.MissingBytes)))
	}
	if dryRun {
		return 0
	}
	if v.ID != id {
		// mcli installs the effective version, and records the id it will launch —
		// which for a child version is the id that inherits, not the parent's.
		id = v.ID
	}
	if inst.Version != id {
		from := inst.Version
		inst.Version = id
		if err := a.Store.Save(); err != nil {
			return a.fatal(err)
		}
		a.Log.Printf("%s", i18n.T("upd.moved", inst.Name, from, id))
	}
	return 0
}
