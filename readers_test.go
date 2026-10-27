package factkit_test

import (
	"errors"
	"path/filepath"
	"testing"

	factkit "github.com/carlos-space/fact-kit-go"
)

// c009: the Go reader reads the facts about itself, which fact-kit-facts ships in this repository. They name the
// error codes, and the codes must be the ones this package returns. The package is found through
// self-reference, because facts/package.json is named fact-kit-facts.
func TestReadsItsOwnErrorCodes(t *testing.T) {
	root := filepath.Join(filepath.Dir(conformanceDir(t)), "facts")
	reader, err := factkit.ReadFacts("fact-kit-facts/fact-kit-go", factkit.ReadFactsOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	value, err := reader.Value("fact-kit-go.read-errors")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range value.(map[string]any)["codes"].([]any) {
		got = append(got, item.(map[string]any)["code"].(string))
	}
	want := []string{
		string(factkit.CodeNotAnIndex),
		string(factkit.CodeUnsupportedIndexFormat),
		string(factkit.CodeNotFound),
		string(factkit.CodeInvalidVersion),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("code %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if !reader.Has("fact-kit-go.read-errors") || reader.Package != "fact-kit-facts" {
		t.Errorf("package %q", reader.Package)
	}
}

func TestErrorsAs(t *testing.T) {
	_, err := factkit.ReadFacts("nobody-facts", factkit.ReadFactsOptions{Root: t.TempDir()})
	var e *factkit.FactReadError
	if !errors.As(err, &e) || e.Code != factkit.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}
