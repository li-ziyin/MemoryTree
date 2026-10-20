// Package validate implements the MUST-level checks of MemoryTree format v1
// and emits stable error codes.
package validate

import (
	"fmt"
	"sort"
)

// Severity distinguishes blocking validation errors from advisory warnings.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Stable error codes. Codes V-* are defined by the format specification;
// V-SCHEMA-1 and W-* are supplementary implementation codes and do not change
// the meaning of the specified codes.
const (
	CodeRoot              = "V-ROOT-1"
	CodeParent            = "V-PARENT-1"
	CodeKindChild         = "V-KIND-1"
	CodeKindEntries       = "V-KIND-2"
	CodeKindGlobal        = "V-KIND-3"
	CodeSlug              = "V-SLUG-1"
	CodeIDFormat          = "V-ID-1"
	CodeIDReference       = "V-ID-2"
	CodeFile              = "V-FILE-1"
	CodeTagSyntax         = "V-TAG-1"
	CodeTagDelta          = "V-TAG-2"
	CodeSupersedeSelf     = "V-SUP-1"
	CodeSupersedePair     = "V-SUP-2"
	CodeSupersededBy      = "V-SUP-3"
	CodeSupersedeStatus   = "V-SUP-4"
	CodeSupersededFinal   = "V-SUP-5"
	CodeSensitivity       = "V-SENS-1"
	CodeTime              = "V-TIME-1"
	CodeTimeFilename      = "V-TIME-2"
	CodeContent           = "V-CONTENT-1"
	CodeSource            = "V-CONTENT-2"
	CodeBodySize          = "V-CONTENT-3"
	CodeSchema            = "V-SCHEMA-1"
	CodeFormatUnsupported = "FORMAT_UNSUPPORTED"

	CodeWarnDepth   = "W-DEPTH"
	CodeWarnBranch  = "W-BRANCH"
	CodeWarnSummary = "W-SUMMARY"
	CodeWarnDate    = "W-DATE"
)

// Issue is a single validation finding.
type Issue struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
}

// Result is the outcome of validating one repository.
type Result struct {
	Root    string  `json:"root"`
	Nodes   int     `json:"nodes"`
	Entries int     `json:"entries"`
	Issues  []Issue `json:"issues"`
}

// Errors returns the number of blocking issues.
func (r *Result) Errors() int {
	n := 0
	for _, is := range r.Issues {
		if is.Severity == SeverityError {
			n++
		}
	}
	return n
}

// Warnings returns the number of advisory issues.
func (r *Result) Warnings() int {
	return len(r.Issues) - r.Errors()
}

// OK reports whether the repository has no blocking issues.
func (r *Result) OK() bool {
	return r.Errors() == 0
}

func (r *Result) add(sev Severity, code, path, format string, args ...any) {
	r.Issues = append(r.Issues, Issue{
		Code:     code,
		Severity: sev,
		Path:     path,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (r *Result) errf(code, path, format string, args ...any) {
	r.add(SeverityError, code, path, format, args...)
}

func (r *Result) warnf(code, path, format string, args ...any) {
	r.add(SeverityWarning, code, path, format, args...)
}

func (r *Result) sortIssues() {
	sort.SliceStable(r.Issues, func(i, j int) bool {
		a, b := r.Issues[i], r.Issues[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Severity != b.Severity {
			return a.Severity == SeverityError
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}
