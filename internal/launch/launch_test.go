package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

func syntheticVersion() *mcv.Version {
	lib := mcv.Library{Name: "com.example:lib:1.0"}
	lib.Downloads.Artifact = &mcv.Download{Path: "com/example/lib/1.0/lib-1.0.jar", SHA1: "x"}
	return &mcv.Version{
		ID:        "1.21.4",
		Type:      "release",
		MainClass: "net.minecraft.client.main.Main",
		Assets:    "19",
		AssetIndex: &mcv.Download{
			Path: "19.json",
			URL:  "https://piston-meta.mojang.com/v1/packages/x/19.json",
		},
		Libraries: []mcv.Library{lib},
		Arguments: &mcv.Arguments{
			JVM: []mcv.ArgValue{
				{Value: []string{"-Djava.library.path=${natives_directory}"}},
				{Value: []string{"-cp", "${classpath}"}},
			},
			Game: []mcv.ArgValue{
				{Value: []string{"--username", "${auth_player_name}"}},
				{Value: []string{"--version", "${version_name}"}},
				{Value: []string{"--gameDir", "${game_directory}"}},
			},
		},
		Logging: &struct {
			Client *mcv.LoggingClient `json:"client,omitempty"`
		}{Client: &mcv.LoggingClient{
			Argument: "-Dlog4j.configurationFile=${path}",
			File:     mcv.LoggingFile{ID: "client-1.21.2.xml"},
		}},
	}
}

func TestPrepBuildsFullCommand(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mc")
	cmd, err := Prep(Options{
		Root:          root,
		VersionsDir:   "versions",
		InstanceName:  "vanilla",
		Version:       syntheticVersion(),
		VersionID:     "1.21.4",
		Runtime:       java.Runtime{Path: "/usr/bin/java", Major: 21},
		Auth:          mcv.Auth{PlayerName: "Kailo", UUID: "abc", AccessToken: "tok", XUID: "1"},
		Xmx:           "4G",
		Xms:           "1G",
		ExtraJVMArgs:  []string{"-XX:+UseG1GC"},
		ExtraGameArgs: []string{"--demo"},
	})
	if err != nil {
		t.Fatal(err)
	}

	joined := cmd.CommandLine()

	// memory flags come first, extra jvm args right after
	if !strings.HasPrefix(joined, "/usr/bin/java -Xmx4G -Xms1G -XX:+UseG1GC") {
		t.Errorf("unexpected prefix: %s", joined[:min(len(joined), 90)])
	}
	if len(cmd.Classpath) != 2 {
		t.Errorf("classpath entries = %v, want library + client jar", cmd.Classpath)
	}
	if !strings.HasSuffix(cmd.Classpath[1], filepath.Join("versions", "1.21.4", "1.21.4.jar")) {
		t.Errorf("client jar not last in classpath: %v", cmd.Classpath)
	}
	for _, want := range []string{
		"-Djava.library.path=" + cmd.NativesDir,
		"-cp " + cmd.Classpath[0],
		"-Dlog4j.configurationFile=" + filepath.Join(root, "assets", "log_configs", "client-1.21.2.xml"),
		"net.minecraft.client.main.Main",
		"--username Kailo",
		"--version 1.21.4",
		"--gameDir " + cmd.GameDir,
		"--demo",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("command line is missing %q\n%s", want, joined)
		}
	}
	// the logging argument has to land before -cp
	if strings.Index(joined, "log4j.configurationFile") > strings.Index(joined, "-cp ") {
		t.Error("logging config must be passed before -cp")
	}
	if cmd.GameDir != filepath.Join(root, "versions", "vanilla") {
		t.Errorf("game dir = %q", cmd.GameDir)
	}
}

func TestPrepUsesInstanceDirectoryForGameData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mc")
	cmd, err := Prep(Options{
		Root:         root,
		VersionsDir:  "versions",
		InstanceName: "second",
		Version:      syntheticVersion(),
		VersionID:    "1.21.4",
		Runtime:      java.Runtime{Path: "/usr/bin/java"},
		Auth:         mcv.Auth{PlayerName: "Kailo", UUID: "abc", AccessToken: "tok"},
		Xmx:          "1G",
		Xms:          "512M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd.GameDir, filepath.Join("versions", "second")) {
		t.Errorf("instances must not share a game dir, got %q", cmd.GameDir)
	}
	if strings.Contains(cmd.CommandLine(), filepath.Join("versions", "1.21.4", "\x00")) {
		t.Error("unexpected command line content")
	}
}

func TestValidateMemory(t *testing.T) {
	for _, ok := range []string{"", "1G", "512M", "4096m", "2g", "1024K"} {
		if err := ValidateMemory(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"4", "G", "4GB", "-1G", "abcM"} {
		if err := ValidateMemory(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestRecommendMemoryIsSane(t *testing.T) {
	xmx, xms := RecommendMemory(50)
	if xmx == "" || xms == "" {
		t.Fatal("recommendation must not be empty")
	}
	if !strings.HasSuffix(xmx, "M") || !strings.HasSuffix(xms, "M") {
		t.Errorf("values must be in MiB: %q %q", xmx, xms)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
