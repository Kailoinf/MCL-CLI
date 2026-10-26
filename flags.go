package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// flagSpec describes one accepted option.
type flagSpec struct {
	takesValue bool
	list       bool
}

// flags is the parsed result.
type flags struct {
	values map[string]string
	lists  map[string][]string
	bools  map[string]bool
	args   []string // positionals
}

func newFlags() *flags {
	return &flags{
		values: map[string]string{},
		lists:  map[string][]string{},
		bools:  map[string]bool{},
	}
}

func (f *flags) has(name string) bool {
	if f.bools[name] {
		return true
	}
	_, ok := f.values[name]
	return ok
}

func (f *flags) str(name, def string) string {
	if v, ok := f.values[name]; ok {
		return v
	}
	return def
}

func (f *flags) boolean(name string) bool { return f.bools[name] }

func (f *flags) int(name string, def int) (int, error) {
	v, ok := f.values[name]
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s", i18n.T("cli.bad_flag_value", "--"+name, v+" is not a number"))
	}
	return n, nil
}

// parseFlags walks the argv for one command. Unknown options are an error so
// typos never silently change behaviour.
func parseFlags(cmd string, argv []string, specs map[string]flagSpec) (*flags, error) {
	f := newFlags()
	i := 0
	for i < len(argv) {
		tok := argv[i]
		if tok == "--" {
			f.args = append(f.args, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(tok, "-") {
			f.args = append(f.args, tok)
			i++
			continue
		}
		name := strings.TrimLeft(tok, "-")
		value := ""
		hasValue := false
		if eq := strings.Index(name, "="); eq >= 0 {
			value, hasValue = name[eq+1:], true
			name = name[:eq]
		}
		spec, ok := specs[name]
		if !ok {
			switch name {
			case "h", "help":
				f.bools["help"] = true
				i++
				continue
			case "y", "yes":
				f.bools["yes"] = true
				i++
				continue
			}
			return nil, fmt.Errorf("%s", i18n.T("cli.unknown_flag", tok, cmd))
		}
		switch {
		case spec.takesValue && spec.list:
			if !hasValue {
				if i+1 >= len(argv) {
					return nil, fmt.Errorf("%s", i18n.T("cli.flag_needs_value", "--"+name))
				}
				value = argv[i+1]
				i++
			}
			f.lists[name] = append(f.lists[name], value)
		case spec.takesValue:
			if !hasValue {
				if i+1 >= len(argv) {
					return nil, fmt.Errorf("%s", i18n.T("cli.flag_needs_value", "--"+name))
				}
				value = argv[i+1]
				i++
			}
			f.values[name] = value
		default:
			if hasValue {
				return nil, fmt.Errorf("%s", i18n.T("cli.flag_takes_no_value", "--"+name))
			}
			f.bools[name] = true
		}
		i++
	}
	return f, nil
}

// globalSpecs are accepted before and after the command name.
var globalSpecs = map[string]flagSpec{
	"lang":     {takesValue: true},
	"config":   {takesValue: true},
	"quiet":    {},
	"verbose":  {},
	"yes":      {},
	"no-color": {},
	"help":     {},
}

// splitGlobal extracts global options from anywhere in argv, leaving the
// command and its own options in rest.
func splitGlobal(argv []string) (glob *flags, rest []string, err error) {
	glob = newFlags()
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		name := strings.TrimLeft(tok, "-")
		if !strings.HasPrefix(tok, "-") || name == "" {
			rest = append(rest, tok)
			continue
		}
		value := ""
		hasValue := false
		if eq := strings.Index(name, "="); eq >= 0 {
			value, hasValue = name[eq+1:], true
			name = name[:eq]
		}
		spec, ok := globalSpecs[name]
		if !ok {
			// not global: keep it (and a possible value) for the command parser
			rest = append(rest, tok)
			continue
		}
		if spec.takesValue && !hasValue {
			if i+1 >= len(argv) {
				return nil, nil, fmt.Errorf("%s", i18n.T("cli.flag_needs_value", "--"+name))
			}
			value = argv[i+1]
			i++
		}
		if spec.takesValue {
			glob.values[name] = value
		} else {
			glob.bools[name] = true
		}
	}
	return glob, rest, nil
}
