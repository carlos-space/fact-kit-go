package factkit

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/tidwall/gjson"
)

// object maps the keys of a JSON object to their values. A key that appears twice keeps its last value, which is
// what JSON.parse in JS does.
type object map[string]gjson.Result

func objectOf(r gjson.Result) object {
	out := object{}
	if !r.IsObject() {
		return out
	}
	r.ForEach(func(k, v gjson.Result) bool {
		out[k.String()] = v
		return true
	})
	return out
}

type pair struct {
	key   string
	value gjson.Result
}

// orderedPairs lists the members of a JSON object in the order of their first appearance. A key that appears
// twice keeps its last value at the position of its first appearance, as JSON.parse in JS does. The order matters
// for the conditions of package.json exports.
func orderedPairs(r gjson.Result) []pair {
	var out []pair
	at := map[string]int{}
	if !r.IsObject() {
		return out
	}
	r.ForEach(func(k, v gjson.Result) bool {
		key := k.String()
		if i, ok := at[key]; ok {
			out[i].value = v
		} else {
			at[key] = len(out)
			out = append(out, pair{key, v})
		}
		return true
	})
	return out
}

// present reports whether a field is there and is not null.
func present(r gjson.Result) bool {
	return r.Exists() && r.Type != gjson.Null
}

// compareUTF16 orders strings by UTF-16 code units, as JS compares strings.
func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}

// jsonString writes a string the way JSON.stringify does.
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteString(strconv.FormatInt(int64(r)+0x100, 16)[1:])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// canonical writes a decoded JSON value with its keys sorted and no spaces. Two rows that no reader can tell
// apart are ordered by this text.
func canonical(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if x == float64(int64(x)) && x > -1e21 && x < 1e21 {
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return jsonString(x)
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		return canonicalEntries(x)
	}
	return "null"
}

func canonicalEntries(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compareUTF16)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = jsonString(k) + ":" + canonical(m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func parseTop(data []byte) gjson.Result {
	return gjson.ParseBytes(data)
}
