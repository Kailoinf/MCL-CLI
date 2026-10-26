// Package config holds the mcli/config "table" stored inside mcli.db and the
// key/value plumbing used by `mcli config get|set|unset|list`.
//
// Every human-readable description is an i18n key (see internal/i18n), so the
// config surface is bilingual like the rest of the CLI.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// Config is the persisted configuration. Every field is a row of the config
// table: its JSON tag is the key the user types on the command line and its
// desc tag is the i18n key describing it.
type Config struct {
	// --- download -------------------------------------------------------
	DownloadRetryTimes int    `toml:"download_retry_times" desc:"cfg.desc.download_retry_times"`
	HTTPTimeout        int    `toml:"http_timeout" desc:"cfg.desc.http_timeout"`
	BmclapiEndpoint    string `toml:"bmclapi_endpoint" desc:"cfg.desc.bmclapi_endpoint"`
	MirrorFallback     bool   `toml:"mirror_fallback" desc:"cfg.desc.mirror_fallback"`

	// --- manifest -------------------------------------------------------
	ManifestAutoRefresh bool `toml:"manifest_auto_refresh" desc:"cfg.desc.manifest_auto_refresh"`
	ManifestTTLHours    int  `toml:"manifest_ttl_hours" desc:"cfg.desc.manifest_ttl_hours"`

	// --- paths ----------------------------------------------------------
	MinecraftDir string `toml:"minecraft_dir" desc:"cfg.desc.minecraft_dir"`
	VersionsDir  string `toml:"versions_dir" desc:"cfg.desc.versions_dir"`
	JavaDir      string `toml:"java_dir" desc:"cfg.desc.java_dir"`
	JavaPath     string `toml:"java_path" desc:"cfg.desc.java_path"`

	// --- logging --------------------------------------------------------
	LogLevelFile     string `toml:"log_level_file" desc:"cfg.desc.log_level_file"`
	LogLevelTerminal string `toml:"log_level_terminal" desc:"cfg.desc.log_level_terminal"`

	// --- jvm ------------------------------------------------------------
	AutoMemoryPercent int    `toml:"auto_memory_percent" desc:"cfg.desc.auto_memory_percent"`
	DefaultXmx        string `toml:"default_xmx" desc:"cfg.desc.default_xmx"`
	DefaultXms        string `toml:"default_xms" desc:"cfg.desc.default_xms"`
	ExtraJvmArgs      string `toml:"extra_jvm_args" desc:"cfg.desc.extra_jvm_args"`

	// --- launch ---------------------------------------------------------
	ConfirmLargeDownload bool `toml:"confirm_large_download" desc:"cfg.desc.confirm_large_download"`
	LargeDownloadMB      int  `toml:"large_download_mb" desc:"cfg.desc.large_download_mb"`
	CloseAfterLaunch     bool `toml:"close_after_launch" desc:"cfg.desc.close_after_launch"`

	// --- authentication -------------------------------------------------
	AuthClientID string `toml:"auth_client_id" desc:"cfg.desc.auth_client_id"`

	// --- interface ------------------------------------------------------
	Language string `toml:"language" desc:"cfg.desc.language"`
}

// Default returns the built-in defaults. Paths that depend on the home
// directory are filled with absolute values so `mcli config list` never shows
// an empty location.
func Default() Config {
	home, _ := os.UserHomeDir()
	mc := filepath.Join(home, ".minecraft")
	if v := os.Getenv("MCLI_MINECRAFT"); v != "" {
		mc = v
	}
	return Config{
		DownloadRetryTimes:   3,
		HTTPTimeout:          30,
		BmclapiEndpoint:      "https://bmclapi2.bangbang93.com",
		MirrorFallback:       true,
		ManifestAutoRefresh:  true,
		ManifestTTLHours:     6,
		MinecraftDir:         mc,
		VersionsDir:          "versions",
		JavaDir:              "",
		JavaPath:             "",
		LogLevelFile:         "debug",
		LogLevelTerminal:     "info",
		AutoMemoryPercent:    50,
		DefaultXmx:           "",
		DefaultXms:           "",
		ExtraJvmArgs:         "",
		ConfirmLargeDownload: true,
		LargeDownloadMB:      50,
		CloseAfterLaunch:     true,
		AuthClientID:         "288ec5dd-6736-4d4b-9b96-30e083a8cad2",
		Language:             "auto",
	}
}

// keyOf reads the user-facing key of a config field: the `toml` tag, else the
// `json` tag, else the field name.
func keyOf(f reflect.StructField) string {
	if t := f.Tag.Get("toml"); t != "" {
		return strings.Split(t, ",")[0]
	}
	if t := f.Tag.Get("json"); t != "" {
		return strings.Split(t, ",")[0]
	}
	return f.Name
}

// RawValue returns the typed value of one key (string, int or bool), used by the
// TOML writer so a patched line keeps its type.
func (c *Config) RawValue(key string) (interface{}, error) {
	f, err := fieldByKey(key)
	if err != nil {
		return nil, err
	}
	v := reflect.ValueOf(c).Elem().FieldByIndex(f.Index)
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int:
		return int(v.Int()), nil
	case reflect.Bool:
		return v.Bool(), nil
	default:
		return nil, fmt.Errorf("%s", i18n.T("err.config.bad_type", key))
	}
}

// FillMissing restores defaults for fields left empty. After unmarshalling an
// older mcli.db, keys that did not exist yet arrive as zero values; this puts
// the defaults back. Empty strings are only refilled when the default is
// non-empty, so an intentionally blank key (e.g. java_path) stays blank.
func (c *Config) FillMissing() {
	d := Default()
	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		f, df := v.Field(i), reflect.ValueOf(d).Field(i)
		switch f.Kind() {
		case reflect.String:
			if f.String() == "" && df.String() != "" {
				f.SetString(df.String())
			}
		case reflect.Int:
			if f.Int() == 0 && df.Int() != 0 {
				f.SetInt(df.Int())
			}
		}
	}
}

// Keys lists every configuration key, sorted.
func Keys() []string {
	t := reflect.TypeOf(Config{})
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		out = append(out, keyOf(t.Field(i)))
	}
	sort.Strings(out)
	return out
}

// Get reads one key as a string.
func (c *Config) Get(key string) (string, error) {
	f, err := fieldByKey(key)
	if err != nil {
		return "", err
	}
	v := reflect.ValueOf(c).Elem().FieldByIndex(f.Index)
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int:
		return strconv.Itoa(int(v.Int())), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	default:
		return "", fmt.Errorf("%s", i18n.T("err.config.bad_type", key))
	}
}

// Set writes one key from a command-line string.
func (c *Config) Set(key, value string) error {
	f, err := fieldByKey(key)
	if err != nil {
		return err
	}
	v := reflect.ValueOf(c).Elem().FieldByIndex(f.Index)
	switch v.Kind() {
	case reflect.String:
		v.SetString(value)
	case reflect.Int:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%s", i18n.T("err.config.int_expected", key, value))
		}
		if err := validateInt(key, n); err != nil {
			return err
		}
		v.SetInt(int64(n))
	case reflect.Bool:
		b, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("%s", i18n.T("err.config.bool_expected", key, value))
		}
		v.SetBool(b)
	default:
		return fmt.Errorf("%s", i18n.T("err.config.bad_type", key))
	}
	return nil
}

// Reset sets one key back to its default value (used by `config unset`).
func (c *Config) Reset(key string) (string, error) {
	f, err := fieldByKey(key)
	if err != nil {
		return "", err
	}
	dv := reflect.ValueOf(Default()).FieldByIndex(f.Index)
	v := reflect.ValueOf(c).Elem().FieldByIndex(f.Index)
	v.Set(dv)
	return c.Get(key)
}

// Describe returns the localized description attached to a key.
func Describe(key string) string {
	f, err := fieldByKey(key)
	if err != nil {
		return ""
	}
	return i18n.T(f.Tag.Get("desc"))
}

// Rows returns key, value, description triples for `mcli config list`.
func (c *Config) Rows() [][3]string {
	out := make([][3]string, 0, len(Keys()))
	for _, k := range Keys() {
		v, _ := c.Get(k)
		out = append(out, [3]string{k, v, Describe(k)})
	}
	return out
}

func fieldByKey(key string) (reflect.StructField, error) {
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if keyOf(f) == key {
			return f, nil
		}
	}
	return reflect.StructField{}, fmt.Errorf("%s", i18n.T("err.config.unknown_key", key))
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y", "on":
		return true, nil
	case "false", "0", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean")
}

func validateInt(key string, n int) error {
	switch key {
	case "download_retry_times":
		if n < 1 || n > 20 {
			return fmt.Errorf("%s", i18n.T("err.config.range", key, 1, 20))
		}
	case "http_timeout":
		if n < 5 || n > 600 {
			return fmt.Errorf("%s", i18n.T("err.config.range", key, 5, 600))
		}
	case "manifest_ttl_hours":
		if n < 1 || n > 720 {
			return fmt.Errorf("%s", i18n.T("err.config.range", key, 1, 720))
		}
	case "auto_memory_percent":
		if n < 10 || n > 90 {
			return fmt.Errorf("%s", i18n.T("err.config.range", key, 10, 90))
		}
	case "large_download_mb":
		if n < 1 {
			return fmt.Errorf("%s", i18n.T("err.config.min", key, 1))
		}
	}
	return nil
}

// Validate checks cross-field sanity before mcli.db is written. It also
// re-checks numeric ranges so a hand-edited mcli.db cannot smuggle in values
// the CLI would have rejected.
func (c *Config) Validate() error {
	for _, k := range []string{"download_retry_times", "http_timeout", "manifest_ttl_hours", "auto_memory_percent", "large_download_mb"} {
		v, err := c.Get(k)
		if err != nil {
			return err
		}
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return fmt.Errorf("%s", i18n.T("err.config.int_expected", k, v))
		}
		if err := validateInt(k, n); err != nil {
			return err
		}
	}
	if c.AuthClientID == "" {
		return fmt.Errorf("%s", i18n.T("err.config.client_id"))
	}
	if c.BmclapiEndpoint != "" && !strings.HasPrefix(c.BmclapiEndpoint, "http") {
		return fmt.Errorf("%s", i18n.T("err.config.endpoint_scheme"))
	}
	for _, k := range []string{"log_level_file", "log_level_terminal"} {
		v, _ := c.Get(k)
		switch strings.ToLower(v) {
		case "debug", "info", "warn", "warning", "error", "off", "none", "silent":
		default:
			return fmt.Errorf("%s", i18n.T("err.config.log_level_invalid", k, v))
		}
	}
	if c.VersionsDir == "" || strings.ContainsAny(c.VersionsDir, `/\`) {
		return fmt.Errorf("%s", i18n.T("err.config.versions_dir"))
	}
	switch strings.ToLower(c.Language) {
	case "auto", "en", "zh", "en-us", "en_us", "zh-cn", "zh_cn", "zh-hans":
	default:
		return fmt.Errorf("%s", i18n.T("err.config.lang", c.Language))
	}
	return nil
}
