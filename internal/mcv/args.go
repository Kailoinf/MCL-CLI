package mcv

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Auth carries everything the game needs to identify the player.
type Auth struct {
	PlayerName  string
	UUID        string
	AccessToken string
	XUID        string
	ClientID    string
	UserType    string // "msa" for Microsoft accounts
	Properties  string // JSON blob, "{}" unless the profile has properties
	Session     string // legacy auth_session, unused by modern clients
}

// Paths is the directory/jvm view of a launch.
type Paths struct {
	GameDir               string
	AssetsRoot            string
	AssetsIndex           string
	NativesDir            string
	LibraryDir            string
	Classpath             string
	Separator             string
	LauncherName          string
	LauncherVersion       string
	VersionName           string
	VersionType           string
	LogConfig             string
	ResolutionWidth       int
	ResolutionHeight      int
	QuickPlay             string
	QuickPlaySingleplayer string
	QuickPlayMultiplayer  string
	QuickPlayRealms       string
}

// Spec is everything needed to build a command line.
type Spec struct {
	Version *Version
	Env     Env
	Auth    Auth
	Paths   Paths
}

// Placeholders builds the ${...} table documented by Mojang.
func (s Spec) Placeholders() map[string]string {
	p := s.Paths
	a := s.Auth
	sep := p.Separator
	if sep == "" {
		sep = string(filepath.ListSeparator)
	}
	libDir := p.LibraryDir
	ph := map[string]string{
		"auth_player_name":      a.PlayerName,
		"auth_uuid":             a.UUID,
		"auth_access_token":     a.AccessToken,
		"auth_xuid":             a.XUID,
		"auth_session":          a.Session,
		"clientid":              a.ClientID,
		"auth_user_type":        a.UserType,
		"user_type":             a.UserType,
		"user_properties":       a.Properties,
		"version_name":          p.VersionName,
		"version_type":          p.VersionType,
		"game_directory":        p.GameDir,
		"assets_root":           p.AssetsRoot,
		"game_assets":           p.AssetsRoot,
		"assets_index_name":     p.AssetsIndex,
		"natives_directory":     p.NativesDir,
		"library_directory":     libDir,
		"classpath":             p.Classpath,
		"classpath_separator":   sep,
		"launcher_name":         p.LauncherName,
		"launcher_version":      p.LauncherVersion,
		"resolution_width":      itoa(p.ResolutionWidth),
		"resolution_height":     itoa(p.ResolutionHeight),
		"quickPlayPath":         p.QuickPlay,
		"quickPlaySingleplayer": p.QuickPlaySingleplayer,
		"quickPlayMultiplayer":  p.QuickPlayMultiplayer,
		"quickPlayRealms":       p.QuickPlayRealms,
	}
	return ph
}

func itoa(i int) string {
	if i == 0 {
		return ""
	}
	return strconv.Itoa(i)
}

// Substitute replaces ${key} tokens. Unknown tokens are left untouched and
// reported through the returned slice so callers can warn about drift in new
// Minecraft versions.
func Substitute(value string, ph map[string]string) (string, []string) {
	var unknown []string
	var b strings.Builder
	for {
		i := strings.Index(value, "${")
		if i < 0 {
			b.WriteString(value)
			break
		}
		b.WriteString(value[:i])
		rest := value[i+2:]
		j := strings.Index(rest, "}")
		if j < 0 {
			b.WriteString(value[i:])
			break
		}
		key := rest[:j]
		if v, ok := ph[key]; ok {
			b.WriteString(v)
		} else {
			b.WriteString("${" + key + "}")
			unknown = append(unknown, key)
		}
		value = rest[j+1:]
	}
	return b.String(), unknown
}

// JVMArgs returns the JVM arguments declared by the version JSON, with rules
// applied and placeholders substituted. Memory flags and the logging argument
// are added by the launch layer.
func (s Spec) JVMArgs() []string {
	v := s.Version
	ph := s.Placeholders()
	var out []string
	if v.Arguments != nil && len(v.Arguments.JVM) > 0 {
		for _, a := range v.Arguments.JVM {
			if !Allowed(a.Rules, s.Env) {
				continue
			}
			for _, raw := range a.Value {
				val, _ := Substitute(raw, ph)
				out = append(out, val)
			}
		}
		return out
	}
	// very old versions carry no jvm arguments; the launcher supplies the
	// classic set itself.
	out = append(out, "-Djava.library.path="+s.Paths.NativesDir)
	out = append(out, "-cp", s.Paths.Classpath)
	return out
}

// GameArgs returns the game arguments declared by the version JSON (or the
// legacy minecraftArguments string).
func (s Spec) GameArgs() []string {
	v := s.Version
	ph := s.Placeholders()
	var out []string
	if v.Arguments != nil && len(v.Arguments.Game) > 0 {
		for _, a := range v.Arguments.Game {
			if !Allowed(a.Rules, s.Env) {
				continue
			}
			for _, raw := range a.Value {
				val, _ := Substitute(raw, ph)
				out = append(out, val)
			}
		}
		return out
	}
	if v.MinecraftArguments != "" {
		for _, raw := range strings.Fields(v.MinecraftArguments) {
			val, _ := Substitute(raw, ph)
			out = append(out, val)
		}
	}
	return out
}

// LoggingArg returns the -Dlog4j.configurationFile style argument for the
// version's logging config, if it has one.
func (s Spec) LoggingArg() (string, bool) {
	if s.Version.Logging == nil || s.Version.Logging.Client == nil {
		return "", false
	}
	c := s.Version.Logging.Client
	if c.Argument == "" || s.Paths.LogConfig == "" {
		return "", false
	}
	ph := map[string]string{"path": s.Paths.LogConfig}
	val, _ := Substitute(c.Argument, ph)
	return val, true
}

// ClasspathEntries builds the classpath in launch order: every allowed
// non-natives library, then the client jar. Duplicates are dropped.
func ClasspathEntries(v *Version, env Env, librariesDir, clientJar string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, l := range v.Libraries {
		if !l.AllowedFor(env) || l.IsNative(env) {
			continue
		}
		if _, ok := l.ArtifactDownload(env); !ok {
			continue
		}
		add(filepath.Join(librariesDir, filepath.FromSlash(l.MavenPath(""))))
	}
	add(clientJar)
	return out
}

// ClasspathString joins classpath entries with the platform separator.
func ClasspathString(entries []string, separator string) string {
	if separator == "" {
		separator = string(filepath.ListSeparator)
	}
	return strings.Join(entries, separator)
}
