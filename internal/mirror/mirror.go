// Package mirror turns an official Mojang download URL into an ordered list of
// candidates: the BMCLAPI mirror first (when enabled), the official URL last.
//
// Verified mappings (2026-10, probed with curl against bmclapi2):
//
//	piston-meta.mojang.com/<path>          -> <bmcl>/<path>
//	/v1/objects/<sha1>/client.jar          -> <bmcl>/version/<id>/client
//	libraries.minecraft.net/<path>         -> <bmcl>/maven/<path>
//	resources.download.minecraft.net/<a>/<h> -> <bmcl>/assets/<a>/<h>
//
// Anything unrecognised is passed through untouched (mirror candidate omitted)
// so a new Mojang host can never break a launch.
package mirror

import (
	"net/url"
	"strings"
)

// Official endpoints.
const (
	ManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	// Adoptium is the fallback source for java runtimes when the mirror has
	// nothing for the requested major version.
	AdoptiumURL = "https://api.adoptium.net/v3/binary/latest"
)

// Candidate is one place a file can be fetched from.
type Candidate struct {
	URL    string
	Source string // "bmclapi", "mojang", "modrinth", "curseforge", …
	// Headers carry what a particular source needs — an API key, for instance.
	// They are never logged: a credential must not reach a terminal or a log file.
	Headers map[string]string
}

// Mapper builds candidate lists.
type Mapper struct {
	// Endpoint is the BMCLAPI root, e.g. https://bmclapi2.bangbang93.com.
	// Empty disables mirroring entirely.
	Endpoint string
	// Fallback keeps the official URL in the list. When false only mirror
	// candidates are returned.
	Fallback bool
}

// MapURL rewrites one URL whose path already matches the mirror layout.
func (m Mapper) MapURL(official string) (string, bool) {
	if m.Endpoint == "" || official == "" {
		return "", false
	}
	u, err := url.Parse(official)
	if err != nil || u.Host == "" {
		return "", false
	}
	base := strings.TrimRight(m.Endpoint, "/")
	path := u.EscapedPath()
	switch u.Host {
	case "piston-meta.mojang.com", "launchermeta.mojang.com", "piston-data.mojang.com":
		return base + path, true
	case "libraries.minecraft.net":
		return base + "/maven" + path, true
	case "resources.download.minecraft.net":
		return base + "/assets" + path, true
	}
	if strings.HasSuffix(u.Host, ".minecraftforge.net") || u.Host == "maven.minecraftforge.net" {
		return base + "/maven" + path, true
	}
	return "", false
}

// Candidates returns the ordered fetch list for an arbitrary official URL.
func (m Mapper) Candidates(official string) []Candidate {
	var out []Candidate
	if mu, ok := m.MapURL(official); ok {
		out = append(out, Candidate{URL: mu, Source: "bmclapi"})
	}
	if m.Fallback || len(out) == 0 {
		if official != "" {
			out = append(out, Candidate{URL: official, Source: "mojang"})
		}
	}
	return out
}

// Manifest lists the version manifest sources.
func (m Mapper) Manifest() []Candidate {
	return m.Candidates(ManifestURL)
}

// VersionJSON lists sources for a single version's JSON document.
func (m Mapper) VersionJSON(versionID, official string) []Candidate {
	var out []Candidate
	if m.Endpoint != "" && versionID != "" {
		out = append(out, Candidate{URL: strings.TrimRight(m.Endpoint, "/") + "/version/" + url.PathEscape(versionID) + "/json", Source: "bmclapi"})
	}
	if m.Fallback || len(out) == 0 {
		if official != "" {
			out = append(out, Candidate{URL: official, Source: "mojang"})
		}
	}
	return out
}

// ClientJar lists sources for the client jar of a version.
func (m Mapper) ClientJar(versionID, official string) []Candidate {
	var out []Candidate
	if m.Endpoint != "" && versionID != "" {
		out = append(out, Candidate{URL: strings.TrimRight(m.Endpoint, "/") + "/version/" + url.PathEscape(versionID) + "/client", Source: "bmclapi"})
	}
	if m.Fallback || len(out) == 0 {
		if official != "" {
			out = append(out, Candidate{URL: official, Source: "mojang"})
		}
	}
	return out
}

// Library maps a maven-style library URL.
func (m Mapper) Library(official string) []Candidate { return m.Candidates(official) }

// Asset maps an asset object URL.
func (m Mapper) Asset(official string) []Candidate { return m.Candidates(official) }

// Java lists sources for a java runtime. Exact mirror layout is probed at
// runtime by the java package, so this only offers the Adoptium fallback plus
// the caller-supplied mirror URL.
func (m Mapper) Java(mirrorURL, official string) []Candidate {
	var out []Candidate
	if m.Endpoint != "" && mirrorURL != "" {
		out = append(out, Candidate{URL: mirrorURL, Source: "bmclapi"})
	}
	if official != "" {
		out = append(out, Candidate{URL: official, Source: "adoptium"})
	}
	return out
}
