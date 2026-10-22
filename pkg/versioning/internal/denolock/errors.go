package denolock

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrorKind says which variant of deno_lockfile's DeserializationError an error is.
type ErrorKind int

// The variants that LockfileContent::from_json can return. InvalidPackageSpecifierVersion exists in the Rust enum
// but no code of version 0.49.0 builds it, so it is not here.
const (
	// FailedDeserializing is "Invalid {section} section: {serde error}".
	FailedDeserializing ErrorKind = iota
	// InvalidNpmPackageID is "Invalid npm package '{id}'".
	InvalidNpmPackageID
	// InvalidNpmPackageDependency is "Invalid npm package dependency '{dep}'".
	InvalidNpmPackageDependency
	// InvalidPackageSpecifier is the error of a "specifiers" key that is not a package requirement. Its text is the
	// text of the denosemver error (the Rust variant is transparent).
	InvalidPackageSpecifier
	// InvalidJSRDependency is "Invalid jsr dependency '{dep}' for '{package}'".
	InvalidJSRDependency
	// MissingPackage is "npm package '{name}' was not found and could not have its version resolved".
	MissingPackage
)

// DeserializationError is the error of FromJSON and Parse. Error returns the Display text of the Rust error, which is
// what Knope would print; the callers add their own "Failed deserializing" prefix, if any.
type DeserializationError struct {
	Kind ErrorKind
	msg  string
	// cause is the denosemver error of InvalidPackageSpecifier.
	cause error
}

// Error returns the Display text of the Rust error.
func (e *DeserializationError) Error() string { return e.msg }

// Unwrap returns the denosemver error of an InvalidPackageSpecifier, and nil for the other kinds.
func (e *DeserializationError) Unwrap() error { return e.cause }

func failedDeserializing(section string, err error) *DeserializationError {
	return &DeserializationError{Kind: FailedDeserializing, msg: "Invalid " + section + " section: " + err.Error()}
}

// serdeError is a message of serde (the deserialisation errors of the derives and of serde_json::Value).
type serdeError string

func (e serdeError) Error() string { return string(e) }

func serdef(format string, args ...any) error { return serdeError(fmt.Sprintf(format, args...)) }

// unexpected is serde's Display of the Unexpected value that serde_json::Value reports for v.
func unexpected(v Value) string {
	switch v.Kind {
	case KindNull:
		return "null" // serde_json's invalid_type prints a unit as "null"
	case KindBool:
		return "boolean `" + v.Str + "`"
	case KindNumber:
		if !isFloatText(v.Str) {
			return "integer `" + v.Str + "`"
		}
		return "floating point `" + withDecimalPoint(v.Str) + "`"
	case KindString:
		return "string " + rustDebugString(v.Str)
	case KindArray:
		return "sequence"
	default:
		return "map"
	}
}

// withDecimalPoint prints a float like serde's WithDecimalPoint: Rust's Display of the f64 (never in exponent
// form), plus ".0" when that has no decimal point.
func withDecimalPoint(text string) string {
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return text
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// rustDebugString quotes s like Rust's Debug for str.
func rustDebugString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0:
			b.WriteString(`\0`)
		default:
			if unicode.IsPrint(r) || r == ' ' {
				b.WriteRune(r)
			} else {
				fmt.Fprintf(&b, `\u{%x}`, r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// invalidType is serde's Error::invalid_type as serde_json formats it: "invalid type: {unexpected}, expected {what}".
func invalidType(v Value, expected string) error {
	return serdef("invalid type: %s, expected %s", unexpected(v), expected)
}
