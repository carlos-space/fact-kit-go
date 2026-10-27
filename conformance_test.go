package factkit_test

// Runs the shared samples in conformance/ against this reader, through the public API only. The same files run
// in every other language, see conformance/README.md. The expected results were written by hand.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	factkit "github.com/carlos-space/fact-kit-go"
)

// conformanceDir finds conformance/ from FACT_KIT_CONFORMANCE, else by walking up from this file. The module zip
// carries this file but not conformance/, so `go test all` in a module that depends on this one skips instead of
// failing. CI sets FACT_KIT_CONFORMANCE, so a missing directory there fails rather than skips.
func conformanceDir(t testing.TB) string {
	t.Helper()
	if dir := os.Getenv("FACT_KIT_CONFORMANCE"); dir != "" {
		return dir
	}
	_, file, _, _ := runtime.Caller(0)
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "conformance")
		if info, err := os.Stat(filepath.Join(candidate, "semver.json")); err == nil && !info.IsDir() {
			return candidate
		}
		if filepath.Dir(dir) == dir {
			t.Skip("conformance/ not found: set FACT_KIT_CONFORMANCE or run the test inside the repository")
		}
	}
}

func readJSON(t testing.TB, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

func subdirs(t testing.TB, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}

// equal compares two decoded JSON values as JS does: numbers by value, a boolean never equal to a number,
// objects without regard to the order of keys.
func equal(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, equal)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, found := y[k]
			if !found || !equal(v, w) {
				return false
			}
		}
		return true
	}
	return false
}

func less16(a, b string) bool {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b))) < 0
}

// outcome is what a reader returned, in the shape of expected.json, or the code of the error.
func outcome(t *testing.T, facts []factkit.SelectedFact, err error) any {
	t.Helper()
	if err != nil {
		var e *factkit.FactReadError
		if !errors.As(err, &e) {
			t.Fatalf("not a *FactReadError: %v", err)
		}
		return map[string]any{"error": string(e.Code)}
	}
	sorted := slices.Clone(facts)
	slices.SortFunc(sorted, func(a, b factkit.SelectedFact) int {
		switch {
		case less16(a.ID, b.ID):
			return -1
		case less16(b.ID, a.ID):
			return 1
		}
		return 0
	})
	list := []any{}
	for _, f := range sorted {
		item := map[string]any{"id": f.ID, "content": f.Content, "since": f.Since}
		if f.Stability != "" {
			item["stability"] = f.Stability
		}
		list = append(list, item)
	}
	return map[string]any{"facts": list}
}

type query struct {
	Spec     string   `json:"spec"`
	Version  *string  `json:"version"`
	Include  []string `json:"include"`
	Platform string   `json:"platform"`
	OS       string   `json:"os"`
}

func readQuery(t testing.TB, path string) query {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var q query
	if err := json.Unmarshal(data, &q); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return q
}

func check(t *testing.T, got, want any) {
	t.Helper()
	if !equal(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Errorf("got  %s\nwant %s", g, w)
	}
}

func TestConformanceCases(t *testing.T) {
	root := filepath.Join(conformanceDir(t), "cases")
	names := subdirs(t, root)
	if len(names) < 80 {
		t.Fatalf("only %d cases in %s", len(names), root)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			q := readQuery(t, filepath.Join(dir, "query.json"))
			index, err := os.ReadFile(filepath.Join(dir, "index.json"))
			if err != nil {
				t.Fatal(err)
			}
			facts, err := factkit.SelectFacts(index, factkit.SelectOptions{
				Version: q.Version, Include: q.Include, Platform: q.Platform, OS: q.OS,
			})
			check(t, outcome(t, facts, err), readJSON(t, filepath.Join(dir, "expected.json")))
		})
	}
}

func TestConformanceResolve(t *testing.T) {
	root := filepath.Join(conformanceDir(t), "resolve")
	names := subdirs(t, root)
	if len(names) < 15 {
		t.Fatalf("only %d cases in %s", len(names), root)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			q := readQuery(t, filepath.Join(dir, "query.json"))
			reader, err := factkit.ReadFacts(q.Spec, factkit.ReadFactsOptions{
				Root: filepath.Join(dir, "root"), Version: q.Version, Include: q.Include, Platform: q.Platform, OS: q.OS,
			})
			var facts []factkit.SelectedFact
			if err == nil {
				facts = reader.Facts
			}
			check(t, outcome(t, facts, err), readJSON(t, filepath.Join(dir, "expected.json")))
		})
	}
}

func TestConformanceSemver(t *testing.T) {
	entries, ok := readJSON(t, filepath.Join(conformanceDir(t), "semver.json")).([]any)
	if !ok {
		t.Fatal("semver.json is not an array")
	}
	compares, lines := 0, 0
	for _, raw := range entries {
		e := raw.(map[string]any)
		if a, isCompare := e["a"].(string); isCompare {
			b := e["b"].(string)
			want := int(e["expected"].(float64))
			compares++
			t.Run(fmt.Sprintf("%s against %s is %d", a, b, want), func(t *testing.T) {
				got, err := factkit.CompareVersions(a, b)
				if err != nil || got != want {
					t.Errorf("got %d, %v, want %d", got, err, want)
				}
			})
			continue
		}
		version, line := e["version"].(string), e["line"].(string)
		lines++
		t.Run(fmt.Sprintf("%s is on line %s", version, line), func(t *testing.T) {
			got, err := factkit.ReleaseLine(version)
			if err != nil || got != line {
				t.Errorf("got %q, %v, want %q", got, err, line)
			}
		})
	}
	if compares <= 50 || lines <= 5 {
		t.Errorf("%d comparisons and %d release lines", compares, lines)
	}
}

func TestInvalidVersionsAreRejected(t *testing.T) {
	for _, v := range []string{"v1.0.0", "1.0", "01.0.0", "", "head"} {
		if _, err := factkit.CompareVersions(v, "1.0.0"); !strings.Contains(fmt.Sprint(err), "invalid-version") {
			t.Errorf("%q: %v", v, err)
		}
	}
}
