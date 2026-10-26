// Package curseforge speaks to CurseForge's REST API, which — unlike Modrinth —
// needs an API key belonging to the person using it. Nothing here runs without that
// key, and the package says so instead of failing somewhere deeper.
//
// The shapes below come from CurseForge's published API: base
// https://api.curseforge.com, GET /v1/mods/search (gameId required, classId to pick
// the content type), GET /v1/mods/{id}/files, and the x-api-key header. What is
// pinned by tests is the request that goes out and the parsing of the answer; the
// live calls have not been exercised, because that needs a key (see docs/status.md).
package curseforge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// BaseURL is the API root.
const BaseURL = "https://api.curseforge.com"

// GameMinecraft is the game id every request carries.
const GameMinecraft = 432

// Class ids, by content type. They are what CurseForge's own discover page uses.
const (
	ClassMods          = 6
	ClassResourcePacks = 12
	ClassModpacks      = 4471
	ClassShaderPacks   = 6552
	ClassDataPacks     = 6945
	ClassWorlds        = 17
)

// Mod loader ids for the modLoaderType filter.
const (
	LoaderForge    = 1
	LoaderFabric   = 4
	LoaderQuilt    = 5
	LoaderNeoForge = 6
)

// Hash algorithms in a file's hash list.
const (
	HashSHA1 = 1
	HashMD5  = 2
)

// ErrNoKey is returned instead of making a request when no API key is configured.
// It is a sentinel so the CLI can turn it into the one instruction that helps.
var ErrNoKey = errors.New("no CurseForge API key")

// ErrKeyRejected means the key exists but CurseForge refused it (401/403).
var ErrKeyRejected = errors.New("CurseForge rejected the API key")

// ClassID maps a project type name onto CurseForge's class id.
func ClassID(projectType string) (int, bool) {
	switch strings.ToLower(projectType) {
	case "mod", "mods":
		return ClassMods, true
	case "resourcepack", "resourcepacks", "texture-pack", "texturepack":
		return ClassResourcePacks, true
	case "modpack", "modpacks":
		return ClassModpacks, true
	case "shader", "shaders", "shaderpack", "shaderpacks":
		return ClassShaderPacks, true
	case "datapack", "datapacks":
		return ClassDataPacks, true
	case "world", "worlds":
		return ClassWorlds, true
	}
	return 0, false
}

// LoaderID maps a loader name onto CurseForge's modLoaderType filter.
func LoaderID(loader string) (int, bool) {
	switch strings.ToLower(loader) {
	case "forge":
		return LoaderForge, true
	case "fabric":
		return LoaderFabric, true
	case "quilt":
		return LoaderQuilt, true
	case "neoforge":
		return LoaderNeoForge, true
	}
	return 0, false
}

// Client is one API key against one base URL.
type Client struct {
	Base   string // empty means BaseURL (tests point it at a stub server)
	Key    string
	Policy dl.Policy
}

// Author is the name shown next to a project.
type Author struct {
	Name string `json:"name"`
}

// Hash is one digest a file publishes; Algo 1 is SHA-1.
type Hash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"`
}

// Dependency names another project a file needs, optionally a specific version.
type Dependency struct {
	ModID        int `json:"modId"`
	RelationType int `json:"relationType"` // 3 is "required"
}

// File is one downloadable file of a project.
type File struct {
	ID           int          `json:"id"`
	ModID        int          `json:"modId"`
	DisplayName  string       `json:"displayName"`
	FileName     string       `json:"fileName"`
	ReleaseType  int          `json:"releaseType"` // 1 release, 2 beta, 3 alpha
	FileDate     string       `json:"fileDate"`
	FileLength   int64        `json:"fileLength"`
	DownloadURL  string       `json:"downloadUrl"`
	GameVersions []string     `json:"gameVersions"`
	Hashes       []Hash       `json:"hashes"`
	Dependencies []Dependency `json:"dependencies"`
}

// SHA1 returns the file's SHA-1 when it publishes one.
func (f File) SHA1() string {
	for _, h := range f.Hashes {
		if h.Algo == HashSHA1 {
			return strings.ToLower(h.Value)
		}
	}
	return ""
}

// RequiredDependencies lists the projects a file says it needs.
func (f File) RequiredDependencies() []int {
	var out []int
	for _, d := range f.Dependencies {
		if d.RelationType == 3 {
			out = append(out, d.ModID)
		}
	}
	return out
}

// Mod is one project.
type Mod struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Summary       string   `json:"summary"`
	DownloadCount int64    `json:"downloadCount"`
	ClassID       int      `json:"classId"`
	Authors       []Author `json:"authors"`
	LatestFiles   []File   `json:"latestFiles"`
	Links         struct {
		Website string `json:"websiteUrl"`
	} `json:"links"`
}

// AuthorLine is the comma-separated author list, for a table cell.
func (m Mod) AuthorLine() string {
	names := make([]string, 0, len(m.Authors))
	for _, a := range m.Authors {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

// Page is one page of search results.
type Page struct {
	Data       []Mod `json:"data"`
	Pagination struct {
		Index       int `json:"index"`
		PageSize    int `json:"pageSize"`
		ResultCount int `json:"resultCount"`
		TotalCount  int `json:"totalCount"`
	} `json:"pagination"`
}

// SearchQuery is what a caller wants to search for.
type SearchQuery struct {
	Text        string
	ClassID     int // 0 means any
	GameVersion string
	LoaderID    int // 0 means any
	Index       int
	PageSize    int
}

// Search asks CurseForge for one page of projects.
func (c Client) Search(q SearchQuery) (Page, error) {
	values := url.Values{}
	values.Set("gameId", strconv.Itoa(GameMinecraft))
	if q.Text != "" {
		values.Set("searchFilter", q.Text)
	}
	if q.ClassID != 0 {
		values.Set("classId", strconv.Itoa(q.ClassID))
	}
	if q.GameVersion != "" {
		values.Set("gameVersion", q.GameVersion)
	}
	if q.LoaderID != 0 {
		values.Set("modLoaderType", strconv.Itoa(q.LoaderID))
	}
	size := q.PageSize
	if size <= 0 {
		size = 10
	}
	values.Set("index", strconv.Itoa(q.Index))
	values.Set("pageSize", strconv.Itoa(size))
	values.Set("sortField", "2") // popularity
	values.Set("sortOrder", "desc")

	var page Page
	if err := c.get("/v1/mods/search?"+values.Encode(), &page); err != nil {
		return Page{}, err
	}
	return page, nil
}

// Files lists a project's files, newest first as the API returns them.
func (c Client) Files(modID int) ([]File, error) {
	var out struct {
		Data []File `json:"data"`
	}
	if err := c.get(fmt.Sprintf("/v1/mods/%d/files", modID), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Find looks a project up by slug or name and returns it when the slug matches
// exactly. CurseForge has no slug endpoint, so the search is the lookup.
func (c Client) Find(slugOrName string, classID int) (Mod, bool, error) {
	page, err := c.Search(SearchQuery{Text: slugOrName, ClassID: classID, PageSize: 20})
	if err != nil {
		return Mod{}, false, err
	}
	want := strings.ToLower(strings.TrimSpace(slugOrName))
	for _, m := range page.Data {
		if strings.ToLower(m.Slug) == want || strings.EqualFold(m.Name, slugOrName) {
			return m, true, nil
		}
	}
	return Mod{}, false, nil
}

// get performs one authenticated request and decodes its body. The key travels in
// the x-api-key header, as CurseForge requires, and is never printed: a rejected key
// comes back as ErrKeyRejected rather than as the header it was sent with.
func (c Client) get(path string, into interface{}) error {
	if strings.TrimSpace(c.Key) == "" {
		return ErrNoKey
	}
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	cands := []mirror.Candidate{{
		URL:    base + path,
		Source: "curseforge",
		Headers: map[string]string{
			"x-api-key": c.Key,
			"Accept":    "application/json",
		},
	}}
	body, _, err := c.Policy.FetchBytes(cands, "curseforge "+path)
	if err != nil {
		var status dl.HTTPStatusError
		if errors.As(err, &status) && (status.Code == 401 || status.Code == 403) {
			return ErrKeyRejected
		}
		return err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("curseforge answered with something unreadable: %v", err)
	}
	return nil
}

// DirFor is the directory a content type lives in inside an instance directory.
func DirFor(classID int) (string, bool) {
	switch classID {
	case ClassMods:
		return "mods", true
	case ClassResourcePacks:
		return "resourcepacks", true
	case ClassShaderPacks:
		return "shaderpacks", true
	case ClassDataPacks:
		return "datapacks", true
	}
	return "", false
}

// PickFile chooses the file to install: the newest release that names the game
// version, and, when a loader is given, one that names it too. A beta is only used
// when no release fits, and an alpha only when nothing else does — the same order a
// person would use when they look at the files list.
func PickFile(files []File, gameVersion, loader string) (File, bool) {
	matches := make([]File, 0, len(files))
	for _, f := range files {
		if f.DownloadURL == "" {
			continue
		}
		if gameVersion != "" && !namesVersion(f, gameVersion) {
			continue
		}
		if loader != "" && !namesLoader(f, loader) {
			continue
		}
		matches = append(matches, f)
	}
	if len(matches) == 0 {
		return File{}, false
	}
	// newest first: the API's dates are ISO-8601, so they sort as text
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].FileDate > matches[j].FileDate })
	for _, want := range []int{1, 2, 3} { // release, beta, alpha
		for _, f := range matches {
			if f.ReleaseType == want {
				return f, true
			}
		}
	}
	return matches[0], true
}

// namesVersion reports whether a file's game versions include this one, accepting a
// "1.21.4" match inside an entry like "1.21.4-rc1".
func namesVersion(f File, version string) bool {
	for _, gv := range f.GameVersions {
		if gv == version || strings.HasPrefix(gv, version+"-") || strings.HasPrefix(gv, version+"+") {
			return true
		}
	}
	return false
}

// namesLoader reports whether a file lists this loader. CurseForge mixes loaders into
// gameVersions ("Fabric", "Forge", "NeoForge"), which is why the check is a scan.
func namesLoader(f File, loader string) bool {
	for _, gv := range f.GameVersions {
		if strings.EqualFold(gv, loader) {
			return true
		}
		if loader == "neoforge" && strings.EqualFold(gv, "NeoForge") {
			return true
		}
	}
	return false
}
