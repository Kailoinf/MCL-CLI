package fabric

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
)

// service stands in for meta.fabricmc.net and records what was asked for, so the
// URL shape is part of the test — a wrong path is the easiest way to break this.
func service(t *testing.T, game, loader string) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/versions/loader/"+game, func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"loader":{"version":"9.9.9","stable":false,"build":2},"intermediary":{"version":"0.0.0"},"launcherMeta":{"version":2,"min_java_version":8}},
			{"loader":{"version":"` + loader + `","stable":true,"build":1},"intermediary":{"version":"0.0.0"},"launcherMeta":{"version":2,"min_java_version":21}}
		]`))
	})
	mux.HandleFunc("/v2/versions/loader/"+game+"/"+loader+"/profile/json", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		_, _ = w.Write([]byte(`{
			"id":"` + ProfileID(game, loader) + `",
			"inheritsFrom":"` + game + `",
			"mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient",
			"arguments":{"game":[],"jvm":["-DFabricMcEmu= net.minecraft.client.main.Main "]},
			"libraries":[{"name":"net.fabricmc:fabric-loader:` + loader + `","url":"https://maven.fabricmc.net/","sha1":"abc","size":123}]
		}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := MetaBase
	MetaBase = srv.URL
	t.Cleanup(func() { MetaBase = old })
	return srv, &asked
}

func policy() dl.Policy {
	// Timeout is a duration: the policy multiplies nothing, it uses it as given.
	return dl.Policy{Retries: 1, Timeout: 5 * time.Second, Log: logx.Discard()}
}

func TestLoadersAndLatestPreferStable(t *testing.T) {
	_, asked := service(t, "1.21.4", "0.16.9")
	entries, err := Loaders(policy(), "1.21.4")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// newest first, as the service returns them, not "stable first"
	if entries[0].Loader.Version != "9.9.9" {
		t.Errorf("order changed: %+v", entries)
	}
	latest, err := Latest(policy(), "1.21.4")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Loader.Version != "0.16.9" {
		t.Errorf("Latest picked %q, want the stable 0.16.9", latest.Loader.Version)
	}
	if latest.LauncherMeta.MinJavaVersion != 21 {
		t.Errorf("launcher metadata lost: %+v", latest.LauncherMeta)
	}
	for _, path := range *asked {
		if !strings.HasPrefix(path, "/v2/versions/loader/1.21.4") {
			t.Errorf("unexpected request %s", path)
		}
	}
}

func TestInstallWritesTheProfileWhereTheLoaderLooks(t *testing.T) {
	_, _ = service(t, "1.21.4", "0.16.9")
	root := t.TempDir()
	versions := filepath.Join(root, "versions")

	id, err := Install(policy(), versions, "1.21.4", "0.16.9", logx.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if id != "fabric-loader-0.16.9-1.21.4" {
		t.Fatalf("id = %q", id)
	}
	path := filepath.Join(versions, id, id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the profile must be written where the version loader reads it: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["inheritsFrom"] != "1.21.4" {
		t.Errorf("inheritsFrom = %v", doc["inheritsFrom"])
	}
	// the argument Fabric publishes has a leading and a trailing space: verbatim
	jvm := doc["arguments"].(map[string]any)["jvm"].([]any)
	if jvm[0] != "-DFabricMcEmu= net.minecraft.client.main.Main " {
		t.Errorf("jvm argument was altered: %q", jvm[0])
	}
}

func TestInstallRefusesAnIncompleteProfile(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/versions/loader/1.21.4/0.1/profile/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"fabric-loader-0.1-1.21.4"}`)) // no mainClass
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := MetaBase
	MetaBase = srv.URL
	defer func() { MetaBase = old }()

	versions := t.TempDir()
	if _, err := Install(policy(), versions, "1.21.4", "0.1", logx.Discard()); err == nil {
		t.Fatal("an incomplete profile must not be installed")
	}
	if entries, err := os.ReadDir(versions); err == nil && len(entries) != 0 {
		t.Errorf("nothing should have been written, found %v", entries)
	}
}

func TestLoadersWithoutABuildIsAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/versions/loader/nope", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := MetaBase
	MetaBase = srv.URL
	defer func() { MetaBase = old }()

	if _, err := Loaders(policy(), "nope"); err == nil {
		t.Fatal("an empty loader list must be an error, not an empty success")
	}
}
