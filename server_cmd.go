package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/launch"
	"github.com/Kailoinf/MCL-CLI/internal/mcserver"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// serverDefaultXmx/serverDefaultXms are what a server runs with when nothing
// else is configured: enough for a handful of players, modest by default.
const (
	serverDefaultXmx = "2G"
	serverDefaultXms = "512M"
)

// cmdServer is the server-side counterpart of the client commands. A server has
// no account, no assets and no classpath: one jar, one directory, one world.
func (a *App) cmdServer(args []string) int {
	if len(args) == 0 || args[0] == "help" || strings.HasPrefix(args[0], "-") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	switch args[0] {
	case "create", "new", "add":
		return a.serverCreate(args[1:])
	case "list", "ls":
		return a.serverList(args[1:])
	case "show", "info":
		return a.serverShow(args[1:])
	case "set":
		return a.serverSet(args[1:])
	case "delete", "rm", "remove":
		return a.serverDelete(args[1:])
	case "start", "run":
		return a.serverStart(args[1:])
	case "stop", "shutdown":
		return a.serverStop(args[1:])
	}
	return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", "server "+args[0])))
}

// serverDir is where one server keeps its jar, world and files.
func (a *App) serverDir(srv *store.Server) string {
	if srv.Dir != "" {
		return srv.Dir
	}
	return filepath.Join(a.MinecraftDir(), "servers", srv.Name)
}

func (a *App) serverOptions(srv *store.Server, v *mcv.Version, versionID string) mcserver.Options {
	xmx, xms := srv.Xmx, srv.Xms
	if xmx == "" {
		xmx = serverDefaultXmx
	}
	if xms == "" {
		xms = serverDefaultXms
	}
	port := srv.Port
	if port <= 0 {
		port = mcserver.DefaultPort
	}
	return mcserver.Options{
		Dir:          a.serverDir(srv),
		Version:      v,
		VersionID:    versionID,
		Xmx:          xmx,
		Xms:          xms,
		Port:         port,
		ExtraJVMArgs: srv.JvmArgs,
	}
}

// askEula prints the licence URL and asks the person to confirm they read it.
// --eula and the global --yes accept without the question, but the URL is always
// printed: accepting a licence silently is not an option.
func (a *App) askEula(auto bool) (bool, error) {
	a.Log.Printf("%s", i18n.T("srv.eula_url", mcserver.EulaURL))
	if auto || a.Yeseverything {
		return true, nil
	}
	return a.prompt(i18n.T("srv.eula_prompt"))
}

// ---------------------------------------------------------------------------
// server create

func (a *App) serverCreate(args []string) int {
	f, err := parseFlags("server create", args, map[string]flagSpec{
		"version": {takesValue: true},
		"java":    {takesValue: true},
		"xmx":     {takesValue: true},
		"xms":     {takesValue: true},
		"port":    {takesValue: true},
		"dir":     {takesValue: true},
		"jvm-arg": {takesValue: true, list: true},
		"eula":    {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server create", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server create")
	}
	name := f.args[0]
	if err := validateInstanceName(name); err != nil {
		return a.fatal(err)
	}
	if _, ok := a.Store.GetServer(name); ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.exists", name)))
	}
	if _, ok := a.Store.GetInstance(name); ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.name_taken", name)))
	}
	if f.str("version", "") == "" {
		return a.usageError("cli.missing_arg", "server create", "--version <version>")
	}
	versionID, err := a.resolveVersion(f.str("version", ""))
	if err != nil {
		return a.fatal(err)
	}
	for _, key := range []string{"xmx", "xms"} {
		if v := f.str(key, ""); v != "" {
			if err := launch.ValidateMemory(v); err != nil {
				return a.usageError("cli.bad_flag_value", "--"+key, err.Error())
			}
		}
	}
	port := mcserver.DefaultPort
	if f.has("port") {
		port, err = f.int("port", port)
		if err != nil {
			return a.fatal(err)
		}
		if port < 1 || port > 65535 {
			return a.usageError("srv.bad_port", port)
		}
	}

	srv := &store.Server{
		Name:      name,
		Version:   versionID,
		Dir:       f.str("dir", ""),
		Java:      f.str("java", ""),
		Xmx:       f.str("xmx", ""),
		Xms:       f.str("xms", ""),
		Port:      port,
		JvmArgs:   f.lists["jvm-arg"],
		CreatedAt: time.Now(),
	}

	// The licence is the one thing a server cannot start without, so it is asked
	// for here as well as on start: nobody should discover it halfway through.
	accepted, err := a.askEula(f.boolean("eula"))
	if err != nil {
		return a.fatal(err)
	}
	if !accepted {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.eula_declined")))
	}
	srv.Eula = true

	a.Store.Data.Servers[name] = srv
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	o := a.serverOptions(srv, nil, versionID)
	if _, err := mcserver.WriteFiles(o, a.Log); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("srv.created", name, versionID))
	a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("srv.field.dir"), 12), o.Dir))
	a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T("srv.field.port"), 12), fmt.Sprintf("%d", o.Port)))
	a.Log.Printf("%s", i18n.T("srv.next_hint", name))
	return 0
}

// ---------------------------------------------------------------------------
// server list / show

func (a *App) serverList(args []string) int {
	f, err := parseFlags("server list", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	names := a.Store.ServerNames()
	if len(names) == 0 {
		a.Log.Printf("%s", i18n.T("srv.none"))
		return 0
	}
	a.Log.Printf("%s", i18n.T("srv.list_header",
		padRight("NAME", 20), padRight("VERSION", 16), padRight("PORT", 7),
		padRight("PID", 9), padRight("STATE", 10), padRight("MEMORY", 12), "DIR"))
	for _, n := range names {
		srv := a.Store.Data.Servers[n]
		o := a.serverOptions(srv, nil, srv.Version)
		state := a.serverState(srv)
		pid := "-"
		if a.serverRunning(srv) {
			state = i18n.T("srv.state_running")
			pid = fmt.Sprintf("%d", srv.Pid)
		}
		a.Log.Printf("%s", i18n.T("srv.list_row",
			padRight(srv.Name, 20), padRight(srv.Version, 16),
			padRight(fmt.Sprintf("%d", o.Port), 7), padRight(pid, 9),
			padRight(state, 10),
			padRight(o.Xmx+"/"+o.Xms, 12), o.Dir))
	}
	return 0
}

// serverRunning reports whether the recorded process is still this server, and
// clears the pid when it is not — a crashed or killed server must not look alive
// on the next command.
func (a *App) serverRunning(srv *store.Server) bool {
	if srv.Pid <= 0 {
		return false
	}
	_, alive, ours := serverProcess(srv.Pid, a.serverDir(srv))
	if alive && ours {
		return true
	}
	srv.Pid = 0
	if err := a.Store.Save(); err != nil {
		a.Log.Warnf("%s", err)
	}
	return false
}

// serverState says whether the jar a server needs is actually on disk.
func (a *App) serverState(srv *store.Server) string {
	jar := filepath.Join(a.serverDir(srv), mcserver.JarName)
	if st, err := os.Stat(jar); err == nil && st.Size() > 0 {
		return i18n.T("srv.state_ready")
	}
	return i18n.T("srv.state_missing")
}

func (a *App) serverShow(args []string) int {
	f, err := parseFlags("server show", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server show", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server show")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	o := a.serverOptions(srv, nil, srv.Version)
	row := func(key, value string) {
		a.Log.Printf("%s", i18n.T("inst.show_row", padRight(i18n.T(key), 14), value))
	}
	row("srv.field.name", srv.Name)
	row("srv.field.version", srv.Version)
	row("srv.field.dir", o.Dir)
	row("srv.field.port", fmt.Sprintf("%d", o.Port))
	row("srv.field.memory", "-Xmx"+o.Xmx+" -Xms"+o.Xms)
	if srv.Java != "" {
		row("srv.field.java", srv.Java)
	} else {
		row("srv.field.java", i18n.T("inst.auto"))
	}
	if len(srv.JvmArgs) > 0 {
		row("srv.field.jvm_args", strings.Join(srv.JvmArgs, " "))
	}
	if a.serverRunning(srv) {
		row("srv.field.pid", fmt.Sprintf("%d", srv.Pid))
	}
	accepted := i18n.T("srv.eula_no")
	if mcserver.EulaAccepted(o.Dir) {
		accepted = i18n.T("srv.eula_yes")
	}
	row("srv.field.eula", accepted)
	row("srv.field.jar", a.serverState(srv))
	row("inst.field.created", srv.CreatedAt.Format("2006-01-02 15:04"))
	if !srv.StartedAt.IsZero() {
		row("inst.field.last_played", srv.StartedAt.Format("2006-01-02 15:04"))
	}
	return 0
}

// ---------------------------------------------------------------------------
// server set / delete

func (a *App) serverSet(args []string) int {
	f, err := parseFlags("server set", args, map[string]flagSpec{
		"version":        {takesValue: true},
		"java":           {takesValue: true},
		"xmx":            {takesValue: true},
		"xms":            {takesValue: true},
		"port":           {takesValue: true},
		"jvm-arg":        {takesValue: true, list: true},
		"clear-jvm-args": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server set", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server set")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	changed := false
	if f.has("version") {
		versionID, err := a.resolveVersion(f.str("version", ""))
		if err != nil {
			return a.fatal(err)
		}
		srv.Version = versionID
		changed = true
	}
	for _, key := range []string{"xmx", "xms"} {
		if v := f.str(key, ""); v != "" {
			if err := launch.ValidateMemory(v); err != nil {
				return a.usageError("cli.bad_flag_value", "--"+key, err.Error())
			}
		}
	}
	for _, kv := range []struct {
		flag string
		dst  *string
	}{{"java", &srv.Java}, {"xmx", &srv.Xmx}, {"xms", &srv.Xms}} {
		if v, ok := f.values[kv.flag]; ok {
			*kv.dst = v
			changed = true
		}
	}
	if f.has("port") {
		port, err := f.int("port", srv.Port)
		if err != nil {
			return a.fatal(err)
		}
		if port < 1 || port > 65535 {
			return a.usageError("srv.bad_port", port)
		}
		srv.Port = port
		changed = true
	}
	if f.boolean("clear-jvm-args") {
		srv.JvmArgs = nil
		changed = true
	}
	if list := f.lists["jvm-arg"]; len(list) > 0 {
		srv.JvmArgs = append(srv.JvmArgs, list...)
		changed = true
	}
	if !changed {
		a.Log.Printf("%s", i18n.T("srv.nothing_changed"))
		return 0
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("srv.changed", name))
	return 0
}

func (a *App) serverDelete(args []string) int {
	f, err := parseFlags("server delete", args, map[string]flagSpec{
		"keep-files": {},
		"yes":        {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server delete", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server delete")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	dir := a.serverDir(srv)
	keep := f.boolean("keep-files")
	if !keep && !a.Yeseverything {
		ok, err := a.prompt(i18n.T("srv.delete_confirm", name, dir))
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			a.Log.Printf("%s", i18n.T("core.cancelled"))
			return 1
		}
	}
	delete(a.Store.Data.Servers, name)
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	if keep {
		a.Log.Printf("%s", i18n.T("srv.delete_kept", dir))
		return 0
	}
	if err := os.RemoveAll(dir); err != nil {
		a.Log.Warnf("%s", err)
	}
	a.Log.Printf("%s", i18n.T("srv.deleted", name))
	return 0
}

// ---------------------------------------------------------------------------
// server start

func (a *App) serverStart(args []string) int {
	f, err := parseFlags("server start", args, map[string]flagSpec{
		"dry-run": {},
		"detach":  {},
		"eula":    {},
		"port":    {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server start", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server start")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	dryRun := f.boolean("dry-run")

	versionID, err := a.resolveVersion(srv.Version)
	if err != nil {
		return a.fatal(err)
	}
	entry := mcv.ManifestVersion{}
	if man, err := a.ManifestStore().Get(false); err == nil {
		if found, ok := man.Find(versionID); ok {
			entry = found
		}
	}
	v, err := a.Loader(dryRun).Lookup(versionID, entry.URL)
	if err != nil {
		return a.fatal(err)
	}
	eff := v.Effective()

	o := a.serverOptions(srv, eff, versionID)
	if f.has("port") {
		port, err := f.int("port", o.Port)
		if err != nil {
			return a.fatal(err)
		}
		if port < 1 || port > 65535 {
			return a.usageError("srv.bad_port", port)
		}
		o.Port = port
	}

	// --- java ------------------------------------------------------------
	need := java.RequiredMajor(eff, versionID, a.Log)
	pinned := firstNonEmpty(srv.Java, a.Store.Cfg().JavaPath)
	rt, err := a.ensureJava(eff, versionID, need, pinned, dryRun)
	if err != nil {
		return a.fatal(err)
	}

	// --- the server jar --------------------------------------------------
	item, ok := mcserver.JarItem(o, a.Mapper())
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.no_jar", versionID)))
	}
	if a.serverJarMissing(item) {
		if dryRun {
			a.Log.Printf("%s", i18n.T("srv.need_jar_dry", item.Desc, humanBytes(item.Size)))
		} else {
			a.Log.Printf("%s", i18n.T("srv.need_jar", item.Desc, humanBytes(item.Size)))
		}
		if !dryRun {
			ok, err := a.confirmFile(item.Desc, item.Size)
			if err != nil {
				return a.fatal(err)
			}
			if !ok {
				return a.fatal(fmt.Errorf("%s", i18n.T("dl.declined", item.Desc)))
			}
			if err := dl.FetchAll(a.Policy(), []dl.Item{item}, 1, a.Log); err != nil {
				return a.fatal(err)
			}
			if a.serverJarMissing(item) {
				return a.fatal(fmt.Errorf("%s", i18n.T("srv.jar_broken", item.Dest)))
			}
		}
	}

	// --- the licence and the files around the world ----------------------
	if !mcserver.EulaAccepted(o.Dir) {
		accepted, err := a.askEula(f.boolean("eula"))
		if err != nil {
			return a.fatal(err)
		}
		if !accepted {
			return a.fatal(fmt.Errorf("%s", i18n.T("srv.eula_declined")))
		}
		srv.Eula = true
	}
	if !dryRun {
		if _, err := mcserver.WriteFiles(o, a.Log); err != nil {
			return a.fatal(err)
		}
	}

	// A server has no command line to inspect except this one.
	cmd := mcserver.Prep(o, *rt)
	a.Log.Printf("%s", i18n.T("srv.java_cmd", rt.Path))
	a.Log.Printf("%s", i18n.T("srv.cmdline", cmd.CommandLine()))
	if dryRun {
		a.Log.Printf("%s", i18n.T("srv.dry_run"))
		return 0
	}

	detach := f.boolean("detach")
	a.Log.Printf("%s", i18n.T("srv.starting", o.Dir))
	pid, code, err := cmd.Run(!detach)
	if err != nil {
		return a.fatal(err)
	}
	srv.StartedAt = time.Now()
	if err := a.Store.Save(); err != nil {
		a.Log.Warnf("%s", err)
	}
	if detach {
		// The pid is the only handle a later `server stop` has on this process.
		srv.Pid = pid
		if err := a.Store.Save(); err != nil {
			a.Log.Warnf("%s", err)
		}
		a.Log.Printf("%s", i18n.T("srv.detached", pid, filepath.Join(o.Dir, "logs", "latest.log")))
		return 0
	}
	srv.Pid = pid
	if code != 0 {
		_ = a.Store.Save()
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.exited_bad", code)))
	}
	_ = a.Store.Save()
	a.Log.Printf("%s", i18n.T("srv.exited", code))
	return 0
}

// serverStop asks a detached server to shut down. A server saves its world on
// the way out, so it is signalled, never killed: the process gets SIGINT, which
// is exactly what Ctrl+C in its console would send.
func (a *App) serverStop(args []string) int {
	f, err := parseFlags("server stop", args, map[string]flagSpec{
		"force": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server stop", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server stop")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	dir := a.serverDir(srv)
	pid, alive, mine := serverProcess(srv.Pid, dir)
	if srv.Pid <= 0 || !alive || !mine {
		if srv.Pid > 0 {
			srv.Pid = 0
			if err := a.Store.Save(); err != nil {
				a.Log.Warnf("%s", err)
			}
		}
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_running", name)))
	}
	a.Log.Printf("%s", i18n.T("srv.stopping", name, pid))
	proc, err := os.FindProcess(pid)
	if err != nil {
		return a.fatal(err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		// Windows cannot deliver a console interrupt; a hard kill is the only way
		// out there, and the world is saved by the server's own autosave.
		a.Log.Warnf("%s", i18n.T("srv.stop_unsupported", pid))
		if f.boolean("force") {
			if err := proc.Kill(); err != nil {
				return a.fatal(err)
			}
		} else {
			return a.fatal(fmt.Errorf("%s", i18n.T("srv.stop_force_hint", name)))
		}
	}
	// A world is saved on the way out, so give it time rather than assuming.
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if _, alive, _ := serverProcess(pid, dir); !alive {
			srv.Pid = 0
			if err := a.Store.Save(); err != nil {
				a.Log.Warnf("%s", err)
			}
			a.Log.Printf("%s", i18n.T("srv.stopped", name))
			return 0
		}
		time.Sleep(500 * time.Millisecond)
	}
	return a.fatal(fmt.Errorf("%s", i18n.T("srv.stop_timeout", name)))
}

// serverProcess reports whether pid is alive and whether it is the server of
// this directory: /proc/<pid>/cmdline names the jar, so a recycled pid cannot be
// signalled by mistake. Where /proc is unavailable the process is assumed to be
// ours, which is the best a portable CLI can do.
func serverProcess(pid int, dir string) (int, bool, bool) {
	if pid <= 0 {
		return pid, false, false
	}
	raw, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "cmdline"))
	if err != nil {
		// no /proc: assume the recorded pid is still this server
		if err := signalZero(pid); err != nil {
			return pid, false, false
		}
		return pid, true, true
	}
	if len(raw) == 0 {
		return pid, false, false
	}
	cmdline := strings.ReplaceAll(string(raw), "\x00", " ")
	ours := strings.Contains(cmdline, dir) || strings.Contains(cmdline, mcserver.JarName)
	return pid, true, ours
}

// signalZero checks that a process exists without disturbing it.
func signalZero(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.Signal(0))
}

// serverJarMissing reports whether the server jar has to be downloaded: it is
// absent, the wrong size, or not the file the version's JSON describes.
func (a *App) serverJarMissing(item dl.Item) bool {
	st, err := os.Stat(item.Dest)
	if err != nil {
		return true
	}
	if item.Size > 0 && st.Size() != item.Size {
		return true
	}
	if item.SHA1 == "" {
		return false
	}
	return !item.Verify()
}
