// Package java finds, matches and installs Java runtimes.
//
// Two sources:
//   - local runtimes (mcli's own jdk dir, JAVA_HOME, PATH, distro JVM dirs,
//     the official launcher's ~/.minecraft/runtime tree)
//   - Mojang's java-runtime product manifest, which the official launcher also
//     uses; it is reachable through BMCLAPI as well as from Mojang directly
//
// Matching follows the launcher convention: a Minecraft version needs at least
// the major version named in its javaVersion block, and the smallest local
// runtime that satisfies it wins.
package java

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/config"
	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
	"github.com/Kailoinf/MCL-CLI/internal/paths"
)

// Runtime is one usable java executable.
type Runtime struct {
	Path    string
	Major   int
	Version string
	Source  string // config | JAVA_HOME | PATH | mcli | system | launcher
}

// RuntimeManifestProduct is the launcher product manifest for java runtimes.
const RuntimeManifestProduct = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

// componentMajors maps Mojang's runtime component names to Java majors.
var componentMajors = []struct {
	Name  string
	Major int
}{
	{"jre-legacy", 8},
	{"java-runtime-alpha", 16},
	{"java-runtime-beta", 17},
	{"java-runtime-gamma", 17},
	{"java-runtime-delta", 21},
	{"java-runtime-epsilon", 25},
}

// MajorForComponent returns the Java major of a runtime component name.
func MajorForComponent(component string) int {
	for _, c := range componentMajors {
		if c.Name == component {
			return c.Major
		}
	}
	return 0
}

// ComponentForMajor picks the component that satisfies a major requirement,
// preferring the smallest component that is new enough.
func ComponentForMajor(need int) string {
	best := ""
	bestMajor := 0
	fallback := ""
	fallbackMajor := 0
	for _, c := range componentMajors {
		if c.Major >= need && (best == "" || c.Major < bestMajor) {
			best, bestMajor = c.Name, c.Major
		}
		if c.Major > fallbackMajor {
			fallback, fallbackMajor = c.Name, c.Major
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// ---------------------------------------------------------------------------
// discovery

// ScanRuntimes lists every java the machine offers, newest-major first.
func ScanRuntimes(cfg *config.Config, p paths.Paths, log *logx.Logger) []Runtime {
	var candidates []struct {
		path   string
		source string
	}
	add := func(path, source string) {
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		for _, c := range candidates {
			if c.path == abs {
				return
			}
		}
		candidates = append(candidates, struct {
			path   string
			source string
		}{abs, source})
	}

	if cfg.JavaPath != "" {
		add(cfg.JavaPath, "config")
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		add(filepath.Join(home, "bin", javaExe()), "JAVA_HOME")
	}
	if lp, err := exec.LookPath(javaExe()); err == nil {
		add(lp, "PATH")
	}
	// mcli's own runtimes
	if p.JDKDir() != "" {
		for _, bin := range globJava(filepath.Join(p.JDKDir(), "*")) {
			add(bin, "mcli")
		}
		for _, bin := range globJava(filepath.Join(p.JDKDir(), "*", "*", "*")) {
			add(bin, "mcli")
		}
	}
	// the official launcher's runtime tree
	if home, err := os.UserHomeDir(); err == nil {
		for _, bin := range globJava(filepath.Join(home, ".minecraft", "runtime", "*", "*", "*")) {
			add(bin, "launcher")
		}
	}
	switch runtime.GOOS {
	case "linux":
		for _, pattern := range []string{
			"/usr/lib/jvm/*", "/usr/lib64/jvm/*", "/usr/java/*",
			"/opt/java*", "/opt/jdk*", "/opt/*/jdk*", "/usr/local/java*",
			"/var/lib/flatpak/app/*/current/active/files/jvm/*",
		} {
			for _, bin := range globJava(pattern) {
				add(bin, "system")
			}
		}
	case "darwin":
		for _, bin := range globJava("/Library/Java/JavaVirtualMachines/*/Contents/Home") {
			add(bin, "system")
		}
		for _, bin := range globJava("/System/Library/Java/JavaVirtualMachines/*/Contents/Home") {
			add(bin, "system")
		}
	case "windows":
		roots := []string{
			`C:\Program Files\Java\*`,
			`C:\Program Files (x86)\Java\*`,
			`C:\Program Files\Eclipse Adoptium\*`,
			`C:\Program Files\Microsoft\jdk*`,
			`C:\Program Files\Amazon Corretto\*`,
			`C:\Program Files\BellSoft\*`,
			`C:\Program Files\Zulu\*`,
			`C:\Program Files\Semeru\*`,
		}
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			roots = append(roots,
				filepath.Join(lad, `Programs\Eclipse Adoptium\*`),
				filepath.Join(lad, `Programs\Microsoft\jdk*`))
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			roots = append(roots, filepath.Join(ad, `.minecraft\runtime\*\*\*`))
		}
		for _, pattern := range roots {
			for _, bin := range globJava(pattern) {
				add(bin, "system")
			}
		}
	}

	log.Debugf("%s", i18n.T("java.scan_start"))
	out := make([]Runtime, 0, len(candidates))
	for _, c := range candidates {
		rt, err := Probe(c.path, c.source)
		if err != nil {
			log.Debugf("%s", i18n.T("java.bad_version", c.path, err))
			continue
		}
		out = append(out, rt)
		log.Debugf("%s", i18n.T("java.found_local", rt.Version, rt.Major, rt.Path))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Major != out[j].Major {
			return out[i].Major < out[j].Major
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func javaExe() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// ExecutableName is the platform's java binary file name.
func ExecutableName() string { return javaExe() }

// globJava expands a pattern and returns any java executables inside.
func globJava(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range matches {
		bin := filepath.Join(m, "bin", javaExe())
		if st, err := os.Stat(bin); err == nil && !st.IsDir() {
			out = append(out, bin)
			continue
		}
		// pattern already pointed at a bin directory
		bin = filepath.Join(m, javaExe())
		if st, err := os.Stat(bin); err == nil && !st.IsDir() {
			out = append(out, bin)
		}
	}
	return out
}

var versionRe = regexp.MustCompile(`version "([^"]+)"`)

// Probe runs `<java> -version` and extracts major + full version.
func Probe(path, source string) (Runtime, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Runtime{}, fmt.Errorf("%s", i18n.T("java.exec_missing", path))
	}
	if st.IsDir() {
		return Runtime{}, fmt.Errorf("%s", i18n.T("java.exec_missing", path))
	}
	cmd := exec.Command(path, "-version")
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil && text == "" {
		return Runtime{}, err
	}
	m := versionRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return Runtime{}, fmt.Errorf("unparsable java -version output")
	}
	full := m[1]
	return Runtime{Path: path, Major: ParseMajor(full), Version: full, Source: source}, nil
}

// ParseMajor turns a java version string into its major number: "1.8.0_401"
// means 8, "21.0.5" means 21.
func ParseMajor(v string) int {
	if strings.HasPrefix(v, "1.") {
		rest := strings.TrimPrefix(v, "1.")
		if i := strings.IndexAny(rest, "._-"); i > 0 {
			rest = rest[:i]
		}
		n, _ := strconv.Atoi(rest)
		return n
	}
	head := v
	if i := strings.IndexAny(head, "._-+"); i > 0 {
		head = head[:i]
	}
	n, _ := strconv.Atoi(head)
	return n
}

// Pick chooses a runtime for a required major: exact match first, then the
// smallest runtime that is new enough.
func Pick(list []Runtime, need int) *Runtime {
	if len(list) == 0 {
		return nil
	}
	var exact, newer *Runtime
	for i := range list {
		rt := &list[i]
		if rt.Major == need && (exact == nil || rt.Source == "mcli") {
			exact = rt
			continue
		}
		if rt.Major > need && (newer == nil || rt.Major < newer.Major) {
			newer = rt
		}
	}
	if exact != nil {
		return exact
	}
	return newer
}

// RequiredMajor reports which Java major a Minecraft version needs. The
// version JSON is authoritative; older documents predate the field, so a
// table keyed by the version id covers them.
func RequiredMajor(v *mcv.Version, versionID string, log *logx.Logger) int {
	if v != nil && v.JavaVersion != nil {
		if v.JavaVersion.MajorVersion > 0 {
			return v.JavaVersion.MajorVersion
		}
		if m := MajorForComponent(v.JavaVersion.Component); m > 0 {
			return m
		}
	}
	if need, ok := majorFromVersionID(versionID); ok {
		return need
	}
	if log != nil {
		log.Warnf("%s", i18n.T("java.assumed_major", 21))
	}
	return 21
}

func majorFromVersionID(id string) (int, bool) {
	m := regexp.MustCompile(`^1\.(\d+)(?:\.(\d+))?`).FindStringSubmatch(id)
	if len(m) >= 2 {
		minor, _ := strconv.Atoi(m[1])
		patch := 0
		if len(m) >= 3 && m[2] != "" {
			patch, _ = strconv.Atoi(m[2])
		}
		switch {
		case minor < 17:
			return 8, true
		case minor == 17:
			return 16, true
		case minor == 18 || minor == 19:
			return 17, true
		case minor == 20:
			if patch < 5 {
				return 17, true
			}
			return 21, true
		default:
			return 21, true
		}
	}
	// old alpha/beta/rd snapshots predate Java metadata entirely
	if strings.HasPrefix(id, "a") || strings.HasPrefix(id, "b") || strings.HasPrefix(id, "rd") || strings.HasPrefix(id, "c") {
		return 8, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// installation

// Installer downloads a runtime from Mojang's product manifest (mirrored by
// BMCLAPI) into the mcli jdk directory.
type Installer struct {
	Mapper mirror.Mapper
	Policy dl.Policy
	Log    *logx.Logger
	// DestRoot is where runtimes land, e.g. ~/.mcl-cli/jdk.
	DestRoot string
	// Workers is the parallel download count.
	Workers int
	// Confirm is asked once before the (large) runtime download.
	Confirm func(desc string, files int, total int64) (bool, error)
}

// productEntry is one downloadable runtime in Mojang's product manifest.
type productEntry struct {
	Manifest struct {
		SHA1 string `json:"sha1"`
		Size int64  `json:"size"`
		URL  string `json:"url"`
	} `json:"manifest"`
	Version struct {
		Name     string    `json:"name"`
		Released time.Time `json:"released"`
	} `json:"version"`
}

type productManifest map[string]map[string][]productEntry

type runtimeManifest struct {
	Files map[string]struct {
		Type       string `json:"type"` // file | directory | link
		Target     string `json:"target"`
		Executable bool   `json:"executable"`
		Downloads  struct {
			Raw *struct {
				SHA1 string `json:"sha1"`
				Size int64  `json:"size"`
				URL  string `json:"url"`
			} `json:"raw"`
		} `json:"downloads"`
	} `json:"files"`
}

// PlatformKey maps GOOS/GOARCH to the key used by the product manifest.
func PlatformKey() (string, bool) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux", true
		case "386":
			return "linux-i386", true
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "windows-x64", true
		case "386":
			return "windows-x86", true
		case "arm64":
			return "windows-arm64", true
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "mac-os", true
		case "arm64":
			return "mac-os-arm64", true
		}
	}
	return "", false
}

// Install ensures the runtime for a component exists under DestRoot and returns
// the java executable path. When the component is unknown for this platform it
// returns an error the CLI turns into an offer to use a local runtime.
func (in Installer) Install(component string, need int) (string, error) {
	platform, ok := PlatformKey()
	if !ok {
		return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if component == "" {
		component = ComponentForMajor(need)
	}
	dest := filepath.Join(in.DestRoot, component)
	exe := filepath.Join(dest, "bin", javaExe())
	if st, err := os.Stat(exe); err == nil && !st.IsDir() {
		return exe, nil
	}

	if err := os.MkdirAll(in.DestRoot, 0o755); err != nil {
		return "", err
	}
	all, err := in.fetchProduct()
	if err != nil {
		return "", err
	}
	plat, ok := all[platform]
	if !ok {
		return "", fmt.Errorf("no java runtimes published for platform %s", platform)
	}
	entries, ok := plat[component]
	if !ok || len(entries) == 0 {
		// fall back to any component that satisfies the requirement
		alt := in.pickComponent(plat, need)
		if alt == "" {
			return "", fmt.Errorf("no java runtime published for %s needing major %d", platform, need)
		}
		component = alt
		entries = plat[component]
		dest = filepath.Join(in.DestRoot, component)
		exe = filepath.Join(dest, "bin", javaExe())
	}

	version := entries[0].Version.Name
	in.log().Infof("%s", i18n.T("java.downloading", need, version))
	man, err := in.fetchRuntimeManifest(entries[0].Manifest.URL)
	if err != nil {
		return "", err
	}

	items, links, dirs, total, execs := in.plan(man, dest)
	if in.Confirm != nil {
		ok, err := in.Confirm(component+" "+version, len(items), total)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%s", i18n.T("dl.declined", component))
		}
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	if err := dl.FetchAll(in.Policy, items, in.Workers, in.log()); err != nil {
		return "", fmt.Errorf("%s", i18n.T("java.install_failed", err))
	}
	for _, e := range execs {
		if err := os.Chmod(e, 0o755); err != nil {
			return "", err
		}
	}
	if err := applyLinks(links); err != nil {
		return "", err
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return "", fmt.Errorf("%s", i18n.T("java.install_failed", "java executable missing after extraction"))
	}
	in.log().Infof("%s", i18n.T("java.extracted", need, dest))
	return exe, nil
}

type linkJob struct{ target, path string }

func (in Installer) plan(man *runtimeManifest, dest string) (items []dl.Item, links []linkJob, dirs []string, total int64, execs []string) {
	names := make([]string, 0, len(man.Files))
	for name := range man.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := man.Files[name]
		rel := filepath.FromSlash(strings.TrimPrefix(name, "/"))
		path := filepath.Join(dest, rel)
		switch f.Type {
		case "directory":
			dirs = append(dirs, path)
		case "link":
			links = append(links, linkJob{target: f.Target, path: path})
		default:
			if f.Downloads.Raw == nil || f.Downloads.Raw.URL == "" {
				continue
			}
			items = append(items, dl.Item{
				Candidates: in.Mapper.Candidates(f.Downloads.Raw.URL),
				Dest:       path,
				SHA1:       f.Downloads.Raw.SHA1,
				Size:       f.Downloads.Raw.Size,
				Desc:       "jdk " + rel,
			})
			total += f.Downloads.Raw.Size
			if f.Executable {
				execs = append(execs, path)
			}
		}
	}
	return items, links, dirs, total, execs
}

func (in Installer) fetchProduct() (productManifest, error) {
	data, _, err := in.Policy.FetchBytes(in.Mapper.Candidates(RuntimeManifestProduct), "java runtime index")
	if err != nil {
		return nil, err
	}
	all := productManifest{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	return all, nil
}

func (in Installer) fetchRuntimeManifest(url string) (*runtimeManifest, error) {
	data, _, err := in.Policy.FetchBytes(in.Mapper.Candidates(url), "java runtime manifest")
	if err != nil {
		return nil, err
	}
	man := &runtimeManifest{}
	if err := json.Unmarshal(data, man); err != nil {
		return nil, err
	}
	return man, nil
}

func (in Installer) pickComponent(plat map[string][]productEntry, need int) string {
	best, bestMajor := "", 0
	fallback, fallbackMajor := "", 0
	for name := range plat {
		major := MajorForComponent(name)
		if major == 0 && len(plat[name]) > 0 {
			major = ParseMajor(plat[name][0].Version.Name)
		}
		if major >= need && (best == "" || major < bestMajor) {
			best, bestMajor = name, major
		}
		if major > fallbackMajor {
			fallback, fallbackMajor = name, major
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// applyLinks recreates symlinks, replacing whatever is there.
func applyLinks(links []linkJob) error {
	for _, l := range links {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
			return err
		}
		_ = os.Remove(l.path)
		if err := os.Symlink(l.target, l.path); err != nil {
			// Windows without developer mode cannot symlink; copy instead.
			if data, err2 := os.ReadFile(filepath.Join(filepath.Dir(l.path), l.target)); err2 == nil {
				if err3 := os.WriteFile(l.path, data, 0o755); err3 == nil {
					continue
				}
			}
			return fmt.Errorf("cannot create %s -> %s: %v", l.path, l.target, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Adoptium fallback (tar.gz single archive)

// AdoptiumBinaryURL builds an Adoptium download URL for a major version.
func AdoptiumBinaryURL(major int) (string, error) {
	osPart := map[string]string{"linux": "linux", "windows": "windows", "darwin": "mac"}[runtime.GOOS]
	archPart := map[string]string{"amd64": "x64", "386": "x32", "arm64": "aarch64"}[runtime.GOARCH]
	if osPart == "" || archPart == "" {
		return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("%s/%d/ga/%s/%s/jdk/hotspot/normal/eclipse",
		strings.TrimRight(mirror.AdoptiumURL, "/"), major, osPart, archPart), nil
}

// InstallFromArchive downloads and unpacks a tar.gz runtime (Adoptium style),
// returning the java executable path. It exists as a fallback for platforms
// Mojang does not publish for.
func (in Installer) InstallFromArchive(major int) (string, error) {
	url, err := AdoptiumBinaryURL(major)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(in.DestRoot, fmt.Sprintf("temurin-%d", major))
	if exe, ok := findJavaUnder(dest); ok {
		return exe, nil
	}
	archive := filepath.Join(os.TempDir(), fmt.Sprintf("mcli-jdk-%d.tar.gz", major))
	if err := in.Policy.Fetch(dl.Item{
		Candidates: []mirror.Candidate{{URL: url, Source: "adoptium"}},
		Dest:       archive,
		Desc:       fmt.Sprintf("jdk %d archive", major),
	}); err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := extractTarGz(archive, dest); err != nil {
		return "", err
	}
	if exe, ok := findJavaUnder(dest); ok {
		return exe, nil
	}
	return "", fmt.Errorf("%s", i18n.T("java.install_failed", "java executable missing in archive"))
}

func findJavaUnder(root string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(root, "*", "bin", javaExe()))
	if len(matches) > 0 {
		return matches[0], true
	}
	candidate := filepath.Join(root, "bin", javaExe())
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, true
	}
	return "", false
}

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(strings.TrimPrefix(hdr.Name, "/"))
		if strings.HasPrefix(name, "..") {
			continue
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
}

func (in Installer) log() *logx.Logger {
	if in.Log != nil {
		return in.Log
	}
	return logx.Discard()
}
