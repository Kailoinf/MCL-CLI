package mcv

import (
	"fmt"
	"runtime"
	"strings"
)

// Rule is one entry of a version JSON "rules" list: it allows or disallows the
// enclosing element for a given OS/arch/feature combination.
type Rule struct {
	Action   string          `json:"action"` // "allow" | "disallow"
	OS       *RuleOS         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

// RuleOS is the OS predicate of a rule.
type RuleOS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"` // regex, matched against os version
	Arch    string `json:"arch,omitempty"`
}

// Env describes the machine a launch is being prepared for.
type Env struct {
	OS       string // runtime.GOOS naming: linux | windows | darwin
	Arch     string // mojang naming: x86 | x86_64 | arm64
	Version  string // OS version, only used for regex rules
	Features map[string]bool
}

// EnvCurrent snapshots this machine.
func EnvCurrent(features map[string]bool) Env {
	if features == nil {
		features = map[string]bool{}
	}
	return Env{OS: runtime.GOOS, Arch: MojangArch(runtime.GOARCH), Features: features}
}

// MojangArch converts GOARCH to the naming used in version JSONs.
func MojangArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm32"
	default:
		return goarch
	}
}

// OSName converts runtime.GOOS to Mojang naming (darwin stays darwin, which is
// what the JSON uses; "osx" only ever appears in natives maps).
func OSName() string { return runtime.GOOS }

func (r Rule) matches(env Env) bool {
	if r.OS != nil {
		if r.OS.Name != "" && !strings.EqualFold(r.OS.Name, env.OS) {
			return false
		}
		if r.OS.Arch != "" && !strings.EqualFold(r.OS.Arch, env.Arch) {
			return false
		}
		if r.OS.Version != "" && env.Version != "" && !matchRegex(r.OS.Version, env.Version) {
			return false
		}
	}
	for k, want := range r.Features {
		have := env.Features[k]
		if have != want {
			return false
		}
	}
	return true
}

// Allowed evaluates a rule list. An empty list means "allowed" (Mojang treats
// a missing rules array as allow-all).
func Allowed(rules []Rule, env Env) bool {
	if len(rules) == 0 {
		return true
	}
	allow := false
	for _, r := range rules {
		if !r.matches(env) {
			continue
		}
		switch r.Action {
		case "allow":
			allow = true
		case "disallow":
			return false
		}
	}
	return allow
}

// matchRegex is a deliberately tiny regex engine: the only pattern Mojang has
// ever used in os.version is "^10\\." style prefixes. Anything more exotic is
// treated as a plain prefix match so behaviour stays predictable.
func matchRegex(pattern, value string) bool {
	p := strings.TrimPrefix(pattern, "^")
	p = strings.TrimSuffix(p, "$")
	p = strings.ReplaceAll(p, `\.`, ".")
	p = strings.ReplaceAll(p, `\\`, `\`)
	// strip a trailing ".*" wildcard and treat the rest as a prefix
	if strings.HasSuffix(p, ".*") {
		p = strings.TrimSuffix(p, ".*")
		return strings.HasPrefix(value, p)
	}
	return strings.HasPrefix(value, p)
}

// FeatureSet builds the feature map for a launch.
func FeatureSet(customResolution bool, extra map[string]bool) map[string]bool {
	f := map[string]bool{
		"is_demo_user":               false,
		"has_custom_resolution":      customResolution,
		"has_quick_plays_support":    false,
		"is_quick_play_singleplayer": false,
		"is_quick_play_multiplayer":  false,
		"is_quick_play_realms":       false,
	}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

// DescribeEnv is a short human string used in logs.
func (e Env) String() string {
	return fmt.Sprintf("%s/%s", e.OS, e.Arch)
}
