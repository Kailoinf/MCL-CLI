// Package mrpack reads Modrinth modpacks: the .mrpack file is a zip whose
// modrinth.index.json lists the files a pack wants, where they go inside the
// instance, and which game version and loader the pack is built for. A launcher
// that installs a pack is mostly copying files to paths someone else decided.
//
// Only what installing needs is read: the index, the file list with its hashes,
// and the overrides (game options, configs, a pre-made world) that ship alongside.
package mrpack

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// Index is the modrinth.index.json of a pack.
type Index struct {
	FormatVersion int               `json:"formatVersion"`
	Name          string            `json:"name"`
	VersionID     string            `json:"versionId"`
	Summary       string            `json:"summary"`
	Dependencies  map[string]string `json:"dependencies"` // "minecraft", "fabric-loader", …
	Files         []File            `json:"files"`
}

// File is one file the pack wants, at a path relative to the instance directory.
type File struct {
	Path       string            `json:"path"`
	Hashes     map[string]string `json:"hashes"`
	FileSize   int64             `json:"fileSize"`
	Downloads  []string          `json:"downloads"`
	Env        map[string]string `json:"env"`
	KnownHoles []string          `json:"-"`
}

// SHA1 is the digest to verify against, when the pack lists one.
func (f File) SHA1() string { return strings.ToLower(f.Hashes["sha1"]) }

// ClientWanted reports whether a file is meant for the client. A pack may ship
// server-only files; installing those into a client instance is a mistake the
// index itself can prevent.
func (f File) ClientWanted() bool {
	switch strings.ToLower(f.Env["client"]) {
	case "unsupported", "false":
		return false
	}
	return true
}

// GameVersion is the Minecraft version the pack is built for.
func (i Index) GameVersion() string { return i.Dependencies["minecraft"] }

// Loader returns the loader name and version the pack asks for, if it names one.
// Modrinth uses keys like "fabric-loader", "quilt-loader", "forge", "neoforge".
func (i Index) Loader() (string, string) {
	for _, key := range []string{"fabric-loader", "quilt-loader", "neoforge", "forge"} {
		if v := i.Dependencies[key]; v != "" {
			return strings.TrimSuffix(key, "-loader"), v
		}
	}
	return "", ""
}

// Open reads the index of a .mrpack file.
func Open(packPath string) (Index, error) {
	zr, err := zip.OpenReader(packPath)
	if err != nil {
		return Index{}, fmt.Errorf("not a readable .mrpack file: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if path.Base(f.Name) != "modrinth.index.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Index{}, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return Index{}, err
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			return Index{}, fmt.Errorf("modrinth.index.json is not readable: %v", err)
		}
		if idx.Name == "" || len(idx.Files) == 0 {
			return Index{}, fmt.Errorf("the pack index names no files")
		}
		if idx.GameVersion() == "" {
			return Index{}, fmt.Errorf("the pack index does not say which Minecraft version it is for")
		}
		return idx, nil
	}
	return Index{}, fmt.Errorf("no modrinth.index.json inside the pack")
}

// Items plans the download of every file the pack wants, rooted at instanceDir.
// Paths are taken from the index as they are, but anything that would escape the
// instance directory is refused: a pack is data from the internet, not an
// instruction to write outside the game.
func Items(idx Index, instanceDir string) ([]dl.Item, error) {
	out := make([]dl.Item, 0, len(idx.Files))
	for _, f := range idx.Files {
		if !f.ClientWanted() || len(f.Downloads) == 0 {
			continue
		}
		dest, err := Destination(instanceDir, f.Path)
		if err != nil {
			return nil, err
		}
		cands := make([]mirror.Candidate, 0, len(f.Downloads))
		for _, u := range f.Downloads {
			cands = append(cands, mirror.Candidate{URL: u, Source: "modpack"})
		}
		out = append(out, dl.Item{
			Candidates: cands,
			Dest:       dest,
			SHA1:       f.SHA1(),
			Size:       f.FileSize,
			Desc:       path.Base(f.Path),
		})
	}
	return out, nil
}

// Destination resolves a pack path inside the instance directory. An absolute
// path, or one that climbs out of the instance, is an error: a pack is data from
// the internet, and quietly rewriting where it wanted to write would hide a
// malformed pack instead of reporting it.
func Destination(instanceDir, packPath string) (string, error) {
	raw := strings.ReplaceAll(packPath, "\\", "/")
	if raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "~") {
		return "", fmt.Errorf("the pack names an unusable path: %q", packPath)
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("the pack names a path outside the instance: %q", packPath)
	}
	dest := filepath.Join(instanceDir, filepath.FromSlash(clean))
	rel, err := filepath.Rel(instanceDir, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the pack names a path outside the instance: %q", packPath)
	}
	return dest, nil
}

// Paths lists the instance-relative paths an index wants, client side only, so a
// later update can compare two pack versions.
func Paths(idx Index) []string {
	out := make([]string, 0, len(idx.Files))
	for _, f := range idx.Files {
		if !f.ClientWanted() || len(f.Downloads) == 0 {
			continue
		}
		out = append(out, f.Path)
	}
	return out
}

// Digests maps each client-side path a pack wants to the SHA-1 it declares. It is
// what gets recorded when a pack is installed: the digest, not just the name, is
// what lets a later update leave someone's own build of the same file alone.
func Digests(idx Index) map[string]string {
	out := map[string]string{}
	for _, f := range idx.Files {
		if !f.ClientWanted() || len(f.Downloads) == 0 {
			continue
		}
		out[f.Path] = f.SHA1()
	}
	return out
}

// Removed lists the files an older pack installed that the newer one no longer
// wants. Paths are compared as the packs wrote them, which is what makes the
// answer trustworthy: anything the user dropped in by hand is not in the old list
// and is therefore never touched.
func Removed(installed map[string]string, next Index) []string {
	wanted := map[string]bool{}
	for _, p := range Paths(next) {
		wanted[p] = true
	}
	var out []string
	for p := range installed {
		if !wanted[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// TotalBytes is how much the pack's files weigh, for the confirmation prompt.
func TotalBytes(items []dl.Item) int64 {
	var total int64
	for _, it := range items {
		total += it.Size
	}
	return total
}

// ExtractOverrides copies the override folders a pack ships into the instance
// directory: `overrides/` for both sides, `client-overrides/` for the client.
// Existing files are kept — a pack must not silently overwrite a world or a
// config someone tuned by hand — and the ones that were skipped are returned.
func ExtractOverrides(packPath, instanceDir string) (written []string, kept []string, err error) {
	zr, err := zip.OpenReader(packPath)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		dir, rel := overridePath(f.Name)
		if dir == "" {
			continue
		}
		// A zip stores its folders as entries of their own ("overrides/" with no
		// name after the prefix). They carry nothing to extract, and resolving an
		// empty path is an error — so they are skipped, not reported.
		if f.FileInfo().IsDir() || rel == "" {
			continue
		}
		dest, err := Destination(instanceDir, rel)
		if err != nil {
			return written, kept, err
		}
		if _, err := os.Stat(dest); err == nil {
			kept = append(kept, rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return written, kept, err
		}
		if err := extractOne(f, dest); err != nil {
			return written, kept, err
		}
		written = append(written, rel)
	}
	return written, kept, nil
}

// overridePath reports the override folder a zip entry belongs to and the path it
// maps to inside the instance.
func overridePath(name string) (dir, rel string) {
	n := strings.ReplaceAll(name, "\\", "/")
	for _, d := range []string{"overrides/", "client-overrides/"} {
		if strings.HasPrefix(n, d) {
			return d, strings.TrimPrefix(n, d)
		}
	}
	return "", ""
}

// extractOne writes one zip entry to disk, refusing paths that escape the instance.
func extractOne(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}
