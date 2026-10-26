package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	case "attach", "console", "con":
		return a.serverAttach(args[1:])
	case "cmd", "command", "say":
		return a.serverCmd(args[1:])
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
	// WriteFiles only fills in what is missing, so an existing eula.txt saying false
	// would survive a create that was told to accept the licence.
	if err := mcserver.AcceptEula(o.Dir); err != nil {
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
		"eula":           {},
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
	if f.boolean("eula") {
		// The licence can be accepted later than creation — doctor points here when
		// it finds a server that will refuse to start. The URL is always printed and
		// the question is always asked unless the caller said --yes.
		ok, err := a.askEula(true)
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("srv.eula_declined")))
		}
		if err := mcserver.AcceptEula(a.serverDir(srv)); err != nil {
			return a.fatal(err)
		}
		a.Log.Printf("%s", i18n.T("srv.eula_written", srv.Name, filepath.Join(a.serverDir(srv), "eula.txt")))
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

	// The pid is the only handle a later `server stop` has on this process, so it
	// is recorded the moment the process exists — in both modes. A foreground
	// server used to go unrecorded until it exited, which meant that anything
	// killing mcli (a `timeout`, a service manager, a closed terminal) left a
	// server running on a port with nothing in the state pointing at it.
	startedAt := time.Now()
	srv.StartedAt = startedAt
	// The pump can only open the console pipe once the server is holding it: a FIFO
	// with no reader on the other end is where a write blocks forever. Starting it
	// here, in onStart, is what keeps mcli from hanging before the server exists.
	var stopPump func()
	onStart := func(pid int) {
		srv.Pid = pid
		if err := a.Store.Save(); err != nil {
			a.Log.Warnf("%s", err)
		}
		if !detach && cmd.ConsoleViaPipe {
			stopPump = a.pumpConsole(o.Dir, name, startedAt)
		}
	}

	if !detach {
		// Foreground: the launcher owns the console (input through the FIFO, output
		// to the terminal). That is what makes Ctrl+C and a `timeout`'s SIGTERM stop
		// the server the way it understands — through its own `stop` — instead of
		// killing a JVM that has no signal handler and saving nothing.
		cmd.ConsoleViaPipe = mcserver.ConsolePipesSupported()
		stopForwarding := a.forwardSignals(srv, name)
		defer stopForwarding()
		defer func() {
			if stopPump != nil {
				stopPump()
			}
		}()
	}

	pid, code, err := cmd.Run(!detach, onStart)
	if err != nil {
		return a.fatal(err)
	}
	if detach {
		a.Log.Printf("%s", i18n.T("srv.detached", pid, filepath.Join(o.Dir, "logs", "latest.log")))
		if mcserver.ConsolePipesSupported() {
			a.Log.Printf("%s", i18n.T("srv.attach_hint_detached", name, name))
		}
		return 0
	}
	// The server is gone: whoever stopped it, the stale pid must not stay behind,
	// or `mcli server stop` would later signal whatever process took that number.
	srv.Pid = 0
	if code != 0 {
		_ = a.Store.Save()
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.exited_bad", code)))
	}
	_ = a.Store.Save()
	a.Log.Printf("%s", i18n.T("srv.exited", code))
	return 0
}

// forwardSignals makes a foreground server exit the way its own console would: the
// signals that reach mcli are turned into the server's own `stop` command, and mcli
// waits for it to save and close. A signal that killed the JVM outright — which is
// what both SIGINT and SIGTERM do to a headless server — leaves the world at the
// last autosave, and a `timeout 100 mcli server start` used to leave the server
// running with nothing in the state pointing at it.
func (a *App) forwardSignals(srv *store.Server, name string) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, foregroundSignals()...)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-sigs:
				a.stopServerGracefully(srv, name, sig.String())
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(done)
	}
}

// stopServerGracefully asks a running server to stop, preferring its own console
// command over a signal, and saying which one it used.
func (a *App) stopServerGracefully(srv *store.Server, name, why string) {
	pid := srv.Pid
	if pid <= 0 {
		return
	}
	if mcserver.ConsolePipesSupported() {
		a.Log.Printf("%s", i18n.T("srv.forwarding", name, why))
		if err := a.writeConsole(srv, "stop"); err == nil {
			return
		}
		a.Log.Warnf("%s", i18n.T("srv.stop_no_console", name, "console pipe"))
	}
	if err := signalServer(pid); err != nil {
		a.Log.Warnf("%s", i18n.T("srv.forward_failed", err))
	}
}

// pumpConsole feeds mcli's own input into the server's console FIFO, so a foreground
// server behaves like an attached one: what a person types reaches it, a piped script
// reaches it, and mcli can write into the same channel to stop it.
//
// It is called after the server has started, so opening the pipe cannot block — and
// it pumps whatever stdin happens to be: a terminal, a pipe from another command,
// `/dev/null` (which simply reports EOF and ends the pump).
func (a *App) pumpConsole(dir, name string, startedAt time.Time) func() {
	in, err := mcserver.OpenConsoleWriter(dir)
	if err != nil {
		a.Log.Warnf("%s", i18n.T("srv.stop_no_console", name, err))
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer in.Close()
		// Nothing is forwarded before the server can take it: a command that arrives
		// during startup is answered with "An unexpected error occurred while trying
		// to execute that command" (measured, with a script feeding `list` and
		// `stop`) because the dispatcher is not up yet. What a person types meanwhile
		// waits in their terminal, and what a pipe holds waits in the pipe.
		if !mcserver.WaitReady(dir, startedAt, 120*time.Second) {
			a.Log.Warnf("%s", i18n.T("srv.console_late", name))
		}
		buf := make([]byte, 1024)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if _, werr := in.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return func() { close(done) }
}

// signalServer asks a server to stop the graceful way: SIGINT, which is what its
// own console sends on Ctrl+C, so the world is saved on the way out.
func signalServer(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(os.Interrupt)
}

// killProcess ends a server the hard way. It is what `server stop --force` uses,
// and the only way to stop one on a platform that cannot deliver a console
// interrupt.
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

// serverStop asks a server to shut down, and waits for it to be gone.
//
// The way it asks matters. A signal is not a graceful stop for a headless JVM:
// measured on this box, both SIGINT and SIGTERM end the process outright, with no
// "Stopping server" and no "Saving worlds" in the server's own log — the world only
// keeps what the last autosave wrote. The server's console command `stop` does the
// real thing, so that is what mcli sends when it has a console channel; the signal
// is the fallback, and then mcli says what that fallback may cost.
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

	switch {
	case f.boolean("force"):
		// The caller asked for the hard way, whatever the console could do.
		a.Log.Printf("%s", i18n.T("srv.stopping_force", name, pid))
		if err := killProcess(pid); err != nil {
			return a.fatal(err)
		}
	case mcserver.ConsolePipesSupported():
		// The server's own `stop`: it saves the world and closes its listeners.
		a.Log.Printf("%s", i18n.T("srv.stopping_console", name, pid))
		if err := a.writeConsole(srv, "stop"); err != nil {
			a.Log.Warnf("%s", i18n.T("srv.stop_no_console", name, err))
			if err := signalServer(pid); err != nil {
				return a.fatal(fmt.Errorf("%s", i18n.T("srv.stop_force_hint", name)))
			}
		}
	default:
		// No console channel here: a signal is all mcli has, and the honest thing
		// is to say that the world keeps its last autosave rather than claiming a
		// save that may not happen.
		a.Log.Printf("%s", i18n.T("srv.stopping_signal", name, pid))
		if err := signalServer(pid); err != nil {
			a.Log.Warnf("%s", i18n.T("srv.stop_unsupported", pid))
			return a.fatal(fmt.Errorf("%s", i18n.T("srv.stop_force_hint", name)))
		}
	}

	// Give it time to save and close: a world write is not instant.
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

// writeConsole sends one line to a running server's console, waiting for the server
// to report readiness first: a command that arrives before its dispatcher exists is
// answered with "An unexpected error occurred while trying to execute that command",
// so whoever asks — `server cmd`, `server stop`, a forwarded signal — waits the same
// way the console pump does.
func (a *App) writeConsole(srv *store.Server, line string) error {
	dir := a.serverDir(srv)
	since := srv.StartedAt
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	if !mcserver.WaitReady(dir, since, 120*time.Second) {
		a.Log.Warnf("%s", i18n.T("srv.console_late", srv.Name))
	}
	in, err := mcserver.OpenConsoleWriter(dir)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = fmt.Fprintln(in, line)
	return err
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

// serverAttach connects to a server that is already running in the background: it
// follows the output the server writes and forwards whatever is typed into the
// server's own stdin pipe. Ctrl+C leaves the console without touching the server
// — stopping it is `server stop` (or the "stop" command once attached).
func (a *App) serverAttach(args []string) int {
	f, err := parseFlags("server attach", args, map[string]flagSpec{
		"lines": {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "server attach", "<name>")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "server attach")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	if !a.serverRunning(srv) {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_running", name)))
	}
	if !mcserver.ConsolePipesSupported() {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.attach_unsupported")))
	}
	dir := a.serverDir(srv)
	out, err := mcserver.OpenConsoleWriter(dir)
	if err != nil {
		return a.fatal(err)
	}
	defer out.Close()

	lines := 20
	if f.has("lines") {
		lines, err = f.int("lines", lines)
		if err != nil {
			return a.fatal(err)
		}
	}

	a.Log.Printf("%s", i18n.T("srv.attach_header", name, srv.Pid))
	a.Log.Printf("%s", i18n.T("srv.attach_hint"))

	// Ctrl+C belongs to the console, not to the server: it leaves this session.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	done := make(chan string, 1)
	go func() {
		<-sigs
		done <- "interrupt"
	}()
	go func() {
		if err := followConsole(mcserver.ConsoleOutputPath(dir), lines, func() bool {
			return a.serverRunning(srv)
		}); err != nil {
			done <- err.Error()
			return
		}
		done <- "ended"
	}()
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			if _, err := fmt.Fprintln(out, sc.Text()); err != nil {
				done <- err.Error()
				return
			}
		}
		done <- "stdin"
	}()

	switch why := <-done; why {
	case "interrupt":
		a.Log.Printf("%s", i18n.T("srv.attach_left", name, name))
	case "ended":
		a.Log.Printf("%s", i18n.T("srv.attach_ended", name))
	case "stdin":
		a.Log.Printf("%s", i18n.T("srv.attach_left", name, name))
	default:
		a.Log.Printf("%s", i18n.T("srv.attach_error", why))
		return 1
	}
	return 0
}

// serverCmd sends one command into a running server's console and returns; for a
// conversation use `server attach`.
func (a *App) serverCmd(args []string) int {
	f, err := parseFlags("server cmd", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.server.usage"))
		return 0
	}
	if len(f.args) < 2 {
		return a.usageError("cli.missing_arg", "server cmd", "<name> <command...>")
	}
	name := f.args[0]
	srv, ok := a.Store.GetServer(name)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_found", name)))
	}
	if !a.serverRunning(srv) {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.not_running", name)))
	}
	if !mcserver.ConsolePipesSupported() {
		return a.fatal(fmt.Errorf("%s", i18n.T("srv.attach_unsupported")))
	}
	line := strings.Join(f.args[1:], " ")
	if err := a.writeConsole(srv, line); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("srv.cmd_sent", line))
	return 0
}

// followConsole prints the last lines of a server's console file and then
// streams whatever the server appends, until stop reports the server is gone.
func followConsole(path string, lines int, stop func() bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if lines > 0 {
		tail := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if len(tail) > lines {
			tail = tail[len(tail)-lines:]
		}
		for _, l := range tail {
			if l != "" {
				fmt.Fprintln(os.Stdout, l)
			}
		}
	}
	offset := int64(len(raw))
	for {
		if st, err := os.Stat(path); err == nil && st.Size() < offset {
			offset = 0 // the server started a fresh console file
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			file.Close()
			return err
		}
		n, err := io.Copy(os.Stdout, file)
		file.Close()
		if err != nil {
			return err
		}
		offset += n
		if !stop() {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
}
