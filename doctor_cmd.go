package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

// ---------------------------------------------------------------------------
// mcli doctor [<instance>] [--all] [--deep]
// mcli repair <instance> [--deep] [--force] [--dry-run]
//
// doctor answers "what is wrong with this machine / this instance", and repair
// fixes exactly what doctor found. Both work from the same mcv.Plan, so they can
// never disagree about which files a version needs.

func (a *App) cmdDoctor(args []string) int {
	f, err := parseFlags("doctor", args, map[string]flagSpec{
		"all":  {},
		"deep": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.doctor.usage"))
		return 0
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "doctor")
	}

	problems := a.doctorEnvironment()
	switch {
	case len(f.args) == 1:
		problems += a.doctorInstance(f.args[0], f.boolean("deep"))
	case f.boolean("all"):
		for _, name := range a.DB.InstanceNames() {
			problems += a.doctorInstance(name, f.boolean("deep"))
		}
	default:
		if n := len(a.DB.InstanceNames()); n > 0 {
			a.Log.Printf("%s", i18n.T("doc.hint_instance", n))
		}
	}
	if problems > 0 {
		a.Log.Printf("%s", i18n.T("doc.problems_total", problems))
		return 1
	}
	a.Log.Printf("%s", i18n.T("doc.all_good"))
	return 0
}

// doctorEnvironment reports the machine, not a single version. It returns the
// number of problems found.
func (a *App) doctorEnvironment() int {
	problems := 0
	a.Log.Printf("%s", i18n.T("doc.env_header"))
	a.Log.Printf("%s", i18n.T("doc.version_line", Version, a.DB.Data.DBVersion))
	for _, line := range []struct {
		key  string
		path string
	}{
		{"cli.mcl_home", a.Paths.Home},
		{"cli.config_path", a.DB.Path},
		{"cli.state_path", a.DB.StatePath},
		{"cli.refresh_path", a.DB.RefreshPath},
		{"cli.session_path", a.DB.SessionPath},
		{"cli.last_path", a.DB.LastPath},
		{"cli.minecraft_dir", a.MinecraftDir()},
	} {
		a.Log.Printf("%s", i18n.T(line.key, line.path))
	}
	if free, ok := freeBytes(a.MinecraftDir()); ok {
		a.Log.Printf("%s", i18n.T("doc.disk_free", a.MinecraftDir(), humanBytes(int64(free))))
	}
	a.Log.Printf("%s", i18n.T("auth.summary", a.accountSummary()))

	// java: what is installed and what the newest recorded launch wanted
	runtimes := java.ScanRuntimes(a.DB.Cfg(), a.Paths, a.Log)
	if len(runtimes) == 0 {
		a.Log.Printf("%s", i18n.T("java.none_installed"))
		problems++
	} else {
		a.Log.Printf("%s", i18n.T("doc.java_header", len(runtimes)))
		a.Log.Printf("%s", i18n.T("java.list_header", "MAJOR", "VERSION", "SOURCE", "PATH"))
		for _, rt := range runtimes {
			a.Log.Printf("%s", i18n.T("java.list_row",
				fmt.Sprintf("%d", rt.Major), rt.Version, rt.Source, rt.Path))
		}
	}

	// manifest cache
	if cache := a.DB.Data.Manifest; cache != nil {
		age := time.Since(cache.FetchedAt).Round(time.Minute)
		versions := 0
		if man, err := a.ManifestStore().Get(false); err == nil {
			versions = len(man.Versions)
		}
		a.Log.Printf("%s", i18n.T("doc.manifest_line", cache.Source, age, versions))
	} else {
		a.Log.Printf("%s", i18n.T("doc.manifest_none"))
	}

	// the newest game log
	if log := latestGameLog(a.Paths.LogsDir()); log != "" {
		a.Log.Printf("%s", i18n.T("doc.log_line", log))
	} else {
		a.Log.Printf("%s", i18n.T("doc.log_none"))
	}
	return problems
}

// latestGameLog returns the most recent game-*.log, or "".
func latestGameLog(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	newest, newestTime := "", time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "game-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newest, newestTime = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return newest
}

// doctorInstance reports on one instance and returns the number of problems.
func (a *App) doctorInstance(name string, deep bool) int {
	a.Log.Printf("%s", i18n.T("doc.instance_header", name))
	inst, ok := a.DB.GetInstance(name)
	if !ok {
		a.Log.Printf("%s", i18n.T("inst.not_found", name))
		return 1
	}
	if inst.Version == "" {
		a.Log.Printf("%s", i18n.T("inst.missing_version", name))
		return 1
	}
	problems := 0

	// version document: from disk when possible, otherwise fetched into memory
	eff, err := a.effectiveVersion(inst.Version)
	if err != nil {
		a.Log.Printf("%s", i18n.T("doc.version_json_missing", inst.Version, err))
		return 1
	}
	need := java.RequiredMajor(eff, inst.Version, a.Log)
	rt := java.Pick(java.ScanRuntimes(a.DB.Cfg(), a.Paths, a.Log), need)
	switch {
	case rt != nil:
		a.Log.Printf("%s", i18n.T("doc.required_java", need, rt.Path, rt.Major))
	default:
		a.Log.Printf("%s", i18n.T("java.none_found", inst.Version, need))
		problems++
	}

	plan, err := a.Installer(true).Plan(eff, mcv.PlanOptions{IncludeAssets: true, FetchIndex: true})
	if err != nil {
		a.Log.Printf("%s", i18n.T("doc.plan_failed", err))
		return problems + 1
	}
	reportPlan(a, plan, deep)

	missing := plan.Check(deep)
	if len(missing) > 0 {
		problems++
		a.Log.Printf("%s", i18n.T("doc.fix_hint", name))
	} else {
		a.Log.Printf("%s", i18n.T("doc.healthy", name))
	}

	// the instance's own directory: natives, saves, mods, size
	dir := a.instanceDir(name)
	if n := countFiles(plan.NativesDir); n > 0 {
		a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("doc.field.natives"), 14),
			i18n.T("doc.natives_line", n, plan.NativesDir)))
	} else {
		a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("doc.field.natives"), 14),
			i18n.T("doc.natives_missing")))
	}
	a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("doc.field.saves"), 14),
		i18n.T("doc.saves_line", countDirs(filepath.Join(dir, "saves")))))
	if _, err := os.Stat(filepath.Join(dir, "mods")); err == nil {
		a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("doc.field.mods"), 14),
			i18n.T("doc.mods_line", countSuffix(filepath.Join(dir, "mods"), ".jar"))))
	}
	a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("inst.field.size"), 14), dirSize(dir)))
	return problems
}

// reportPlan prints one plan's file counts, the same way for doctor and repair.
func reportPlan(a *App, plan *mcv.Plan, deep bool) {
	total := len(plan.Files) + len(plan.Assets)
	missing := plan.Check(deep)
	absent, wrongSize, wrongHash := 0, 0, 0
	for _, m := range missing {
		switch m.Kind {
		case mcv.MissingAbsent:
			absent++
		case mcv.MissingWrongSize:
			wrongSize++
		default:
			wrongHash++
		}
	}
	label := func(key string) string { return padRight(i18n.T(key), 14) }
	a.Log.Printf("%s", i18n.T("inst.show_row", label("doc.field.files"),
		i18n.T("doc.files_line", total-len(missing), total, absent)))
	if wrongSize+wrongHash > 0 {
		a.Log.Printf("%s", i18n.T("inst.show_row", label("doc.field.damaged"),
			i18n.T("doc.damaged_line", wrongSize, wrongHash)))
	}
	if len(missing) > 0 {
		a.Log.Printf("%s", i18n.T("inst.show_row", label("doc.field.missing_bytes"),
			humanBytes(mcv.MissingBytes(missing))))
	}
	if plan.AssetsIndexID != "" {
		state := i18n.T("doc.mark_missing")
		if plan.IndexReadable {
			state = i18n.T("doc.mark_present")
		}
		a.Log.Printf("%s", i18n.T("inst.show_row", label("doc.field.index"),
			i18n.T("doc.index_line", plan.AssetsIndexID, len(plan.Assets), state)))
	}
	if !deep && len(plan.Assets) > 0 {
		a.Log.Infof("%s", i18n.T("doc.deep_hint"))
	}
}

// ---------------------------------------------------------------------------
// repair

func (a *App) cmdRepair(args []string) int {
	f, err := parseFlags("repair", args, map[string]flagSpec{
		"deep":    {},
		"force":   {},
		"dry-run": {},
		"yes":     {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.repair.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "repair", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "repair")
	}
	name := f.args[0]
	inst, ok := a.DB.GetInstance(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", name)))
	}
	if inst.Version == "" {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.missing_version", name)))
	}
	dryRun := f.boolean("dry-run")

	eff, err := a.effectiveVersion(inst.Version)
	if err != nil {
		return a.fatal(err)
	}
	plan, err := a.Installer(dryRun).Plan(eff, mcv.PlanOptions{IncludeAssets: true, FetchIndex: true})
	if err != nil {
		return a.fatal(err)
	}

	var wanted []mcv.Missing
	if f.boolean("force") {
		for _, it := range plan.Items() {
			wanted = append(wanted, mcv.Missing{Item: it, Kind: mcv.MissingAbsent})
		}
	} else {
		wanted = plan.Check(f.boolean("deep"))
	}
	total := len(plan.Files) + len(plan.Assets)
	if len(wanted) == 0 {
		a.Log.Printf("%s", i18n.T("doc.repair_none", name, total))
	} else {
		a.Log.Printf("%s", i18n.T("doc.repair_plan", name, len(wanted), humanBytes(mcv.MissingBytes(wanted))))
		if f.boolean("force") {
			a.Log.Printf("%s", i18n.T("doc.repair_force", total, humanBytes(plan.AssetBytes)))
		}
	}
	if dryRun {
		// A plan can name thousands of files: show a sample, not the whole list.
		const show = 20
		for i, m := range wanted {
			if i == show {
				a.Log.Infof("%s", i18n.T("dl.and_more", len(wanted)-show))
				break
			}
			a.Log.Infof("%s: %s", m.Kind, m.Item.Dest)
		}
		a.Log.Printf("%s", i18n.T("mcv.dry_run"))
		return 0
	}

	fetched := 0
	var written int64
	if len(wanted) > 0 {
		if !a.Yeseverything && a.DB.Cfg().ConfirmLargeDownload {
			ok, err := a.confirmBatch(name, len(wanted), mcv.MissingBytes(wanted))
			if err != nil {
				return a.fatal(err)
			}
			if !ok {
				return a.fatal(fmt.Errorf("%s", i18n.T("dl.declined", name)))
			}
		}
		items := make([]dl.Item, 0, len(wanted))
		for _, m := range wanted {
			if f.boolean("force") {
				// --force means "fetch it again", and dl skips anything that
				// already verifies, so the file has to be gone first.
				if err := os.Remove(m.Item.Dest); err != nil && !os.IsNotExist(err) {
					a.Log.Warnf("%s", err)
				}
			}
			items = append(items, m.Item)
		}
		if err := dl.FetchAll(a.Policy(), items, 6, a.Log); err != nil {
			return a.fatal(err)
		}
		fetched = len(items)
		for _, it := range items {
			if st, err := os.Stat(it.Dest); err == nil {
				written += st.Size()
			}
		}
		a.Log.Printf("%s", i18n.T("doc.repair_done", name, fetched, humanBytes(written)))
	}

	// --- verify and finish -----------------------------------------------
	// Ensure re-checks everything and re-extracts the natives, so a repair ends
	// in exactly the state a launch expects.
	res, err := a.Installer(false).Ensure(eff)
	if err != nil {
		return a.fatal(err)
	}
	if res.MissingFiles > 0 {
		return a.fatal(fmt.Errorf("%s", i18n.T("doc.repair_left", res.MissingFiles, name)))
	}

	// A repaired install that cannot run is not repaired: fix the runtime too.
	need := java.RequiredMajor(eff, inst.Version, a.Log)
	pinned := firstNonEmpty(inst.Java, a.DB.Cfg().JavaPath)
	if _, err := a.ensureJava(eff, inst.Version, need, pinned, false); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("doc.repair_ok", name))
	return 0
}

// effectiveVersion loads a version document without writing anything: from disk
// when it is cached, otherwise fetched into memory.
func (a *App) effectiveVersion(versionID string) (*mcv.Version, error) {
	entry := mcv.ManifestVersion{}
	if man, err := a.ManifestStore().Get(false); err == nil {
		if found, ok := man.Find(versionID); ok {
			entry = found
		}
	}
	v, err := a.Loader(true).Lookup(versionID, entry.URL)
	if err != nil {
		return nil, err
	}
	return v.Effective(), nil
}

// countFiles counts regular files under dir (0 when it does not exist).
func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// countDirs counts the subdirectories of dir (worlds in saves/, for instance).
func countDirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// countSuffix counts files in dir with the given suffix.
func countSuffix(dir, suffix string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), suffix) {
			n++
		}
	}
	return n
}
