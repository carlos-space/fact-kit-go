// Package factkit reads the facts that a fact-kit package ships in its built index.json.
package factkit

import "fmt"

// Code is the stable code of a FactReadError. The message is for people and can change.
type Code string

// The codes a reader returns. They are the same strings as the codes of the JS reader.
const (
	CodeNotAnIndex             Code = "not-an-index"
	CodeUnsupportedIndexFormat Code = "unsupported-index-format"
	CodeNotFound               Code = "not-found"
	CodeInvalidVersion         Code = "invalid-version"
)

// FactReadError is the error ReadFacts and SelectFacts return for a problem with the index, the package or the
// version. Get the code with errors.As(err, &e) and then e.Code.
type FactReadError struct {
	Code    Code
	Message string
}

func (e *FactReadError) Error() string {
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

func newError(code Code, format string, args ...any) *FactReadError {
	return &FactReadError{Code: code, Message: fmt.Sprintf(format, args...)}
}
