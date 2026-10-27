package factkit

import (
	"regexp"
	"slices"

	"github.com/Masterminds/semver/v3"
	"github.com/tidwall/gjson"
)

// SelectOptions chooses what one reader sees.
type SelectOptions struct {
	// Version is the version of the described component the reader runs, or "head" for the unreleased state.
	// nil means the reader is newer than every released version, so it gets the latest published content.
	// A pointer, because an empty string is a version that is not valid, and not the same as no version.
	Version *string
	// Include lists the labels of pending rows that a reader on "head" also gets.
	Include []string
	// Platform applies a row with a platforms condition only when it lists this platform. Empty means none.
	Platform string
	// OS applies a row with an os condition only when it lists this os. Empty means none.
	OS string
}

// SelectedFact is a fact as one reader sees it, with the row that applies flattened into the fields.
type SelectedFact struct {
	ID string
	// Content is the content of the chosen row, decoded as encoding/json does into any. Numbers are float64.
	Content any
	// Since is the since of the chosen row as it was written: a version string, a []any of strings, or "next".
	// It is "0.0.0" when the chosen row has none and so always applies.
	Since any
	// Stability is "experimental" for an experimental row, else empty.
	Stability string
}

// NoSince is what Since reads as when a row does not have one.
const NoSince = "0.0.0"

type readerKind int

const (
	readerLatest readerKind = iota
	readerHead
	readerVersion
)

type reader struct {
	kind readerKind
	v    *semver.Version
}

type boundKind int

const (
	boundMin boundKind = iota
	boundVersion
	boundMax
)

// bound is the version a row starts at, seen from one reader: minus infinity, a version, or plus infinity.
type bound struct {
	kind boundKind
	v    *semver.Version
}

func compareBounds(a, b bound) int {
	if a.kind != b.kind {
		return sign(int(a.kind) - int(b.kind))
	}
	if a.kind == boundVersion {
		return a.v.Compare(b.v)
	}
	return 0
}

// row is one candidate content of a fact. A field that is not there does not Exist.
type row struct {
	since, content, pending, stability, platforms, os gjson.Result
}

func (r row) specificity() int {
	n := 0
	if present(r.platforms) {
		n++
	}
	if present(r.os) {
		n++
	}
	return n
}

func (r row) canonical() string {
	m := map[string]any{}
	for k, f := range map[string]gjson.Result{
		"since": r.since, "content": r.content, "pending": r.pending,
		"stability": r.stability, "platforms": r.platforms, "os": r.os,
	} {
		if f.Exists() {
			m[k] = f.Value()
		}
	}
	return canonicalEntries(m)
}

// rowsOf returns the rows of a fact. Without variants the whole fact is one row that starts at when.since.
func rowsOf(fact object) []row {
	if variants := fact["variants"]; variants.IsArray() {
		var rows []row
		for _, v := range variants.Array() {
			if !v.IsObject() {
				continue
			}
			o := objectOf(v)
			rows = append(rows, row{
				since: o["since"], content: o["content"], pending: o["pending"],
				stability: o["stability"], platforms: o["platforms"], os: o["os"],
			})
		}
		return rows
	}
	when := objectOf(fact["when"])
	return []row{{since: when["since"], content: fact["content"], platforms: when["platforms"], os: when["os"]}}
}

func isNext(r gjson.Result) bool {
	return r.Type == gjson.String && r.Str == "next"
}

func boundOf(since gjson.Result, rd reader, id string) (bound, bool, error) {
	what := "fact " + id + ": since"
	if !since.Exists() {
		return bound{kind: boundMin}, true, nil
	}
	switch {
	case isNext(since):
		if rd.kind == readerHead {
			return bound{kind: boundMax}, true, nil
		}
		return bound{}, false, nil
	case since.Type == gjson.String:
		s, err := mustParse(since.Str, what)
		if err != nil {
			return bound{}, false, err
		}
		if rd.kind == readerVersion && rd.v.Compare(s) < 0 {
			return bound{}, false, nil
		}
		return bound{kind: boundVersion, v: s}, true, nil
	case !since.IsArray():
		return bound{}, false, newError(CodeInvalidVersion, "%s must be a string or an array", what)
	}
	hasNext := false
	var versions []*semver.Version
	for _, item := range since.Array() {
		if isNext(item) {
			hasNext = true
			continue
		}
		if item.Type != gjson.String {
			return bound{}, false, newError(CodeInvalidVersion, "%s %s is not a semver 2.0.0 version", what, item.Raw)
		}
		v, err := mustParse(item.Str, what)
		if err != nil {
			return bound{}, false, err
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		if rd.kind == readerHead {
			return bound{kind: boundMax}, true, nil
		}
		return bound{}, false, nil
	}
	newest := versions[0]
	for _, v := range versions[1:] {
		if v.Compare(newest) > 0 {
			newest = v
		}
	}
	if rd.kind == readerVersion {
		if i := slices.IndexFunc(versions, func(s *semver.Version) bool { return compareLines(s, rd.v) == 0 }); i >= 0 {
			if rd.v.Compare(versions[i]) >= 0 {
				return bound{kind: boundVersion, v: versions[i]}, true, nil
			}
			return bound{}, false, nil
		}
		// No version on the reader's line: the row applies once the reader's line is newer than all of them.
		if slices.ContainsFunc(versions, func(s *semver.Version) bool { return compareLines(rd.v, s) <= 0 }) {
			return bound{}, false, nil
		}
		return bound{kind: boundVersion, v: newest}, true, nil
	}
	if rd.kind == readerHead && hasNext {
		return bound{kind: boundMax}, true, nil
	}
	return bound{kind: boundVersion, v: newest}, true, nil
}

func listHas(list gjson.Result, want string) bool {
	if !list.IsArray() {
		return false
	}
	return slices.ContainsFunc(list.Array(), func(item gjson.Result) bool {
		return item.Type == gjson.String && item.Str == want
	})
}

type candidate struct {
	row   row
	bound bound
}

// rank orders two candidates: the larger bound first, then the one with more conditions.
func rank(a, b candidate) int {
	if c := compareBounds(a.bound, b.bound); c != 0 {
		return c
	}
	return sign(a.row.specificity() - b.row.specificity())
}

// chooseRow returns the row that applies to the reader: the largest bound, then the more specific one, then the
// smaller canonical text, so the result does not depend on the order of the rows.
func chooseRow(rows []row, rd reader, opts SelectOptions, id string) (*row, error) {
	var best *candidate
	for _, r := range rows {
		if r.pending.Exists() &&
			!(rd.kind == readerHead && r.pending.Type == gjson.String && slices.Contains(opts.Include, r.pending.Str)) {
			continue
		}
		if present(r.platforms) && (opts.Platform == "" || !listHas(r.platforms, opts.Platform)) {
			continue
		}
		if present(r.os) && (opts.OS == "" || !listHas(r.os, opts.OS)) {
			continue
		}
		b, ok, err := boundOf(r.since, rd, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		c := candidate{r, b}
		if best == nil {
			best = &c
			continue
		}
		order := rank(c, *best)
		if order > 0 || (order == 0 && compareUTF16(c.row.canonical(), best.row.canonical()) < 0) {
			best = &c
		}
	}
	if best == nil {
		return nil, nil
	}
	return &best.row, nil
}

var formatPattern = regexp.MustCompile(`^(\d+)\.\d+$`)

// parseIndex checks the top of an index and returns its facts and package.
func parseIndex(data []byte, label string) ([]gjson.Result, string, error) {
	if !gjson.ValidBytes(data) {
		return nil, "", newError(CodeNotAnIndex, "%s is not JSON", label)
	}
	top := gjson.ParseBytes(data)
	if !top.IsObject() {
		return nil, "", newError(CodeNotAnIndex, "%s is not a fact index", label)
	}
	o := objectOf(top)
	// A missing field is format 1.0. Readers take any 1.x and ignore fields they do not know.
	format := o["indexFormat"]
	major := ""
	if !format.Exists() {
		major = "1"
	} else if format.Type == gjson.String {
		if m := formatPattern.FindStringSubmatch(format.Str); m != nil {
			major = m[1]
		}
	}
	if trimZeros(major) != "1" {
		return nil, "", newError(CodeUnsupportedIndexFormat,
			"%s has index format %s, and this reader takes 1.x", label, format.Raw)
	}
	if !o["facts"].IsArray() || o["package"].Type != gjson.String {
		return nil, "", newError(CodeNotAnIndex, "%s is not a fact index", label)
	}
	return o["facts"].Array(), o["package"].Str, nil
}

func trimZeros(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}

func readerOf(version *string) (reader, error) {
	switch {
	case version == nil:
		return reader{kind: readerLatest}, nil
	case *version == "head":
		return reader{kind: readerHead}, nil
	}
	v, err := mustParse(*version, "query version")
	if err != nil {
		return reader{}, err
	}
	return reader{kind: readerVersion, v: v}, nil
}

// newestReleased is the newest version any published row of the index names, which is how new a reader of the
// latest published state is.
func newestReleased(facts []gjson.Result) *semver.Version {
	var newest *semver.Version
	for _, f := range facts {
		if !f.IsObject() {
			continue
		}
		for _, r := range rowsOf(objectOf(f)) {
			if r.pending.Exists() || !r.since.Exists() {
				continue
			}
			items := []gjson.Result{r.since}
			if r.since.IsArray() {
				items = r.since.Array()
			}
			for _, item := range items {
				if item.Type != gjson.String || item.Str == "next" {
					continue
				}
				if v, ok := parseVersion(item.Str); ok && (newest == nil || v.Compare(newest) > 0) {
					newest = v
				}
			}
		}
	}
	return newest
}

func removed(fact object, rd reader, latest *semver.Version) bool {
	when := objectOf(fact["when"])
	at := when["removeIn"]
	if at.Type != gjson.String {
		return false
	}
	if rd.kind == readerHead {
		return true
	}
	if at.Str == "next" {
		return false
	}
	gone, ok := parseVersion(at.Str)
	if !ok {
		return false
	}
	current := latest
	if rd.kind == readerVersion {
		current = rd.v
	}
	return current != nil && current.Compare(gone) >= 0
}

// SelectFacts returns the facts one reader sees, from the bytes of an index.json of format 1.x.
//
// Each fact has rows (its variants, or the whole fact as one row). A row applies to the reader when its since
// has been reached, its pending label is included, and its platforms and os conditions list the reader's
// platform and os. Of the rows that apply, the one with the largest bound wins, and on a tie the one with more
// conditions. A fact without an applying row, or whose when.removeIn has been reached, is left out.
// The result keeps the order of the index. Errors are *FactReadError.
func SelectFacts(index []byte, opts SelectOptions) ([]SelectedFact, error) {
	facts, _, err := parseIndex(index, "the index")
	if err != nil {
		return nil, err
	}
	return selectParsed(facts, opts)
}

func selectParsed(facts []gjson.Result, opts SelectOptions) ([]SelectedFact, error) {
	rd, err := readerOf(opts.Version)
	if err != nil {
		return nil, err
	}
	var latest *semver.Version
	if rd.kind == readerLatest {
		latest = newestReleased(facts)
	}
	out := []SelectedFact{}
	for _, f := range facts {
		if !f.IsObject() {
			continue
		}
		fact := objectOf(f)
		id := fact["id"]
		if id.Type != gjson.String {
			continue
		}
		chosen, err := chooseRow(rowsOf(fact), rd, opts, id.Str)
		if err != nil {
			return nil, err
		}
		if chosen == nil || removed(fact, rd, latest) {
			continue
		}
		sel := SelectedFact{ID: id.Str, Content: chosen.content.Value(), Since: NoSince}
		if chosen.since.Exists() {
			sel.Since = chosen.since.Value()
		}
		if chosen.stability.Type == gjson.String {
			sel.Stability = chosen.stability.Str
		}
		out = append(out, sel)
	}
	return out, nil
}
