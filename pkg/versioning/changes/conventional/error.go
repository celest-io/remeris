package conventional

// ErrorKind says what is wrong with a message that is not a conventional commit. The kinds and their messages are
// those of git-conventional's ErrorKind.
type ErrorKind int

// The error kinds. [Parse] only returns [MissingType], [InvalidBody] and [InvalidFormat], like the crate: the
// others exist in the crate's API but its parser never selects them (see [Parse]).
const (
	// MissingType is the kind of every failure inside the summary line.
	MissingType ErrorKind = iota + 1
	// InvalidScope is a scope with an invalid format. It is never returned.
	InvalidScope
	// MissingDescription is a summary without a description. It is never returned.
	MissingDescription
	// InvalidBody is a body (or the line after the summary) with an invalid format.
	InvalidBody
	// InvalidFooter is a footer with an invalid format. It is never returned.
	InvalidFooter
	// InvalidFormat is any other part that does not conform.
	InvalidFormat
)

// String returns the crate's message for the kind.
func (k ErrorKind) String() string {
	switch k {
	case MissingType:
		return "Missing type in the commit summary, expected `type: description`"
	case InvalidScope:
		return "Incorrect scope syntax in commit summary, expected `type(scope): description`"
	case MissingDescription:
		return "Missing description in commit summary, expected `type: description`"
	case InvalidBody:
		return "Incorrect body syntax"
	case InvalidFooter:
		return "Incorrect footer syntax"
	case InvalidFormat:
		return "Incorrect conventional commit format"
	default:
		return "Unknown conventional commit error"
	}
}

// Error is returned by [Parse] for a message that is not a conventional commit.
type Error struct {
	// Kind is what went wrong.
	Kind ErrorKind
	// Commit is the message that was parsed.
	Commit string
}

// Error returns the message of the kind.
func (e *Error) Error() string { return e.Kind.String() }
