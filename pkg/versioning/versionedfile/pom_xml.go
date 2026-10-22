package versionedfile

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codeMavenXML             = "remeris::maven_pom::xml"
	codeMavenMissingProperty = "remeris::maven_pom::missing_property"
	codeMavenEncoding        = "remeris::maven_pom::encoding"

	mavenRoot       = "project"
	mavenVersion    = "version"
	mavenArtifactID = "artifactId"
	mavenProjectVer = "project.version"

	bomLen        = 3
	placeholder   = 'x'
	lineFeed      = "\n"
	carriageFeed  = "\r\n"
	asciiLimit    = 0x80
	declStart     = "<?xml"
	declEnd       = "?>"
	declEncoding  = "encoding"
	utf8Label     = "utf-8"
	unitIndent    = "  "
	unitIndentTab = "\t"
)

// utf8BOM and the UTF-16 byte order marks. A UTF-8 BOM is skipped and kept; UTF-16 is not supported.
var (
	utf8BOM     = []byte{0xEF, 0xBB, 0xBF}
	utf16BEBOM  = []byte{0xFE, 0xFF}
	utf16LEBOM  = []byte{0xFF, 0xFE}
	errNoRoot   = errors.New("no root element")
	errTwoRoots = errors.New("more than one root element")
	errOutside  = errors.New("unexpected characters outside the root element")
)

// singleByteLabels are the declared encodings whose bytes map one to one to characters, so byte offsets in the
// file stay valid when the bytes are read as they are. Labels are lower case.
var singleByteLabels = map[string]bool{
	"iso-8859-1": true, "iso8859-1": true, "iso_8859-1": true, "latin1": true, "latin-1": true, "l1": true,
	"us-ascii": true, "ascii": true,
}

// pomLayout is where things sit in the original bytes of a pom.xml. Every offset is into the file content.
type pomLayout struct {
	hasVersion  bool
	versionText []byte // the character data of project/version, as xmltree's get_text reads it
	hasText     bool   // some of it is not just white space
	inner       span   // the inner range of project/version; empty for a self-closing element
	selfClosing bool   // project/version is written <version/>
	versionTag  span   // the whole start tag of a self-closing project/version

	hasArtifact bool
	artifactTag int  // offset of the "<" of project/artifactId
	artifactEnd int  // offset just after project/artifactId
	closeStart  int  // offset of the "</project>" tag
	rootEmpty   bool // the root is written <project/>
	rootTag     span // the whole start tag of an empty root
	rootStart   int
}

type span struct{ start, end int }

// mavenPomFile is a pom.xml (F6.2). It is an immutable value. Unlike Knope, which writes the whole tree again,
// SetVersion replaces only the bytes inside the direct-child <version> of <project> (Quirk Q7).
type mavenPomFile struct {
	path   string
	raw    string
	layout pomLayout
	diff   Diff
}

func newMavenPomFile(path, content string) (File, error) {
	layout, err := scanPom(path, content)
	if err != nil {
		return nil, err
	}
	return mavenPomFile{path: path, raw: content, layout: layout}, nil
}

func mavenXMLError(path string, cause error) error {
	return diag.New("Invalid XML in " + path + ": " + cause.Error()).
		WithCode(codeMavenXML).
		WithHelp("remeris expects the pom.xml file to be maven project with a version property").
		WithCause(cause)
}

func mavenMissing(path, property string) error {
	return diag.New(path + " was missing required property " + property).WithCode(codeMavenMissingProperty)
}

// scanPom reads content with encoding/xml and records the offsets that SetVersion needs.
func scanPom(path, content string) (pomLayout, error) {
	data, base, singleByte, err := pomInput(path, content)
	if err != nil {
		return pomLayout{}, err
	}
	layout, rootIsProject, err := scanTokens(data)
	if err != nil {
		return pomLayout{}, mavenXMLError(path, err)
	}
	if !rootIsProject {
		return pomLayout{}, mavenMissing(path, mavenRoot)
	}
	layout.shift(base)
	if singleByte && layout.hasText && hasHighByte(content[layout.inner.start:layout.inner.end]) {
		// The text was read from placeholders. Use the original bytes, which are not a version.
		layout.versionText = []byte(content[layout.inner.start:layout.inner.end])
	}
	return layout, nil
}

// shift moves every offset by n, the length of the byte order mark that was skipped.
func (l *pomLayout) shift(n int) {
	l.inner.start, l.inner.end = l.inner.start+n, l.inner.end+n
	l.versionTag.start, l.versionTag.end = l.versionTag.start+n, l.versionTag.end+n
	l.rootTag.start, l.rootTag.end = l.rootTag.start+n, l.rootTag.end+n
	l.artifactTag += n
	l.artifactEnd += n
	l.closeStart += n
	l.rootStart += n
}

// pomInput returns the bytes to tokenize and the offset of the first of them in content. A UTF-8 byte order mark is
// left out. Byte offsets must refer to the original bytes, so a file that declares ISO-8859-1 or US-ASCII is
// tokenized with each byte of 0x80 and above replaced by a placeholder of one byte: encoding/xml then sees valid
// UTF-8 and every offset stays the same. Any other declared encoding is an error.
func pomInput(path, content string) (data []byte, base int, singleByte bool, err error) {
	data = []byte(content)
	if bytes.HasPrefix(data, utf16BEBOM) || bytes.HasPrefix(data, utf16LEBOM) {
		return nil, 0, false, unsupportedEncoding(path, "UTF-16")
	}
	if bytes.HasPrefix(data, utf8BOM) {
		data, base = data[bomLen:], bomLen
	}
	label := declaredEncoding(data)
	switch lower := strings.ToLower(label); {
	case label == "" || lower == utf8Label || lower == "utf8":
		return data, base, false, nil
	case singleByteLabels[lower]:
		clean := make([]byte, len(data))
		for i, b := range data {
			if b >= asciiLimit {
				b = placeholder
			}
			clean[i] = b
		}
		return clean, base, true, nil
	default:
		return nil, 0, false, unsupportedEncoding(path, label)
	}
}

func unsupportedEncoding(path, label string) error {
	return diag.New("Unsupported XML encoding " + label + " in " + path).
		WithCode(codeMavenEncoding).
		WithHelp("remeris can read pom.xml files in UTF-8, ISO-8859-1 or US-ASCII. " +
			"Editing a file in place needs byte offsets, so it does not convert other encodings.")
}

// declaredEncoding returns the value of the encoding pseudo-attribute of the XML declaration, or "".
func declaredEncoding(data []byte) string {
	if !bytes.HasPrefix(data, []byte(declStart)) || len(data) <= len(declStart) || !isXMLSpace(data[len(declStart)]) {
		return ""
	}
	end := bytes.Index(data, []byte(declEnd))
	if end < 0 {
		return ""
	}
	decl := data[len(declStart):end]
	i := bytes.Index(decl, []byte(declEncoding))
	if i < 0 {
		return ""
	}
	rest := bytes.TrimLeft(decl[i+len(declEncoding):], " \t\r\n")
	if len(rest) == 0 || rest[0] != '=' {
		return ""
	}
	rest = bytes.TrimLeft(rest[1:], " \t\r\n")
	if len(rest) == 0 || (rest[0] != '"' && rest[0] != '\'') {
		return ""
	}
	value, _, found := bytes.Cut(rest[1:], rest[:1])
	if !found {
		return ""
	}
	return string(value)
}

func hasHighByte(s string) bool {
	for i := range len(s) {
		if s[i] >= asciiLimit {
			return true
		}
	}
	return false
}

func isXMLSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

// blank reports whether b holds XML white space only.
func blank(b []byte) bool {
	for _, c := range b {
		if !isXMLSpace(c) {
			return false
		}
	}
	return true
}

// passThrough accepts the encodings pomInput lets through. The bytes are not converted: pomInput made them valid
// UTF-8 of the same length.
func passThrough(label string, input io.Reader) (io.Reader, error) {
	lower := strings.ToLower(label)
	if lower == utf8Label || lower == "utf8" || singleByteLabels[lower] {
		return input, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}

// childKind tells which direct child of <project> is open while scanning.
type childKind int

const (
	childOther childKind = iota
	childVersion
	childArtifact
)

// scanTokens walks the whole document, so a malformed file is an error wherever the problem is.
func scanTokens(data []byte) (layout pomLayout, rootIsProject bool, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = passThrough
	var (
		depth    int
		seenRoot bool
		kind     childKind
		tagStart int
	)
	for {
		start := int(dec.InputOffset())
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return pomLayout{}, false, err
		}
		end := int(dec.InputOffset())
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case depth == 0:
				if seenRoot {
					return pomLayout{}, false, errTwoRoots
				}
				seenRoot = true
				rootIsProject = t.Name.Local == mavenRoot
				layout.rootStart = start
			case depth == 1 && rootIsProject:
				kind = childOther
				switch {
				case t.Name.Local == mavenVersion && !layout.hasVersion:
					kind, tagStart = childVersion, start
					layout.inner.start = end
				case t.Name.Local == mavenArtifactID && !layout.hasArtifact:
					kind = childArtifact
					layout.artifactTag = start
				}
			}
			depth++
		case xml.EndElement:
			depth--
			switch {
			case depth == 0:
				layout.closeStart = start
				if end == start { // a zero-width end element is the second half of <project/>
					layout.rootEmpty = true
					layout.rootTag = span{layout.rootStart, end}
				}
			case depth == 1 && kind == childVersion:
				layout.hasVersion, kind = true, childOther
				if end == start {
					layout.selfClosing = true
					layout.versionTag = span{tagStart, end}
					layout.inner = span{end, end}
				} else {
					layout.inner.end = start
				}
			case depth == 1 && kind == childArtifact:
				layout.hasArtifact, kind = true, childOther
				layout.artifactEnd = end
			}
		case xml.CharData:
			if depth == 0 && !blank(t) {
				return pomLayout{}, false, errOutside
			}
			if depth == 2 && kind == childVersion && !blank(t) {
				layout.versionText = append(layout.versionText, t...)
				layout.hasText = true
			}
		}
	}
	if !seenRoot {
		return pomLayout{}, false, errNoRoot
	}
	return layout, rootIsProject, nil
}

func (f mavenPomFile) Path() string { return f.path }

// Version returns the text of project/version, not trimmed (Knope's get_text). White space alone counts as missing.
func (f mavenPomFile) Version() (semver.Version, error) {
	if !f.layout.hasVersion || !f.layout.hasText {
		return semver.Version{}, mavenMissing(f.path, mavenProjectVer)
	}
	return semver.Parse(string(f.layout.versionText))
}

// SetVersion replaces the content of project/version, or inserts the element when there is none. Every other byte
// stays as it was.
func (f mavenPomFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency != nil {
		return nil, UnsupportedDependency(f.path)
	}
	version := v.String()
	raw := spliceVersion(f.raw, f.layout, version)
	layout, err := scanPom(f.path, raw)
	if err != nil {
		return nil, err
	}
	out := f
	out.raw, out.layout = raw, layout
	// Like Knope, a later call replaces the diff of an earlier one.
	out.diff = Diff{}.Add(mavenProjectVer + " = " + version)
	return out, nil
}

// spliceVersion returns raw with version written in the project/version element described by l.
func spliceVersion(raw string, l pomLayout, version string) string {
	switch {
	case l.hasVersion && l.selfClosing:
		// <version/> becomes <version>v</version>; white space before the slash goes.
		name := tagName(raw, l.versionTag.start)
		cut := trimSpaceBefore(raw, l.versionTag.start, l.versionTag.end-2)
		return raw[:cut] + ">" + version + "</" + name + ">" + raw[l.versionTag.end:]
	case l.hasVersion:
		return raw[:l.inner.start] + version + raw[l.inner.end:]
	}
	element := "<" + mavenVersion + ">" + version + "</" + mavenVersion + ">"
	switch {
	case l.hasArtifact:
		indent, eol, _ := lineIndent(raw, l.artifactTag)
		return raw[:l.artifactEnd] + eol + indent + element + raw[l.artifactEnd:]
	case l.rootEmpty:
		name := tagName(raw, l.rootTag.start)
		cut := trimSpaceBefore(raw, l.rootTag.start, l.rootTag.end-2)
		return raw[:cut] + ">" + element + "</" + name + ">" + raw[l.rootTag.end:]
	}
	indent, eol, atLineStart := lineIndent(raw, l.closeStart)
	if !atLineStart {
		return raw[:l.closeStart] + element + raw[l.closeStart:]
	}
	unit := unitIndent
	if strings.Contains(indent, unitIndentTab) {
		unit = unitIndentTab
	}
	lineStart := l.closeStart - len(indent)
	return raw[:lineStart] + indent + unit + element + eol + raw[lineStart:]
}

// lineIndent returns the white space that starts the line of offset, the line ending in use there, and whether
// only white space comes before offset on its line.
func lineIndent(raw string, offset int) (indent, eol string, atLineStart bool) {
	lineStart := strings.LastIndexByte(raw[:offset], '\n') + 1
	eol = lineFeed
	if lineStart >= 2 && raw[lineStart-2] == '\r' {
		eol = carriageFeed
	}
	prefix := raw[lineStart:offset]
	if strings.TrimLeft(prefix, " \t") != "" {
		return "", eol, false
	}
	return prefix, eol, true
}

// tagName returns the name written in the start tag that begins at offset.
func tagName(raw string, offset int) string {
	rest := raw[offset+1:]
	end := strings.IndexAny(rest, " \t\r\n/>")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// trimSpaceBefore moves end back over white space, not past floor.
func trimSpaceBefore(raw string, floor, end int) int {
	for end > floor+1 && isXMLSpace(raw[end-1]) {
		end--
	}
	return end
}

// Write returns the write action, or nil if SetVersion was never called.
func (f mavenPomFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}
