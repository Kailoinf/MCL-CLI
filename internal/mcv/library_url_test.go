package mcv

import (
	"path/filepath"
	"testing"
)

// fabricStyleLibrary is how Fabric's launcher profile lists a library: a maven
// name, the repository, and the checksum beside them — no downloads block.
func fabricStyleLibrary() Library {
	return Library{
		Name: "net.fabricmc:fabric-loader:0.20.1",
		URL:  "https://maven.fabricmc.net/",
		SHA1: "b1a2c3", Size: 987,
	}
}

func TestLibraryWithoutDownloadsResolvesFromItsRepository(t *testing.T) {
	v := &Version{ID: "fabric-loader-0.20.1-26.3", Libraries: []Library{
		fabricStyleLibrary(),
		{Name: "com.example:nothing:1.0"}, // no downloads, no repository
	}}
	env := EnvCurrent(FeatureSet(false, nil))
	libs := filepath.Join(t.TempDir(), "libraries")

	entries := ClasspathEntries(v, env, libs, "/mc/versions/26.3/26.3.jar")
	// the loader has to be on the classpath, or the game cannot find its main class
	wantJar := filepath.Join(libs, "net", "fabricmc", "fabric-loader", "0.20.1", "fabric-loader-0.20.1.jar")
	found := false
	for _, e := range entries {
		if e == wantJar {
			found = true
		}
		if e == filepath.Join(libs, "com", "example", "nothing", "1.0", "nothing-1.0.jar") {
			t.Errorf("a library with neither downloads nor a repository must stay out: %s", e)
		}
	}
	if !found {
		t.Fatalf("the loader is missing from the classpath: %v", entries)
	}

	// and the installer has to plan it, with the URL Fabric declared
	d, ok := fabricStyleLibrary().ArtifactURL(env)
	if !ok {
		t.Fatal("ArtifactURL must resolve a maven-style library")
	}
	if d.URL != "https://maven.fabricmc.net/net/fabricmc/fabric-loader/0.20.1/fabric-loader-0.20.1.jar" {
		t.Errorf("url = %s", d.URL)
	}
	if d.SHA1 != "b1a2c3" || d.Size != 987 {
		t.Errorf("checksum/size lost: %+v", d)
	}
}

func TestNativesEntriesStillHaveNoClasspathArtifact(t *testing.T) {
	// a modern natives entry is a library with a classifier and no downloads: it
	// belongs in natives extraction, never on the classpath
	nat := Library{Name: "org.lwjgl:lwjgl:3.3.3:natives-linux", URL: "https://libraries.minecraft.net/"}
	env := EnvCurrent(FeatureSet(false, nil))
	if _, ok := nat.ArtifactDownload(env); ok {
		t.Error("a natives classifier must not produce a classpath artifact")
	}
}
