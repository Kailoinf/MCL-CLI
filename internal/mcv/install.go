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

// Ensure makes the version ready to launch: client jar, libraries, natives,
// asset index, assets and the logging config.
func (in Installer) Ensure(v *Version) (*Result, error) {
	if v.ID == "" {
		return nil, fmt.Errorf("%s", i18n.T("mcv.missing_sources", "unknown"))
	}
	if v.MainClass == "" {
		return nil, fmt.Errorf("%s", i18n.T("mcv.no_main_class", v.ID))
	}
	versionsDir := filepath.Join(in.Root, in.VersionsDir)
	libsDir := filepath.Join(in.Root, "libraries")
	assetsDir := filepath.Join(in.Root, "assets")
	indexesDir := filepath.Join(assetsDir, "indexes")
	objectsDir := filepath.Join(assetsDir, "objects")
	logDir := filepath.Join(assetsDir, "log_configs")

	res := &Result{
		VersionID:  v.ID,
		ClientJar:  filepath.Join(versionsDir, v.ID, v.ID+".jar"),
		NativesDir: filepath.Join(versionsDir, v.ID, "natives"),
	}
	in.log().Infof("%s", i18n.T("mcv.installing", v.ID, in.Root))

	// --- client jar ------------------------------------------------------
	client, err := in.clientItem(v, res.ClientJar)
	if err != nil {
		return nil, err
	}
	head := []dl.Item{client}

	// --- libraries -------------------------------------------------------
	for _, l := range v.Libraries {
		if !l.AllowedFor(in.Env) {
			continue
		}
		if art, ok := l.ArtifactURL(in.Env); ok && !l.IsNative(in.Env) {
			dest := libraryDest(libsDir, art, l, "")
			head = append(head, dl.Item{
				Candidates: in.Mapper.Library(art.URL),
				Dest:       dest,
				SHA1:       art.SHA1,
				Size:       art.Size,
				Desc:       "library " + l.Name,
			})
		}
		if nat, ok := l.NativeURL(in.Env); ok {
			c, _ := l.NativeClassifier(in.Env)
			dest := libraryDest(libsDir, nat, l, c)
			head = append(head, dl.Item{
				Candidates: in.Mapper.Library(nat.URL),
				Dest:       dest,
				SHA1:       nat.SHA1,
				Size:       nat.Size,
				Desc:       "natives " + l.Name,
			})
			res.NativesCount++
		}
	}

	// --- asset index -----------------------------------------------------
	if v.AssetIndex != nil && v.AssetIndex.URL != "" {
		name := assetIndexName(v)
		res.AssetsIndexID = strings.TrimSuffix(name, ".json")
		res.AssetsIndex = filepath.Join(indexesDir, name)
		head = append(head, dl.Item{
			Candidates: in.Mapper.Candidates(v.AssetIndex.URL),
			Dest:       res.AssetsIndex,
			SHA1:       v.AssetIndex.SHA1,
			Size:       v.AssetIndex.Size,
			Desc:       "asset index " + res.AssetsIndexID,
		})
	}

	// --- logging config --------------------------------------------------
	if v.Logging != nil && v.Logging.Client != nil && v.Logging.Client.File.URL != "" {
		f := v.Logging.Client.File
		res.LogConfig = filepath.Join(logDir, f.ID)
		head = append(head, dl.Item{
			Candidates: in.Mapper.Candidates(f.URL),
			Dest:       res.LogConfig,
			SHA1:       f.SHA1,
			Size:       f.Size,
			Desc:       "logging config " + f.ID,
		})
	}

	// --- plan ------------------------------------------------------------
	res.TotalFiles = len(head)
	missing := []dl.Item{}
	for _, it := range head {
		if it.Verify() {
			res.Skipped++
			continue
		}
		missing = append(missing, it)
		res.MissingBytes += it.Size
	}

	// asset objects are only enumerable after the index is readable
	var assetItems []dl.Item
	if v.AssetIndex != nil {
		objects, err := in.loadIndexForPlan(res, v.AssetIndex.URL)
		if err != nil {
			return nil, err
		}
		if objects != nil {
			var total int64
			for _, o := range objects {
				total += o.Size
			}
			in.log().Infof("%s", i18n.T("mcv.assets_index", res.AssetsIndexID, len(objects), humanBytes(total)))
			for _, o := range objects {
				dest := filepath.Join(objectsDir, o.Hash[:2], o.Hash)
				it := dl.Item{
					Candidates: in.Mapper.Asset(AssetObjectURL + "/" + o.Hash[:2] + "/" + o.Hash),
					Dest:       dest,
					SHA1:       o.Hash,
					Size:       o.Size,
					Desc:       "asset " + o.Hash[:8],
				}
				res.TotalFiles++
				if it.Verify() {
					res.Skipped++
					continue
				}
				assetItems = append(assetItems, it)
				res.MissingBytes += o.Size
			}
		}
	}
	missing = append(missing, assetItems...)
	res.MissingFiles = len(missing)

	in.log().Infof("%s", i18n.T("mcv.install_summary", res.MissingFiles, humanBytes(res.MissingBytes), res.Skipped))
	if res.MissingFiles == 0 {
		res.AlreadyPresent = true
		in.log().Infof("%s", i18n.T("mcv.already_installed", v.ID))
		if !in.DryRun {
			if err := in.extractAllNatives(v, libsDir, res.NativesDir); err != nil {
				return nil, err
			}
		}
		return res, nil
	}
	if in.DryRun {
		in.log().Infof("%s", i18n.T("mcv.dry_run"))
		return res, nil
	}

	// --- confirmation and download ---------------------------------------
	if in.ConfirmTotal != nil && len(missing) > 0 {
		ok, err := in.ConfirmTotal(v.ID, len(missing), res.MissingBytes)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s", i18n.T("dl.declined", v.ID))
		}
	}
	if err := in.fetchAll(head, res); err != nil {
		return nil, err
	}
	if err := in.fetchAll(assetItems, res); err != nil {
		return nil, err
	}
	if err := in.extractAllNatives(v, libsDir, res.NativesDir); err != nil {
		return nil, err
	}
	in.log().Infof("%s", i18n.T("mcv.install_done", v.ID))
	return res, nil
}

func (in Installer) clientItem(v *Version, dest string) (dl.Item, error) {
	if d, ok := v.Downloads["client"]; ok && d.URL != "" {
		return dl.Item{
			Candidates: in.Mapper.ClientJar(v.ID, d.URL),
			Dest:       dest,
			SHA1:       d.SHA1,
			Size:       d.Size,
			Desc:       "client " + v.ID,
		}, nil
	}
	if in.Mapper.Endpoint == "" {
		return dl.Item{}, fmt.Errorf("%s", i18n.T("mcv.no_client_jar", v.ID))
	}
	return dl.Item{
		Candidates: in.Mapper.ClientJar(v.ID, ""),
		Dest:       dest,
		Desc:       "client " + v.ID,
	}, nil
}

// loadIndexForPlan returns the asset objects for planning. It reads the index
// from disk, or, in dry runs and when the file is missing, fetches it into
// memory without writing anything.
func (in Installer) loadIndexForPlan(res *Result, indexURL string) (map[string]AssetObject, error) {
	if res.AssetsIndex == "" {
		return nil, nil
	}
	if raw, err := os.ReadFile(res.AssetsIndex); err == nil {
		idx := &AssetIndex{}
		if err := json.Unmarshal(raw, idx); err != nil {
			return nil, fmt.Errorf("%s: %v", res.AssetsIndex, err)
		}
		return idx.Objects, nil
	}
	if !in.DryRun || indexURL == "" {
		return nil, nil
	}
	data, _, err := in.Policy.FetchBytes(in.Mapper.Candidates(indexURL), "asset index "+res.AssetsIndexID)
	if err != nil {
		return nil, nil // planning without asset counts is acceptable
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
		if err := ExtractNatives(src, nativesDir, l); err != nil {
			return err
		}
	}
	return nil
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
