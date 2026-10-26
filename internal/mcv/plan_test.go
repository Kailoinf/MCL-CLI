package mcv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Kailoinf/MCL-CLI/internal/dl"
	"github.com/Kailoinf/MCL-CLI/internal/logx"
)

// planVersion is a small synthetic version: one plain library, one natives
// library, an asset index and a logging config.
func planVersion() *Version {
	v := &Version{
		ID:        "1.21.4",
		MainClass: "net.minecraft.client.main.Main",
		Assets:    "19",
		AssetIndex: &Download{
			Path: "19.json", URL: "https://example.invalid/19.json",
			SHA1: "aa", Size: 100,
		},
		Libraries: []Library{
			plainLibrary(),
			nativesLibrary(),
		},
	}
	v.Downloads = map[string]Download{
		"client": {Path: "client.jar", URL: "https://example.invalid/client.jar", SHA1: "bb", Size: 200},
	}
	v.Logging = &struct {
		Client *LoggingClient `json:"client,omitempty"`
	}{Client: &LoggingClient{Argument: "-Dlog4j.configurationFile=${path}", File: LoggingFile{
		ID: "client-1.21.xml", URL: "https://example.invalid/client-1.21.xml", SHA1: "cc", Size: 50,
	}}}
	return v
}

// plainLibrary is a normal maven library with its download block.
func plainLibrary() Library {
	l := Library{Name: "org.example:plain:1.0"}
	l.Downloads.Artifact = &Download{
		Path: "org/example/plain/1.0/plain-1.0.jar", SHA1: "dd", Size: 123,
		URL: "https://example.invalid/maven/org/example/plain/1.0/plain-1.0.jar",
	}
	return l
}

// nativesLibrary uses the modern form, where the classifier is part of the name.
func nativesLibrary() Library {
	l := Library{Name: "org.lwjgl:lwjgl:3.3.3:natives-linux"}
	l.Downloads.Artifact = &Download{
		Path: "org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar", SHA1: "ee", Size: 456,
		URL: "https://example.invalid/maven/org/lwjgl/lwjgl/3.3.3/lwjgl-3.3.3-natives-linux.jar",
	}
	return l
}

// writeAssetIndex puts a tiny asset index on disk so the plan can enumerate it.
func writeAssetIndex(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "assets", "indexes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"objects":{"icons/icon_16x16.png":{"hash":"5ff04807c356f1beed0b86ccf659b44b9983e3fa","size":781}}}`
	if err := os.WriteFile(filepath.Join(dir, "19.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testInstaller(root string) Installer {
	return Installer{
		Root:        root,
		VersionsDir: "versions",
		Env:         EnvCurrent(FeatureSet(false, nil)),
		Log:         logx.Discard(),
		Workers:     2,
	}
}

func TestPlanListsVersionFilesAndAssets(t *testing.T) {
	root := t.TempDir()
	writeAssetIndex(t, root)
	plan, err := testInstaller(root).Plan(planVersion(), PlanOptions{IncludeAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	// client jar + plain library + natives library + asset index + logging config
	if len(plan.Files) != 5 {
		t.Errorf("files = %d, want 5: %+v", len(plan.Files), plan.Files)
	}
	if plan.NativesCount != 1 {
		t.Errorf("natives count = %d, want 1", plan.NativesCount)
	}
	if !plan.IndexReadable || len(plan.Assets) != 1 {
		t.Fatalf("assets = %d (readable %v), want 1", len(plan.Assets), plan.IndexReadable)
	}
	if plan.AssetBytes != 781 {
		t.Errorf("asset bytes = %d, want 781", plan.AssetBytes)
	}
	obj := plan.Assets[0]
	if filepath.Base(obj.Dest) != "5ff04807c356f1beed0b86ccf659b44b9983e3fa" {
		t.Errorf("asset destination = %s", obj.Dest)
	}
	if filepath.Base(filepath.Dir(obj.Dest)) != "5f" {
		t.Errorf("asset destination must be objects/<first2>/<hash>: %s", obj.Dest)
	}
	// nothing exists yet, so everything is missing - and Check must not write
	if got := len(plan.Check(false)); got != 6 {
		t.Errorf("missing = %d, want 6", got)
	}
	if _, err := os.Stat(filepath.Join(root, "assets", "objects")); err == nil {
		t.Error("planning must not create directories")
	}
	if len(plan.Items()) != 6 {
		t.Errorf("Items() = %d, want 6", len(plan.Items()))
	}
}

func TestPlanCheckDistinguishesMissingDamagedAndHealthy(t *testing.T) {
	root := t.TempDir()
	writeAssetIndex(t, root)
	inst := testInstaller(root)
	plan, err := inst.Plan(planVersion(), PlanOptions{IncludeAssets: true})
	if err != nil {
		t.Fatal(err)
	}

	// put every planned file in place with the right size and content
	for _, it := range plan.Items() {
		if err := os.MkdirAll(filepath.Dir(it.Dest), 0o755); err != nil {
			t.Fatal(err)
		}
		size := it.Size
		if size == 0 {
			size = 16
		}
		if err := os.WriteFile(it.Dest, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the asset object has a SHA-1 in the index, so deep checking needs real bytes:
	// a file of the right size but wrong content must be reported as wrong hash.
	//
	// Shallow: the version's own files are hash-verified, so zero-filled files are
	// damaged rather than healthy; the asset object is only size-checked and the
	// size matches, so it passes.
	missing := plan.Check(false)
	kinds := map[MissingKind]int{}
	for _, m := range missing {
		kinds[m.Kind]++
	}
	if kinds[MissingWrongHash] != 5 || kinds[MissingWrongSize] != 0 || kinds[MissingAbsent] != 0 {
		t.Errorf("shallow check = %+v, want 5 wrong-hash entries", kinds)
	}
	if deep := plan.Check(true); len(deep) != 6 {
		t.Errorf("deep check = %d, want 6 (the asset object is wrong too)", len(deep))
	}

	// shrink one library: the shallow check must catch it as wrong size
	var lib dl.Item
	for _, it := range plan.Files {
		if filepath.Base(it.Dest) == "plain-1.0.jar" {
			lib = it
		}
	}
	if lib.Dest == "" {
		t.Fatalf("library not planned: %+v", plan.Files)
	}
	if lib.Size == 0 {
		// the synthetic library carries no size, so give the check something to
		// compare against and verify the size branch directly
		lib.Size = 1000
	}
	if err := os.WriteFile(lib.Dest, make([]byte, lib.Size+1), 0o644); err != nil {
		t.Fatal(err)
	}
	plan.Files = []dl.Item{lib}
	plan.Assets = nil
	if got := plan.Check(false); len(got) != 1 || got[0].Kind != MissingWrongSize {
		t.Errorf("wrong-size file: %+v", got)
	}
	if err := os.Remove(lib.Dest); err != nil {
		t.Fatal(err)
	}
	if got := plan.Check(false); len(got) != 1 || got[0].Kind != MissingAbsent {
		t.Errorf("absent file: %+v", got)
	}
}

func TestPlanWithoutIndexFetchStaysOffline(t *testing.T) {
	root := t.TempDir() // no index on disk, FetchIndex false: no network may be used
	plan, err := testInstaller(root).Plan(planVersion(), PlanOptions{IncludeAssets: true, FetchIndex: false})
	if err != nil {
		t.Fatal(err)
	}
	if plan.IndexReadable || len(plan.Assets) != 0 {
		t.Errorf("no index on disk and no fetch allowed: %+v", plan)
	}
	// the index file itself is still part of the plan
	found := false
	for _, it := range plan.Files {
		if filepath.Base(it.Dest) == "19.json" {
			found = true
		}
	}
	if !found {
		t.Errorf("the asset index must be planned even when it is not readable yet: %+v", plan.Files)
	}
}
