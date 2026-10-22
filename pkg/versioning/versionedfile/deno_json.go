package versionedfile

import (
	"fmt"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codeDenoJSONParse     = "remeris::versioned_file::deno_json::parse"
	codeDenoJSONSerialize = "remeris::versioned_file::deno_json::serialize"

	denoBOMPrefix = "\xef\xbb\xbf"
)

// denoJSONFile is a deno.json or deno.jsonc (F6.2). It is an immutable value that keeps the text of the file. Unlike
// Knope, which prints the parsed tree again and loses the comments (Quirk Q6), an edit replaces only the bytes of the
// top level "version" value, so comments, trailing commas, key order, indentation, line endings, a byte order mark and
// the final newline stay as they were.
type denoJSONFile struct {
	path string
	raw  string
	// version is nil when the file has no usable version: a missing key, a value that is not a string or not a version.
	version *semver.Version
	diff    Diff
}

// denoDoc is a parsed file: the root value and the length of the byte order mark that hujson did not see.
type denoDoc struct {
	root hujson.Value
	obj  *hujson.Object // nil if the root is not an object
	bom  int
}

func parseDenoJSON(path, content string) (denoDoc, error) {
	var doc denoDoc
	if strings.HasPrefix(content, denoBOMPrefix) {
		doc.bom = len(denoBOMPrefix)
	}
	if err := checkDenoDepth(content[doc.bom:]); err != nil {
		return denoDoc{}, diag.New("Could not parse " + path + ": " + err.Error()).
			WithCode(codeDenoJSONParse).WithHelp("Make sure the file is valid JSON")
	}
	root, err := hujson.Parse([]byte(content[doc.bom:]))
	if err != nil {
		return denoDoc{}, diag.New("Could not parse " + path + ": " + err.Error()).
			WithCode(codeDenoJSONParse).WithHelp("Make sure the file is valid JSON")
	}
	doc.root = root
	doc.obj, _ = root.Value.(*hujson.Object)
	return doc, nil
}

// maxDenoDepth is the deepest nesting of objects and arrays that is parsed. hujson recurses without a limit, and a
// few megabytes of "[" would overflow the stack.
const maxDenoDepth = 10000

// checkDenoDepth fails if brackets nest deeper than maxDenoDepth. Brackets in strings and comments do not count.
// It does not check the syntax: hujson does.
func checkDenoDepth(s string) error {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '{':
			depth++
			if depth > maxDenoDepth {
				return fmt.Errorf("nesting is deeper than %d levels", maxDenoDepth)
			}
		case ']', '}':
			depth--
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '/':
			switch {
			case strings.HasPrefix(s[i:], "//"):
				if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
					i += j
				} else {
					i = len(s)
				}
			case strings.HasPrefix(s[i:], "/*"):
				if j := strings.Index(s[i+2:], "*/"); j >= 0 {
					i += 2 + j + 1
				} else {
					i = len(s)
				}
			}
		}
	}
	return nil
}

// lastVersion returns the index of the last top level "version" member, as the JSON tree of Knope keeps the last of
// duplicated keys, or -1.
func (d denoDoc) lastVersion() int {
	if d.obj == nil {
		return -1
	}
	for i := len(d.obj.Members) - 1; i >= 0; i-- {
		name, ok := d.obj.Members[i].Name.Value.(hujson.Literal)
		if ok && name.Kind() == '"' && name.String() == keyVersion {
			return i
		}
	}
	return -1
}

// version reads the top level version; nil if there is none.
func (d denoDoc) version() *semver.Version {
	i := d.lastVersion()
	if i < 0 {
		return nil
	}
	lit, ok := d.obj.Members[i].Value.Value.(hujson.Literal)
	if !ok || lit.Kind() != '"' {
		return nil
	}
	v, err := semver.Parse(lit.String())
	if err != nil {
		return nil
	}
	return &v
}

func newDenoJSONFile(path, content string) (File, error) {
	doc, err := parseDenoJSON(path, content)
	if err != nil {
		return nil, err
	}
	return denoJSONFile{path: path, raw: content, version: doc.version()}, nil
}

func (f denoJSONFile) Path() string { return f.path }

// Version returns the version read when the file was parsed, or the one SetVersion set. A file without a usable
// version (Knope's get_version is None) gives ErrNoVersion.
func (f denoJSONFile) Version() (semver.Version, error) {
	if f.version == nil {
		return semver.Version{}, ErrNoVersion
	}
	return *f.version, nil
}

// SetVersion sets the top level version. A dependency is ignored, with no diff: deno.lock owns dependency versions.
// The diff is "version = {v}", joined with ", " over successive calls. The version member is added at the end of the
// object if it is missing (the file cannot be released then, see F7.2).
func (f denoJSONFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency != nil {
		return f, nil
	}
	doc, err := parseDenoJSON(f.path, f.raw)
	if err != nil {
		return nil, err
	}
	if doc.obj == nil {
		return nil, diag.New("Could not update " + f.path + ": the top level value is not a JSON object").
			WithCode(codeDenoJSONSerialize).WithHelp("Make sure the file is a JSON object")
	}
	literal := string(hujson.String(v.String()))
	out := f
	switch i := doc.lastVersion(); {
	case i < 0:
		out.raw = insertDenoVersion(f.raw, doc, literal)
	default:
		value := doc.obj.Members[i].Value
		start, end := value.StartOffset+doc.bom, value.EndOffset+doc.bom
		// An equal value is left as it is, whatever escapes it is written with.
		if cur := doc.version(); cur == nil || cur.String() != v.String() {
			out.raw = f.raw[:start] + literal + f.raw[end:]
		}
	}
	out.version = &v
	out.diff = f.diff.Add("version = " + v.String())
	return out, nil
}

// insertDenoVersion adds `"version": literal` as the last member of the root object. The new member takes the
// indentation of the previous one, or a single space if that one is not at the start of a line, and a trailing comma
// is kept if the object had one. A comment that ends the previous member's line stays with that member.
func insertDenoVersion(raw string, doc denoDoc, literal string) string {
	member := `"` + keyVersion + `": ` + literal
	members := doc.obj.Members
	if len(members) == 0 {
		at := doc.root.StartOffset + doc.bom + len("{")
		return raw[:at] + member + raw[at:]
	}
	last := members[len(members)-1]
	sep := memberSeparator(raw, last.Name.StartOffset+doc.bom)
	valueEnd := last.Value.EndOffset + doc.bom
	if last.Value.AfterExtra != nil { // trailing comma
		after := valueEnd + len(last.Value.AfterExtra) + len(",")
		at := endOfLineComment(raw, after)
		return raw[:at] + commentSeparator(raw, at, after, sep) + member + "," + raw[at:]
	}
	at := endOfLineComment(raw, valueEnd)
	return raw[:valueEnd] + "," + raw[valueEnd:at] + commentSeparator(raw, at, valueEnd, sep) + member + raw[at:]
}

// memberSeparator is what goes before a new member that follows the one whose name starts at nameStart: a line break
// (as the file writes it) and that member's indentation, or one space if it is not the first thing on its line.
func memberSeparator(raw string, nameStart int) string {
	nl := strings.LastIndexByte(raw[:nameStart], '\n')
	if nl < 0 || strings.Trim(raw[nl+1:nameStart], " \t") != "" {
		return " "
	}
	eol := "\n"
	if nl > 0 && raw[nl-1] == '\r' {
		eol = "\r\n"
	}
	return eol + raw[nl+1:nameStart]
}

// commentSeparator is sep, unless a line comment was skipped (at > from) and sep keeps the new member on that line,
// where it would end up inside the comment: then the member goes on its own line.
func commentSeparator(raw string, at, from int, sep string) string {
	if at == from || sep != " " {
		return sep
	}
	if strings.HasPrefix(raw[at:], "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// endOfLineComment returns the offset after the spaces and the // comment that follow from on the same line, or from
// itself if there is no such comment. Like hujson, the comment ends only at a line feed. The line break (CRLF or LF)
// is not included; a lone CR belongs to the comment.
func endOfLineComment(raw string, from int) int {
	i := from
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	if !strings.HasPrefix(raw[i:], "//") {
		return from
	}
	j := strings.IndexByte(raw[i:], '\n')
	if j < 0 {
		return len(raw)
	}
	end := i + j
	if end > i+len("//") && raw[end-1] == '\r' {
		end--
	}
	return end
}

// Write returns the write action, or nil if nothing was edited.
func (f denoJSONFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}
