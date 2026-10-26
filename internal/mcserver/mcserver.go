// Package mcserver prepares and starts a Minecraft Java server: one version's
// server jar, a directory of its own, the two text files a server needs before
// its first start, and the java command line.
//
// It is deliberately much smaller than internal/launch: a server has no account,
// no assets and no classpath. Mojang's server jar is self-contained, and since
// 1.18 it is a bundler that unpacks the libraries it runs itself, so the whole
// install is one file.
package mcserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// EulaURL is the licence every server refuses to run without. It is printed
// before asking, never assumed.
const EulaURL = "https://aka.ms/MinecraftEULA"

// EulaText is what eula.txt has to say, in the wording Mojang's own server
// writes on its first run.
const EulaText = "#By changing the setting below to TRUE you are indicating your agreement to our EULA (" +
	EulaURL + ").\neula=true\n"

// DefaultPort is what a server listens on when nothing else is asked for.
const DefaultPort = 25565

// Options describes one server-side install and launch.
type Options struct {
	// Dir is the server's own directory: jar, eula.txt, server.properties, world.
	Dir          string
	Version      *mcv.Version
	VersionID    string
	Xmx          string
	Xms          string
	Port         int
	ExtraJVMArgs []string
}

// JarName is the file name the server jar is stored under.
const JarName = "server.jar"

// JarItem returns the version's server jar as a download item rooted at the
// server's directory. A version that ships no server — a client-only snapshot,
// an old alpha, a modded profile — reports false.
func JarItem(o Options, mapper mirror.Mapper) (dl.Item, bool) {
	if o.Version == nil {
		return dl.Item{}, false
	}
	d, ok := o.Version.Downloads["server"]
	if !ok || d.URL == "" {
		return dl.Item{}, false
	}
	id := o.VersionID
	if id == "" {
		id = o.Version.ID
	}
	return dl.Item{
		Candidates: mapper.Candidates(d.URL),
		Dest:       filepath.Join(o.Dir, JarName),
		SHA1:       d.SHA1,
		Size:       d.Size,
		Desc:       "server " + id,
	}, true
}

// JarPath is where the server jar lives.
func JarPath(o Options) string { return filepath.Join(o.Dir, JarName) }

// Properties is the minimal server.properties for a first start: the port and a
// name for the server. Everything else — difficulty, player limits, the online
// mode — the server writes itself on first run, and a person editing this file
// afterwards must keep their edits.
func Properties(port int) string {
	if port <= 0 || port > 65535 {
		port = DefaultPort
	}
	return "server-port=" + itoa(port) + "\nmotd=mcli server\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// WriteFiles writes eula.txt and server.properties when they are missing, and
// returns the names it created. An existing file is never touched: the world,
// the properties and the licence belong to whoever runs this server.
func WriteFiles(o Options, log *logx.Logger) ([]string, error) {
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, err
	}
	var wrote []string
	files := []struct {
		name string
		body string
	}{
		{"eula.txt", EulaText},
		{"server.properties", Properties(o.Port)},
	}
	for _, f := range files {
		path := filepath.Join(o.Dir, f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(f.body), 0o644); err != nil {
			return wrote, err
		}
		wrote = append(wrote, path)
		if log != nil {
			log.Infof("%s", f.name)
		}
	}
	return wrote, nil
}

// Command is a prepared server start.
type Command struct {
	Java string
	Args []string
	Dir  string
	Jar  string
	// ConsoleViaPipe routes the server's input through the console FIFO.
	ConsoleViaPipe bool
}

// Prep assembles the java command line: memory flags, the caller's extra JVM
// arguments, the jar, and the console switch every server expects when it is not
// driven through its own GUI.
func Prep(o Options, rt java.Runtime) *Command {
	dir := o.Dir
	args := []string{"-Xmx" + o.Xmx, "-Xms" + o.Xms}
	args = append(args, o.ExtraJVMArgs...)
	args = append(args, "-jar", JarPath(o), "nogui")
	return &Command{Java: rt.Path, Args: args, Dir: dir, Jar: JarPath(o)}
}

// CommandLine renders the command for logs and --dry-run output.
func (c *Command) CommandLine() string {
	parts := append([]string{c.Java}, c.Args...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			parts[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
		}
	}
	return strings.Join(parts, " ")
}

// ConsoleInputPath is the pipe a detached server reads commands from, and the
// same file `server attach` writes them into.
func ConsoleInputPath(dir string) string { return filepath.Join(dir, "console.in") }

// ConsoleOutputPath is where a detached server's own output is captured.
func ConsoleOutputPath(dir string) string { return filepath.Join(dir, "logs", "console.log") }

// EulaAccepted reports whether eula.txt exists in dir and accepts the licence.
func EulaAccepted(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if !strings.HasPrefix(line, "eula") {
			continue
		}
		_, value, _ := strings.Cut(line, "=")
		if strings.TrimSpace(value) == "true" {
			return true
		}
	}
	return false
}

// Run starts the server. When wait is false the child keeps running and its pid
// is returned. A server's console is its stdin and stdout, so both are inherited
// — the log file the server writes itself stays the durable record.
//
// onStart, when given, is called with the pid as soon as the child exists. An
// attached run waits here for as long as the server lives, so a caller that wants
// to record the pid — the one handle `server stop` has on it — must be told
// before the wait begins, not after it ends.
func (c *Command) Run(wait bool, onStart func(pid int)) (pid int, exitCode int, err error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return 0, 0, err
	}
	cmd := exec.Command(c.Java, c.Args...)
	cmd.Dir = c.Dir
	switch {
	case wait && c.ConsoleViaPipe:
		// The launcher owns the console: input comes through the FIFO (so mcli can
		// also write the server's own `stop` into it), and output still goes to the
		// person's terminal.
		input, err := OpenConsoleInput(c.Dir)
		if err != nil {
			return 0, 0, err
		}
		defer input.Close()
		cmd.Stdin = input
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	case wait:
		// An attached run is the console: the person starting it types into it.
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	default:
		// A detached server must not inherit mcli's stdout: an open pipe would
		// keep whoever launched mcli waiting for a process that never ends. It
		// gets its own console file instead, and no stdin.
		console, err := c.consoleFile()
		if err != nil {
			return 0, 0, err
		}
		defer console.Close()
		cmd.Stdout = console
		cmd.Stderr = console
		// A console to attach to later, when the system can offer one; without
		// it the server simply has no input and `/dev/null` stands in.
		if input, err := OpenConsoleInput(c.Dir); err == nil {
			defer input.Close()
			cmd.Stdin = input
		} else {
			devNull, err := os.Open(os.DevNull)
			if err != nil {
				return 0, 0, err
			}
			defer devNull.Close()
			cmd.Stdin = devNull
		}
	}
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}
	pid = cmd.Process.Pid
	if onStart != nil {
		onStart(pid)
	}
	if !wait {
		// release the child so mcli can exit without killing it
		_ = cmd.Process.Release()
		return pid, 0, nil
	}
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return cmd.Process.Pid, ee.ExitCode(), nil
		}
		return cmd.Process.Pid, 0, err
	}
	return cmd.Process.Pid, 0, nil
}

// consoleFile is where a detached server's own output goes. The server writes
// logs/latest.log itself as well; this file catches what it prints outside the
// logging framework — a stack trace before the logger is up, and the like.
func (c *Command) consoleFile() (*os.File, error) {
	dir := filepath.Join(c.Dir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "console.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// ReadyLine is what a Minecraft server prints once it accepts commands. It is the
// signal a launcher has to wait for before forwarding anything: a command that
// arrives earlier is answered with "An unexpected error occurred while trying to
// execute that command", because the dispatcher is not up yet.
const ReadyLine = "Done ("

// WaitReady waits for the server in dir to report that it is ready, and reports
// whether it saw the line. `since` is when the wait started: the log has to have been
// written after that, because until the new server rotates `latest.log` it still
// holds the previous run's output — and its `Done (` from last time is not readiness
// now. A server that cannot start will never answer, so callers bound this with their
// own timeout.
func WaitReady(dir string, since time.Time, timeout time.Duration) bool {
	path := filepath.Join(dir, "logs", "latest.log")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, statErr := os.Stat(path)
		if statErr == nil && !info.ModTime().Before(since) {
			if raw, err := os.ReadFile(path); err == nil && strings.Contains(string(raw), ReadyLine) {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
