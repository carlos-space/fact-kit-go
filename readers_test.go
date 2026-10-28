package factkit_test

import (
	"errors"
	"os"
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

// publish comes back as the index says, and no version is checked.
func TestReadFactsReturnsPublish(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "node_modules", "demo-facts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := `{"name":"demo-facts","version":"1.0.0","exports":{"./index.json":"./index.json"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(publish string) {
		index := `{"indexFormat":"1.2","package":"demo-facts","entry":"","describes":{"demo-component":"0.50.0"}` + publish + `,"facts":[]}`
		if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(index), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`,"publish":{"target":"npm","name":"demo"}`)
	reader, err := factkit.ReadFacts("demo-facts", factkit.ReadFactsOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if reader.Publish == nil || reader.Publish.Target != "npm" || reader.Publish.Name != "demo" {
		t.Errorf("publish %+v", reader.Publish)
	}
	write("")
	reader, err = factkit.ReadFacts("demo-facts", factkit.ReadFactsOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if reader.Publish != nil {
		t.Errorf("publish %+v, want nil", reader.Publish)
	}
}
