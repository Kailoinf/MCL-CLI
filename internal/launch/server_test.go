package launch

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/java"
	"github.com/Kailoinf/MCL-CLI/internal/mcv"
)

func TestSplitServer(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port string
		ok   bool
	}{
		{"", "", "", false},
		{"   ", "", "", false},
		{":25565", "", "", false},
		{"play.example.net", "play.example.net", "25565", true},
		{"play.example.net:25566", "play.example.net", "25566", true},
		{"play.example.net:", "play.example.net", "25565", true},
		{"[::1]:25566", "[::1]", "25566", true},
		{"[::1]", "[::1]", "25565", true},
		{"2001:db8::1", "[2001:db8::1]", "25565", true},
	}
	for _, c := range cases {
		host, port, ok := SplitServer(c.in)
		if host != c.host || port != c.port || ok != c.ok {
			t.Errorf("SplitServer(%q) = %q, %q, %v; want %q, %q, %v",
				c.in, host, port, ok, c.host, c.port, c.ok)
		}
	}
	if got := ServerAddress("play.example.net"); got != "play.example.net:25565" {
		t.Errorf("ServerAddress without a port = %q", got)
	}
	if got := ServerAddress(""); got != "" {
		t.Errorf("ServerAddress of nothing = %q", got)
	}
}

// quickPlayVersion is a synthetic modern version whose quick play and demo
// arguments are gated by features, the way a real 1.20+ JSON gates them.
func quickPlayVersion() *mcv.Version {
	v := syntheticVersion()
	v.Arguments.Game = append(v.Arguments.Game,
		mcv.ArgValue{
			Rules: []mcv.Rule{{Action: "allow", Features: map[string]bool{"is_quick_play_multiplayer": true}}},
			Value: []string{"--quickPlayMultiplayer", "${quickPlayMultiplayer}"},
		},
		mcv.ArgValue{
			Rules: []mcv.Rule{{Action: "allow", Features: map[string]bool{"is_demo_user": true}}},
			Value: []string{"--demo"},
		},
	)
	return v
}

// legacyVersion is a synthetic old version: no arguments block, only the
// minecraftArguments string.
func legacyVersion() *mcv.Version {
	v := syntheticVersion()
	v.Arguments = nil
	v.MinecraftArguments = "--username ${auth_player_name} --gameDir ${game_directory}"
	return v
}

func prepWithSwitches(t *testing.T, v *mcv.Version, o Options) []string {
	t.Helper()
	o.Root = filepath.Join(t.TempDir(), "mc")
	o.VersionsDir = "versions"
	o.InstanceName = "srv"
	o.Version = v
	o.VersionID = v.ID
	o.Runtime = java.Runtime{Path: "/usr/bin/java", Major: 21}
	o.Auth = mcv.Auth{PlayerName: "Kailo", UUID: "abc", AccessToken: "tok", XUID: "1"}
	o.Xmx, o.Xms = "2G", "512M"
	cmd, err := Prep(o)
	if err != nil {
		t.Fatal(err)
	}
	return cmd.Args
}

func TestPrepUsesQuickPlayOnModernVersions(t *testing.T) {
	args := prepWithSwitches(t, quickPlayVersion(), Options{Server: "example.com"})
	if !slices.Contains(args, "--quickPlayMultiplayer") {
		t.Fatalf("modern version: quick play argument missing: %v", args)
	}
	if i := slices.Index(args, "--quickPlayMultiplayer"); args[i+1] != "example.com:25565" {
		t.Errorf("quick play value = %q, want example.com:25565", args[i+1])
	}
	// the classic pair belongs to old versions only
	if slices.Contains(args, "--server") {
		t.Errorf("modern version: --server must not be added as well: %v", args)
	}

	args = prepWithSwitches(t, quickPlayVersion(), Options{Server: "example.com", Demo: true, Fullscreen: true})
	if n := count(args, "--demo"); n != 1 {
		t.Errorf("--demo appears %d times, want exactly 1: %v", n, args)
	}
	if !slices.Contains(args, "--fullscreen") {
		t.Errorf("--fullscreen missing: %v", args)
	}
	if slices.Contains(args, "--quickPlayPath") {
		t.Errorf("--quickPlayPath must stay out: mcli has no quick play file: %v", args)
	}
}

func TestPrepAddsClassicServerArgsOnLegacyVersions(t *testing.T) {
	args := prepWithSwitches(t, legacyVersion(), Options{
		Server: "play.example.net:25566", Demo: true, Fullscreen: true,
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--server play.example.net", "--port 25566", "--demo", "--fullscreen"} {
		if !strings.Contains(joined, want) {
			t.Errorf("legacy version: %q missing from %s", want, joined)
		}
	}
	if slices.Contains(args, "--quickPlayMultiplayer") {
		t.Errorf("legacy version must not get a quick play argument: %v", args)
	}
	// a legacy version keeps its own args too
	if !strings.Contains(joined, "--username Kailo") {
		t.Errorf("legacy minecraftArguments lost: %s", joined)
	}
}

func TestPrepWithoutSwitchesAddsNothingExtra(t *testing.T) {
	args := prepWithSwitches(t, quickPlayVersion(), Options{})
	for _, unwanted := range []string{"--server", "--port", "--demo", "--fullscreen", "--quickPlayMultiplayer"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("plain launch must not contain %s: %v", unwanted, args)
		}
	}
}

func count(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}
