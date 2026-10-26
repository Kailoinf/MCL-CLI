// Package fabric talks to Fabric's version service: the list of loader builds
// published for a game version, and the launcher profile JSON that turns one into
// something a launcher can start.
//
// The profile is deliberately left in the shape Fabric publishes it: it is a
// version JSON with inheritsFrom, mainClass, libraries pointing at
// maven.fabricmc.net, and the jvm argument that emulates the vanilla main class.
// mcv.Version already parses exactly that, so installing a loader is writing one
// file where the loader expects it.
package fabric

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
	"github.com/Kailoinf/MCL-CLI/internal/mirror"
)

// MetaBase is Fabric's version service. It is a plain HTTPS host (not mirrored by
// BMCLAPI), and it is a variable only so tests can point it at their own server.
var MetaBase = "https://meta.fabricmc.net"

// loaderService is the "source" label used in logs and candidate lists.
const source = "fabric"

// LoaderEntry is one loader build for a game version, as the service reports it.
type LoaderEntry struct {
	Loader struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Build   int    `json:"build"`
		Maven   string `json:"maven"`
	} `json:"loader"`
	Intermediary struct {
		Version string `json:"version"`
	} `json:"intermediary"`
	LauncherMeta struct {
		Version        int `json:"version"`
		MinJavaVersion int `json:"min_java_version"`
	} `json:"launcherMeta"`
}

// Loaders lists the loader builds for one game version, newest first — the order
// the service itself returns.
func Loaders(p dl.Policy, game string) ([]LoaderEntry, error) {
	body, err := get(p, MetaBase+"/v2/versions/loader/"+urlPath(game), "loader list "+game)
	if err != nil {
		return nil, err
	}
	var out []LoaderEntry
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("fabric: cannot parse the loader list for %s: %v", game, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s", game)
	}
	return out, nil
}

// Latest picks the newest stable loader for a game version, falling back to the
// newest build when nothing is marked stable.
func Latest(p dl.Policy, game string) (LoaderEntry, error) {
	entries, err := Loaders(p, game)
	if err != nil {
		return LoaderEntry{}, err
	}
	for _, e := range entries {
		if e.Loader.Stable {
			return e, nil
		}
	}
	return entries[0], nil
}

// ProfileID is the version id Fabric's own launcher uses, and therefore the
// directory name the profile lives in.
func ProfileID(game, loader string) string {
	return "fabric-loader-" + loader + "-" + game
}

// ProfileJSON fetches the launcher profile for one game + loader pair.
func ProfileJSON(p dl.Policy, game, loader string) ([]byte, error) {
	url := MetaBase + "/v2/versions/loader/" + urlPath(game) + "/" + urlPath(loader) + "/profile/json"
	return get(p, url, "fabric profile "+game+" "+loader)
}

// profileHead is the little of a profile that has to be right before it is
// written: a profile without these is not something to install.
type profileHead struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	MainClass    string `json:"mainClass"`
}

// Install writes the profile into <versionsDir>/<id>/<id>.json — the one place
// the version loader, the installer and every command after them look — and
// returns the version id it installed.
func Install(p dl.Policy, versionsDir, game, loader string, log *logx.Logger) (string, error) {
	body, err := ProfileJSON(p, game, loader)
	if err != nil {
		return "", err
	}
	head := profileHead{}
	if err := json.Unmarshal(body, &head); err != nil {
		return "", fmt.Errorf("fabric: the profile for %s/%s is not valid JSON: %v", game, loader, err)
	}
	if head.ID == "" || head.InheritsFrom == "" || head.MainClass == "" {
		return "", fmt.Errorf("fabric: the profile for %s/%s is incomplete (id %q, inheritsFrom %q, mainClass %q)",
			game, loader, head.ID, head.InheritsFrom, head.MainClass)
	}
	dir := filepath.Join(versionsDir, head.ID)
	path := filepath.Join(dir, head.ID+".json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	if head.ID != ProfileID(game, loader) {
		// Not fatal — the service is the authority on its own naming — but worth
		// knowing, because the directory name is what `mcli create --version` uses.
		if log != nil {
			log.Infof("fabric profile id %s differs from the expected %s", head.ID, ProfileID(game, loader))
		}
	}
	return head.ID, nil
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

// urlPath escapes a single path segment: game and loader ids are safe today, but
// a version id with a slash must never be able to change the request.
func urlPath(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "/", "%2F"), " ", "%20")
}
