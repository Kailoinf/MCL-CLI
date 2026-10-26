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
	DownloadRetryTimes int    `json:"download_retry_times" desc:"cfg.desc.download_retry_times"`
	HTTPTimeout        int    `json:"http_timeout" desc:"cfg.desc.http_timeout"`
	BmclapiEndpoint    string `json:"bmclapi_endpoint" desc:"cfg.desc.bmclapi_endpoint"`
	MirrorFallback     bool   `json:"mirror_fallback" desc:"cfg.desc.mirror_fallback"`

	// --- manifest -------------------------------------------------------
	ManifestAutoRefresh bool `json:"manifest_auto_refresh" desc:"cfg.desc.manifest_auto_refresh"`
	ManifestTTLHours    int  `json:"manifest_ttl_hours" desc:"cfg.desc.manifest_ttl_hours"`

	// --- paths ----------------------------------------------------------
	MinecraftDir string `json:"minecraft_dir" desc:"cfg.desc.minecraft_dir"`
	VersionsDir  string `json:"versions_dir" desc:"cfg.desc.versions_dir"`
	JavaDir      string `json:"java_dir" desc:"cfg.desc.java_dir"`
	JavaPath     string `json:"java_path" desc:"cfg.desc.java_path"`

	// --- logging --------------------------------------------------------
	LogLevelFile     string `json:"log_level_file" desc:"cfg.desc.log_level_file"`
	LogLevelTerminal string `json:"log_level_terminal" desc:"cfg.desc.log_level_terminal"`

	// --- jvm ------------------------------------------------------------
	AutoMemoryPercent int    `json:"auto_memory_percent" desc:"cfg.desc.auto_memory_percent"`
	DefaultXmx        string `json:"default_xmx" desc:"cfg.desc.default_xmx"`
	DefaultXms        string `json:"default_xms" desc:"cfg.desc.default_xms"`
	ExtraJvmArgs      string `json:"extra_jvm_args" desc:"cfg.desc.extra_jvm_args"`

	// --- launch ---------------------------------------------------------
	ConfirmLargeDownload bool `json:"confirm_large_download" desc:"cfg.desc.confirm_large_download"`
	LargeDownloadMB      int  `json:"large_download_mb" desc:"cfg.desc.large_download_mb"`
	CloseAfterLaunch     bool `json:"close_after_launch" desc:"cfg.desc.close_after_launch"`

	// --- interface ------------------------------------------------------
	Language string `json:"language" desc:"cfg.desc.language"`
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
		Language:             "auto",
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
		out = append(out, t.Field(i).Tag.Get("json"))
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
		if f.Tag.Get("json") == key {
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
