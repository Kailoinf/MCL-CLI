package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/auth"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/launch"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

func (a *App) cmdLaunch(args []string) int {
	f, err := parseFlags("launch", args, map[string]flagSpec{
		"last":     {},
		"version":  {takesValue: true},
		"java":     {takesValue: true},
		"xmx":      {takesValue: true},
		"xms":      {takesValue: true},
		"jvm-arg":  {takesValue: true, list: true},
		"game-arg": {takesValue: true, list: true},
		"width":    {takesValue: true},
		"height":   {takesValue: true},
		"dry-run":  {},
		"wait":     {},
		"account":  {takesValue: true},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("cli.cmd.launch.usage"))
		return 0
	}
	dryRun := f.boolean("dry-run")

	// --- resolve target instance -----------------------------------------
	var instName, versionID string
	var base *store.Instance
	switch {
	case f.boolean("last"):
		last := a.DB.Data.Last
		if last == nil {
			return a.fatal(fmt.Errorf("%s", i18n.T("launch.last_missing")))
		}
		instName, versionID = last.Instance, last.Version
		a.Log.Infof("%s", i18n.T("launch.last_used", instName))
		base = &store.Instance{
			Name:     instName,
			Version:  last.Version,
			Java:     last.Java,
			Xmx:      last.Xmx,
			Xms:      last.Xms,
			JvmArgs:  last.JvmArgs,
			GameArgs: last.GameArgs,
		}
	case len(f.args) > 0:
		instName = f.args[0]
		inst, ok := a.DB.GetInstance(instName)
		if !ok {
			return a.fatal(fmt.Errorf("%s", i18n.T("inst.not_found", instName)))
		}
		base = inst
		versionID = inst.Version
	default:
		return a.usageError("cli.missing_arg", "launch", "<name> | --last")
	}
	if base == nil {
		base = &store.Instance{Name: instName}
	}
	if v := f.str("version", ""); v != "" {
		versionID = v
	}
	if versionID == "" {
		return a.fatal(fmt.Errorf("%s", i18n.T("launch.no_source", instName)))
	}

	xmx := firstNonEmpty(f.str("xmx", ""), base.Xmx, a.DB.Cfg().DefaultXmx)
	xms := firstNonEmpty(f.str("xms", ""), base.Xms, a.DB.Cfg().DefaultXms)
	if err := launch.ValidateMemory(xmx); err != nil {
		return a.fatal(err)
	}
	if err := launch.ValidateMemory(xms); err != nil {
		return a.fatal(err)
	}
	jvmArgs := append(append([]string{}, base.JvmArgs...), f.lists["jvm-arg"]...)
	jvmArgs = append(jvmArgs, splitArgs(a.DB.Cfg().ExtraJvmArgs)...)
	gameArgs := append(append([]string{}, base.GameArgs...), f.lists["game-arg"]...)
	width, err := f.int("width", 0)
	if err != nil {
		return a.fatal(err)
	}
	height, err := f.int("height", 0)
	if err != nil {
		return a.fatal(err)
	}
	if (width == 0) != (height == 0) {
		return a.fatal(fmt.Errorf("%s", i18n.T("launch.resolution_pair")))
	}

	a.Log.Printf("%s", i18n.T("launch.preparing", instName, versionID))

	// --- version document ------------------------------------------------
	entry := mcv.ManifestVersion{}
	if man, err := a.ManifestStore().Get(false); err != nil {
		a.Log.Warnf("%s", err)
	} else if found, ok := man.Find(versionID); ok {
		entry = found
	} else {
		a.Log.Warnf("%s", i18n.T("mcv.unknown_version", versionID))
	}
	v, err := a.Loader(dryRun).Lookup(versionID, entry.URL)
	if err != nil {
		return a.fatal(err)
	}
	eff := v.Effective()
	for _, key := range unknownPlaceholders(eff, width, height) {
		a.Log.Warnf("%s", i18n.T("launch.unknown_placeholder", key))
	}

	// --- java ------------------------------------------------------------
	need := java.RequiredMajor(eff, versionID, a.Log)
	pinned := firstNonEmpty(f.str("java", ""), base.Java, a.DB.Cfg().JavaPath)
	rt, err := a.ensureJava(eff, versionID, need, pinned, dryRun)
	if err != nil {
		return a.fatal(err)
	}
	a.Log.Printf("%s", i18n.T("launch.java_cmd", rt.Path))

	// --- files -----------------------------------------------------------
	if _, err := a.Installer(dryRun).Ensure(eff); err != nil {
		return a.fatal(err)
	}

	// --- account ---------------------------------------------------------
	authBlock := mcv.Auth{
		PlayerName:  "Player",
		UUID:        strings.Repeat("0", 32),
		AccessToken: "0",
		ClientID:    auth.ClientID,
		UserType:    "msa",
		Properties:  "{}",
	}
	acc := a.findAccount(f.str("account", ""))
	if acc == nil {
		acc, _ = a.activeAccount()
	}
	switch {
	case acc != nil:
		if _, sess, err := a.session(acc); err != nil {
			return a.fatal(err)
		} else {
			authBlock = authFor(acc, sess)
		}
	case dryRun:
		a.Log.Warnf("%s", i18n.T("auth.no_account"))
	default:
		return a.fatal(fmt.Errorf("%s", i18n.T("auth.no_account")))
	}

	// --- command line ----------------------------------------------------
	cmd, err := launch.Prep(launch.Options{
		Root:            a.MinecraftDir(),
		VersionsDir:     a.DB.Cfg().VersionsDir,
		InstanceName:    instName,
		Version:         eff,
		VersionID:       versionID,
		Runtime:         *rt,
		Auth:            authBlock,
		Xmx:             xmx,
		Xms:             xms,
		ExtraJVMArgs:    jvmArgs,
		ExtraGameArgs:   gameArgs,
		Width:           width,
		Height:          height,
		LauncherName:    LauncherName,
		LauncherVersion: Version,
		Log:             a.Log,
		DryRun:          dryRun,
	})
	if err != nil {
		return a.fatal(err)
	}

	a.Log.Printf("%s", i18n.T("launch.memory", cmd.Xmx, cmd.Xms))
	a.Log.Printf("%s", i18n.T("launch.classpath_n", len(cmd.Classpath)))
	a.Log.Printf("%s", i18n.T("launch.natives", cmd.NativesDir))
	a.Log.Printf("%s", i18n.T("inst.game_dir", cmd.GameDir))

	// --- remember for --last ---------------------------------------------
	a.DB.Data.Last = &store.LastLaunch{
		Instance: instName,
		Version:  versionID,
		Java:     f.str("java", ""),
		Xmx:      xmx,
		Xms:      xms,
		JvmArgs:  jvmArgs,
		GameArgs: gameArgs,
		GameDir:  cmd.GameDir,
		At:       time.Now(),
	}
	if inst, ok := a.DB.GetInstance(instName); ok {
		inst.LastPlayed = time.Now()
		if inst.Version == "" {
			inst.Version = versionID
		}
	}
	if !dryRun {
		if err := a.DB.Save(); err != nil {
			a.Log.Warnf("%s", err)
		} else {
			a.Log.Debugf("%s", i18n.T("launch.saved_args"))
		}
	}

	if dryRun {
		a.Log.Printf("%s", i18n.T("launch.dry_run"))
		a.Log.Printf("%s", i18n.T("launch.cmdline", cmd.CommandLine()))
		return 0
	}

	// --- run -------------------------------------------------------------
	gameLogPath := filepath.Join(a.Paths.LogsDir(),
		fmt.Sprintf("game-%s-%s.log", instName, time.Now().Format("20060102-150405")))
	gameLog, err := os.OpenFile(gameLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return a.fatal(err)
	}
	defer gameLog.Close()
	a.Log.Printf("%s", i18n.T("launch.log_file", gameLogPath))
	a.Log.Debugf("%s", i18n.T("launch.cmdline", cmd.CommandLine()))

	wait := f.boolean("wait") || !a.DB.Cfg().CloseAfterLaunch
	a.Log.Printf("%s", i18n.T("launch.spawning"))
	pid, code, err := cmd.Run(wait, gameLog)
	if err != nil {
		return a.fatal(fmt.Errorf("%s", i18n.T("launch.failed", err)))
	}
	if wait {
		if code == 0 {
			a.Log.Printf("%s", i18n.T("launch.exited", code))
			return 0
		}
		a.Log.Printf("%s", i18n.T("launch.exit_fail", code))
		return code
	}
	a.Log.Printf("%s", i18n.T("launch.started", pid))
	return 0
}

// ensureJava picks a runtime for the required major, offering to download one
// when the machine has none.
func (a *App) ensureJava(v *mcv.Version, versionID string, need int, pinned string, dryRun bool) (*java.Runtime, error) {
	cfg := a.DB.Cfg()
	if pinned != "" {
		rt, err := java.Probe(pinned, "config")
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("java.pinned", pinned))
		}
		if rt.Major < need {
			return nil, fmt.Errorf("%s", i18n.T("java.too_old", pinned, rt.Major, versionID, need))
		}
		return &rt, nil
	}
	runtimes := java.ScanRuntimes(cfg, a.Paths, a.Log)
	if rt := java.Pick(runtimes, need); rt != nil {
		a.Log.Debugf("%s", i18n.T("java.selected", rt.Path, rt.Major))
		return rt, nil
	}

	a.Log.Printf("%s", i18n.T("java.none_found", versionID, need))
	if dryRun {
		return &java.Runtime{Path: java.ExecutableName(), Major: need, Version: "unknown", Source: "placeholder"}, nil
	}
	if !a.Yeseverything {
		ok, err := a.prompt(i18n.T("java.offer_download", need, cfg.JavaDir))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s", i18n.T("launch.no_java"))
		}
	}
	component := ""
	if v.JavaVersion != nil {
		component = v.JavaVersion.Component
	}
	exe, err := a.JavaInstaller().Install(component, need)
	if err != nil {
		a.Log.Warnf("%s", i18n.T("java.install_failed", err))
		exe, err = a.JavaInstaller().InstallFromArchive(need)
		if err != nil {
			return nil, err
		}
	}
	rt, err := java.Probe(exe, "mcli")
	if err != nil {
		return nil, err
	}
	a.Log.Printf("%s", i18n.T("java.extracted", need, filepath.Dir(filepath.Dir(exe))))
	return &rt, nil
}

// unknownPlaceholders reports placeholders a version uses that mcli does not
// fill, so forward-compatibility drift is visible instead of silent.
func unknownPlaceholders(v *mcv.Version, width, height int) []string {
	spec := mcv.Spec{
		Version: v,
		Env:     mcv.EnvCurrent(mcv.FeatureSet(width > 0 && height > 0, nil)),
		Auth: mcv.Auth{
			PlayerName: "x", UUID: "x", AccessToken: "x", XUID: "x", ClientID: "x",
			UserType: "msa", Properties: "{}", Session: "x",
		},
		Paths: mcv.Paths{
			GameDir: "x", AssetsRoot: "x", AssetsIndex: v.Assets, NativesDir: "x",
			LibraryDir: "x", Classpath: "x", Separator: string(filepath.ListSeparator),
			LauncherName: "x", LauncherVersion: "x", VersionName: v.ID, VersionType: v.Type,
			LogConfig: "x", ResolutionWidth: width, ResolutionHeight: height,
		},
	}
	seen := map[string]bool{}
	var out []string
	collect := func(list []mcv.ArgValue) {
		for _, arg := range list {
			if !mcv.Allowed(arg.Rules, spec.Env) {
				continue
			}
			for _, raw := range arg.Value {
				_, unknown := mcv.Substitute(raw, spec.Placeholders())
				for _, u := range unknown {
					if !seen[u] {
						seen[u] = true
						out = append(out, u)
					}
				}
			}
		}
	}
	if v.Arguments != nil {
		collect(v.Arguments.JVM)
		collect(v.Arguments.Game)
	}
	if v.Logging != nil && v.Logging.Client != nil {
		_, unknown := mcv.Substitute(v.Logging.Client.Argument, map[string]string{"path": "x"})
		for _, u := range unknown {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitArgs splits a config string into shell-ish words without needing a
// shell: quotes are honoured, no expansion happens.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := rune(0)
	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t' || r == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
