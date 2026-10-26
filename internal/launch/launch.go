// Package launch turns a prepared version + java runtime + account into a real
// process: it assembles the classpath, fills in the ${...} placeholders,
// applies memory flags and starts java.
package launch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	Xmx           string
	Xms           string
	ExtraJVMArgs  []string
	ExtraGameArgs []string

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

	cpEntries := mcv.ClasspathEntries(v, mcv.EnvCurrent(mcv.FeatureSet(o.Width > 0 && o.Height > 0, nil)), libsDir, clientJar)
	sep := string(filepath.ListSeparator)
	cp := mcv.ClasspathString(cpEntries, sep)

	logConfig := ""
	if v.Logging != nil && v.Logging.Client != nil {
		logConfig = filepath.Join(assetsRoot, "log_configs", v.Logging.Client.File.ID)
	}

	spec := mcv.Spec{
		Version: v,
		Env:     mcv.EnvCurrent(mcv.FeatureSet(o.Width > 0 && o.Height > 0, nil)),
		Auth:    o.Auth,
		Paths: mcv.Paths{
			GameDir:          gameDir,
			AssetsRoot:       assetsRoot,
			AssetsIndex:      v.Assets,
			NativesDir:       nativesDir,
			LibraryDir:       libsDir,
			Classpath:        cp,
			Separator:        sep,
			LauncherName:     o.LauncherName,
			LauncherVersion:  o.LauncherVersion,
			VersionName:      v.ID,
			VersionType:      v.Type,
			LogConfig:        logConfig,
			ResolutionWidth:  o.Width,
			ResolutionHeight: o.Height,
		},
	}

	xmx, xms := o.Xmx, o.Xms
	if xmx == "" || xms == "" {
		axmx, axms := RecommendMemory()
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
	args = append(args, spec.GameArgs()...)
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
// is actually available, clamped to something Minecraft can live with.
func RecommendMemory() (xmx, xms string) {
	total, available, ok := systemMemory()
	if !ok || total == 0 {
		return "2G", "512M"
	}
	// never exceed half of physical memory, never exceed 75% of what is free
	limit := total / 2
	if free := available * 3 / 4; free < limit {
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

// systemMemory reads physical and available memory where the platform allows
// it. Other platforms fall back to a conservative guess.
func systemMemory() (total, available int64, ok bool) {
	if runtime.GOOS != "linux" {
		return 0, 0, false
	}
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
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
