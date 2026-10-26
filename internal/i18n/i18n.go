// Package i18n holds every user-facing string in mcli.
//
// Rules:
//   - no user-visible literal is ever written inline in other packages; they
//     all call i18n.T("key") / i18n.T("key", args...)
//   - English is the source of truth and the fallback
//   - translation lookup is keyed by a dotted id, never by the English text,
//     so wording can change without breaking the Chinese catalog
//
// Language selection order: --lang flag > config language > $MCLI_LANG >
// $LC_ALL / $LC_MESSAGES / $LANG > English. "auto" means "use the environment".
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang is a supported language tag.
type Lang string

// Supported languages.
const (
	EN   Lang = "en"
	ZH   Lang = "zh"
	Auto Lang = "auto"
)

var current = EN

// catalogs maps language -> key -> text. English must contain every key.
var catalogs = map[Lang]map[string]string{
	EN: en,
	ZH: zh,
}

// Set switches the active language. "auto" detects from the environment.
func Set(l string) error {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "", "auto", "detect":
		current = detect()
		return nil
	case "en", "en-us", "en_us", "english":
		current = EN
		return nil
	case "zh", "zh-cn", "zh_cn", "zh-hans", "cn", "chinese":
		current = ZH
		return nil
	default:
		if _, ok := catalogs[Lang(l)]; ok {
			current = Lang(l)
			return nil
		}
		return fmt.Errorf("unsupported language %q (want auto|en|zh)", l)
	}
}

// Current returns the active language.
func Current() Lang { return current }

// IsAuto returns true when the language was auto-detected.
func IsAuto() bool { return autoDetected }

var autoDetected bool

func detect() Lang {
	for _, env := range []string{"MCLI_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(os.Getenv(env))
		if v == "" {
			continue
		}
		if strings.Contains(v, "zh") || strings.Contains(v, "chinese") {
			autoDetected = true
			return ZH
		}
		if strings.Contains(v, "en") {
			autoDetected = true
			return EN
		}
	}
	autoDetected = true
	return EN
}

// T returns the text for key in the active language, formatted with args when
// any are given. Unknown keys fall back to English, then to the key itself
// wrapped in <!...!> so a missing translation is impossible to miss.
func T(key string, args ...interface{}) string {
	s, ok := catalogs[current][key]
	if !ok {
		s, ok = catalogs[EN][key]
	}
	if !ok {
		s = "<!" + key + "!>"
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Has reports whether a key exists in any catalog (used by tests).
func Has(key string) bool {
	_, ok := catalogs[EN][key]
	return ok
}

// Keys returns every known key (used by tests and `mcli config list` docs).
func Keys() []string {
	out := make([]string, 0, len(en))
	for k := range en {
		out = append(out, k)
	}
	return out
}

// Langs lists the selectable languages.
func Langs() []string { return []string{string(EN), string(ZH)} }
