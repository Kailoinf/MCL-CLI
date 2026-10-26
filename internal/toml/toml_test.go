package toml

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

type sampleConfig struct {
	Retries  int    `toml:"retries"`
	Endpoint string `toml:"endpoint"`
	Enabled  bool   `toml:"enabled"`
	Ignored  string `toml:"-"`
}

type sampleAccount struct {
	Name    string    `toml:"name"`
	UUID    string    `toml:"uuid"`
	Active  bool      `toml:"active"`
	AddedAt time.Time `toml:"added_at"`
}

type sampleInstance struct {
	Name    string   `toml:"name"`
	Version string   `toml:"version"`
	JvmArgs []string `toml:"jvm_args"`
}

type sampleDoc struct {
	Version  int                        `toml:"state_version"`
	Salt     string                     `toml:"salt"`
	Config   sampleConfig               `toml:"config"`
	Accounts map[string]*sampleAccount  `toml:"accounts"`
	Inst     map[string]*sampleInstance `toml:"instances"`
	Last     *sampleInstance            `toml:"last_launch"`
}

func testDoc() sampleDoc {
	return sampleDoc{
		Version: 1,
		Salt:    "YWJjZGVmZ2hpamtsbW5vcA==",
		Config:  sampleConfig{Retries: 3, Endpoint: "https://bmclapi2.bangbang93.com", Enabled: true},
		Accounts: map[string]*sampleAccount{
			"abc": {Name: "Kailo", UUID: "abc", Active: true, AddedAt: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)},
		},
		Inst: map[string]*sampleInstance{
			"vanilla":   {Name: "vanilla", Version: "1.21.4", JvmArgs: []string{"-XX:+UseG1GC", "-Dfoo=1"}},
			"my.dotted": {Name: "my.dotted", Version: "1.7.10"},
		},
		Last: &sampleInstance{Name: "vanilla", Version: "1.21.4"},
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	orig := testDoc()
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got := sampleDoc{}
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("cannot parse what we wrote:\n%s\nerror: %v", data, err)
	}
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round trip mismatch\n--- original ---\n%+v\n--- parsed ---\n%+v\n--- file ---\n%s", orig, got, data)
	}
}

func TestMarshalIsReadableAndOrdered(t *testing.T) {
	data, err := Marshal(testDoc())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	// root scalars must precede tables
	if strings.Index(text, "state_version") > strings.Index(text, "[config]") {
		t.Errorf("root keys must come before tables:\n%s", text)
	}
	for _, want := range []string{
		`state_version = 1`,
		"[config]",
		`endpoint = "https://bmclapi2.bangbang93.com"`,
		"[accounts.abc]",
		"[instances.\"my.dotted\"]", // keys needing quotes are quoted
		`jvm_args = ["-XX:+UseG1GC", "-Dfoo=1"]`,
		"[last_launch]",
		`added_at = "2026-10-26T12:00:00Z"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ignored") {
		t.Error(`fields tagged "-" must not be written`)
	}
}

func TestUnmarshalHandWrittenFile(t *testing.T) {
	// exactly the kind of file a user edits by hand
	file := `
# mcli configuration
state_version = 1
salt = "c2FsdA=="        # base64 salt, do not share

[config]
retries = 5              # attempts per file
endpoint = 'https://bmclapi2.bangbang93.com'   # literal string
enabled = true

[accounts.abc]
name = "Kailo \"The\" Player"
uuid = "abc"
active = false
added_at = "2026-01-02T03:04:05Z"

[instances.vanilla]
name = "vanilla"
version = "1.21.4"
jvm_args = [
  "-XX:+UseG1GC",
  "-Dlog4j2.formatMsgNoLookups=true",
]
`
	doc := sampleDoc{}
	unknown, err := UnmarshalWithReport([]byte(file), &doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Errorf("no unknown keys expected, got %v", unknown)
	}
	if doc.Version != 1 || doc.Salt != "c2FsdA==" {
		t.Errorf("root scalars = %+v", doc)
	}
	if doc.Config.Retries != 5 || !doc.Config.Enabled {
		t.Errorf("config = %+v", doc.Config)
	}
	if doc.Config.Endpoint != "https://bmclapi2.bangbang93.com" {
		t.Errorf("literal string not handled: %q", doc.Config.Endpoint)
	}
	acc := doc.Accounts["abc"]
	if acc == nil || acc.Name != `Kailo "The" Player` || acc.Active {
		t.Errorf("account = %+v", acc)
	}
	if acc.AddedAt.Year() != 2026 || acc.AddedAt.Hour() != 3 {
		t.Errorf("time not parsed: %v", acc.AddedAt)
	}
	// arrays spanning several lines are joined and parsed
	inst := doc.Inst["vanilla"]
	if inst == nil || len(inst.JvmArgs) != 2 || inst.JvmArgs[1] != "-Dlog4j2.formatMsgNoLookups=true" {
		t.Errorf("array across lines = %+v", inst)
	}
}

func TestNestedTableAndMultiLineString(t *testing.T) {
	file := `
[config]
retries = 1
endpoint = """https://example.com
/second"""
`
	doc := sampleDoc{}
	if _, err := UnmarshalWithReport([]byte(file), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Config.Endpoint != "https://example.com\n/second" {
		t.Errorf("multi-line string = %q", doc.Config.Endpoint)
	}
}

func TestUnknownKeysAreReported(t *testing.T) {
	file := `
[config]
retries = 1
retires = 2        # typo
[endpointz]
a = 1
`
	doc := sampleDoc{}
	unknown, err := UnmarshalWithReport([]byte(file), &doc)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(unknown, ",")
	if !strings.Contains(joined, "config.retires") || !strings.Contains(joined, "endpointz") {
		t.Errorf("unknown keys = %v", unknown)
	}
	if doc.Config.Retries != 1 {
		t.Errorf("known key must still be applied: %+v", doc.Config)
	}
}

func TestErrorCases(t *testing.T) {
	bad := []string{
		`[config`,             // malformed header
		`key = "unterminated`, // unterminated string
		`[[array]]`,           // arrays of tables unsupported
		`key`,                 // no value
		`key = @`,             // unsupported value
	}
	for _, in := range bad {
		doc := sampleDoc{}
		if err := Unmarshal([]byte(in), &doc); err == nil {
			t.Errorf("%q should fail to parse", in)
		}
	}
}

func TestSetKeyInSectionKeepsEverythingElse(t *testing.T) {
	file := `# my notes
state_version = 1

[config]
retries = 3            # keep this comment
endpoint = "https://example.com"

# section below must survive
[instances.vanilla]
name = "vanilla"
version = "1.21.4"
`
	out, ok, err := SetKeyInSection([]byte(file), "config", "retries", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("retries exists, so the edit must succeed")
	}
	text := string(out)
	if !strings.Contains(text, "retries = 7") {
		t.Errorf("value not replaced:\n%s", text)
	}
	for _, keep := range []string{"# my notes", "# keep this comment", `endpoint = "https://example.com"`, "[instances.vanilla]", `version = "1.21.4"`} {
		if !strings.Contains(text, keep) {
			t.Errorf("edit dropped %q:\n%s", keep, text)
		}
	}
	// a key in another section must not be touched
	if _, ok, _ := SetKeyInSection([]byte(file), "instances.vanilla", "nope", 1); ok {
		t.Error("absent key must report false")
	}
	// strings and booleans render correctly
	out2, ok, err := SetKeyInSection([]byte(file), "config", "endpoint", "https://bmclapi2.bangbang93.com")
	if err != nil || !ok || !strings.Contains(string(out2), `endpoint = "https://bmclapi2.bangbang93.com"`) {
		t.Errorf("string edit failed: %v %v\n%s", err, ok, out2)
	}
}

func TestGetKeyInSection(t *testing.T) {
	file := "[config]\nretries = 4\n"
	v, ok, err := GetKeyInSection([]byte(file), "config", "retries")
	if err != nil || !ok || v != "4" {
		t.Errorf("GetKeyInSection = %q %v %v", v, ok, err)
	}
	if _, ok, _ = GetKeyInSection([]byte(file), "config", "missing"); ok {
		t.Error("missing key must report false")
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{3, "3"},
		{true, "true"},
		{"plain", `"plain"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), `"2026-01-02T03:04:05Z"`},
		{[]string{"a"}, `["a"]`},
	}
	for _, c := range cases {
		got, err := FormatValue(c.in)
		if err != nil {
			t.Fatalf("%v: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("FormatValue(%v) = %q want %q", c.in, got, c.want)
		}
	}
	if _, err := FormatValue(struct{}{}); err == nil {
		t.Error("structs are not scalars")
	}
}

func TestQuotedKeysWithDots(t *testing.T) {
	file := "[instances.\"survival.2026\"]\nname = \"survival.2026\"\nversion = \"1.21.4\"\n"
	doc := sampleDoc{}
	if _, err := UnmarshalWithReport([]byte(file), &doc); err != nil {
		t.Fatal(err)
	}
	inst := doc.Inst["survival.2026"]
	if inst == nil || inst.Version != "1.21.4" {
		t.Fatalf("quoted key not decoded: %+v", doc.Inst)
	}
}
