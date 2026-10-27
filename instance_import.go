package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/store"
)

// ---------------------------------------------------------------------------
// taking over an existing .minecraft
//
// A launcher is judged by what it does with the machine it lands on: people
// already have .minecraft/versions filled by the official launcher, PCL, HMCL
// and modded profiles. Those directories are versions, not instances, so mcli
// has to be able to see them (scan) and adopt them (import) before it can be
// called usable.

// diskVersion is one directory under versions/.
type diskVersion struct {
	ID      string
	Type    string // version json "type", empty when unknown
	Parent  string // inheritsFrom, empty for plain vanilla
	HasJSON bool
	HasJar  bool
	Dir     string
}

// scanDisk lists the version directories on disk. It reads only local files:
// no network, so it works on a fully installed machine that is offline.
func (a *App) scanDisk() ([]diskVersion, error) {
	dir := a.VersionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]diskVersion, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		v := diskVersion{ID: id, Dir: filepath.Join(dir, id)}
		if raw, err := os.ReadFile(filepath.Join(v.Dir, id+".json")); err == nil {
			v.HasJSON = true
			head := struct {
				Type         string `json:"type"`
				InheritsFrom string `json:"inheritsFrom"`
			}{}
			if json.Unmarshal(raw, &head) == nil {
				v.Type, v.Parent = head.Type, head.InheritsFrom
			}
		}
		if _, err := os.Stat(filepath.Join(v.Dir, id+".jar")); err == nil {
			v.HasJar = true
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// instanceOfVersion returns the instance currently bound to a version id.
func (a *App) instanceOfVersion(id string) (*store.Instance, bool) {
	for _, name := range a.Store.InstanceNames() {
		inst := a.Store.Data.Instances[name]
		if inst.Version == id || inst.Name == id {
			return inst, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// scan

func (a *App) instanceScan(args []string) int {
	f, err := parseFlags("instance scan", args, map[string]flagSpec{
		"all": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("inst.scan_usage"))
		return 0
	}
	if len(f.args) > 0 {
		return a.usageError("cli.unexpected_arg", f.args[0], "instance scan")
	}
	disk, err := a.scanDisk()
	if err != nil {
		return a.fatal(err)
	}
	if len(disk) == 0 {
		a.Log.Printf("%s", i18n.T("inst.scan_none", a.VersionsDir()))
		return 0
	}

	all := f.boolean("all")
	a.Log.Printf("%s", i18n.T("inst.scan_header",
		padRight("VERSION", 34), padRight("TYPE", 10), padRight("JSON", 5),
		padRight("JAR", 5), padRight("PARENT", 22), "INSTANCE"))
	shown, registered, unregistered := 0, 0, 0
	for _, d := range disk {
		inst, known := a.instanceOfVersion(d.ID)
		if known {
			registered++
		} else if d.HasJSON {
			unregistered++
		}
		if !all && known {
			continue
		}
		shown++
		typeLabel := d.Type
		if typeLabel == "" {
			typeLabel = i18n.T("common.unknown")
		}
		a.Log.Printf("%s", i18n.T("inst.scan_row",
			padRight(d.ID, 34), padRight(typeLabel, 10),
			padRight(mark(d.HasJSON), 5), padRight(mark(d.HasJar), 5),
			padRight(dash(d.Parent), 22), instanceLabel(inst, known)))
	}
	if shown == 0 {
		a.Log.Printf("%s", i18n.T("inst.scan_all_registered", len(disk)))
		return 0
	}
	a.Log.Printf("%s", i18n.T("inst.scan_summary", len(disk), registered, unregistered))
	if unregistered > 0 {
		a.Log.Printf("%s", i18n.T("inst.scan_hint"))
	}
	return 0
}

func mark(ok bool) string {
	if ok {
		return i18n.T("inst.mark_yes")
	}
	return i18n.T("inst.mark_no")
}

func instanceLabel(inst *store.Instance, known bool) string {
	if !known {
		return "-"
	}
	if inst.Name == inst.Version {
		return inst.Name
	}
	return fmt.Sprintf("%s (%s)", inst.Name, inst.Version)
}

// ---------------------------------------------------------------------------
// import

func (a *App) instanceImport(args []string) int {
	f, err := parseFlags("instance import", args, map[string]flagSpec{
		"as":    {takesValue: true},
		"force": {},
	})
	if err != nil {
		return a.fatal(err)
	}
	if f.boolean("help") {
		a.Log.Printf("%s", i18n.T("inst.import_usage"))
		return 0
	}
	if len(f.args) == 0 {
		return a.usageError("cli.missing_arg", "instance import", "<version> [--as <name>]")
	}
	if len(f.args) > 1 {
		return a.usageError("cli.unexpected_arg", f.args[1], "instance import")
	}

	version := f.args[0]
	name := f.str("as", "")
	if name == "" {
		name = version
	}
	if err := validateInstanceName(name); err != nil {
		return a.fatal(err)
	}
	if existing, ok := a.Store.GetInstance(name); ok && !f.boolean("force") {
		if existing.Version == version {
			a.Log.Printf("%s", i18n.T("inst.import_already", name, version))
			return 0
		}
		return a.fatal(fmt.Errorf("%s", i18n.T("inst.exists", name)))
	}

	// Either the version is already on disk (adopt it) or it can be installed
	// from the manifest by id later (register it now).
	local := false
	if _, err := os.Stat(filepath.Join(a.VersionsDir(), version, version+".json")); err == nil {
		local = true
	} else {
		resolved, err := a.resolveVersion(version)
		if err != nil {
			return a.fatal(err)
		}
		version = resolved
		if err := a.knownVersion(version); err != nil {
			return a.fatal(err)
		}
	}

	a.Store.Data.Instances[name] = &store.Instance{
		Name:      name,
		Version:   version,
		CreatedAt: time.Now(),
	}
	if err := a.Store.Save(); err != nil {
		return a.fatal(err)
	}
	source := i18n.T("inst.import_source_remote")
	if local {
		source = i18n.T("inst.import_source_local")
	}
	a.Log.Printf("%s", i18n.T("inst.import_ok", version, name, source))
	if name != version {
		a.Log.Printf("%s", i18n.T("inst.import_dir_note", version,
			filepath.Join(a.VersionsDir(), version), name, a.instanceDir(name)))
	} else {
		a.Log.Printf("%s", i18n.T("inst.game_dir", a.instanceDir(name)))
	}
	a.Log.Printf("%s", i18n.T("inst.import_next", name, name))
	return 0
}

// adoptUnregistered registers every version directory that is a real version (it has
// a JSON) and is not an instance yet. Files stay where they are — taking over an
// install another launcher made must not move anything — and the return value names
// what was adopted so the caller can say it out loud instead of changing state
// quietly.
func (a *App) adoptUnregistered() []string {
	disk, err := a.scanDisk()
	if err != nil {
		return nil
	}
	adopted := make([]string, 0, len(disk))
	for _, d := range disk {
		if !d.HasJSON {
			continue
		}
		if _, ok := a.Store.GetInstance(d.ID); ok {
			continue
		}
		if err := validateInstanceName(d.ID); err != nil {
			continue
		}
		if _, ok := a.instanceForVersion(d.ID); ok {
			continue
		}
		a.Store.Data.Instances[d.ID] = &store.Instance{
			Name:      d.ID,
			Version:   d.ID,
			CreatedAt: time.Now(),
		}
		adopted = append(adopted, d.ID)
	}
	if len(adopted) > 0 {
		if err := a.Store.Save(); err != nil {
			a.Log.Warnf("%s", err)
			return nil
		}
	}
	return adopted
}

// instanceForVersion answers whether some instance already points at this version,
// which is the other reason not to register it twice.
func (a *App) instanceForVersion(version string) (*store.Instance, bool) {
	for _, inst := range a.Store.Data.Instances {
		if inst.Version == version {
			return inst, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// name validation

// validateInstanceName accepts what the other launchers accept, because those
// names are already on people's disks: "26.3-Fabric 0.20.1" is a perfectly
// normal version directory. Rejected: path separators, the characters Windows
// forbids, control characters, a leading dash or dot, a trailing dot, the
// reserved device names, and anything over 64 characters.
func validateInstanceName(name string) error {
	bad := func() error { return fmt.Errorf("%s", i18n.T("inst.bad_name", name)) }
	if name == "" || len([]rune(name)) > 64 || strings.TrimSpace(name) != name {
		return bad()
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "-") || strings.HasPrefix(name, ".") {
		return bad()
	}
	if strings.HasSuffix(name, ".") {
		return bad()
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return bad()
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return bad()
		}
	}
	base := strings.ToUpper(name)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return bad()
	}
	return nil
}
