package mcv

import "testing"

func linuxEnv() Env { return Env{OS: "linux", Arch: "x86_64", Features: map[string]bool{}} }

func TestAllowed(t *testing.T) {
	env := linuxEnv()
	cases := []struct {
		name  string
		rules []Rule
		want  bool
	}{
		{"no rules means allow", nil, true},
		{"allow linux", []Rule{{Action: "allow", OS: &RuleOS{Name: "linux"}}}, true},
		{"allow windows does not match", []Rule{{Action: "allow", OS: &RuleOS{Name: "windows"}}}, false},
		{"disallow linux wins", []Rule{
			{Action: "allow"},
			{Action: "disallow", OS: &RuleOS{Name: "linux"}},
		}, false},
		{"feature mismatch", []Rule{
			{Action: "allow", Features: map[string]bool{"is_demo_user": true}},
		}, false},
		{"feature match", []Rule{
			{Action: "allow", Features: map[string]bool{"is_demo_user": false}},
		}, true},
		{"arch x86 does not match x86_64", []Rule{{Action: "allow", OS: &RuleOS{Arch: "x86"}}}, false},
		{"arch x86_64 matches", []Rule{{Action: "allow", OS: &RuleOS{Arch: "x86_64"}}}, true},
	}
	for _, c := range cases {
		if got := Allowed(c.rules, env); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOSVersionRule(t *testing.T) {
	env := linuxEnv()
	env.Version = "10.0.19045"
	if !Allowed([]Rule{{Action: "allow", OS: &RuleOS{Version: "^10\\."}}}, env) {
		t.Error("prefix regex should match")
	}
	if Allowed([]Rule{{Action: "allow", OS: &RuleOS{Version: "^11\\."}}}, env) {
		t.Error("prefix regex should not match")
	}
}

func TestFeatureSet(t *testing.T) {
	f := FeatureSet(true, nil)
	if !f["has_custom_resolution"] {
		t.Error("custom resolution should be set")
	}
	if f["is_demo_user"] {
		t.Error("demo user must default to false")
	}
}

func TestMojangArch(t *testing.T) {
	if MojangArch("amd64") != "x86_64" || MojangArch("386") != "x86" {
		t.Error("arch mapping is wrong")
	}
}
