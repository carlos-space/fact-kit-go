package factkit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/gjson"
)

// ReadFactsOptions says where to look and what the reader sees. See SelectOptions for the last four fields.
type ReadFactsOptions struct {
	// Root is the directory the package is resolved from, normally the hub being built.
	// Empty means the current working directory.
	Root     string
	Version  *string
	Include  []string
	Platform string
	OS       string
}

// Publish says where the described component is published and its name there.
type Publish struct {
	// Target is npm, pypi, crates, go or swift.
	Target string
	// Name is the name of the package on that target.
	Name string
}

// FactReader holds the facts one reader sees from an installed fact package.
//
// Unlike the JS reader, it does not compare describes with an installed product, and it has no onRead hook.
type FactReader struct {
	// Package is the name of the fact package, from the index.
	Package string
	// Version is the version of the installed fact package, from its package.json.
	Version string
	// Entry is the sub-path of the entry the index describes.
	Entry string
	// Describes names the component this entry describes and the version it covers.
	Describes map[string]string
	// Publish is where the component is published, or nil when the index has no publish. It checks no version.
	Publish *Publish
	// Facts are the facts this reader sees, each flattened to the row that applies.
	Facts []SelectedFact

	byID map[string]int
}

// Has reports whether a fact with this id exists.
func (r *FactReader) Has(id string) bool {
	_, ok := r.byID[id]
	return ok
}

// Get returns the fact with this id. It returns an error when there is none, so a typo fails the build.
func (r *FactReader) Get(id string) (SelectedFact, error) {
	i, ok := r.byID[id]
	if !ok {
		return SelectedFact{}, fmt.Errorf("factkit: no fact %q in %s", id, r.Package)
	}
	return r.Facts[i], nil
}

// Value returns the "value" of the content of the fact with this id. It returns an error when there is no such
// fact or its content has no value.
func (r *FactReader) Value(id string) (any, error) {
	fact, err := r.Get(id)
	if err != nil {
		return nil, err
	}
	if content, ok := fact.Content.(map[string]any); ok {
		if v, ok := content["value"]; ok {
			return v, nil
		}
	}
	return nil, fmt.Errorf("factkit: fact %q in %s has no value content", id, r.Package)
}

// ReadFacts reads the built index.json of "<package>" or "<package>/<path>" as installed under opts.Root.
// The package must export "./index.json" or "./<path>/index.json". The index may be format 1.0 or 1.1.
// Errors are *FactReadError, use errors.As.
func ReadFacts(spec string, opts ReadFactsOptions) (*FactReader, error) {
	root := opts.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	packageName, entry, err := splitSpec(spec)
	if err != nil {
		return nil, err
	}
	file, err := resolveIndexFile(root, packageName, entry)
	if err != nil {
		return nil, newError(CodeNotFound,
			"ReadFacts: cannot resolve %q from %s. Install %s there and check that it exports its index.json",
			spec, root, packageName)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, newError(CodeNotFound, "ReadFacts: cannot read %s: %v", file, err)
	}
	label := "ReadFacts: " + file
	facts, pkg, err := parseIndex(data, label)
	if err != nil {
		return nil, err
	}
	selected, err := selectParsed(facts, SelectOptions{
		Version: opts.Version, Include: opts.Include, Platform: opts.Platform, OS: opts.OS,
	})
	if err != nil {
		return nil, err
	}
	top := objectOf(parseTop(data))
	describes := map[string]string{}
	for k, v := range objectOf(top["describes"]) {
		describes[k] = v.String()
	}
	var publish *Publish
	if p := objectOf(top["publish"]); p["target"].Type == gjson.String && p["name"].Type == gjson.String {
		publish = &Publish{Target: p["target"].Str, Name: p["name"].Str}
	}
	entryName := entry
	if e := top["entry"]; e.Type == gjson.String {
		entryName = e.Str
	}
	version := ""
	if real, err := filepath.EvalSymlinks(file); err == nil {
		version = owningVersion(real, pkg)
	}
	byID := make(map[string]int, len(selected))
	for i, f := range selected {
		byID[f.ID] = i
	}
	return &FactReader{
		Package: pkg, Version: version, Entry: entryName, Describes: describes, Publish: publish, Facts: selected, byID: byID,
	}, nil
}
