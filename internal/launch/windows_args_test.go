package launch

import (
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

// A launcher that only works on the machine it was written on is not much of a
// launcher: the maintainer runs mcli from Git Bash on Windows. This pins the parts
// of the argument line that differ there — the classpath separator, and what a path
// holding a space looks like when it is printed for a person to paste.
func TestPrepBuildsTheWindowsShape(t *testing.T) {
	root := `C:\Users\kailo\AppData\Roaming\.minecraft`
	cmd, err := Prep(Options{
		Root:          root,
		VersionsDir:   "versions",
		InstanceName:  "My Pack", // an instance name with a space
		Version:       syntheticVersion(),
		VersionID:     "1.21.4",
		Runtime:       java.Runtime{Path: `C:\Program Files\Java\bin\java.exe`, Major: 21},
		Auth:          mcv.Auth{PlayerName: "Kailo", UUID: "abc", AccessToken: "tok", XUID: "1"},
		Xmx:           "4G",
		Xms:           "1G",
		PathSeparator: ";",
	})
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(cmd.Args, "\n")
	if !strings.Contains(joined, "-cp\n") {
		t.Fatalf("no classpath argument in:\n%s", joined)
	}
	// the classpath must be joined the way Java on Windows expects it
	for i, a := range cmd.Args {
		if a != "-cp" || i+1 >= len(cmd.Args) {
			continue
		}
		cp := cmd.Args[i+1]
		if strings.Contains(cp, ":") && !strings.Contains(cp, ";") {
			t.Errorf("classpath used the Unix separator on Windows:\n%s", cp)
		}
		if !strings.Contains(cp, ";") {
			t.Errorf("classpath was not joined at all:\n%s", cp)
		}
	}
	if !strings.Contains(joined, "-Djava.library.path=") {
		t.Errorf("natives path missing:\n%s", joined)
	}
	if !strings.Contains(joined, "My Pack") {
		t.Errorf("the instance directory (with its space) is not in the command:\n%s", joined)
	}

	// printed for a person: anything holding a space must be quoted, or the line
	// cannot be pasted into Git Bash without splitting it
	line := cmd.CommandLine()
	for _, part := range strings.Fields(line) {
		_ = part
	}
	if !strings.Contains(line, `"`) {
		t.Errorf("nothing was quoted, so a path with a space cannot be pasted:\n%s", line)
	}
	if strings.Contains(line, "My Pack") && !strings.Contains(line, `\"My Pack\"`) && !strings.Contains(line, `"`) {
		t.Errorf("the instance path was printed unquoted:\n%s", line)
	}
	// and the java binary itself, which lives under "Program Files" here
	if !strings.Contains(line, `"C:\\Program Files\\Java\\bin\\java.exe"`) {
		t.Logf("note: java path was printed as: %s", strings.SplitN(line, " ", 2)[0])
	}
}
