// Package launch turns a prepared version + java runtime + account into a real
// process: it assembles the classpath, fills in the ${...} placeholders,
// applies memory flags and starts java.
package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

// Options describe one launch.
type Options struct {
	// Root is the .minecraft directory.
	Root        string
	VersionsDir string
	// InstanceName names the isolated game directory.
	InstanceName string
	// Version is the effective (merged) version document.
	Version *mcv.Version
	// VersionID is the directory name holding the version files.
	VersionID string
	Runtime   java.Runtime

	Auth mcv.Auth

	Xmx string
	Xms string
	// MemoryPercent is the share of available memory used when sizing the heap
	// automatically (config `auto_memory_percent`).
	MemoryPercent int
	// PathSeparator joins the classpath. Empty means the platform's own separator
	// (":" everywhere but Windows, where Java wants ";"). It is settable so that
	// the Windows shape can be tested from any platform.
	PathSeparator string
	ExtraJVMArgs  []string
	ExtraGameArgs []string
	// Server asks the game to join "host[:port]" right away — either with the
	// quick play arguments a modern version JSON declares, or with the classic
	// --server/--port pair on an old one. Fullscreen and Demo are the same kind
	// of one-off switch: they are never stored with the instance.
	Server     string
	Fullscreen bool
	Demo       bool

	Width  int
	Height int

	LauncherName    string
	LauncherVersion string

	Log         *logx.Logger
	DryRun      bool
	Wait        bool
	GameLogFile string
}

// Command is a fully built java invocation.
type Command struct {
	Java       string
	Args       []string
	GameDir    string
	JarDir     string
	NativesDir string
	Classpath  []string
	Version    string
	Xmx        string
	Xms        string
}

// Prep builds the command line for a launch.
func Prep(o Options) (*Command, error) {
	v := o.Version
	if v == nil {
		return nil, fmt.Errorf("%s", i18n.T("launch.no_source", o.InstanceName))
	}
	versionsDir := filepath.Join(o.Root, o.VersionsDir)
	libsDir := filepath.Join(o.Root, "libraries")
	assetsRoot := filepath.Join(o.Root, "assets")
	// Version files live under the version's own directory, not the instance's:
	// `mcli launch demo --version 1.20.1` must use the 1.20.1 files, and a
	// profile with an inherited jar (Fabric) reuses the parent's copy.
	nativesDir := filepath.Join(versionsDir, v.ID, "natives")
	_, clientJar := mcv.ClientJarTarget(o.Root, o.VersionsDir, v)
	gameDir := filepath.Join(versionsDir, o.InstanceName)
	if o.InstanceName == "" {
		gameDir = filepath.Join(o.Root, "game")
	}

	// quick play / demo are feature switches: the version's JSON decides which
	// arguments they unlock, so they have to be set before anything else is built.
	host, port, _ := SplitServer(o.Server)
	address := ServerAddress(o.Server)
	features := map[string]bool{}
	if o.Server != "" {
		// Only the multiplayer switch: has_quick_plays_support would also unlock
		// --quickPlayPath, and mcli has no quick play file to hand the client.
		features["is_quick_play_multiplayer"] = true
	}
	if o.Demo {
		features["is_demo_user"] = true
	}
	env := mcv.EnvCurrent(mcv.FeatureSet(o.Width > 0 && o.Height > 0, features))

	cpEntries := mcv.ClasspathEntries(v, env, libsDir, clientJar)
	sep := o.PathSeparator
	if sep == "" {
		sep = string(filepath.ListSeparator)
	}
	cp := mcv.ClasspathString(cpEntries, sep)

	logConfig := ""
	if v.Logging != nil && v.Logging.Client != nil {
		logConfig = filepath.Join(assetsRoot, "log_configs", v.Logging.Client.File.ID)
	}

	spec := mcv.Spec{
		Version: v,
		Env:     env,
		Auth:    o.Auth,
		Paths: mcv.Paths{
			GameDir:              gameDir,
			AssetsRoot:           assetsRoot,
			AssetsIndex:          v.Assets,
			NativesDir:           nativesDir,
			LibraryDir:           libsDir,
			Classpath:            cp,
			Separator:            sep,
			LauncherName:         o.LauncherName,
			LauncherVersion:      o.LauncherVersion,
			VersionName:          v.ID,
			VersionType:          v.Type,
			LogConfig:            logConfig,
			ResolutionWidth:      o.Width,
			ResolutionHeight:     o.Height,
			QuickPlayMultiplayer: address,
		},
	}

	xmx, xms := o.Xmx, o.Xms
	if xmx == "" || xms == "" {
		axmx, axms := RecommendMemory(o.MemoryPercent)
		if xmx == "" {
			xmx = axmx
		}
		if xms == "" {
			xms = axms
		}
	}

	jvm := []string{"-Xmx" + xmx, "-Xms" + xms}
	jvm = append(jvm, o.ExtraJVMArgs...)
	if logArg, ok := spec.LoggingArg(); ok {
		// keep the logging config next to the other system properties
		jsonArgs := spec.JVMArgs()
		inserted := false
		for i, a := range jsonArgs {
			if a == "-cp" || a == "-classpath" {
				jsonArgs = append(jsonArgs[:i], append([]string{logArg}, jsonArgs[i:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			jsonArgs = append(jsonArgs, logArg)
		}
		jvm = append(jvm, jsonArgs...)
	} else {
		jvm = append(jvm, spec.JVMArgs()...)
	}

	args := append([]string{}, jvm...)
	args = append(args, v.MainClass)
	game := spec.GameArgs()
	args = append(args, game...)
	// The quick play arguments belong to the version's own JSON (gated by
	// is_quick_play_multiplayer) and are filled in above; versions that predate
	// them only understand --server/--port, so they get the pair by hand.
	if host != "" && !slices.Contains(game, "--quickPlayMultiplayer") {
		args = append(args, "--server", host, "--port", port)
	}
	if o.Demo && !slices.Contains(game, "--demo") {
		args = append(args, "--demo")
	}
	// No version JSON declares --fullscreen, although every client accepts it.
	if o.Fullscreen {
		args = append(args, "--fullscreen")
	}
	args = append(args, o.ExtraGameArgs...)

	return &Command{
		Java:       o.Runtime.Path,
		Args:       args,
		GameDir:    gameDir,
		JarDir:     versionsDir,
		NativesDir: nativesDir,
		Classpath:  cpEntries,
		Version:    v.ID,
		Xmx:        xmx,
		Xms:        xms,
	}, nil
}

// SplitServer separates "host[:port]" into host and port. A missing port becomes
// 25565, the client's own default. An IPv6 literal keeps its brackets (a bare one
// is bracketed so host + ":" + port stays readable). The last result is false
// when there is no host to connect to at all.
func SplitServer(s string) (host, port string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", false
		}
		host = s[:end+1]
		rest := strings.TrimPrefix(s[end+1:], ":")
		if rest == "" {
			rest = "25565"
		}
		return host, rest, true
	}
	if strings.Count(s, ":") > 1 {
		return "[" + s + "]", "25565", true
	}
	host, port, found := strings.Cut(s, ":")
	if host == "" {
		return "", "", false
	}
	if !found || port == "" {
		port = "25565"
	}
	return host, port, true
}

// ServerAddress renders the quick play value: the address the client is handed.
func ServerAddress(server string) string {
	host, port, ok := SplitServer(server)
	if !ok {
		return ""
	}
	return host + ":" + port
}

// CommandLine renders the command for logs and --dry-run output.
func (c *Command) CommandLine() string {
	parts := append([]string{c.Java}, c.Args...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			parts[i] = strconv.Quote(p)
		}
	}
	return strings.Join(parts, " ")
}

// Run starts the process. When Wait is false the child keeps running and its
// pid is returned; when Wait is true the exit code is returned.
func (c *Command) Run(wait bool, gameLog *os.File) (pid int, exitCode int, err error) {
	cmd := exec.Command(c.Java, c.Args...)
	cmd.Dir = c.GameDir
	if err := os.MkdirAll(c.GameDir, 0o755); err != nil {
		return 0, 0, err
	}
	cmd.Dir = c.GameDir
	if gameLog != nil {
		cmd.Stdout = gameLog
		cmd.Stderr = gameLog
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}
	if !wait {
		// release the child so mcli can exit without killing it
		_ = cmd.Process.Release()
		return cmd.Process.Pid, 0, nil
	}
	err = cmd.Wait()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return cmd.Process.Pid, ee.ExitCode(), nil
		}
		return cmd.Process.Pid, 0, err
	}
	return cmd.Process.Pid, 0, nil
}

// RecommendMemory suggests -Xmx/-Xms from the machine's memory: a share of what
// is actually available, clamped to something Minecraft can live with. percent
// is config `auto_memory_percent` (a value outside 1..90 falls back to 50).
func RecommendMemory(percent int) (xmx, xms string) {
	if percent <= 0 || percent > 90 {
		percent = 50
	}
	total, available, ok := systemMemory()
	if !ok || total == 0 {
		return "2G", "512M"
	}
	// never exceed half of physical memory, and never more than `percent` of
	// what happens to be free right now
	limit := total / 2
	if free := available * int64(percent) / 100; free < limit {
		limit = free
	}
	if limit < 512<<20 {
		limit = 512 << 20
	}
	if limit > 8<<30 {
		limit = 8 << 30
	}
	initial := limit / 4
	if initial < 256<<20 {
		initial = 256 << 20
	}
	return mib(limit), mib(initial)
}

func mib(bytes int64) string {
	return strconv.FormatInt(bytes/(1<<20), 10) + "M"
}

// parseMeminfo reads the two lines of /proc/meminfo that size the heap. Values
// there are in kibibytes. A missing MemAvailable is not an error: it means an old
// kernel, and half the physical memory is the honest fallback.
func parseMeminfo(body string) (total, available int64, ok bool) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = val * 1024
		case "MemAvailable:":
			available = val * 1024
		}
	}
	if total == 0 {
		return 0, 0, false
	}
	if available == 0 {
		available = total / 2
	}
	return total, available, true
}

// ValidateMemory checks a user supplied -Xmx/-Xms style value.
func ValidateMemory(v string) error {
	if v == "" {
		return nil
	}
	s := strings.TrimSpace(v)
	if len(s) < 2 || strings.HasPrefix(s, "-") {
		return fmt.Errorf("%s", i18n.T("launch.xmx_invalid", v))
	}
	unit := s[len(s)-1]
	switch unit {
	case 'k', 'K', 'm', 'M', 'g', 'G', 't', 'T':
	default:
		return fmt.Errorf("%s", i18n.T("launch.xmx_invalid", v))
	}
	if n, err := strconv.Atoi(s[:len(s)-1]); err != nil || n <= 0 {
		return fmt.Errorf("%s", i18n.T("launch.xmx_invalid", v))
	}
	return nil
}
