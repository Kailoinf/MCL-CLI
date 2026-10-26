package curseforge

import "testing"

func file(name, date string, rel int, versions ...string) File {
	return File{
		ID: 1, FileName: name, DisplayName: name, FileDate: date, ReleaseType: rel,
		FileLength: 10, DownloadURL: "https://media.forgecdn.net/" + name, GameVersions: versions,
	}
}

func TestPickFilePrefersAReleaseForTheGameVersion(t *testing.T) {
	files := []File{
		file("alpha.jar", "2026-01-03T00:00:00Z", 3, "1.21.4", "Fabric"),
		file("beta.jar", "2026-01-02T00:00:00Z", 2, "1.21.4", "Fabric"),
		file("release.jar", "2026-01-01T00:00:00Z", 1, "1.21.4", "Fabric"),
		file("other-game.jar", "2026-01-04T00:00:00Z", 1, "1.20.1", "Fabric"),
	}
	got, ok := PickFile(files, "1.21.4", "fabric")
	if !ok || got.FileName != "release.jar" {
		t.Fatalf("PickFile = %+v ok=%v, want the release for 1.21.4", got, ok)
	}
}

func TestPickFileFallsBackInOrder(t *testing.T) {
	betas := []File{file("beta.jar", "2026-01-02T00:00:00Z", 2, "1.21.4")}
	if got, ok := PickFile(betas, "1.21.4", ""); !ok || got.FileName != "beta.jar" {
		t.Errorf("a beta must be used when no release exists: %+v", got)
	}
	alphas := []File{file("alpha.jar", "2026-01-02T00:00:00Z", 3, "1.21.4")}
	if got, ok := PickFile(alphas, "1.21.4", ""); !ok || got.FileName != "alpha.jar" {
		t.Errorf("an alpha must be used when nothing else exists: %+v", got)
	}
}

func TestPickFileRespectsTheLoaderAndTheGameVersion(t *testing.T) {
	files := []File{
		file("forge.jar", "2026-01-05T00:00:00Z", 1, "1.21.4", "Forge"),
		file("fabric.jar", "2026-01-01T00:00:00Z", 1, "1.21.4", "Fabric"),
	}
	if got, ok := PickFile(files, "1.21.4", "fabric"); !ok || got.FileName != "fabric.jar" {
		t.Errorf("loader filter ignored: %+v", got)
	}
	// an rc suffix still counts as the version it belongs to
	rc := []File{file("rc.jar", "2026-01-01T00:00:00Z", 1, "1.21.4-rc1")}
	if got, ok := PickFile(rc, "1.21.4", ""); !ok || got.FileName != "rc.jar" {
		t.Errorf("an -rc entry must match its version: %+v", got)
	}
	if _, ok := PickFile(files, "1.20.1", ""); ok {
		t.Error("nothing matches 1.20.1 and nothing may be chosen")
	}
}

func TestPickFileSkipsFilesWithoutADownloadURL(t *testing.T) {
	files := []File{{FileName: "no-url.jar", ReleaseType: 1, FileDate: "2026-01-09T00:00:00Z", GameVersions: []string{"1.21.4"}}}
	if _, ok := PickFile(files, "1.21.4", ""); ok {
		t.Error("a file with no downloadUrl cannot be installed")
	}
}
