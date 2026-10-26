package mcv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// Download describes one downloadable object or library artifact.
type Download struct {
	Path string `json:"path,omitempty"`
	SHA1 string `json:"sha1,omitempty"`
	Size int64  `json:"size,omitempty"`
	URL  string `json:"url,omitempty"`
}

// ArgValue is one entry of arguments.game / arguments.jvm. Mojang allows either
// a bare string or an object carrying rules plus a string-or-array value, so
// this type implements a custom unmarshaller.
type ArgValue struct {
	Rules []Rule
	Value []string
}

// UnmarshalJSON accepts both the string and the object form.
func (a *ArgValue) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		a.Value = []string{s}
		return nil
	}
	var obj struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	a.Rules = obj.Rules
	raw := strings.TrimSpace(string(obj.Value))
	switch {
	case raw == "" || raw == "null":
		a.Value = nil
	case strings.HasPrefix(raw, "["):
		var vs []string
		if err := json.Unmarshal(obj.Value, &vs); err != nil {
			return err
		}
		a.Value = vs
	default:
		var s string
		if err := json.Unmarshal(obj.Value, &s); err != nil {
			return err
		}
		a.Value = []string{s}
	}
	return nil
}

// Library is one entry of the version JSON "libraries" array.
type Library struct {
	Name    string            `json:"name"`
	Rules   []Rule            `json:"rules"`
	Natives map[string]string `json:"natives,omitempty"`
	Extract *struct {
		Exclude []string `json:"exclude"`
	} `json:"extract,omitempty"`
	Downloads struct {
		Artifact    *Download           `json:"artifact,omitempty"`
		Classifiers map[string]Download `json:"classifiers,omitempty"`
	} `json:"downloads"`
	// URL is filled in for very old JSONs that carry "url" instead of
	// "downloads", and for Fabric's launcher profile, which lists its own maven
	// repository this way; the launcher appends the maven path to it.
	URL string `json:"url,omitempty"`
	// SHA1 and Size sit next to "name" in the Fabric profile (and nowhere else):
	// with them, an entry without a downloads block is still verifiable.
	SHA1 string `json:"sha1,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// Parts splits a maven coordinate: group:artifact:version[:classifier].
func (l Library) Parts() []string { return strings.Split(l.Name, ":") }

// GroupPath is the maven directory prefix, e.g. com/google/guava.
func (l Library) GroupPath() string {
	p := l.Parts()
	if len(p) == 0 {
		return ""
	}
	return strings.ReplaceAll(p[0], ".", "/")
}

// MavenPath builds the classic repository path (optionally with classifier).
func (l Library) MavenPath(classifier string) string {
	p := l.Parts()
	if len(p) < 3 {
		return ""
	}
	group, artifact, version := l.GroupPath(), p[1], p[2]
	file := fmt.Sprintf("%s-%s", artifact, version)
	if classifier != "" {
		file += "-" + classifier
	}
	return fmt.Sprintf("%s/%s/%s/%s.jar", group, artifact, version, file)
}

// AllowedFor reports whether this library applies to the given environment.
func (l Library) AllowedFor(env Env) bool { return Allowed(l.Rules, env) }

// NativeClassifier returns the classifier holding this library's natives for
// the environment, if it has one. It covers both the legacy "natives" map and
// the modern style where the classifier is part of the name
// (e.g. org.lwjgl:lwjgl:3.3.3:natives-linux).
func (l Library) NativeClassifier(env Env) (string, bool) {
	if len(l.Natives) > 0 {
		for _, key := range nativeOSKeys(env) {
			if c, ok := l.Natives[key]; ok && c != "" {
				return strings.ReplaceAll(c, "${arch}", archPlaceholder(env)), true
			}
		}
		return "", false
	}
	p := l.Parts()
	if len(p) >= 4 && strings.HasPrefix(p[3], "natives-") {
		return p[3], true
	}
	return "", false
}

// IsNative reports whether the library is a natives package for this env.
func (l Library) IsNative(env Env) bool {
	_, ok := l.NativeClassifier(env)
	return ok
}

// NativeDownload returns the artifact holding natives, if any.
func (l Library) NativeDownload(env Env) (Download, bool) {
	c, ok := l.NativeClassifier(env)
	if !ok {
		return Download{}, false
	}
	if d, ok := l.Downloads.Classifiers[c]; ok {
		return d, true
	}
	// modern style: the plain artifact *is* the natives jar
	if l.Downloads.Artifact != nil && strings.HasPrefix(c, "natives-") {
		return *l.Downloads.Artifact, true
	}
	return Download{}, false
}

// ArtifactDownload returns the classpath artifact, if any. Modern natives
// entries (classifier inside the name) carry no classpath artifact; classic
// libraries with a "natives" map still ship one.
func (l Library) ArtifactDownload(env Env) (Download, bool) {
	if len(l.Natives) == 0 {
		if c, ok := l.NativeClassifier(env); ok && strings.HasPrefix(c, "natives-") {
			// modern natives entries carry no classpath artifact
			return Download{}, false
		}
	}
	if l.Downloads.Artifact == nil {
		// No downloads block: Fabric's launcher profile (and the very oldest
		// Mojang JSONs) name a maven repository instead and expect the launcher to
		// build the path. Without this the loader never reaches the classpath —
		// the game then dies looking for its main class.
		path := l.MavenPath("")
		if l.URL == "" || path == "" {
			return Download{}, false
		}
		return Download{
			Path: path,
			URL:  strings.TrimRight(l.URL, "/") + "/" + path,
			SHA1: l.SHA1,
			Size: l.Size,
		}, true
	}
	return *l.Downloads.Artifact, true
}

// ArtifactURL returns the artifact URL, reconstructing it for ancient version
// JSONs that only carry a base "url" field.
func (l Library) ArtifactURL(env Env) (Download, bool) {
	d, ok := l.ArtifactDownload(env)
	if !ok {
		return Download{}, false
	}
	if d.URL == "" && l.URL != "" {
		d.URL = strings.TrimRight(l.URL, "/") + "/" + l.MavenPath("")
	}
	if d.URL == "" {
		d.URL = "https://libraries.minecraft.net/" + l.MavenPath("")
	}
	return d, true
}

// NativeURL is ArtifactURL for the natives jar.
func (l Library) NativeURL(env Env) (Download, bool) {
	d, ok := l.NativeDownload(env)
	if !ok {
		return Download{}, false
	}
	if d.URL == "" {
		c, _ := l.NativeClassifier(env)
		base := l.URL
		if base == "" {
			base = "https://libraries.minecraft.net/"
		}
		d.URL = strings.TrimRight(base, "/") + "/" + l.MavenPath(c)
	}
	return d, true
}

func nativeOSKeys(env Env) []string {
	keys := []string{env.OS}
	switch env.OS {
	case "darwin":
		keys = append(keys, "osx", "osx-"+archPlaceholder(env))
	case "windows":
		keys = append(keys, "windows-"+archPlaceholder(env))
	case "linux":
		keys = append(keys, "linux-"+archPlaceholder(env))
	}
	return keys
}

func archPlaceholder(env Env) string {
	switch env.Arch {
	case "x86_64":
		return "64"
	case "x86":
		return "32"
	case "arm64":
		return "arm64"
	default:
		return env.Arch
	}
}

// JavaVersion is the javaVersion block: which runtime the version wants.
type JavaVersion struct {
	Component    string `json:"component,omitempty"`
	MajorVersion int    `json:"majorVersion,omitempty"`
}

// LoggingFile is a log configuration object.
type LoggingFile struct {
	ID   string `json:"id"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// LoggingClient is the client logging entry.
type LoggingClient struct {
	Argument string      `json:"argument"`
	File     LoggingFile `json:"file"`
	Type     string      `json:"type"`
}

// Arguments holds the modern argument lists.
type Arguments struct {
	Game []ArgValue `json:"game"`
	JVM  []ArgValue `json:"jvm"`
}

// Version is a version JSON document, possibly inheriting from another one.
type Version struct {
	ID                 string              `json:"id"`
	InheritsFrom       string              `json:"inheritsFrom,omitempty"`
	Type               string              `json:"type,omitempty"`
	MainClass          string              `json:"mainClass,omitempty"`
	Assets             string              `json:"assets,omitempty"`
	AssetIndex         *Download           `json:"assetIndex,omitempty"`
	Downloads          map[string]Download `json:"downloads,omitempty"`
	JavaVersion        *JavaVersion        `json:"javaVersion,omitempty"`
	Libraries          []Library           `json:"libraries,omitempty"`
	Arguments          *Arguments          `json:"arguments,omitempty"`
	MinecraftArguments string              `json:"minecraftArguments,omitempty"`
	Logging            *struct {
		Client *LoggingClient `json:"client,omitempty"`
	} `json:"logging,omitempty"`
	// Jar names the version whose client jar this one runs, when it is not its
	// own. Modded profiles (Fabric, OptiFine, …) inherit from a vanilla version
	// and usually ship no client download at all.
	Jar                    string `json:"jar,omitempty"`
	ComplianceLevel        int    `json:"complianceLevel,omitempty"`
	MinimumLauncherVersion int    `json:"minimumLauncherVersion,omitempty"`
	ReleaseTime            string `json:"releaseTime,omitempty"`
	Time                   string `json:"time,omitempty"`

	// Path is where the JSON was read from, Source which mirror served it.
	Path   string `json:"-"`
	Source string `json:"-"`
	parent *Version
}

// Parent returns the version this one inherits from, if loaded.
func (v *Version) Parent() *Version { return v.parent }

// NativesTargets returns the directories this version's JVM arguments expect
// native libraries in, resolved against nativesDir.
//
// A plain Mojang version only says ${natives_directory}. Newer ones — and every
// modded profile PCL or HMCL flattens — point JNA, LWJGL and Netty at their own
// subdirectory (…/natives/jna, …/natives/lwjgl, …/natives/netty) because those
// libraries unpack their natives from the classpath at run time, while
// …/natives/java is what java.library.path gets pointed at for everything else.
// Creating those directories (and putting the natives where the arguments look
// for them) is what makes an install produced by another launcher usable.
func (v *Version) NativesTargets(nativesDir string) []string {
	if v == nil || v.Arguments == nil || nativesDir == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if p != nativesDir && !strings.HasPrefix(p, nativesDir+string(filepath.Separator)) {
			return // someone else's directory: not ours to manage
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, arg := range v.Arguments.JVM {
		for _, raw := range arg.Value {
			for _, key := range nativesArgKeys {
				if !strings.HasPrefix(raw, key) {
					continue
				}
				value := strings.ReplaceAll(raw[len(key):], "${natives_directory}", nativesDir)
				for _, part := range splitPathList(value) {
					add(filepath.Clean(part))
				}
			}
		}
	}
	return out
}

// nativesArgKeys are the JVM properties that name a natives directory.
var nativesArgKeys = []string{
	"-Djava.library.path=",
	"-Djna.tmpdir=",
	"-Dorg.lwjgl.system.SharedLibraryExtractPath=",
	"-Dio.netty.native.workdir=",
}

// splitPathList splits a path list on both the platform separator and ';', so a
// Windows-style value read on Linux (or the reverse) still parses.
func splitPathList(s string) []string {
	sep := string(filepath.ListSeparator)
	if sep != ";" {
		s = strings.ReplaceAll(s, ";", sep)
	}
	return strings.Split(s, sep)
}

// ClientJarTarget returns the version id whose client jar this version runs and
// the absolute path that jar lives at.
//
// A modded profile inherits from (or was flattened out of) a vanilla version.
// The official launcher uses the "jar" field for that; in the wild the vanilla
// client jar is often already copied into the profile's own directory (PCL and
// HMCL both do), so an existing local jar wins over re-downloading 40 MB into
// the parent's directory. Order: the "jar" field, a jar already in the version's
// own directory, the inherited parent, then its own directory as a last resort.
func ClientJarTarget(root, versionsDir string, v *Version) (jarID, path string) {
	dir := filepath.Join(root, versionsDir)
	own := filepath.Join(dir, v.ID, v.ID+".jar")
	if v.Jar != "" && v.Jar != v.ID {
		return v.Jar, filepath.Join(dir, v.Jar, v.Jar+".jar")
	}
	if _, err := os.Stat(own); err == nil {
		return v.ID, own
	}
	if v.InheritsFrom != "" {
		return v.InheritsFrom, filepath.Join(dir, v.InheritsFrom, v.InheritsFrom+".jar")
	}
	return v.ID, own
}

// Effective returns the merged version: child fields win, parent libraries and
// arguments come first. It is what launching and installing operate on.
func (v *Version) Effective() *Version {
	chain := []*Version{}
	for cur := v; cur != nil; cur = cur.parent {
		chain = append(chain, cur)
	}
	out := chain[len(chain)-1]
	for i := len(chain) - 2; i >= 0; i-- {
		out = merge(out, chain[i])
	}
	return out
}

func merge(parent, child *Version) *Version {
	out := *child
	if out.MainClass == "" {
		out.MainClass = parent.MainClass
	}
	if out.Assets == "" {
		out.Assets = parent.Assets
	}
	if out.AssetIndex == nil {
		out.AssetIndex = parent.AssetIndex
	}
	if out.JavaVersion == nil {
		out.JavaVersion = parent.JavaVersion
	}
	if out.Logging == nil {
		out.Logging = parent.Logging
	}
	if out.MinecraftArguments == "" {
		out.MinecraftArguments = parent.MinecraftArguments
	}
	if out.Jar == "" {
		out.Jar = parent.Jar
	}
	out.Downloads = map[string]Download{}
	for k, d := range parent.Downloads {
		out.Downloads[k] = d
	}
	for k, d := range child.Downloads {
		out.Downloads[k] = d
	}
	out.Arguments = mergeArguments(parent.Arguments, child.Arguments)
	out.Libraries = mergeLibraries(parent.Libraries, child.Libraries)
	return &out
}

func mergeArguments(parent, child *Arguments) *Arguments {
	switch {
	case parent == nil:
		return child
	case child == nil:
		return parent
	}
	out := &Arguments{}
	out.Game = append(append([]ArgValue{}, parent.Game...), child.Game...)
	out.JVM = append(append([]ArgValue{}, parent.JVM...), child.JVM...)
	return out
}

func mergeLibraries(parent, child []Library) []Library {
	out := make([]Library, 0, len(parent)+len(child))
	childByName := map[string]Library{}
	for _, l := range child {
		childByName[l.Name] = l
	}
	for _, l := range parent {
		if override, ok := childByName[l.Name]; ok {
			out = append(out, override)
			delete(childByName, l.Name)
			continue
		}
		out = append(out, l)
	}
	for _, l := range child {
		if _, ok := childByName[l.Name]; !ok {
			continue
		}
		out = append(out, l)
		delete(childByName, l.Name)
	}
	return out
}

// ---------------------------------------------------------------------------
// loading

// Loader reads version JSONs from disk and downloads them when missing.
type Loader struct {
	// Dir is the versions directory (…/versions).
	Dir    string
	Mapper mirror.Mapper
	Policy dl.Policy
	Log    *logx.Logger
	// DryRun forbids writing anything to disk.
	DryRun bool
}

// JSONPath is where a version's JSON lives.
func (l Loader) JSONPath(id string) string {
	return filepath.Join(l.Dir, id, id+".json")
}

// JarPath is where a version's client jar lives.
func (l Loader) JarPath(id string) string {
	return filepath.Join(l.Dir, id, id+".jar")
}

// NativesDir is where a version's natives are extracted.
func (l Loader) NativesDir(id string) string {
	return filepath.Join(l.Dir, id, "natives")
}

// Lookup reads a version JSON, downloading it when needed and following
// inheritsFrom recursively. officialURL may be empty for child versions.
func (l Loader) Lookup(id, officialURL string) (*Version, error) {
	return l.lookup(id, officialURL, 0)
}

func (l Loader) lookup(id, officialURL string, depth int) (*Version, error) {
	if depth > 5 {
		return nil, fmt.Errorf("inheritsFrom chain too deep at %s", id)
	}
	path := l.JSONPath(id)
	var v *Version
	if raw, err := os.ReadFile(path); err == nil {
		v = &Version{}
		if err := json.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		v.Path = path
		v.Source = "cache"
		l.log().Debugf("%s", i18n.T("mcv.loaded", id, path))
	} else {
		data, source, err := l.download(id, officialURL)
		if err != nil {
			return nil, fmt.Errorf("%s", i18n.T("mcv.version_json_missing", id, err))
		}
		v = &Version{}
		if err := json.Unmarshal(data, v); err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		v.Source = source
		if !l.DryRun {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return nil, err
			}
			v.Path = path
		}
		l.log().Infof("%s", i18n.T("mcv.loaded", id, source))
	}
	if v.ID == "" {
		v.ID = id
	}
	if v.InheritsFrom != "" {
		l.log().Debugf("%s", i18n.T("mcv.inherits", id, v.InheritsFrom))
		parent, err := l.lookup(v.InheritsFrom, "", depth+1)
		if err != nil {
			return nil, err
		}
		v.parent = parent
	}
	return v, nil
}

func (l Loader) download(id, officialURL string) ([]byte, string, error) {
	if officialURL == "" {
		// A child version without a manifest entry can still be fetched from
		// the mirror by id alone.
		data, source, err := l.Policy.FetchBytes(l.Mapper.VersionJSON(id, ""), id+".json")
		if err == nil {
			return data, source, nil
		}
		return nil, "", fmt.Errorf("%s", i18n.T("mcv.missing_sources", id))
	}
	return l.Policy.FetchBytes(l.Mapper.VersionJSON(id, officialURL), id+".json")
}

func (l Loader) log() *logx.Logger {
	if l.Log != nil {
		return l.Log
	}
	return logx.Discard()
}
