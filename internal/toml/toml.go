// Package toml implements the small, dependency-free TOML dialect mcli needs to
// store its configuration and state in a human-editable mcli.toml.
//
// Supported (and tested):
//
//	root key/value pairs            db_version = 2
//	[table] and [table.sub]         [config], [accounts."uuid.with.dots"]
//	basic strings with escapes      "a\"b\nc", \uXXXX
//	literal strings                 'C:\Users\kailo'
//	multi-line basic strings        """…""" (with escaped newlines)
//	integers (with underscores), floats, booleans
//	arrays of scalars               ["-XX:+UseG1GC", "-Dfoo=1"]
//	comments (# …) and blank lines
//
// Not supported, on purpose: [[array of tables]], inline tables, dates as
// datetime values (times are written as RFC3339 strings), and dotted keys as
// values. Anything unsupported produces a clear error instead of being ignored.
//
// The mapping between TOML and Go values is driven by struct tags: a `toml`
// tag names the key, and a `json` tag is used as a fallback so plain JSON-ish
// structs keep working. Unknown keys in the file are reported to the caller
// (reportUnknown) rather than silently dropped.
package toml

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Marshal

// Marshal renders v as TOML. v must be a struct (or pointer to one).
func Marshal(v interface{}) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil, fmt.Errorf("toml: cannot marshal a nil pointer")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("toml: marshal expects a struct, got %s", rv.Kind())
	}
	var b strings.Builder
	if err := marshalStruct(&b, rv, nil, true); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// marshalStruct writes root scalars (when isRoot) and every sub-table.
func marshalStruct(b *strings.Builder, rv reflect.Value, path []string, isRoot bool) error {
	t := rv.Type()
	timeType := reflect.TypeOf(time.Time{})
	// scalars first: TOML requires root keys before root tables
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		fv := rv.Field(i)
		kind := fv.Kind()
		isTime := f.Type == timeType || (kind == reflect.Ptr && f.Type.Elem() == timeType)
		if !isTime {
			if kind == reflect.Struct || kind == reflect.Map || (kind == reflect.Ptr && fv.Type().Elem().Kind() == reflect.Struct) {
				continue
			}
		}
		key := fieldKey(f)
		if key == "-" {
			continue
		}
		text, ok, err := scalarText(fv)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		fmt.Fprintf(b, "%s = %s\n", quoteKey(key), text)
	}
	// then nested tables in declaration order
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		fv := rv.Field(i)
		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		name := fieldKey(f)
		if name == "-" {
			continue
		}
		switch fv.Kind() {
		case reflect.Struct:
			if _, isTime := fv.Interface().(time.Time); isTime {
				continue
			}
			if !hasContent(fv) {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(b, "[%s]\n", joinPath(append(path, name)))
			if err := marshalStruct(b, fv, append(path, name), false); err != nil {
				return err
			}
		case reflect.Map:
			if fv.Len() == 0 {
				continue
			}
			keys := fv.MapKeys()
			sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface()) })
			for _, k := range keys {
				mv := fv.MapIndex(k)
				if mv.Kind() == reflect.Ptr {
					if mv.IsNil() {
						continue
					}
					mv = mv.Elem()
				}
				if mv.Kind() != reflect.Struct {
					continue
				}
				b.WriteString("\n")
				fmt.Fprintf(b, "[%s]\n", joinPath(append(path, name, fmt.Sprint(k.Interface()))))
				if err := marshalStruct(b, mv, append(path, name, fmt.Sprint(k.Interface())), false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// hasContent reports whether a struct has anything worth writing.
func hasContent(rv reflect.Value) bool {
	t := rv.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		text, ok, err := scalarText(rv.Field(i))
		if err == nil && ok && text != "" {
			return true
		}
		if rv.Field(i).Kind() == reflect.Struct {
			if hasContent(rv.Field(i)) {
				return true
			}
		}
	}
	return false
}

// scalarText renders one field, ok=false meaning "omit".
func scalarText(fv reflect.Value) (text string, ok bool, err error) {
	switch fv.Kind() {
	case reflect.String:
		return strconv.Quote(fv.String()), true, nil
	case reflect.Bool:
		return strconv.FormatBool(fv.Bool()), true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(fv.Int(), 10), true, nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(fv.Float(), 'g', -1, 64), true, nil
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return "", false, nil
		}
		if fv.Len() == 0 {
			return "", false, nil
		}
		parts := make([]string, 0, fv.Len())
		for i := 0; i < fv.Len(); i++ {
			parts = append(parts, strconv.Quote(fv.Index(i).String()))
		}
		return "[" + strings.Join(parts, ", ") + "]", true, nil
	}
	if t, isTime := fv.Interface().(time.Time); isTime {
		if t.IsZero() {
			return "", false, nil
		}
		return strconv.Quote(t.UTC().Format(time.RFC3339)), true, nil
	}
	return "", false, nil
}

func fieldKey(f reflect.StructField) string {
	if tag := f.Tag.Get("toml"); tag != "" {
		return strings.Split(tag, ",")[0]
	}
	if tag := f.Tag.Get("json"); tag != "" {
		return strings.Split(tag, ",")[0]
	}
	return f.Name
}

func quoteKey(k string) string {
	if k != "" && strings.IndexFunc(k, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0 {
		return k
	}
	return strconv.Quote(k)
}

func joinPath(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = quoteKey(p)
	}
	return strings.Join(out, ".")
}

// ---------------------------------------------------------------------------
// Unmarshal

// Unmarshal parses TOML into v (a pointer to a struct).
func Unmarshal(data []byte, v interface{}) error {
	_, err := UnmarshalWithReport(data, v)
	return err
}

// UnmarshalWithReport is Unmarshal plus a list of keys present in the file that
// the struct does not know about — useful for warning about typos in a
// hand-edited config without breaking forward compatibility.
func UnmarshalWithReport(data []byte, v interface{}) ([]string, error) {
	tree, err := parseTree(string(data))
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil, fmt.Errorf("toml: unmarshal expects a non-nil pointer")
	}
	var unknown []string
	if err := decodeInto(tree, rv.Elem(), "", &unknown); err != nil {
		return nil, err
	}
	return unknown, nil
}

type tree = map[string]interface{}

func parseTree(data string) (tree, error) {
	root := tree{}
	current := root
	var currentPath []string
	lines := splitLines(data)
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[") {
				return nil, fmt.Errorf("line %d: arrays of tables are not supported", i+1)
			}
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: malformed table header %q", i+1, line)
			}
			path, err := splitKey(strings.TrimSpace(line[1 : len(line)-1]))
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", i+1, err)
			}
			currentPath = path
			current = root
			for _, seg := range path {
				next, ok := current[seg].(tree)
				if !ok {
					next = tree{}
					current[seg] = next
				}
				current = next
			}
			continue
		}
		eq := indexOutsideQuotes(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected key = value, got %q", i+1, line)
		}
		keyPart := strings.TrimSpace(line[:eq])
		valuePart := strings.TrimSpace(line[eq+1:])
		path, err := splitKey(keyPart)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		// multi-line basic strings
		if strings.HasPrefix(valuePart, `"""`) {
			rest := strings.TrimPrefix(valuePart, `"""`)
			var sb strings.Builder
			if idx := strings.Index(rest, `"""`); idx >= 0 {
				sb.WriteString(rest[:idx])
				valuePart = sb.String()
			} else {
				sb.WriteString(rest)
				for {
					i++
					if i >= len(lines) {
						return nil, fmt.Errorf("line %d: unterminated multi-line string", i)
					}
					next := lines[i]
					if idx := strings.Index(next, `"""`); idx >= 0 {
						sb.WriteString("\n")
						sb.WriteString(next[:idx])
						break
					}
					sb.WriteString("\n")
					sb.WriteString(next)
				}
				valuePart = sb.String()
			}
			valuePart = unescapeBasic(strings.TrimPrefix(valuePart, "\n"))
			if err := assign(current, path, valuePart); err != nil {
				return nil, fmt.Errorf("line %d: %v", i+1, err)
			}
			continue
		}
		// arrays may span several lines
		if strings.HasPrefix(valuePart, "[") {
			for arrayIncomplete(valuePart) {
				i++
				if i >= len(lines) {
					return nil, fmt.Errorf("unterminated array")
				}
				valuePart += " " + strings.TrimSpace(stripComment(lines[i]))
			}
		}
		val, err := parseValue(valuePart)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		if err := assign(current, path, val); err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
	}
	_ = currentPath
	return root, nil
}

func splitLines(data string) []string {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	return strings.Split(data, "\n")
}

// assign walks/creates the intermediate tables and sets the leaf value.
func assign(t tree, path []string, value interface{}) error {
	if len(path) == 0 {
		return fmt.Errorf("empty key")
	}
	for _, seg := range path[:len(path)-1] {
		next, ok := t[seg].(tree)
		if !ok {
			next = tree{}
			t[seg] = next
		}
		t = next
	}
	t[path[len(path)-1]] = value
	return nil
}

// commentIndex returns the index of a trailing # comment, or -1.
func commentIndex(line string) int {
	var inBasic, inLiteral, esc bool
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case esc:
			esc = false
		case inBasic && c == '\\':
			esc = true
		case inBasic && c == '"':
			inBasic = false
		case inLiteral && c == '\'':
			inLiteral = false
		case !inBasic && !inLiteral && c == '"':
			inBasic = true
		case !inBasic && !inLiteral && c == '\'':
			inLiteral = true
		case !inBasic && !inLiteral && c == '#':
			return i
		}
	}
	return -1
}

// stripComment removes a trailing # comment, ignoring # inside strings.
func stripComment(line string) string {
	if i := commentIndex(line); i >= 0 {
		return line[:i]
	}
	return line
}

// arrayIncomplete reports whether an array literal still needs more lines.
func arrayIncomplete(s string) bool {
	depth := 0
	var inBasic, inLiteral, esc bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inBasic && c == '\\':
			esc = true
		case inBasic && c == '"':
			inBasic = false
		case inLiteral && c == '\'':
			inLiteral = false
		case !inBasic && !inLiteral && c == '"':
			inBasic = true
		case !inBasic && !inLiteral && c == '\'':
			inLiteral = true
		case !inBasic && !inLiteral && c == '[':
			depth++
		case !inBasic && !inLiteral && c == ']':
			depth--
		}
	}
	return depth > 0
}

// indexOutsideQuotes finds the first occurrence of ch outside any string.
func indexOutsideQuotes(s string, ch byte) int {
	var inBasic, inLiteral, esc bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inBasic && c == '\\':
			esc = true
		case inBasic && c == '"':
			inBasic = false
		case inLiteral && c == '\'':
			inLiteral = false
		case !inBasic && !inLiteral && c == '"':
			inBasic = true
		case !inBasic && !inLiteral && c == '\'':
			inLiteral = true
		case !inBasic && !inLiteral && c == ch:
			return i
		}
	}
	return -1
}

// splitKey splits "a.b.\"c.d\"" into its segments.
func splitKey(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var inBasic, inLiteral, esc bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			cur.WriteByte(c)
			esc = false
		case inBasic && c == '\\':
			cur.WriteByte(c)
			esc = true
		case inBasic && c == '"':
			inBasic = false
			part := cur.String()
			cur.Reset()
			if part != "" {
				out = append(out, part)
			}
		case inLiteral && c == '\'':
			inLiteral = false
			part := cur.String()
			cur.Reset()
			if part != "" {
				out = append(out, part)
			}
		case !inBasic && !inLiteral && c == '"':
			inBasic = true
		case !inBasic && !inLiteral && c == '\'':
			inLiteral = true
		case !inBasic && !inLiteral && c == '.':
			if seg := strings.TrimSpace(cur.String()); seg != "" {
				out = append(out, seg)
			}
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if inBasic || inLiteral {
		return nil, fmt.Errorf("unterminated quoted key")
	}
	if seg := strings.TrimSpace(cur.String()); seg != "" {
		out = append(out, seg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty key")
	}
	return out, nil
}

// parseValue converts one TOML value literal.
func parseValue(s string) (interface{}, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("missing value")
	}
	switch {
	case strings.HasPrefix(s, `"`):
		if !strings.HasSuffix(s, `"`) || len(s) < 2 {
			return nil, fmt.Errorf("unterminated string")
		}
		return unescapeBasic(s[1 : len(s)-1]), nil
	case strings.HasPrefix(s, `'`):
		if !strings.HasSuffix(s, `'`) || len(s) < 2 {
			return nil, fmt.Errorf("unterminated literal string")
		}
		return s[1 : len(s)-1], nil
	case strings.HasPrefix(s, "["):
		if !strings.HasSuffix(s, "]") {
			return nil, fmt.Errorf("unterminated array")
		}
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []interface{}{}, nil
		}
		parts := splitTopLevel(inner)
		vals := make([]interface{}, 0, len(parts))
		for _, p := range parts {
			if strings.TrimSpace(p) == "" {
				continue // TOML allows trailing commas
			}
			v, err := parseValue(p)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		return vals, nil
	case s == "true":
		return true, nil
	case s == "false":
		return false, nil
	}
	numeric := strings.ReplaceAll(s, "_", "")
	if i, err := strconv.ParseInt(numeric, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(numeric, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("unsupported value %q", s)
}

func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	var inBasic, inLiteral, esc bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inBasic && c == '\\':
			esc = true
		case inBasic && c == '"':
			inBasic = false
		case inLiteral && c == '\'':
			inLiteral = false
		case !inBasic && !inLiteral && c == '"':
			inBasic = true
		case !inBasic && !inLiteral && c == '\'':
			inLiteral = true
		case !inBasic && !inLiteral && c == '[':
			depth++
		case !inBasic && !inLiteral && c == ']':
			depth--
		case !inBasic && !inLiteral && c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func unescapeBasic(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '0':
			b.WriteByte(0)
		case 'u', 'U':
			width := 4
			if s[i] == 'U' {
				width = 8
			}
			if i+width < len(s) {
				hex := s[i+1 : i+1+width]
				if n, err := strconv.ParseUint(hex, 16, 32); err == nil {
					var buf [4]byte
					sz := utf8.EncodeRune(buf[:], rune(n))
					b.Write(buf[:sz])
					i += width
					continue
				}
			}
			b.WriteByte(s[i])
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// DecodeMap decodes an already-parsed tree — for example the result of
// json.Unmarshal into map[string]interface{} — using exactly the same rules as
// Unmarshal. It exists so legacy JSON documents can be imported with the TOML
// decoder instead of a second set of struct tags. JSON numbers arrive as
// float64 and are accepted where an integer is expected.
func DecodeMap(m map[string]interface{}, v interface{}) ([]string, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil, fmt.Errorf("toml: DecodeMap expects a non-nil pointer")
	}
	var unknown []string
	if err := decodeInto(tree(m), rv.Elem(), "", &unknown); err != nil {
		return unknown, err
	}
	return unknown, nil
}

// ---------------------------------------------------------------------------
// decoding into Go values

func decodeInto(t tree, rv reflect.Value, path string, unknown *[]string) error {
	switch rv.Kind() {
	case reflect.Map:
		// map[string]T (T struct or scalar)
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(rv.Type()))
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub, ok := t[k].(tree)
			if !ok {
				if scalar, err := coerceScalar(t[k], rv.Type().Elem()); err == nil {
					mv := reflect.New(rv.Type().Elem()).Elem()
					mv.Set(scalar)
					rv.SetMapIndex(reflect.ValueOf(k), mv)
					continue
				}
				*unknown = append(*unknown, join(path, k))
				continue
			}
			elemType := rv.Type().Elem()
			ptr := false
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
				ptr = true
			}
			if elemType.Kind() != reflect.Struct {
				*unknown = append(*unknown, join(path, k))
				continue
			}
			val := reflect.New(elemType)
			if err := decodeInto(sub, val.Elem(), join(path, k), unknown); err != nil {
				return err
			}
			if ptr {
				rv.SetMapIndex(reflect.ValueOf(k), val)
			} else {
				rv.SetMapIndex(reflect.ValueOf(k), val.Elem())
			}
		}
		return nil
	case reflect.Struct:
		if t0, ok := rv.Interface().(time.Time); ok {
			_ = t0
			return nil
		}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Type().Field(i)
			if f.PkgPath != "" {
				continue
			}
			key := fieldKey(f)
			if key == "-" {
				continue
			}
			raw, ok := t[key]
			if !ok {
				continue
			}
			field := rv.Field(i)
			if sub, isTree := raw.(tree); isTree {
				// nested table, e.g. [config] or [manifest_cache]
				if field.Kind() == reflect.Ptr {
					if field.IsNil() {
						field.Set(reflect.New(field.Type().Elem()))
					}
					if err := decodeInto(sub, field.Elem(), join(path, key), unknown); err != nil {
						return err
					}
					continue
				}
				if field.Kind() == reflect.Struct || field.Kind() == reflect.Map {
					if err := decodeInto(sub, field, join(path, key), unknown); err != nil {
						return err
					}
					continue
				}
				*unknown = append(*unknown, join(path, key))
				continue
			}
			if err := setScalar(field, raw); err != nil {
				return fmt.Errorf("%s: %v", join(path, key), err)
			}
		}
		// keys the struct does not know
		for k := range t {
			found := false
			for i := 0; i < rv.NumField(); i++ {
				f := rv.Type().Field(i)
				if f.PkgPath == "" && fieldKey(f) == k {
					found = true
					break
				}
			}
			if !found {
				*unknown = append(*unknown, join(path, k))
			}
		}
		return nil
	case reflect.Ptr:
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		sub, ok := t[""].(tree)
		if !ok {
			return nil
		}
		return decodeInto(sub, rv.Elem(), path, unknown)
	default:
		return fmt.Errorf("toml: cannot decode into %s", rv.Kind())
	}
}

func setScalar(field reflect.Value, raw interface{}) error {
	switch field.Kind() {
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setScalar(field.Elem(), raw)
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return fmt.Errorf("expected a string")
		}
		field.SetString(s)
		return nil
	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("expected a boolean")
		}
		field.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch n := raw.(type) {
		case int64:
			field.SetInt(n)
		case float64:
			field.SetInt(int64(n))
		default:
			return fmt.Errorf("expected an integer")
		}
		return nil
	case reflect.Float32, reflect.Float64:
		switch n := raw.(type) {
		case int64:
			field.SetFloat(float64(n))
		case float64:
			field.SetFloat(n)
		default:
			return fmt.Errorf("expected a number")
		}
		return nil
	case reflect.Slice:
		list, ok := raw.([]interface{})
		if !ok {
			return fmt.Errorf("expected an array")
		}
		if field.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("only string arrays are supported")
		}
		out := reflect.MakeSlice(field.Type(), 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("expected a string array")
			}
			out = reflect.Append(out, reflect.ValueOf(s))
		}
		field.Set(out)
		return nil
	case reflect.Struct:
		if field.Type() == reflect.TypeOf(time.Time{}) {
			s, ok := raw.(string)
			if !ok {
				return fmt.Errorf("expected an RFC3339 time string")
			}
			if strings.TrimSpace(s) == "" {
				field.Set(reflect.ValueOf(time.Time{}))
				return nil
			}
			ts, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return fmt.Errorf("bad time %q: %v", s, err)
			}
			field.Set(reflect.ValueOf(ts))
			return nil
		}
		return fmt.Errorf("cannot assign a scalar to a struct")
	default:
		return fmt.Errorf("unsupported field kind %s", field.Kind())
	}
}

func coerceScalar(raw interface{}, typ reflect.Type) (reflect.Value, error) {
	v := reflect.New(typ).Elem()
	if err := setScalar(v, raw); err != nil {
		return reflect.Value{}, err
	}
	return v, nil
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// ---------------------------------------------------------------------------
// editing one key while keeping the rest of the file, comments included

// FormatValue renders a Go scalar the way it would be written into a file.
func FormatValue(v interface{}) (string, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return "", fmt.Errorf("toml: invalid value")
	}
	text, ok, err := scalarText(rv)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("toml: unsupported value type %s", rv.Kind())
	}
	return text, nil
}

// SetKeyInSection replaces `key = …` inside [section] without touching anything
// else in the document (comments, ordering, other tables). It reports false when
// the section or the key is not present, so the caller can fall back to a full
// rewrite.
func SetKeyInSection(data []byte, section, key string, value interface{}) ([]byte, bool, error) {
	text, err := FormatValue(value)
	if err != nil {
		return nil, false, err
	}
	lines := splitLines(string(data))
	inSection := false
	found := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(stripComment(raw))
		if strings.HasPrefix(trimmed, "[") {
			if inSection && found {
				break
			}
			path, err := splitKey(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")))
			if err != nil {
				continue
			}
			inSection = len(path) == 1 && path[0] == section
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}
		eq := indexOutsideQuotes(trimmed, '=')
		if eq < 0 {
			continue
		}
		k, err := splitKey(strings.TrimSpace(trimmed[:eq]))
		if err != nil || len(k) != 1 || k[0] != key {
			continue
		}
		// keep the original indentation and any trailing comment
		indent := raw[:len(raw)-len(strings.TrimLeft(raw, " 	"))]
		comment := ""
		if ci := commentIndex(raw); ci >= 0 {
			comment = " " + strings.TrimSpace(raw[ci:])
		}
		lines[i] = indent + quoteKey(key) + " = " + text + comment
		found = true
	}
	if !found {
		return nil, false, nil
	}
	return []byte(strings.Join(lines, "\n")), true, nil
}

// GetKeyInSection reads a raw scalar out of a section, mainly for tests.
func GetKeyInSection(data []byte, section, key string) (string, bool, error) {
	t, err := parseTree(string(data))
	if err != nil {
		return "", false, err
	}
	sec, ok := t[section].(tree)
	if !ok {
		return "", false, nil
	}
	raw, ok := sec[key]
	if !ok {
		return "", false, nil
	}
	return fmt.Sprint(raw), true, nil
}
