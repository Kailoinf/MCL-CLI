package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/launch"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// ---------------------------------------------------------------------------
// mcli instance [list|show|set|copy|rename]
//
// Instances are created by `mcli create`, but a launcher has to let the user
// revisit those choices later: memory, a pinned java, extra game arguments or a
// different version. `mcli instance set` is that door, and it never silently
// drops anything the user did not mention.

func (a *App) cmdInstance(args []string) int {
	if len(args) == 0 {
		return a.instanceList()
	}
	action, rest := args[0], args[1:]
	switch action {
	case "list", "ls", "--list":
		return a.instanceList()
	case "show", "info", "get":
		return a.instanceShow(rest)
	case "set", "edit", "config":
		return a.instanceSet(rest)
	case "copy", "clone", "cp":
		return a.instanceCopy(rest)
	case "rename", "mv":
		return a.instanceRename(rest)
	case "scan", "ls-versions", "available":
		return a.instanceScan(rest)
	case "import", "adopt", "take":
		return a.instanceImport(rest)
	case "help", "--help":
		a.Log.Printf("%s", i18n.T("cli.cmd.instance.usage"))
		return 0
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", action)))
	}
}

func (a *App) instanceList() int {
	names := a.Store.InstanceNames()
	if len(names) == 0 {
		a.Log.Printf("%s", i18n.T("inst.no_instances"))
		// An empty list next to a full .minecraft is the confusing case: say what
		// is there and how to adopt it, instead of looking like nothing was found.
		a.reportUnregistered(nil)
		return 0
	}
	a.Log.Printf("%s", i18n.T("inst.list_header",
		padRight("NAME", 20), padRight("VERSION", 20), padRight("JAVA", 12),
		padRight("STATE", 11), padRight("LAST PLAYED", 20), "GAME DIR"))
	for _, n := range names {
		inst := a.Store.Data.Instances[n]
		jv := inst.Java
		if jv == "" {
			jv = i18n.T("inst.auto")
		}
		played := "-"
		if !inst.LastPlayed.IsZero() {
			played = inst.LastPlayed.Format("2006-01-02 15:04")
		}
		version := inst.Version
		if version == "" {
			version = "-"
		}
		a.Log.Printf("%s", i18n.T("inst.list_row",
			padRight(inst.Name, 20), padRight(version, 20), padRight(jv, 12),
			padRight(a.instanceState(n), 11), padRight(played, 20), a.instanceDir(n)))
	}
	a.reportUnregistered(names)
	return 0
}

// reportUnregistered nudges the user about version directories that exist on
// disk (an official-launcher or PCL install, a modded profile) but are not
// registered as instances yet. Without this, mcli looks empty right next to a
// full .minecraft.
func (a *App) reportUnregistered(registered []string) {
	disk, err := a.scanDisk()
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, n := range registered {
		known[n] = true
		if inst, ok := a.Store.GetInstance(n); ok {
			known[inst.Version] = true
		}
	}
	n := 0
	for _, d := range disk {
		if !known[d.ID] && d.HasJSON {
			n++
		}
	}
	if n > 0 {
		a.Log.Printf("%s", i18n.T("inst.external_hint", n, a.VersionsDir()))
	}
}

// instanceState gives a cheap readiness estimate that never touches the
// network: the version JSON on disk decides "missing", the client jar and the
// asset index decide "incomplete". `mcli doctor <name>` is the precise answer.
func (a *App) instanceState(name string) string {
	inst, ok := a.Store.GetInstance(name)
	if !ok || inst.Version == "" {
		return i18n.T("inst.state.missing")
	}
	raw, err := os.ReadFile(filepath.Join(a.VersionsDir(), inst.Version, inst.Version+".json"))
	if err != nil {
		return i18n.T("inst.state.missing")
	}
	v := &mcv.Version{}
	if err := json.Unmarshal(raw, v); err != nil {
		return i18n.T("inst.state.missing")
	}
	if _, err := os.Stat(filepath.Join(a.VersionsDir(), inst.Version, inst.Version+".jar")); err != nil {
		return i18n.T("inst.state.incomplete")
	}
	if v.AssetIndex != nil && v.AssetIndex.URL != "" {
		index := filepath.Join(a.MinecraftDir(), "assets", "indexes", v.AssetIndexName())
		if _, err := os.Stat(index); err != nil {
			return i18n.T("inst.state.incomplete")
		}
	}
	return i18n.T("inst.state.ready")
}

// ---------------------------------------------------------------------------
// show

func (a *App) instanceShow(args []string) int {
	f, err := parseFlags("instance show", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.instance.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "instance show", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "instance show")
	}
	name := f.args[0]
	inst, ok := a.Store.GetInstance(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", name)))
	}
	dir := a.instanceDir(name)

	row := func(label, value string) {
		a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T(label), 14), value))
	}
	row("inst.field.name", inst.Name)
	row("inst.field.version", dash(inst.Version))
	row("inst.field.created", timeOrDash(inst.CreatedAt))
	row("inst.field.last_played", timeOrDash(inst.LastPlayed))
	row("inst.field.java", orAuto(inst.Java))
	row("inst.field.memory", memoryLine(inst))
	row("inst.field.jvm_args", argLine(inst.JvmArgs))
	row("inst.field.game_args", argLine(inst.GameArgs))
	if inst.Pack != "" {
		// Which pack an instance was built from is part of what it is, so it
		// belongs in `instance show` rather than only in the state file.
		row("inst.field.pack", packLine(inst))
	}
	row("inst.field.files", a.instanceState(name))
	row("inst.field.size", dirSize(dir))
	row("inst.field.game_dir", dir)
	return 0
}

// memoryLine renders whichever heap flags are actually configured; an instance
// that sets neither really does use the automatic values, so it says "auto".
func memoryLine(inst *store.Instance) string {
	var parts []string
	if inst.Xmx != "" {
		parts = append(parts, "-Xmx"+inst.Xmx)
	}
	if inst.Xms != "" {
		parts = append(parts, "-Xms"+inst.Xms)
	}
	if len(parts) == 0 {
		return i18n.T("inst.auto")
	}
	return strings.Join(parts, " ")
}

func argLine(args []string) string {
	if len(args) == 0 {
		return i18n.T("common.none")
	}
	return strings.Join(args, " ")
}

func orAuto(v string) string {
	if v == "" {
		return i18n.T("inst.auto")
	}
	return v
}

func dash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// timeOrDash keeps an unset time honest instead of printing 0001-01-01.
func timeOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// dirSize sums a directory tree; it returns "-" when the path does not exist.
func dirSize(dir string) string {
	var total int64
	seen := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			seen = true
		}
		return nil
	})
	if !seen {
		return "-"
	}
	return humanBytes(total)
}

// ---------------------------------------------------------------------------
// set

func (a *App) instanceSet(args []string) int {
	f, err := parseFlags("instance set", args, map[string]flagSpec{
		"version":         {takesValue: true},
		"force":           {},
		"java":            {takesValue: true},
		"xmx":             {takesValue: true},
		"xms":             {takesValue: true},
		"jvm-arg":         {takesValue: true, list: true},
		"game-arg":        {takesValue: true, list: true},
		"clear-jvm-args":  {},
		"clear-game-args": {},
		"loader":          {takesValue: true},
		"loader-version":  {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("inst.set_usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "instance set", "<name> [options]")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "instance set")
	}
	name := f.args[0]
	inst, ok := a.Store.GetInstance(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", name)))
	}

	var changed []string

	if f.has("version") {
		version, err := a.resolveVersion(f.str("version", ""))
		if err != nil {
			return a.fatal(err)
		}
		if version == "" {
			return a.usageError("cli.missing_arg", "instance set", "--version <version>")
		}
		if !f.boolean("force") {
			if err := a.knownVersion(version); err != nil {
				return a.fatal(err)
			}
		}
		inst.Version = version
		changed = append(changed, "version="+version)
	}
	// --loader is the same choice create takes: it is how an existing instance
	// becomes moddable (or stops being), without touching anything else.
	if f.has("loader") {
		loader, err := parseLoader(f.str("loader", ""))
		if err != nil {
			return a.usageError("loader.bad", f.str("loader", ""), loaderChoices())
		}
		// Whether the instance is modded is asked of the version JSON, not of the
		// way its id is spelled: a profile another launcher wrote is named its own
		// way and still runs a loader.
		spec, err := a.instanceSpecOf(inst, "", "")
		if err != nil {
			return a.fatal(err)
		}
		target, err := a.ensureLoaderTarget(versionRef{id: inst.Version, game: spec.Game}, loader, f.str("loader-version", ""))
		if err != nil {
			return a.fatal(err)
		}
		if target != "" {
			inst.Version = target
			changed = append(changed, "version="+target)
		} else if loader == loaderVanilla && spec.Loader != "" {
			// ensureLoaderTarget returned the game version it inherits
			game, err := a.baseGameVersion(inst.Version)
			if err != nil {
				return a.fatal(err)
			}
			inst.Version = game
			changed = append(changed, "version="+game)
		}
	}
	if f.has("java") {
		value := autoValue(f.str("java", ""))
		if value != "" {
			if _, err := java.Probe(value, "config"); err != nil {
				return a.fatal(fmt.Errorf("%s", i18n.T("java.pinned", value)))
			}
		}
		inst.Java = value
		changed = append(changed, "java="+orAuto(value))
	}
	if f.has("xmx") {
		value := autoValue(f.str("xmx", ""))
		if err := launch.ValidateMemory(value); err != nil {
			return a.fatal(err)
		}
		inst.Xmx = value
		changed = append(changed, "xmx="+orAuto(value))
	}
	if f.has("xms") {
		value := autoValue(f.str("xms", ""))
		if err := launch.ValidateMemory(value); err != nil {
			return a.fatal(err)
		}
		inst.Xms = value
		changed = append(changed, "xms="+orAuto(value))
	}
	jvmChanged := false
	if f.boolean("clear-jvm-args") {
		inst.JvmArgs = nil
		jvmChanged = true
	}
	if len(f.lists["jvm-arg"]) > 0 {
		inst.JvmArgs = append(inst.JvmArgs, f.lists["jvm-arg"]...)
		jvmChanged = true
	}
	if jvmChanged {
		changed = append(changed, "jvm_args="+argLine(inst.JvmArgs))
	}
	gameChanged := false
	if f.boolean("clear-game-args") {
		inst.GameArgs = nil
		gameChanged = true
	}
	if len(f.lists["game-arg"]) > 0 {
		inst.GameArgs = append(inst.GameArgs, f.lists["game-arg"]...)
		gameChanged = true
	}
	if gameChanged {
		changed = append(changed, "game_args="+argLine(inst.GameArgs))
	}

	if len(changed) == 0 {
		// Two different situations, told apart on purpose: no options at all (show
		// the usage) versus options that add up to no change (do not pretend the
		// command was mistyped).
		if f.any() {
			a.Log.Printf("%s", i18n.T("inst.set_same", name))
		} else {
			a.Log.Printf("%s", i18n.T("inst.set_nothing"))
			a.Log.Printf("%s", i18n.T("inst.set_usage"))
		}
		return 0
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("inst.updated", name, strings.Join(changed, " ")))
	a.Log.Printf("%s", i18n.T("inst.game_dir", a.instanceDir(name)))
	return 0
}

// autoValue maps an empty value or the literal "auto" onto "use the default".
func autoValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "auto") {
		return ""
	}
	return v
}

// ---------------------------------------------------------------------------
// copy

func (a *App) instanceCopy(args []string) int {
	f, err := parseFlags("instance copy", args, map[string]flagSpec{
		"with-files": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.instance.usage"))
		return 0
	}
	if len(f.args) < 2 {
		return a.usageError("cli.missing_arg", "instance copy", "<source> <target> [--with-files]")
	}
	if len(f.args) > 2 {
		return a.usageError("cli.unexpected_arg", f.args[2], "instance copy")
	}
	src, dst := f.args[0], f.args[1]
	from, ok := a.Store.GetInstance(src)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", src)))
	}
	if err := validateInstanceName(dst); err != nil {
		return a.fatal(err)
	}
	if _, exists := a.Store.GetInstance(dst); exists {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.exists", dst)))
	}

	copied := &store.Instance{
		Name:      dst,
		Version:   from.Version,
		CreatedAt: time.Now(),
		Java:      from.Java,
		Xmx:       from.Xmx,
		Xms:       from.Xms,
		JvmArgs:   append([]string{}, from.JvmArgs...),
		GameArgs:  append([]string{}, from.GameArgs...),
	}
	a.Store.Data.Instances[dst] = copied

	files, bytes, copiedFiles := 0, int64(0), false
	if f.boolean("with-files") {
		srcDir, dstDir := a.instanceDir(src), a.instanceDir(dst)
		if _, err := os.Stat(srcDir); err == nil {
			if err := os.MkdirAll(dstDir, 0o755); err != nil {
				return a.fatal(err)
			}
			// os.CopyFS is stdlib (Go 1.23+) and refuses to overwrite, which is
			// exactly the guarantee we want for a save directory.
			if err := os.CopyFS(dstDir, os.DirFS(srcDir)); err != nil {
				return a.fatal(fmt.Errorf("%s", i18n.T("inst.copy_failed", srcDir, err)))
			}
			files, bytes = countTree(dstDir)
			copiedFiles = true
		} else {
			a.Log.Warnf("%s", i18n.T("inst.copy_no_files", srcDir))
		}
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("inst.copy_ok", src, dst))
	if copiedFiles {
		a.Log.Printf("%s", i18n.T("inst.copy_files", files, humanBytes(bytes)))
	}
	a.Log.Printf("%s", i18n.T("inst.game_dir", a.instanceDir(dst)))
	return 0
}

func countTree(dir string) (files int, bytes int64) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes
}

// ---------------------------------------------------------------------------
// rename

func (a *App) instanceRename(args []string) int {
	f, err := parseFlags("instance rename", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.instance.usage"))
		return 0
	}
	if len(f.args) < 2 {
		return a.usageError("cli.missing_arg", "instance rename", "<old> <new>")
	}
	if len(f.args) > 2 {
		return a.usageError("cli.unexpected_arg", f.args[2], "instance rename")
	}
	oldName, newName := f.args[0], f.args[1]
	inst, ok := a.Store.GetInstance(oldName)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", oldName)))
	}
	if err := validateInstanceName(newName); err != nil {
		return a.fatal(err)
	}
	if _, exists := a.Store.GetInstance(newName); exists {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.exists", newName)))
	}
	if oldName == newName {
		a.Log.Printf("%s", i18n.T("inst.rename_same", oldName))
		return 0
	}

	inst.Name = newName
	delete(a.Store.Data.Instances, oldName)
	a.Store.Data.Instances[newName] = inst

	// move the game directory (saves, options, resource packs) with it
	oldDir, newDir := a.instanceDir(oldName), a.instanceDir(newName)
	if _, err := os.Stat(oldDir); err == nil {
		if _, err := os.Stat(newDir); err == nil {
			return a.fatal(fmt.Errorf("%s", i18n.T("inst.rename_dir_busy", newDir)))
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			return a.fatal(fmt.Errorf("%s", i18n.T("inst.rename_dir_failed", err)))
		}
	}
	// keep `launch --last` pointing at the instance the user knows
	if last, err := a.Store.Last(); err == nil && last != nil && last.Instance == oldName {
		last.Instance = newName
		if err := a.Store.SaveLast(last); err != nil {
			a.Log.Warnf("%s", err)
		}
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("inst.rename_ok", oldName, newName))
	a.Log.Printf("%s", i18n.T("inst.game_dir", newDir))
	return 0
}

// packLine describes the modpack an instance was built from, with its version.
func packLine(inst *store.Instance) string {
	if inst.Pack == "" {
		return "-"
	}
	version := inst.PackNumber
	if version == "" {
		version = inst.PackVersion
	}
	if version == "" {
		return inst.Pack
	}
	return inst.Pack + " " + version
}
