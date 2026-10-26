package toml

import (
	"strings"
	"testing"
)

// A map of plain values nested inside a struct is a table of keys. It used to be
// dropped entirely by the writer, which silently lost data from a state file — the
// kind of failure that is worse than an error, because nothing complains.
type nestedInner struct {
	Files map[string]string `toml:"pack_files,omitempty"`
	Name  string            `toml:"name"`
}

type nestedDoc struct {
	Instances map[string]*nestedInner `toml:"instances"`
}

func TestNestedMapOfScalarsRoundTrips(t *testing.T) {
	doc := nestedDoc{Instances: map[string]*nestedInner{
		"a": {Name: "a", Files: map[string]string{"mods/x.jar": "aa11", "mods/y.jar": "bb22"}},
		"b": {Name: "b"},
	}}
	out, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "[instances.a.pack_files]") {
		t.Fatalf("the nested map did not become its own table:\n%s", text)
	}
	if !strings.Contains(text, `"mods/x.jar" = "aa11"`) {
		t.Fatalf("the nested map's keys were not written:\n%s", text)
	}
	// the empty map must not open a table at all
	if strings.Contains(text, "[instances.b.pack_files]") {
		t.Errorf("an empty map should be left out:\n%s", text)
	}

	var back nestedDoc
	if err := Unmarshal(out, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := back.Instances["a"].Files["mods/y.jar"]; got != "bb22" {
		t.Errorf("nested map lost on the way back: %q", got)
	}
	if len(back.Instances["b"].Files) != 0 {
		t.Errorf("an instance without files grew some: %v", back.Instances["b"].Files)
	}
	// a map key that needs quoting must survive the trip
	doc.Instances["a"].Files = map[string]string{"mods/a file with spaces.jar": "cc33"}
	out, err = Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.Instances["a"].Files["mods/a file with spaces.jar"]; got != "cc33" {
		t.Errorf("a quoted key did not survive: %q (%s)", got, out)
	}
}
