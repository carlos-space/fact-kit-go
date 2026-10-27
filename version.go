package factkit

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// parseVersion parses a full semver 2.0.0 version X.Y.Z. It rejects a leading v, 1.0 and 01.0.0.
// Build metadata is accepted and ignored by comparisons.
func parseVersion(text string) (*semver.Version, bool) {
	v, err := semver.StrictNewVersion(text)
	if err != nil {
		return nil, false
	}
	return v, true
}

func mustParse(text, what string) (*semver.Version, error) {
	v, ok := parseVersion(text)
	if !ok {
		return nil, newError(CodeInvalidVersion, "%s %q is not a semver 2.0.0 version", what, text)
	}
	return v, nil
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// compareLines compares only the release lines X.Y of two versions.
func compareLines(a, b *semver.Version) int {
	if c := sign(int(a.Major()) - int(b.Major())); c != 0 {
		return c
	}
	return sign(int(a.Minor()) - int(b.Minor()))
}

// CompareVersions compares two semver 2.0.0 versions and returns -1, 0 or 1.
// It returns a *FactReadError with CodeInvalidVersion when one of them is not a full X.Y.Z version.
func CompareVersions(a, b string) (int, error) {
	x, err := mustParse(a, "version")
	if err != nil {
		return 0, err
	}
	y, err := mustParse(b, "version")
	if err != nil {
		return 0, err
	}
	return x.Compare(y), nil
}

// ReleaseLine returns the release line X.Y of a version. 0.49.0-alpha.1 is on line 0.49.
func ReleaseLine(version string) (string, error) {
	v, err := mustParse(version, "version")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", v.Major(), v.Minor()), nil
}
