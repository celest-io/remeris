package denosemver

// ParseError is a parse error shaped like the thiserror types of deno_semver: Error returns the Display text of
// the type ("Invalid version", "Invalid npm version", ...) and Unwrap returns its source, if any, which is a
// *Failure for the grammar errors. Use errors.As with *Failure to reach the message of the grammar.
type ParseError struct {
	msg string
	src error
}

// Error returns the Display text of the Rust error type.
func (e *ParseError) Error() string { return e.msg }

// Unwrap returns the source error: a *Failure, or nil.
func (e *ParseError) Unwrap() error { return e.src }

// The errors a package name can fail with. They are compared with errors.Is.
var (
	// ErrNoPackageName is PackageReqPartsParseError::NoPackageName.
	ErrNoPackageName = &ParseError{msg: "Did not contain a package name"}
	// ErrInvalidPackageName is PackageReqPartsParseError::InvalidPackageName: a scope without a name.
	ErrInvalidPackageName = &ParseError{msg: "Did not contain a valid package name"}
	// ErrMissingAtSymbol is PackageReqPartsParseError::MissingAtSymbol: "scope/name" without the '@'.
	ErrMissingAtSymbol = &ParseError{msg: "Packages in the format <scope>/<name> must start with an '@' symbol"}
)

// PackageReqParseError is the error of the PackageReq and JsrDepPackageReq parsers. Its Source is one of the
// package name errors above, or a *ParseError for the version requirement.
type PackageReqParseError struct {
	// Text is the input that failed.
	Text string
	// Source says why.
	Source error
}

// Error returns "Invalid package requirement '{text}'".
func (e *PackageReqParseError) Error() string {
	return "Invalid package requirement " + quote(e.Text)
}

// Unwrap returns Source.
func (e *PackageReqParseError) Unwrap() error { return e.Source }

// PackageNvParseError is the error of ParsePackageNv.
type PackageNvParseError struct {
	// Message is the Display of the parser failure, code snippet included.
	Message string
	// Text is the input that failed.
	Text string
}

// Error returns "Invalid package name and version '{text}'. {message}".
func (e *PackageNvParseError) Error() string {
	return "Invalid package name and version " + quote(e.Text) + ". " + e.Message
}

// NotExpectedSchemeError is JsrDepPackageReqParseError::NotExpectedScheme.
type NotExpectedSchemeError struct {
	// Scheme is the text before the first colon, or the whole input without a colon.
	Scheme string
}

// Error returns "Unexpected JSR dependency scheme '{scheme}'. Expected 'npm:' or 'jsr:'".
func (e *NotExpectedSchemeError) Error() string {
	return "Unexpected JSR dependency scheme " + quote(e.Scheme) + ". Expected 'npm:' or 'jsr:'"
}

// quote wraps s in single quotes, like the '{}' of the Rust format strings (no escaping).
func quote(s string) string { return "'" + s + "'" }
