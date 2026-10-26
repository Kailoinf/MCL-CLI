package curseforge

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
)

// stub answers the two endpoints with canned JSON and records what it was asked.
type stub struct {
	srv      *httptest.Server
	lastPath string
	lastKey  string
	lastQ    url.Values
	hits     int
}

func newStub(t *testing.T, body string, status int) *stub {
	t.Helper()
	s := &stub{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits++
		s.lastPath = r.URL.Path
		s.lastKey = r.Header.Get("x-api-key")
		s.lastQ = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) client(key string) Client {
	return Client{Base: s.srv.URL, Key: key, Policy: dl.Policy{Retries: 1}}
}

const searchBody = `{
  "data": [
    {"id": 238222, "name": "Just Enough Items", "slug": "jei", "summary": "View items and recipes",
     "downloadCount": 1234567, "classId": 6,
     "authors": [{"name": "mezz"}, {"name": "someone"}],
     "links": {"websiteUrl": "https://www.curseforge.com/minecraft/mc-mods/jei"}},
    {"id": 999, "name": "Something Else", "slug": "something-else", "classId": 6, "authors": []}
  ],
  "pagination": {"index": 0, "pageSize": 2, "resultCount": 2, "totalCount": 42}
}`

func TestSearchSendsTheKeyAndFilters(t *testing.T) {
	s := newStub(t, searchBody, http.StatusOK)
	page, err := s.client("secret-key").Search(SearchQuery{
		Text: "jei", ClassID: ClassMods, GameVersion: "1.21.4", LoaderID: LoaderFabric, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.lastPath != "/v1/mods/search" {
		t.Errorf("path = %q", s.lastPath)
	}
	if s.lastKey != "secret-key" {
		t.Errorf("the key was not sent in x-api-key: %q", s.lastKey)
	}
	for name, want := range map[string]string{
		"gameId": "432", "classId": "6", "gameVersion": "1.21.4",
		"modLoaderType": "4", "pageSize": "2", "index": "0",
	} {
		if got := s.lastQ.Get(name); got != want {
			t.Errorf("query %s = %q, want %q", name, got, want)
		}
	}
	if len(page.Data) != 2 || page.Data[0].Slug != "jei" || page.Data[0].DownloadCount != 1234567 {
		t.Errorf("page = %+v", page.Data)
	}
	if page.Pagination.TotalCount != 42 {
		t.Errorf("pagination = %+v", page.Pagination)
	}
	if got := page.Data[0].AuthorLine(); got != "mezz, someone" {
		t.Errorf("AuthorLine = %q", got)
	}
}

func TestNoKeyMeansNoRequest(t *testing.T) {
	s := newStub(t, searchBody, http.StatusOK)
	_, err := s.client("").Search(SearchQuery{Text: "jei"})
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
	if s.hits != 0 {
		t.Fatalf("a request went out without a key (%d hits)", s.hits)
	}
}

func TestRejectedKeyIsRecognisable(t *testing.T) {
	s := newStub(t, `{"error":"invalid key"}`, http.StatusForbidden)
	_, err := s.client("wrong").Search(SearchQuery{Text: "jei"})
	if !errors.Is(err, ErrKeyRejected) {
		t.Fatalf("err = %v, want ErrKeyRejected", err)
	}
	// the message must not leak the key itself
	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("the error quoted the key: %v", err)
	}
}

const filesBody = `{
  "data": [
    {"id": 3001, "modId": 238222, "displayName": "jei-1.21.4.jar", "fileName": "jei-1.21.4.jar",
     "releaseType": 1, "fileLength": 1234,
     "downloadUrl": "https://media.forgecdn.net/files/3001/jei.jar",
     "gameVersions": ["1.21.4", "Fabric"],
     "hashes": [{"value": "AA11", "algo": 2}, {"value": "cc33", "algo": 1}],
     "dependencies": [{"modId": 306612, "relationType": 3}, {"modId": 5, "relationType": 2}]}
  ]
}`

func TestFilesParsesHashesAndDependencies(t *testing.T) {
	s := newStub(t, filesBody, http.StatusOK)
	files, err := s.client("k").Files(238222)
	if err != nil {
		t.Fatal(err)
	}
	if s.lastPath != "/v1/mods/238222/files" {
		t.Errorf("path = %q", s.lastPath)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	// algo 1 is SHA-1; the md5-looking value comes first on purpose
	if got := files[0].SHA1(); got != "cc33" {
		t.Errorf("SHA1() = %q, want the algo-1 digest", got)
	}
	if got := files[0].RequiredDependencies(); len(got) != 1 || got[0] != 306612 {
		t.Errorf("RequiredDependencies() = %v", got)
	}
	if files[0].DownloadURL != "https://media.forgecdn.net/files/3001/jei.jar" {
		t.Errorf("download url = %q", files[0].DownloadURL)
	}
}

func TestFindMatchesTheSlug(t *testing.T) {
	s := newStub(t, searchBody, http.StatusOK)
	c := s.client("k")
	if mod, ok, err := c.Find("jei", ClassMods); err != nil || !ok || mod.ID != 238222 {
		t.Errorf("Find(jei) = %+v ok=%v err=%v", mod, ok, err)
	}
	if _, ok, err := c.Find("nothing-like-it", ClassMods); err != nil || ok {
		t.Errorf("Find(unknown) should not match: ok=%v err=%v", ok, err)
	}
}

func TestClassAndLoaderIDs(t *testing.T) {
	for name, want := range map[string]int{
		"mod": ClassMods, "resourcepack": ClassResourcePacks, "modpack": ClassModpacks,
		"shader": ClassShaderPacks, "datapack": ClassDataPacks, "world": ClassWorlds,
	} {
		if got, ok := ClassID(name); !ok || got != want {
			t.Errorf("ClassID(%q) = %d %v, want %d", name, got, ok, want)
		}
	}
	if _, ok := ClassID("nonsense"); ok {
		t.Error("an unknown type must not map to a class")
	}
	for name, want := range map[string]int{
		"forge": LoaderForge, "fabric": LoaderFabric, "quilt": LoaderQuilt, "neoforge": LoaderNeoForge,
	} {
		if got, ok := LoaderID(name); !ok || got != want {
			t.Errorf("LoaderID(%q) = %d %v, want %d", name, got, ok, want)
		}
	}
	if _, ok := LoaderID("vanilla"); ok {
		t.Error("vanilla is not a loader id")
	}
}
