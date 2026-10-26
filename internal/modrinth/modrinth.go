// Package modrinth talks to Modrinth's public API: searching for mods, resource
// packs, shaders and data packs, and finding the file to install for a given game
// version and loader.
//
// Only the two endpoints mcli actually needs are wrapped, and only the fields it
// shows: a launcher has no business mirroring an entire content platform. Modrinth
// needs no API key, which is why it is the one wired here.
package modrinth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// Base is the API root; a variable so tests can point it at their own server.
var Base = "https://api.modrinth.com/v2"

// source labels these requests in logs and candidate lists.
const source = "modrinth"

// Types are the project types mcli knows how to place in an instance.
var Types = map[string]string{
	"mod":          "mods",
	"resourcepack": "resourcepacks",
	"shader":       "shaderpacks",
	"datapack":     "datapacks",
	"modpack":      "modpacks",
}

// DirFor returns the directory inside an instance a project type belongs in.
// The second result is false for a project type mcli cannot install.
func DirFor(projectType string) (string, bool) {
	d, ok := Types[strings.ToLower(strings.TrimSpace(projectType))]
	return d, ok
}

// Hit is one search result, reduced to what a table row needs.
type Hit struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	ProjectType string   `json:"project_type"`
	Downloads   int64    `json:"downloads"`
	Categories  []string `json:"categories"`
	Versions    []string `json:"versions"`
	DateModifed string   `json:"date_modified"`
}

// SearchOptions narrows a search. Empty fields are simply left out of the query.
type SearchOptions struct {
	// Type is a Modrinth project type: mod, resourcepack, shader, datapack,
	// modpack.
	Type string
	// GameVersion matches against the versions a project supports.
	GameVersion string
	// Loader is matched as a category: fabric, quilt, forge, neoforge.
	Loader string
	Limit  int
	Offset int
}

// Result is one page of search results.
type Result struct {
	Hits      []Hit `json:"hits"`
	TotalHits int   `json:"total_hits"`
	Offset    int   `json:"offset"`
	Limit     int   `json:"limit"`
}

// Search runs one query. A search is one request: paging further is the caller's
// business (--offset), not something to special-case here.
func Search(p dl.Policy, query string, o SearchOptions) (Result, error) {
	q := url.Values{}
	q.Set("query", query)
	if o.Limit <= 0 {
		o.Limit = 10
	}
	q.Set("limit", strconv.Itoa(o.Limit))
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	if facets := facetsFor(o); facets != "" {
		q.Set("facets", facets)
	}
	body, err := get(p, Base+"/search?"+q.Encode(), "search "+query)
	if err != nil {
		return Result{}, err
	}
	var out Result
	if err := json.Unmarshal(body, &out); err != nil {
		return Result{}, fmt.Errorf("modrinth: cannot read the search result: %v", err)
	}
	return out, nil
}

// facetsFor builds Modrinth's facet argument: a JSON array of AND-ed arrays.
func facetsFor(o SearchOptions) string {
	var groups [][]string
	if o.Type != "" {
		groups = append(groups, []string{"project_type:" + strings.ToLower(o.Type)})
	}
	if o.GameVersion != "" {
		groups = append(groups, []string{"versions:" + o.GameVersion})
	}
	if o.Loader != "" {
		groups = append(groups, []string{"categories:" + strings.ToLower(o.Loader)})
	}
	if len(groups) == 0 {
		return ""
	}
	blob, err := json.Marshal(groups)
	if err != nil {
		return ""
	}
	return string(blob)
}

// File is one downloadable file of a version.
type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	Hashes   struct {
		SHA1   string `json:"sha1"`
		SHA512 string `json:"sha512"`
	} `json:"hashes"`
}

// Dependency names another project this version needs.
type Dependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"`
}

// Version is one published version of a project.
type Version struct {
	ID            string       `json:"id"`
	ProjectID     string       `json:"project_id"`
	Name          string       `json:"name"`
	VersionNumber string       `json:"version_number"`
	GameVersions  []string     `json:"game_versions"`
	Loaders       []string     `json:"loaders"`
	Files         []File       `json:"files"`
	Dependencies  []Dependency `json:"dependencies"`
}

// Versions lists the versions of a project, optionally filtered to the game
// version and loader an instance runs. The first result is the newest match,
// which is what the API returns first.
func Versions(p dl.Policy, project, gameVersion, loader string) ([]Version, error) {
	q := url.Values{}
	if gameVersion != "" {
		blob, _ := json.Marshal([]string{gameVersion})
		q.Set("game_versions", string(blob))
	}
	if loader != "" {
		blob, _ := json.Marshal([]string{strings.ToLower(loader)})
		q.Set("loaders", string(blob))
	}
	path := Base + "/project/" + url.PathEscape(project) + "/version"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	body, err := get(p, path, "versions of "+project)
	if err != nil {
		return nil, err
	}
	var out []Version
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("modrinth: cannot read the version list of %s: %v", project, err)
	}
	return out, nil
}

// PickFile returns the file to install from a version: the one marked primary, or
// the first one when the API marks none.
func PickFile(v Version) (File, bool) {
	for _, f := range v.Files {
		if f.Primary && f.URL != "" {
			return f, true
		}
	}
	for _, f := range v.Files {
		if f.URL != "" {
			return f, true
		}
	}
	return File{}, false
}

// get fetches one URL through the download policy, so retries, timeouts and the
// stall watchdog behave exactly like every other request mcli makes.
func get(p dl.Policy, url, desc string) ([]byte, error) {
	body, _, err := p.FetchBytes([]mirror.Candidate{{URL: url, Source: source}}, desc)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Project is the little of a project's own page that mcli needs: what type it is,
// and a title to show in messages.
type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	ProjectType string `json:"project_type"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
}

// ProjectInfo looks one project up by slug or id. Search hits do not carry every
// field, and installing needs to know the project type — that is what this is for.
func ProjectInfo(p dl.Policy, idOrSlug string) (Project, error) {
	body, err := get(p, Base+"/project/"+url.PathEscape(idOrSlug), "project "+idOrSlug)
	if err != nil {
		return Project{}, err
	}
	var out Project
	if err := json.Unmarshal(body, &out); err != nil {
		return Project{}, fmt.Errorf("modrinth: cannot read project %s: %v", idOrSlug, err)
	}
	if out.ProjectType == "" {
		return Project{}, fmt.Errorf("modrinth: %s has no project type", idOrSlug)
	}
	return out, nil
}
