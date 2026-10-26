package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/auth"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/launch"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// ---------------------------------------------------------------------------
// login / logout

func (a *App) cmdLogin(args []string) int {
	f, err := parseFlags("login", args, map[string]flagSpec{
		"label":      {takesValue: true},
		"force":      {},
		"timeout":    {takesValue: true},
		"flow":       {takesValue: true},
		"no-browser": {},
		"no-switch":  {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.login.usage"))
		return 0
	}
	timeoutSec, err := f.int("timeout", 0)
	if err != nil {
		return a.fatal(err)
	}
	client := a.AuthClient(0)

	// Two flows, because different OAuth client ids support different ones:
	// the default id works with the browser (authorization code + loopback)
	// flow, while a device-code capable id can be used with --flow device.
	var ms *auth.MSTokens
	switch strings.ToLower(f.str("flow", "browser")) {
	case "browser", "web":
		client.UseBrowserFlow()
		ms, err = client.BrowserLogin(time.Duration(timeoutSec)*time.Second, func(u string) {
			a.Log.Printf("%s", i18n.T("auth.browser_url", u))
			if !f.boolean("no-browser") {
				auth.OpenBrowser(u, a.Log)
			}
		})
	case "device":
		dev, derr := client.RequestDeviceCode()
		if derr != nil {
			return a.fatal(derr)
		}
		a.Log.Printf("%s", i18n.T("auth.device_code", dev.VerificationURI, dev.UserCode))
		a.Log.Printf("%s", i18n.T("auth.scopes", authScopeString()))
		ms, err = client.WaitForToken(dev, time.Duration(timeoutSec)*time.Second)
	default:
		return a.usageError("cli.bad_flag_value", "--flow", f.str("flow", ""))
	}
	if err != nil {
		return a.fatal(err)
	}
	sess, err := client.Login(ms)
	if err != nil {
		return a.fatal(err)
	}

	acc, ok := a.Store.Data.Accounts[sess.Profile.ID]
	if !ok || f.boolean("force") {
		old := a.Store.ActiveAccount()
		if old != nil && f.boolean("force") {
			a.Log.Infof("%s", i18n.T("auth.not_you", old.Name))
		}
		acc = &store.Account{}
	}
	// Remember who was active so --no-switch can put them back: adding a second
	// account should not silently move every later launch onto it.
	previous := a.Store.ActiveAccount()
	a.Log.Infof("%s", i18n.T("auth.storing"))
	if err := a.storeSession(acc, sess, f.str("label", "")); err != nil {
		return a.fatal(err)
	}
	if f.boolean("no-switch") && previous != nil && previous.UUID != acc.UUID {
		a.Store.SetActive(previous.UUID)
		if err := a.Store.Save(); err != nil {
			return a.fatal(err)
		}
	}
	a.Log.Printf("%s", i18n.T("auth.logged_in", sess.Profile.Name, sess.Profile.ID))
	a.Log.Printf("%s", i18n.T("auth.saved_identity", a.Store.StatePath))
	a.Log.Printf("%s", i18n.T("auth.saved_refresh", a.Store.RefreshPath))
	a.Log.Printf("%s", i18n.T("auth.saved_session", a.Store.SessionPath))
	a.Log.Printf("%s", i18n.T("auth.expires", sess.MC.ExpiresAt.Format(time.RFC3339)))
	if f.boolean("no-switch") && previous != nil && previous.UUID != acc.UUID {
		a.Log.Printf("%s", i18n.T("auth.active_kept", previous.Name))
	}
	a.Log.Printf("%s", i18n.T("auth.accounts_count", len(a.Store.Data.Accounts)))
	return 0
}

func authScopeString() string {
	// kept in one place so the log line matches the request
	return "service::user.auth.xboxlive.com::MBI_SSL offline_access"
}

func (a *App) cmdLogout(args []string) int {
	f, err := parseFlags("logout", args, map[string]flagSpec{
		"all": {},
		"yes": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.logout.usage"))
		return 0
	}
	if f.boolean("all") || len(a.Store.Data.Accounts) == 0 {
		if len(a.Store.Data.Accounts) == 0 {
			a.Log.Printf("%s", i18n.T("auth.logout_none"))
			return 0
		}
		names := make([]string, 0, len(a.Store.Data.Accounts))
		for uuid, acc := range a.Store.Data.Accounts {
			names = append(names, acc.Name)
			a.Store.DropAccount(uuid)
		}
		if err := a.Store.Save(); err != nil {
			return a.fatal(err)
		}
		if err := a.Store.SaveTokens(); err != nil {
			return a.fatal(err)
		}
		for _, n := range names {
			a.Log.Printf("%s", i18n.T("auth.logout_done", n))
		}
		a.Log.Printf("%s", i18n.T("auth.logout_tokens", a.Store.RefreshPath, a.Store.SessionPath))
		return 0
	}

	target := ""
	if len(f.args) > 0 {
		target = f.args[0]
	}
	acc := a.findAccount(target)
	if acc == nil {
		if target == "" {
			a.Log.Printf("%s", i18n.T("auth.logout_none"))
			return 0
		}
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", target)))
	}
	a.Store.DropAccount(acc.UUID)
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	if err := a.Store.SaveTokens(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("auth.logout_done", acc.Name))
	a.Log.Printf("%s", i18n.T("auth.logout_tokens", a.Store.RefreshPath, a.Store.SessionPath))
	return 0
}

// findAccount resolves an account by uuid, player name or label. An empty
// selector means "the active account".
func (a *App) findAccount(selector string) *store.Account {
	if selector == "" {
		return a.Store.ActiveAccount()
	}
	s := strings.ToLower(selector)
	for uuid, acc := range a.Store.Data.Accounts {
		if strings.ToLower(uuid) == s ||
			strings.ToLower(acc.Name) == s ||
			strings.ToLower(acc.Label) == s ||
			strings.EqualFold(strings.ReplaceAll(uuid, "-", ""), s) {
			return acc
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// list

func (a *App) cmdList(args []string) int {
	f, err := parseFlags("list", args, map[string]flagSpec{
		"instances": {},
		"accounts":  {},
		"available": {},
		"type":      {takesValue: true},
		"search":    {takesValue: true},
		"limit":     {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.list.usage"))
		return 0
	}
	explicit := f.has("instances") || f.has("accounts") || f.has("available")
	if !explicit || f.has("instances") {
		a.listInstances()
	}
	if f.has("accounts") {
		a.listAccounts()
	}
	if f.has("available") {
		if code := a.listAvailable(f); code != 0 {
			return code
		}
	}
	return 0
}

func (a *App) instanceDir(name string) string {
	return filepath.Join(a.VersionsDir(), name)
}

func (a *App) listAccounts() { a.accountList() }

func (a *App) listAvailable(f *flags) int {
	limit, err := f.int("limit", 20)
	if err != nil {
		return a.fatal(err)
	}
	man, err := a.ManifestStore().Get(false)
	if err != nil {
		return a.fatal(err)
	}
	versions := man.Filter(f.str("type", ""), f.str("search", ""), limit)
	a.Log.Printf("%s", i18n.T("mcv.latest", man.Latest.Release, man.Latest.Snapshot))
	for _, v := range versions {
		a.Log.Printf("%-24s %-12s %s", v.ID, v.Type, v.ReleaseTime.Format("2006-01-02"))
	}
	return 0
}

// ---------------------------------------------------------------------------
// create / delete

var _ = 0

// nameRuleDoc is the human description of what validateInstanceName enforces;
// it lives in instance_import.go next to the code it documents.

func (a *App) cmdCreate(args []string) int {
	f, err := parseFlags("create", args, map[string]flagSpec{
		"version":        {takesValue: true},
		"java":           {takesValue: true},
		"xmx":            {takesValue: true},
		"xms":            {takesValue: true},
		"jvm-arg":        {takesValue: true, list: true},
		"game-arg":       {takesValue: true, list: true},
		"loader":         {takesValue: true},
		"loader-version": {takesValue: true},
		"force":          {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.create.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "create", "<name>")
	}
	name := f.args[0]
	if err := validateInstanceName(name); err != nil {
		return a.fatal(err)
	}
	if _, exists := a.Store.GetInstance(name); exists && !f.boolean("force") {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.exists", name)))
	}
	version, err := a.resolveVersion(f.str("version", ""))
	if err != nil {
		return a.fatal(err)
	}
	if version == "" {
		return a.usageError("cli.missing_arg", "create", "--version")
	}
	if !f.boolean("force") {
		man, err := a.ManifestStore().Get(false)
		if err != nil {
			a.Log.Warnf("%s", err)
		} else if _, ok := man.Find(version); !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("inst.version_unknown", version)))
		}
	}
	// --loader is a choice, and a loader belongs to the version rather than to the
	// command: Fabric publishes a profile that inherits the game version, and the
	// instance then runs that profile. `--loader vanilla` is the default and undoes
	// nothing, which keeps the common case flag-free.
	loader, err := parseLoader(f.str("loader", "vanilla"))
	if err != nil {
		return a.usageError("loader.bad", f.str("loader", ""), loaderChoices())
	}
	if id, err := a.ensureLoaderTarget(versionRef{id: version}, loader, f.str("loader-version", "")); err != nil {
		return a.fatal(err)
	} else if id != "" {
		version = id
	}
	if err := launch.ValidateMemory(f.str("xmx", "")); err != nil {
		return a.fatal(err)
	}
	if err := launch.ValidateMemory(f.str("xms", "")); err != nil {
		return a.fatal(err)
	}
	inst := &store.Instance{
		Name:      name,
		Version:   version,
		CreatedAt: time.Now(),
		Java:      f.str("java", ""),
		Xmx:       f.str("xmx", ""),
		Xms:       f.str("xms", ""),
		JvmArgs:   f.lists["jvm-arg"],
		GameArgs:  f.lists["game-arg"],
	}
	a.Store.Data.Instances[name] = inst
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("inst.create_ok", name, version))
	a.Log.Printf("%s", i18n.T("inst.game_dir", a.instanceDir(name)))
	return 0
}

func (a *App) cmdDelete(args []string) int {
	f, err := parseFlags("delete", args, map[string]flagSpec{
		"keep-files": {},
		"yes":        {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.delete.usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "delete", "<name>")
	}
	name := f.args[0]
	if _, ok := a.Store.GetInstance(name); !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", name)))
	}
	dir := a.instanceDir(name)
	keep := f.boolean("keep-files")
	if !keep && !a.Yeseverything {
		ok, err := a.prompt(i18n.T("inst.delete_confirm", name, dir))
		if err != nil {
			return a.fatal(err)
		}
		if !ok {
			a.Log.Printf("%s", i18n.T("core.cancelled"))
			return 1
		}
	}
	delete(a.Store.Data.Instances, name)
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	if !keep {
		if err := os.RemoveAll(dir); err != nil {
			a.Log.Warnf("%s", err)
		}
	} else {
		a.Log.Printf("%s", i18n.T("inst.delete_kept", name))
	}
	a.Log.Printf("%s", i18n.T("inst.deleted", name))
	return 0
}

// ---------------------------------------------------------------------------
// refresh

func (a *App) cmdRefresh(args []string) int {
	f, err := parseFlags("refresh", args, map[string]flagSpec{
		"versions": {},
		"all":      {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.refresh.usage"))
		return 0
	}
	if _, err := a.ManifestStore().Refresh(); err != nil {
		return a.fatal(err)
	}
	if !f.boolean("all") {
		return 0
	}
	for _, name := range a.Store.InstanceNames() {
		inst := a.Store.Data.Instances[name]
		path := a.Loader(false).JSONPath(inst.Version)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			a.Log.Warnf("%s", err)
		}
		man, err := a.ManifestStore().Get(false)
		if err != nil {
			return a.fatal(err)
		}
		entry, _ := man.Find(inst.Version)
		if _, err := a.Loader(false).Lookup(inst.Version, entry.URL); err != nil {
			return a.fatal(err)
		}
		a.Log.Printf("%s", i18n.T("mcv.loaded", inst.Version, "mirror"))
	}
	return 0
}

// ---------------------------------------------------------------------------
// config

func (a *App) cmdConfig(args []string) int {
	if len(args) == 0 {
		return a.configList()
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "list", "ls", "--list":
		return a.configList()
	case "path", "--path":
		a.Log.Printf("%s", i18n.T("cli.config_path", a.Store.Path))
		a.Log.Printf("%s", i18n.T("cli.state_path", a.Store.StatePath))
		a.Log.Printf("%s", i18n.T("cli.refresh_path", a.Store.RefreshPath))
		a.Log.Printf("%s", i18n.T("cli.session_path", a.Store.SessionPath))
		a.Log.Printf("%s", i18n.T("cli.last_path", a.Store.LastPath))
		return 0
	case "get":
		if len(rest) == 0 {
			return a.usageError("cli.missing_arg", "config get", "<key>")
		}
		value, err := a.Store.Cfg().Get(rest[0])
		if err != nil {
			return a.fatal(err)
		}
		a.Log.Printf("%s", i18n.T("cfg.current", rest[0], value))
		return 0
	case "set":
		if len(rest) < 2 {
			return a.usageError("cli.missing_arg", "config set", "<key> <value>")
		}
		if err := a.Store.Cfg().Set(rest[0], strings.Join(rest[1:], " ")); err != nil {
			return a.fatal(err)
		}
		// patch mcli.toml in place so comments and unrelated keys survive
		if err := a.Store.SaveConfigValue(rest[0]); err != nil {
			return a.fatal(err)
		}
		value, _ := a.Store.Cfg().Get(rest[0])
		a.Log.Printf("%s", i18n.T("cfg.set_ok", rest[0], value))
		return 0
	case "unset":
		if len(rest) == 0 {
			return a.usageError("cli.missing_arg", "config unset", "<key>")
		}
		value, err := a.Store.Cfg().Reset(rest[0])
		if err != nil {
			return a.fatal(err)
		}
		if err := a.Store.SaveConfigValue(rest[0]); err != nil {
			return a.fatal(err)
		}
		a.Log.Printf("%s", i18n.T("cfg.unset_ok", rest[0], value))
		return 0
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", action)))
	}
}

func (a *App) configList() int {
	key, val := 24, 26
	a.Log.Printf("%s", i18n.T("cfg.list_header", "KEY", "VALUE", "DESCRIPTION"))
	rows := a.Store.Cfg().Rows()
	for _, r := range rows {
		v := r[1]
		if v == "" {
			v = i18n.T("common.none")
		}
		a.Log.Printf("%-*s %-*s %s", key, r[0], val, v, r[2])
	}
	return 0
}

// ---------------------------------------------------------------------------
// java

func (a *App) cmdJava(args []string) int {
	action := ""
	rest := args
	if len(args) > 0 {
		action = args[0]
		rest = args[1:]
	}
	if action == "" || action == "list" || action == "ls" {
		return a.javaList()
	}
	switch action {
	case "install", "download":
		return a.javaInstall(rest)
	case "which":
		return a.javaWhich(rest)
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("cli.unknown_command", action)))
	}
}

func (a *App) javaList() int {
	runtimes := java.ScanRuntimes(a.Store.Cfg(), a.Paths, a.Log)
	if len(runtimes) == 0 {
		a.Log.Printf("%s", i18n.T("java.none_installed"))
		return 0
	}
	a.Log.Printf("%s", i18n.T("java.list_header", "MAJOR", "VERSION", "SOURCE", "PATH"))
	for _, rt := range runtimes {
		a.Log.Printf("%s", i18n.T("java.list_row",
			strconv.Itoa(rt.Major), rt.Version, rt.Source, rt.Path))
	}
	return 0
}

func (a *App) javaInstall(args []string) int {
	f, err := parseFlags("java install", args, map[string]flagSpec{
		"component": {takesValue: true},
		"archive":   {},
		"force":     {},
		"yes":       {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if len(f.args) == 0 && f.str("component", "") == "" {
		return a.usageError("cli.missing_arg", "java install", "<major>")
	}
	major := 0
	if len(f.args) > 0 {
		major, err = strconv.Atoi(f.args[0])
		if err != nil {
			return a.fatal(fmt.Errorf("%s", i18n.T("cli.bad_flag_value", "major", f.args[0])))
		}
	}
	component := f.str("component", "")
	if component != "" {
		if m := java.MajorForComponent(component); m > 0 && major == 0 {
			major = m
		}
	}
	dest := a.Store.Cfg().JavaDir
	installer := a.JavaInstaller(f.boolean("force"))
	if !f.boolean("archive") {
		exe, err := installer.Install(component, major)
		if err == nil {
			a.Log.Printf("%s", i18n.T("java.extracted", major, filepath.Dir(filepath.Dir(exe))))
			return 0
		}
		a.Log.Warnf("%s", i18n.T("java.install_failed", err))
	}
	exe, err := installer.InstallFromArchive(major)
	if err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("java.extracted", major, filepath.Dir(filepath.Dir(exe))))
	a.Log.Printf("%s", i18n.T("java.offer_download", major, dest))
	return 0
}

func (a *App) javaWhich(args []string) int {
	f, err := parseFlags("java which", args, map[string]flagSpec{})
	if err != nil {
		return a.fatal(err)
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "java which", "<version>")
	}
	versionID := f.args[0]
	man, err := a.ManifestStore().Get(false)
	if err != nil {
		return a.fatal(err)
	}
	entry, ok := man.Find(versionID)
	if !ok {
		return a.fatal(fmt.Errorf("%s", i18n.T("mcv.unknown_version", versionID)))
	}
	v, err := a.Loader(false).Lookup(versionID, entry.URL)
	if err != nil {
		return a.fatal(err)
	}
	need := java.RequiredMajor(v.Effective(), versionID, a.Log)
	a.Log.Printf("%s", i18n.T("java.required_major", need))
	runtimes := java.ScanRuntimes(a.Store.Cfg(), a.Paths, a.Log)
	rt := java.Pick(runtimes, need)
	if rt == nil {
		a.Log.Printf("%s", i18n.T("java.none_found", versionID, need))
		return 1
	}
	a.Log.Printf("%s", i18n.T("java.selected", rt.Path, rt.Major))
	return 0
}

func (a *App) listInstances() { a.instanceList() }

// resolveVersion turns convenience aliases ("latest", "latest-snapshot") into a
// concrete version id. Everything else is returned unchanged, so a plain
// `mcli create x --version 1.21.4` still works offline.
func (a *App) resolveVersion(id string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "latest", "release", "latest-release", "stable":
		man, err := a.ManifestStore().Get(false)
		if err != nil {
			return "", err
		}
		if man.Latest.Release == "" {
			return "", fmt.Errorf("%s", i18n.T("inst.no_latest"))
		}
		a.Log.Infof("%s", i18n.T("inst.resolved", id, man.Latest.Release))
		return man.Latest.Release, nil
	case "snapshot", "latest-snapshot", "snap":
		man, err := a.ManifestStore().Get(false)
		if err != nil {
			return "", err
		}
		if man.Latest.Snapshot == "" {
			return "", fmt.Errorf("%s", i18n.T("inst.no_latest"))
		}
		a.Log.Infof("%s", i18n.T("inst.resolved", id, man.Latest.Snapshot))
		return man.Latest.Snapshot, nil
	}
	return id, nil
}

// knownVersion rejects a version id that the manifest does not list. A manifest
// that cannot be loaded is not an error here: being offline must not stop a
// user from editing an instance.
func (a *App) knownVersion(id string) error {
	man, err := a.ManifestStore().Get(false)
	if err != nil {
		a.Log.Warnf("%s", err)
		return nil
	}
	if _, ok := man.Find(id); !ok {
		return fmt.Errorf("%s", i18n.T("inst.version_unknown", id))
	}
	return nil
}

// autoCreateInstance creates an instance named after a version id, so
// `mcli launch 1.21.4` works without a separate `mcli create`.
func (a *App) autoCreateInstance(name string) (*store.Instance, error) {
	version, err := a.resolveVersion(name)
	if err != nil {
		return nil, err
	}
	if err := validateInstanceName(version); err != nil {
		return nil, err
	}
	man, err := a.ManifestStore().Get(false)
	if err != nil {
		return nil, err
	}
	if _, ok := man.Find(version); !ok {
		return nil, fmt.Errorf("%s", i18n.T("launch.unknown_target", name, version, name))
	}
	inst := &store.Instance{Name: version, Version: version, CreatedAt: time.Now()}
	a.Store.Data.Instances[version] = inst
	if err := a.Store.Save(); err != nil {
		return nil, err
	}
	a.Log.Printf("%s", i18n.T("launch.auto_create", version, version))
	return inst, nil
}
