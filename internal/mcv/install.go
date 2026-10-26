package mcv

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/i18n"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// AssetObjectURL is where a plain (unmirrored) asset object lives.
const AssetObjectURL = "https://resources.download.minecraft.net"

// AssetIndex is the parsed asset index.
type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

// AssetObject is one entry of the asset index.
type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// Installer downloads everything a version needs to run.
type Installer struct {
	// Root is the .minecraft directory.
	Root string
	// VersionsDir is the version directory name (usually "versions").
	VersionsDir string
	Mapper      mirror.Mapper
	Policy      dl.Policy
	Log         *logx.Logger
	Env         Env
	// DryRun plans everything but downloads nothing.
	DryRun bool
	// Workers is the parallel download count for the many small files.
	Workers int
	// ConfirmTotal is asked once before a large batch (nil = always yes).
	ConfirmTotal func(desc string, files int, total int64) (bool, error)
}

// Result summarizes an install run.
type Result struct {
	VersionID      string
	ClientJar      string
	NativesDir     string
	AssetsIndex    string
	AssetsIndexID  string
	LogConfig      string
	TotalFiles     int
	Downloaded     int
	Skipped        int
	MissingFiles   int
	MissingBytes   int64
	AlreadyPresent bool
	NativesCount   int
}

// PlanOptions controls how a plan is built.
type PlanOptions struct {
	// FetchIndex fetches the asset index into memory when the local copy is
	// missing, so that a first install knows the whole asset set. Without it a
	// plan built on an empty cache would list the index file but no objects.
	FetchIndex bool
	// IncludeAssets enumerates the asset objects the index lists.
	IncludeAssets bool
}

// Plan lists every file a version needs on disk. Building one never writes and
// (apart from the optional index fetch) never downloads, which is what lets
// `mcli doctor` report on an install and `mcli repair` fix exactly the broken
// parts instead of re-downloading a whole version.
type Plan struct {
	VersionID     string
	ClientJar     string
	NativesDir    string
	AssetsIndex   string
	AssetsIndexID string
	LogConfig     string
	// Files are the version's own files: client jar, libraries, natives jars,
	// asset index and logging config.
	Files []dl.Item
	// Assets are the asset objects, empty when the index could not be read.
	Assets []dl.Item
	// IndexReadable reports whether the asset objects could be enumerated. When
	// it is false, Assets is empty: the index itself is not on disk yet.
	IndexReadable bool
	// AssetBytes is the total size of the asset objects.
	AssetBytes int64
	// NativesCount is how many natives jars the version lists.
	NativesCount int
}

// Items returns every file the plan needs, the version's own files first.
func (p *Plan) Items() []dl.Item {
	out := make([]dl.Item, 0, len(p.Files)+len(p.Assets))
	out = append(out, p.Files...)
	return append(out, p.Assets...)
}

// MissingKind says why a planned file cannot be used.
type MissingKind int

const (
	// MissingAbsent: there is no file at the destination.
	MissingAbsent MissingKind = iota
	// MissingWrongSize: a file is there, but not the expected size.
	MissingWrongSize
	// MissingWrongHash: right size, wrong digest.
	MissingWrongHash
)

// String is only used in logs and tests.
func (k MissingKind) String() string {
	switch k {
	case MissingWrongSize:
		return "wrong size"
	case MissingWrongHash:
		return "wrong hash"
	default:
		return "absent"
	}
}

// Missing is one planned file that is missing or wrong.
type Missing struct {
	Item dl.Item
	Kind MissingKind
}

// Check reports the planned files that are missing or wrong.
//
// The version's own files are always hash-verified — there are only a few dozen
// of them and a wrong library is a launch failure. Asset objects are checked for
// presence and size unless deep is set: hashing 460 MB of assets on every launch
// would be pure waste, so deep is for `mcli doctor --deep` and for repair.
func (p *Plan) Check(deep bool) []Missing {
	var out []Missing
	verify := func(items []dl.Item, hash bool) {
		for _, it := range items {
			st, err := os.Stat(it.Dest)
			if err != nil || st.IsDir() {
				out = append(out, Missing{Item: it, Kind: MissingAbsent})
				continue
			}
			if it.Size > 0 && st.Size() != it.Size {
				out = append(out, Missing{Item: it, Kind: MissingWrongSize})
				continue
			}
			if !hash {
				continue
			}
			if !it.Verify() {
				out = append(out, Missing{Item: it, Kind: MissingWrongHash})
			}
		}
	}
	verify(p.Files, true)
	verify(p.Assets, deep)
	return out
}

// MissingBytes totals the size of the unusable files.
func MissingBytes(missing []Missing) int64 {
	var total int64
	for _, m := range missing {
		total += m.Item.Size
	}
	return total
}

// Plan builds the file list for one version, without touching the disk.
func (in Installer) Plan(v *Version, opts PlanOptions) (*Plan, error) {
	if v.ID == "" {
		return nil, fmt.Errorf("%s", i18n.T("mcv.missing_sources", "unknown"))
	}
	if v.MainClass == "" {
		return nil, fmt.Errorf("%s", i18n.T("mcv.no_main_class", v.ID))
	}
	versionsDir := filepath.Join(in.Root, in.VersionsDir)
	libsDir := filepath.Join(in.Root, "libraries")
	assetsDir := filepath.Join(in.Root, "assets")

	plan := &Plan{
		VersionID:  v.ID,
		NativesDir: filepath.Join(versionsDir, v.ID, "natives"),
	}

	// --- client jar ------------------------------------------------------
	jarID, jarPath := ClientJarTarget(in.Root, in.VersionsDir, v)
	plan.ClientJar = jarPath
	client, err := in.clientItem(v, jarID, jarPath)
	if err != nil {
		return nil, err
	}
	plan.Files = append(plan.Files, client)

	// --- libraries -------------------------------------------------------
	for _, l := range v.Libraries {
		if !l.AllowedFor(in.Env) {
			continue
		}
		if art, ok := l.ArtifactURL(in.Env); ok && !l.IsNative(in.Env) {
			plan.Files = append(plan.Files, dl.Item{
				Candidates: in.Mapper.Library(art.URL),
				Dest:       libraryDest(libsDir, art, l, ""),
				SHA1:       art.SHA1,
				Size:       art.Size,
				Desc:       "library " + l.Name,
			})
		}
		if nat, ok := l.NativeURL(in.Env); ok {
			c, _ := l.NativeClassifier(in.Env)
			plan.Files = append(plan.Files, dl.Item{
				Candidates: in.Mapper.Library(nat.URL),
				Dest:       libraryDest(libsDir, nat, l, c),
				SHA1:       nat.SHA1,
				Size:       nat.Size,
				Desc:       "natives " + l.Name,
			})
			plan.NativesCount++
		}
	}

	// --- asset index -----------------------------------------------------
	if v.AssetIndex != nil && v.AssetIndex.URL != "" {
		name := assetIndexName(v)
		plan.AssetsIndexID = strings.TrimSuffix(name, ".json")
		plan.AssetsIndex = filepath.Join(assetsDir, "indexes", name)
		plan.Files = append(plan.Files, dl.Item{
			Candidates: in.Mapper.Candidates(v.AssetIndex.URL),
			Dest:       plan.AssetsIndex,
			SHA1:       v.AssetIndex.SHA1,
			Size:       v.AssetIndex.Size,
			Desc:       "asset index " + plan.AssetsIndexID,
		})
	}

	// --- logging config --------------------------------------------------
	if v.Logging != nil && v.Logging.Client != nil && v.Logging.Client.File.URL != "" {
		f := v.Logging.Client.File
		plan.LogConfig = filepath.Join(assetsDir, "log_configs", f.ID)
		plan.Files = append(plan.Files, dl.Item{
			Candidates: in.Mapper.Candidates(f.URL),
			Dest:       plan.LogConfig,
			SHA1:       f.SHA1,
			Size:       f.Size,
			Desc:       "logging config " + f.ID,
		})
	}

	// --- asset objects ---------------------------------------------------
	if opts.IncludeAssets && v.AssetIndex != nil {
		objects, err := in.assetObjects(plan.AssetsIndex, v.AssetIndex.URL, opts.FetchIndex)
		if err != nil {
			return nil, err
		}
		if objects != nil {
			plan.IndexReadable = true
			objectsDir := filepath.Join(assetsDir, "objects")
			for _, o := range objects {
				plan.AssetBytes += o.Size
				plan.Assets = append(plan.Assets, dl.Item{
					Candidates: in.Mapper.Asset(AssetObjectURL + "/" + o.Hash[:2] + "/" + o.Hash),
					Dest:       filepath.Join(objectsDir, o.Hash[:2], o.Hash),
					SHA1:       o.Hash,
					Size:       o.Size,
					Desc:       "asset " + o.Hash[:8],
				})
			}
		}
	}
	return plan, nil
}

// Ensure makes the version ready to launch: client jar, libraries, natives,
// asset index, assets and the logging config.
func (in Installer) Ensure(v *Version) (*Result, error) {
	plan, err := in.Plan(v, PlanOptions{IncludeAssets: true, FetchIndex: true})
	if err != nil {
		return nil, err
	}
	res := &Result{
		VersionID:     plan.VersionID,
		ClientJar:     plan.ClientJar,
		NativesDir:    plan.NativesDir,
		AssetsIndex:   plan.AssetsIndex,
		AssetsIndexID: plan.AssetsIndexID,
		LogConfig:     plan.LogConfig,
		NativesCount:  plan.NativesCount,
	}
	libsDir := filepath.Join(in.Root, "libraries")
	in.log().Infof("%s", i18n.T("mcv.installing", v.ID, in.Root))
	if plan.IndexReadable {
		in.log().Infof("%s", i18n.T("mcv.assets_index", plan.AssetsIndexID, len(plan.Assets), humanBytes(plan.AssetBytes)))
	}

	missing := plan.Check(false)
	res.TotalFiles = len(plan.Files) + len(plan.Assets)
	res.Skipped = res.TotalFiles - len(missing)
	res.MissingFiles = len(missing)
	res.MissingBytes = MissingBytes(missing)
	in.log().Infof("%s", i18n.T("mcv.install_summary", res.MissingFiles, humanBytes(res.MissingBytes), res.Skipped))

	if res.MissingFiles == 0 {
		res.AlreadyPresent = true
		in.log().Infof("%s", i18n.T("mcv.already_installed", v.ID))
		if !in.DryRun {
			if err := in.extractAllNatives(v, libsDir, plan.NativesDir); err != nil {
				return nil, err
			}
		}
		return res, nil
	}
	if in.DryRun {
		in.log().Infof("%s", i18n.T("mcv.dry_run"))
		return res, nil
	}

	// --- confirmation and downloads --------------------------------------
	if in.ConfirmTotal != nil {
		ok, err := in.ConfirmTotal(v.ID, res.MissingFiles, res.MissingBytes)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s", i18n.T("dl.declined", v.ID))
		}
	}
	items := make([]dl.Item, 0, len(missing))
	for _, m := range missing {
		items = append(items, m.Item)
	}
	if err := in.fetchAll(items, res); err != nil {
		return nil, err
	}
	if err := in.extractAllNatives(v, libsDir, plan.NativesDir); err != nil {
		return nil, err
	}
	in.log().Infof("%s", i18n.T("mcv.install_done", v.ID))
	return res, nil
}

func (in Installer) clientItem(v *Version, jarID, dest string) (dl.Item, error) {
	// The mirror's client endpoint is keyed by version id, and for an inherited
	// jar that key is the parent (the vanilla) version.
	if d, ok := v.Downloads["client"]; ok && d.URL != "" {
		return dl.Item{
			Candidates: in.Mapper.ClientJar(jarID, d.URL),
			Dest:       dest,
			SHA1:       d.SHA1,
			Size:       d.Size,
			Desc:       "client " + jarID,
		}, nil
	}
	if in.Mapper.Endpoint == "" {
		return dl.Item{}, fmt.Errorf("%s", i18n.T("mcv.no_client_jar", v.ID))
	}
	return dl.Item{
		Candidates: in.Mapper.ClientJar(jarID, ""),
		Dest:       dest,
		Desc:       "client " + jarID,
	}, nil
}

// assetObjects returns the objects an asset index lists.
//
// The index is read from disk; when it is missing and allowFetch is set it is
// fetched into memory without being written, because a first install has to know
// the object list before the index file itself has been downloaded. A corrupt
// local index is an error; a failed fetch is not (a plan without asset counts is
// still useful, and doctor should work offline).
func (in Installer) assetObjects(indexPath, indexURL string, allowFetch bool) (map[string]AssetObject, error) {
	if indexPath == "" {
		return nil, nil
	}
	if raw, err := os.ReadFile(indexPath); err == nil {
		idx := &AssetIndex{}
		if err := json.Unmarshal(raw, idx); err != nil {
			return nil, fmt.Errorf("%s: %v", indexPath, err)
		}
		return idx.Objects, nil
	}
	if !allowFetch || indexURL == "" {
		return nil, nil
	}
	data, _, err := in.Policy.FetchBytes(in.Mapper.Candidates(indexURL), "asset index")
	if err != nil {
		return nil, nil
	}
	idx := &AssetIndex{}
	if err := json.Unmarshal(data, idx); err != nil {
		return nil, nil
	}
	return idx.Objects, nil
}

func (in Installer) fetchAll(items []dl.Item, res *Result) error {
	if len(items) == 0 {
		return nil
	}
	if err := dl.FetchAll(in.Policy, items, in.Workers, in.log()); err != nil {
		return err
	}
	res.Downloaded = len(items)
	return nil
}

func (in Installer) extractAllNatives(v *Version, libsDir, nativesDir string) error {
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return err
	}
	// The version's arguments may point at subdirectories (jna/lwjgl/netty/java);
	// create them so nothing references a missing directory.
	targets := v.NativesTargets(nativesDir)
	for _, t := range targets {
		if err := os.MkdirAll(t, 0o755); err != nil {
			return err
		}
	}
	in.log().Infof("%s", i18n.T("mcv.natives_extract", nativesDir))
	for _, l := range v.Libraries {
		if !l.AllowedFor(in.Env) {
			continue
		}
		nat, ok := l.NativeURL(in.Env)
		if !ok {
			continue
		}
		c, _ := l.NativeClassifier(in.Env)
		src := libraryDest(libsDir, nat, l, c)
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("%s", i18n.T("mcv.missing_sources", l.Name))
		}
		for _, dir := range nativeDirsFor(targets, nativesDir, l.Name) {
			if err := ExtractNatives(src, dir, l); err != nil {
				return err
			}
		}
	}
	return nil
}

// nativeDirsFor decides where one natives jar is unpacked. The base directory
// always gets the files (that is where a legacy java.library.path points), plus
// the family subdirectory the version asks for: LWJGL's natives belong in
// …/natives/lwjgl, JNA's in …/natives/jna, Netty's in …/natives/netty, and
// everything else in the first directory java.library.path mentions.
func nativeDirsFor(targets []string, nativesDir, libName string) []string {
	dirs := []string{nativesDir}
	family := ""
	switch {
	case strings.HasPrefix(libName, "org.lwjgl"):
		family = "lwjgl"
	case strings.HasPrefix(libName, "net.java.dev.jna"):
		family = "jna"
	case strings.HasPrefix(libName, "io.netty"):
		family = "netty"
	}
	extra := ""
	if family != "" {
		for _, t := range targets {
			if filepath.Base(t) == family {
				extra = t
				break
			}
		}
	} else {
		for _, t := range targets {
			if t != nativesDir {
				extra = t
				break
			}
		}
	}
	if extra != "" && extra != nativesDir {
		dirs = append(dirs, extra)
	}
	return dirs
}

// ReadAssetIndex parses an index file.
func ReadAssetIndex(path string) (*AssetIndex, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	idx := &AssetIndex{}
	if err := json.Unmarshal(raw, idx); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return idx, nil
}

// ExtractNatives unzips one natives jar into dir, honouring the library's
// extract.exclude list and skipping metadata.
func ExtractNatives(jarPath, dir string, l Library) error {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return fmt.Errorf("%s: %v", jarPath, err)
	}
	defer r.Close()
	for _, f := range r.File {
		name := f.Name
		if strings.HasSuffix(name, "/") {
			continue
		}
		if strings.HasPrefix(name, "META-INF/") {
			continue
		}
		if excluded(name, l) {
			continue
		}
		clean := filepath.Clean(strings.TrimPrefix(name, "/"))
		if strings.HasPrefix(clean, "..") {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if fi, err := os.Stat(dest); err == nil && fi.Size() == int64(f.UncompressedSize64) {
			continue
		}
		if err := writeZipEntry(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func excluded(name string, l Library) bool {
	if l.Extract == nil {
		return false
	}
	for _, ex := range l.Extract.Exclude {
		if ex != "" && strings.HasPrefix(name, ex) {
			return true
		}
	}
	return false
}

func writeZipEntry(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	return nil
}

// libraryDest picks the on-disk location for a library download, preferring
// the path from the version JSON and falling back to the maven layout.
func libraryDest(libsDir string, d Download, l Library, classifier string) string {
	if d.Path != "" {
		return filepath.Join(libsDir, filepath.FromSlash(d.Path))
	}
	return filepath.Join(libsDir, filepath.FromSlash(l.MavenPath(classifier)))
}

// AssetIndexName is the file name of this version's asset index, as stored in
// assets/indexes. Exported so `mcli instance`/`mcli doctor` can check for it
// without duplicating the fallback chain.
func (v *Version) AssetIndexName() string { return assetIndexName(v) }

func assetIndexName(v *Version) string {
	if v.AssetIndex != nil {
		if v.AssetIndex.Path != "" {
			return filepath.Base(v.AssetIndex.Path)
		}
		if v.AssetIndex.URL != "" {
			return filepath.Base(v.AssetIndex.URL)
		}
	}
	if v.Assets != "" {
		return v.Assets + ".json"
	}
	return "assets.json"
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f%s", v, units[i])
}

func (in Installer) log() *logx.Logger {
	if in.Log != nil {
		return in.Log
	}
	return logx.Discard()
}
