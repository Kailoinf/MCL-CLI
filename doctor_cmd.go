package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcserver"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/store"
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
		// One name: an instance first, and a server when that is what it names —
		// both are things mcli installs and both can be broken.
		if _, ok := a.Store.GetInstance(f.args[0]); ok {
			problems += a.doctorInstance(f.args[0], f.boolean("deep"))
		} else if _, ok := a.Store.GetServer(f.args[0]); ok {
			problems += a.doctorServer(f.args[0])
		} else {
			a.Log.Printf("%s", i18n.T("inst.not_found", f.args[0]))
			problems++
		}
	case f.boolean("all"):
		for _, name := range a.Store.InstanceNames() {
			problems += a.doctorInstance(name, f.boolean("deep"))
		}
		for _, name := range a.Store.ServerNames() {
			problems += a.doctorServer(name)
		}
	default:
		if n := len(a.Store.InstanceNames()); n > 0 {
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
	a.Log.Printf("%s", i18n.T("doc.version_line", Version, a.Store.Data.StateVersion))
	for _, line := range []struct {
		key  string
		path string
	}{
		{"cli.mcl_home", a.Paths.Home},
		{"cli.config_path", a.Store.Path},
		{"cli.state_path", a.Store.StatePath},
		{"cli.refresh_path", a.Store.RefreshPath},
		{"cli.session_path", a.Store.SessionPath},
		{"cli.last_path", a.Store.LastPath},
		{"cli.minecraft_dir", a.MinecraftDir()},
	} {
		a.Log.Printf("%s", i18n.T(line.key, line.path))
	}
	if free, ok := freeBytes(a.MinecraftDir()); ok {
		a.Log.Printf("%s", i18n.T("doc.disk_free", a.MinecraftDir(), humanBytes(int64(free))))
	}
	a.Log.Printf("%s", i18n.T("auth.summary", a.accountSummary()))

	// java: what is installed and what the newest recorded launch wanted
	runtimes := java.ScanRuntimes(a.Store.Cfg(), a.Paths, a.Log)
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
	if cache := a.Store.Data.Manifest; cache != nil {
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
	inst, ok := a.Store.GetInstance(name)
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
	rt := java.Pick(java.ScanRuntimes(a.Store.Cfg(), a.Paths, a.Log), need)
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
	inst, ok := a.Store.GetInstance(name)
	if !ok {
		// Not an instance: a server has one file to repair and gets the same
		// command, so `mcli repair <server>` fetches its jar and nothing else.
		if srv, isServer := a.Store.GetServer(name); isServer {
			return a.repairServer(srv, f.boolean("dry-run"), f.boolean("force"))
		}
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
		if !a.Yeseverything && a.Store.Cfg().ConfirmLargeDownload {
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
	pinned := firstNonEmpty(inst.Java, a.Store.Cfg().JavaPath)
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

// ---------------------------------------------------------------------------
// servers
//
// A server is the other half of the project and gets the same treatment: what is
// missing, what does not match, and what mcli will not touch. repair fetches the
// jar, and nothing else — eula.txt, server.properties and the world belong to the
// person running the server.

// doctorServer reports on one server install, and returns the number of problems.
func (a *App) doctorServer(name string) int {
	a.Log.Printf("%s", i18n.T("doc.server_header", name))
	srv, ok := a.Store.GetServer(name)
	if !ok {
		a.Log.Printf("%s", i18n.T("srv.not_found", name))
		return 1
	}
	dir := a.serverDir(srv)
	if _, err := os.Stat(dir); err != nil {
		a.Log.Printf("%s", i18n.T("doc.srv_dir_missing", dir))
		return 1
	}
	problems := 0

	// --- the jar, against the version's own checksum --------------------
	eff, err := a.effectiveVersion(srv.Version)
	if err != nil {
		a.Log.Printf("%s", i18n.T("doc.version_json_missing", srv.Version, err))
		problems++
	} else {
		need := java.RequiredMajor(eff, srv.Version, a.Log)
		if rt := java.Pick(java.ScanRuntimes(a.Store.Cfg(), a.Paths, a.Log), need); rt != nil {
			a.Log.Printf("%s", i18n.T("doc.required_java", need, rt.Path, rt.Major))
		} else {
			a.Log.Printf("%s", i18n.T("java.none_found", srv.Version, need))
			problems++
		}

		o := a.serverOptions(srv, eff, srv.Version)
		item, ok := mcserver.JarItem(o, a.Mapper())
		jar := mcserver.JarPath(o)
		switch info, statErr := os.Stat(jar); {
		case !ok:
			a.Log.Printf("%s", i18n.T("doc.srv_jar_unknown", srv.Version))
			problems++
		case os.IsNotExist(statErr):
			a.Log.Printf("%s", i18n.T("doc.srv_jar_missing", jar, humanBytes(item.Size), name))
			problems++
		default:
			got, hashErr := sha1File(jar)
			switch {
			case hashErr != nil:
				a.Log.Printf("%s", i18n.T("doc.srv_jar_unreadable", jar, hashErr))
				problems++
			case got != item.SHA1:
				a.Log.Printf("%s", i18n.T("doc.srv_jar_corrupt", jar, humanBytes(info.Size()), humanBytes(item.Size), name))
				problems++
			default:
				a.Log.Printf("%s", i18n.T("doc.srv_jar_ok", jar, humanBytes(info.Size())))
			}
		}
	}

	// --- licence, properties, port --------------------------------------
	eulaPath := filepath.Join(dir, "eula.txt")
	switch _, err := os.ReadFile(eulaPath); {
	case err != nil:
		a.Log.Printf("%s", i18n.T("doc.srv_eula_missing", mcserver.EulaURL, name))
		problems++
	case !mcserver.EulaAccepted(dir):
		a.Log.Printf("%s", i18n.T("doc.srv_eula_false", eulaPath, mcserver.EulaURL, name))
		problems++
	default:
		a.Log.Printf("%s", i18n.T("doc.srv_eula_ok"))
	}

	if raw, err := os.ReadFile(filepath.Join(dir, "server.properties")); err != nil {
		a.Log.Printf("%s", i18n.T("doc.srv_props_missing"))
	} else if port, ok := parsePropertyPort(string(raw)); !ok {
		a.Log.Printf("%s", i18n.T("doc.srv_props_bad"))
		problems++
	} else {
		a.Log.Printf("%s", i18n.T("doc.srv_port_line", port))
		if other, clash := a.serverPortClash(name, port); clash {
			a.Log.Printf("%s", i18n.T("doc.srv_port_clash", port, other))
			problems++
		} else if pid, alive, _ := serverProcess(srv.Pid, dir); alive {
			a.Log.Printf("%s", i18n.T("doc.srv_running", pid))
		} else if busy, why := portBusy(port); busy {
			a.Log.Printf("%s", i18n.T("doc.srv_port_busy", port, why))
			problems++
		} else {
			a.Log.Printf("%s", i18n.T("doc.srv_port_free", port))
		}
	}

	// --- the record of the process, and the last log --------------------
	if srv.Pid > 0 {
		if _, alive, _ := serverProcess(srv.Pid, dir); !alive {
			a.Log.Printf("%s", i18n.T("doc.srv_stale_pid", srv.Pid))
			problems++
		}
	}
	if log := mcserverLog(dir); log != "" {
		if raw, err := os.ReadFile(log); err == nil {
			state := i18n.T("doc.srv_log_not_ready")
			if strings.Contains(string(raw), mcserver.ReadyLine) {
				state = i18n.T("doc.srv_log_ready")
			}
			a.Log.Printf("%s", i18n.T("doc.srv_log_line", log, state))
		}
	} else {
		a.Log.Printf("%s", i18n.T("doc.srv_no_log"))
	}
	if free, ok := freeBytes(dir); ok {
		a.Log.Printf("%s", i18n.T("doc.disk_free", dir, humanBytes(int64(free))))
	}
	a.Log.Printf("%s", i18n.T("doc.srv_dir_size", dirSize(dir)))

	if problems == 0 {
		a.Log.Printf("%s", i18n.T("doc.srv_healthy", name))
	} else {
		a.Log.Printf("%s", i18n.T("doc.srv_fix_hint", name))
	}
	return problems
}

// mcserverLog is the log a server writes: logs/latest.log. latestGameLog looks for
// the *client*'s game-*.log files, which never exist in a server directory.
func mcserverLog(dir string) string {
	path := filepath.Join(dir, "logs", "latest.log")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// parsePropertyPort reads the port out of a server.properties file.
func parsePropertyPort(body string) (int, bool) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "server-port" {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port < 1 || port > 65535 {
			return 0, false
		}
		return port, true
	}
	return 0, false
}

// serverPortClash reports whether another registered server claims the same port,
// which would make whichever starts second fail.
func (a *App) serverPortClash(name string, port int) (string, bool) {
	for _, other := range a.Store.ServerNames() {
		if other == name {
			continue
		}
		srv, ok := a.Store.GetServer(other)
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(a.serverDir(srv), "server.properties"))
		if err != nil {
			continue
		}
		if otherPort, ok := parsePropertyPort(string(raw)); ok && otherPort == port {
			return other, true
		}
	}
	return "", false
}

// portBusy reports whether anything is listening on the loopback port, and what the
// kernel says about it. It is a read-only probe: doctor never binds a real port for
// long enough to matter, and a running server is reported as running instead.
func portBusy(port int) (bool, string) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		ln.Close()
		return false, ""
	}
	return true, err.Error()
}

// repairServer makes a server's jar what the version says it should be, and touches
// nothing else. eula.txt, server.properties and the world are the person's own: a
// repair that overwrote them would be a repair that cost someone a world.
func (a *App) repairServer(srv *store.Server, dryRun, force bool) int {
	name := srv.Name
	eff, err := a.effectiveVersion(srv.Version)
	if err != nil {
		return a.fatal(err)
	}
	o := a.serverOptions(srv, eff, srv.Version)
	item, ok := mcserver.JarItem(o, a.Mapper())
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.need_jar", srv.Version, name)))
	}
	jar := mcserver.JarPath(o)

	needed, corrupt := false, false
	if force {
		needed = true
	} else if info, err := os.Stat(jar); err != nil {
		needed = true
	} else if got, err := sha1File(jar); err != nil || got != item.SHA1 {
		needed, corrupt = true, true
	} else {
		_ = info
	}

	switch {
	case !needed:
		a.Log.Printf("%s", i18n.T("doc.repair_srv_none", name, humanBytes(item.Size)))
		return 0
	case corrupt && !force:
		a.Log.Printf("%s", i18n.T("doc.repair_srv_corrupt", name, humanBytes(item.Size)))
	default:
		a.Log.Printf("%s", i18n.T("doc.repair_srv_plan", name, jar, humanBytes(item.Size)))
	}
	if dryRun {
		a.Log.Printf("%s", i18n.T("mcv.dry_run"))
		return 0
	}
	if !a.Yeseverything {
		ok, err := a.confirmBatch(name, 1, item.Size)
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("dl.declined", name)))
		}
	}
	if force {
		// A forced repair replaces the file outright rather than trusting a size
		// match, which is the point of asking for it.
		if err := os.Remove(jar); err != nil && !os.IsNotExist(err) {
			return a.fatal(err)
		}
	}
	if err := dl.FetchAll(a.Policy(), []dl.Item{item}, 1, a.Log); err != nil {
		return a.fatal(err)
	}
	if info, err := os.Stat(jar); err == nil {
		a.Log.Printf("%s", i18n.T("doc.repair_srv_ok", name, humanBytes(info.Size())))
	}
	return 0
}
