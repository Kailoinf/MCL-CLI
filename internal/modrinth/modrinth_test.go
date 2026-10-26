package modrinth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
)

func policy() dl.Policy {
	return dl.Policy{Retries: 1, Timeout: 5 * time.Second, Log: logx.Discard()}
}

// fakeAPI stands in for api.modrinth.com and records the request URLs, so the
// facet and filter encoding is part of the test.
func fakeAPI(t *testing.T) *[]string {
	t.Helper()
	var asked []string
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		_, _ = w.Write([]byte(`{
			"hits":[{"project_id":"AANobbMI","slug":"sodium","title":"Sodium","author":"jellysquid3",
				"description":"A rendering engine replacement","project_type":"mod","downloads":123456,
				"categories":["fabric","optimization"],"versions":["1.21.4","1.21.3"]}],
			"offset":0,"limit":10,"total_hits":1}`))
	})
	mux.HandleFunc("/project/sodium/version", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		_, _ = w.Write([]byte(`[
			{"id":"c3YkZvne","project_id":"AANobbMI","name":"Sodium 0.6.13","version_number":"mc1.21.4-0.6.13-fabric",
			 "game_versions":["1.21.4"],"loaders":["fabric"],
			 "files":[
				{"url":"https://cdn.modrinth.com/data/x/other.jar","filename":"other.jar","primary":false,"size":1,"hashes":{"sha1":"aaa","sha512":"bbb"}},
				{"url":"https://cdn.modrinth.com/data/x/sodium.jar","filename":"sodium.jar","primary":true,"size":1234,"hashes":{"sha1":"ccc","sha512":"ddd"}}
			 ],
			 "dependencies":[{"project_id":"P7dR8mSH","version_id":"","dependency_type":"required"}]}
		]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := Base
	Base = srv.URL
	t.Cleanup(func() { Base = old })
	return &asked
}

func TestSearchEncodesFacets(t *testing.T) {
	asked := fakeAPI(t)
	res, err := Search(policy(), "sodium", SearchOptions{Type: "mod", GameVersion: "1.21.4", Loader: "fabric", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Slug != "sodium" {
		t.Fatalf("hits = %+v", res.Hits)
	}
	if res.Hits[0].Downloads != 123456 || res.Hits[0].Author != "jellysquid3" {
		t.Errorf("fields lost: %+v", res.Hits[0])
	}
	if res.TotalHits != 1 {
		t.Errorf("total = %d", res.TotalHits)
	}
	req := (*asked)[0]
	for _, want := range []string{"query=sodium", "limit=5", "facets="} {
		if !strings.Contains(req, want) {
			t.Errorf("%s missing from %s", want, req)
		}
	}
	// the facet argument is a JSON array of AND-ed arrays, URL encoded
	if !strings.Contains(req, "project_type%3Amod") || !strings.Contains(req, "categories%3Afabric") ||
		!strings.Contains(req, "versions%3A1.21.4") {
		t.Errorf("facets were not encoded as Modrinth expects: %s", req)
	}
}

func TestSearchWithoutFiltersHasNoFacets(t *testing.T) {
	asked := fakeAPI(t)
	if _, err := Search(policy(), "sodium", SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*asked)[0], "facets=") {
		t.Errorf("no filters must mean no facets: %s", (*asked)[0])
	}
}

func TestVersionsFiltersAndPickFilePrefersPrimary(t *testing.T) {
	asked := fakeAPI(t)
	versions, err := Versions(policy(), "sodium", "1.21.4", "fabric")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d", len(versions))
	}
	if !strings.Contains((*asked)[0], "game_versions=%5B%221.21.4%22%5D") ||
		!strings.Contains((*asked)[0], "loaders=%5B%22fabric%22%5D") {
		t.Errorf("filters were not encoded as Modrinth expects: %s", (*asked)[0])
	}
	file, ok := PickFile(versions[0])
	if !ok {
		t.Fatal("no file picked")
	}
	if !file.Primary || file.Filename != "sodium.jar" || file.Hashes.SHA1 != "ccc" || file.Size != 1234 {
		t.Errorf("wrong file picked: %+v", file)
	}
	if len(versions[0].Dependencies) != 1 || versions[0].Dependencies[0].ProjectID != "P7dR8mSH" {
		t.Errorf("dependencies lost: %+v", versions[0].Dependencies)
	}
}

func TestPickFileFallsBackToTheFirstUsableFile(t *testing.T) {
	v := Version{Files: []File{{Filename: "empty.jar"}, {URL: "https://x/y.jar", Filename: "y.jar"}}}
	file, ok := PickFile(v)
	if !ok || file.Filename != "y.jar" {
		t.Errorf("picked %+v (ok=%v)", file, ok)
	}
	if _, ok := PickFile(Version{}); ok {
		t.Error("a version without files must not produce a file")
	}
}

func TestDirForKnowsWhereThingsGo(t *testing.T) {
	cases := map[string]string{
		"mod": "mods", "resourcepack": "resourcepacks", "shader": "shaderpacks",
		"datapack": "datapacks", "ResourcePack": "resourcepacks",
	}
	for in, want := range cases {
		got, ok := DirFor(in)
		if !ok || got != want {
			t.Errorf("DirFor(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := DirFor("plugin"); ok {
		t.Error("an unknown project type must be reported as unsupported")
	}
}
