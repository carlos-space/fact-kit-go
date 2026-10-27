package factkit

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// The resolution follows the rules of Node's require: self-reference, the nearest node_modules going up,
// symbolic links, and the exports of package.json. NODE_PATH and the global folders are not looked at.

var (
	errInvalidTarget = errors.New("invalid package target")
	errHard          = errors.New("invalid package export")
	errNoFile        = errors.New("not found")
)

// conditions are the conditions that match for require.
var conditions = map[string]bool{"node-addons": true, "node": true, "require": true, "module-sync": true, "default": true}

// invalidSegment matches a path segment that is empty, ".", ".." or node_modules, also percent-encoded.
var invalidSegment = regexp.MustCompile(
	`(?i)(^|\\|/)((\.|%2e)(\.|%2e)?|(n|%6e)(o|%6f)(d|%64)(e|%65)(_|%5f)(m|%6d)(o|%6f)(d|%64)(u|%75)(l|%6c)(e|%65)(s|%73))?(\\|/|$)`)

type targetKind int

const (
	targetNone  targetKind = iota // nothing matched, a caller goes on with the next key
	targetNull                    // null, which stops the search with no result
	targetFound                   // a path, relative to the package directory and starting with "./"
)

func resolveTarget(t gjson.Result, subpath string, pattern bool) (string, targetKind, error) {
	switch {
	case t.Type == gjson.String:
		s := t.Str
		if !strings.HasPrefix(s, "./") || invalidSegment.MatchString(s[2:]) {
			return "", targetNone, errInvalidTarget
		}
		if subpath == "" {
			return s, targetFound, nil
		}
		if !pattern {
			return "", targetNone, errInvalidTarget
		}
		if invalidSegment.MatchString(subpath) {
			return "", targetNone, errHard
		}
		return strings.ReplaceAll(s, "*", subpath), targetFound, nil
	case t.IsArray():
		for _, item := range t.Array() {
			s, kind, err := resolveTarget(item, subpath, pattern)
			if errors.Is(err, errInvalidTarget) {
				continue
			}
			if err != nil {
				return "", targetNone, err
			}
			if kind == targetFound {
				return s, kind, nil
			}
		}
		return "", targetNone, nil
	case t.IsObject():
		for _, p := range orderedPairs(t) {
			if !conditions[p.key] {
				continue
			}
			s, kind, err := resolveTarget(p.value, subpath, pattern)
			if err != nil {
				return "", targetNone, err
			}
			if kind != targetNone {
				return s, kind, nil
			}
		}
		return "", targetNone, nil
	case t.Type == gjson.Null:
		return "", targetNull, nil
	}
	return "", targetNone, errInvalidTarget
}

// patternKeyCompare is Node's PATTERN_KEY_COMPARE. It returns 1 when b is the better match.
func patternKeyCompare(a, b string) int {
	ai, bi := strings.Index(a, "*"), strings.Index(b, "*")
	baseA, baseB := len(a), len(b)
	if ai != -1 {
		baseA = ai + 1
	}
	if bi != -1 {
		baseB = bi + 1
	}
	switch {
	case baseA > baseB:
		return -1
	case baseB > baseA:
		return 1
	case ai == -1:
		return 1
	case bi == -1:
		return -1
	case len(a) > len(b):
		return -1
	case len(b) > len(a):
		return 1
	}
	return 0
}

// resolveExports resolves the sub-path "./index.json" or "./<path>/index.json" through the exports field.
func resolveExports(pkgDir string, exports gjson.Result, sub string) (string, error) {
	pairs := orderedPairs(exports)
	// A string, an array, or an object of conditions stands for the "." export only, which is never asked for.
	if !exports.IsObject() || (len(pairs) > 0 && !strings.HasPrefix(pairs[0].key, ".")) {
		return "", errNoFile
	}
	var rel string
	kind := targetNone
	var err error
	exact := false
	for _, p := range pairs {
		if p.key == sub {
			exact = true
			rel, kind, err = resolveTarget(p.value, "", false)
		}
	}
	if !exact {
		best, bestSub := "", ""
		var bestValue gjson.Result
		for _, p := range pairs {
			i := strings.Index(p.key, "*")
			if i == -1 || !strings.HasPrefix(sub, p.key[:i]) {
				continue
			}
			trailer := p.key[i+1:]
			if len(sub) >= len(p.key) && strings.HasSuffix(sub, trailer) &&
				patternKeyCompare(best, p.key) == 1 && strings.LastIndex(p.key, "*") == i {
				best, bestValue = p.key, p.value
				bestSub = sub[i : len(sub)-len(trailer)]
			}
		}
		if best == "" {
			return "", errNoFile
		}
		rel, kind, err = resolveTarget(bestValue, bestSub, true)
	}
	if err != nil || kind != targetFound {
		return "", errNoFile
	}
	return existingFile(filepath.Join(pkgDir, filepath.FromSlash(rel[2:])))
}

func existingFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errNoFile
	}
	return path, nil
}

func readPackageJSON(dir string) (object, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, false, nil
	}
	if !gjson.ValidBytes(data) || !gjson.ParseBytes(data).IsObject() {
		return nil, true, errNoFile
	}
	return objectOf(gjson.ParseBytes(data)), true, nil
}

// resolveIndexFile finds the index.json of `<package>/<entry>` the way require.resolve does from root.
func resolveIndexFile(root, packageName, entry string) (string, error) {
	sub := "./index.json"
	if entry != "" {
		sub = "./" + entry + "/index.json"
	}
	// 1. self-reference: only the nearest package.json counts.
	for dir := root; ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "node_modules" {
			break
		}
		pkg, found, err := readPackageJSON(dir)
		if err != nil {
			return "", err
		}
		if found {
			if pkg["name"].Type == gjson.String && pkg["name"].Str == packageName && present(pkg["exports"]) {
				return resolveExports(dir, pkg["exports"], sub)
			}
			break
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	// 2. the nearest node_modules/<package> going up, which may be a symbolic link.
	for dir := root; ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) != "node_modules" {
			pkgDir := filepath.Join(dir, "node_modules", filepath.FromSlash(packageName))
			pkg, found, err := readPackageJSON(pkgDir)
			if err != nil {
				return "", err
			}
			if found {
				if present(pkg["exports"]) {
					return resolveExports(pkgDir, pkg["exports"], sub)
				}
				// 4. without exports the sub-path is a file path. When the file is not there, Node goes on
				// to the next node_modules up.
				if file, err := existingFile(filepath.Join(pkgDir, filepath.FromSlash(sub[2:]))); err == nil {
					return file, nil
				}
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", errNoFile
}

// splitSpec splits "@scope/name/path" or "name/path" into the package name and the path, which is empty for
// a spec that names only the package.
func splitSpec(spec string) (string, string, error) {
	parts := strings.Split(spec, "/")
	nameLength := 1
	if strings.HasPrefix(spec, "@") {
		nameLength = 2
	}
	if len(parts) < nameLength || slicesContainsEmpty(parts) {
		return "", "", newError(CodeNotFound, `ReadFacts: %q is not of the form "<package>" or "<package>/<path>"`, spec)
	}
	return strings.Join(parts[:nameLength], "/"), strings.Join(parts[nameLength:], "/"), nil
}

func slicesContainsEmpty(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			return true
		}
	}
	return false
}

// owningVersion is the version of the package called name that contains file, found by walking up from it.
func owningVersion(file, name string) string {
	for dir := filepath.Dir(file); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		pkg, found, err := readPackageJSON(dir)
		if err != nil || !found {
			continue
		}
		if pkg["name"].Type == gjson.String && pkg["name"].Str == name {
			return pkg["version"].Str
		}
	}
	return ""
}
