package versionedfile

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes of pubspec.yaml files.
const (
	codePubSpecDeserialize = "remeris::pubspec_yaml::deserialize"
	codePubSpecSerialize   = "remeris::pubspec_yaml::serialize"

	pubspecBOM = "\xef\xbb\xbf"
	crlf       = "\r\n"
)

// pubSpecFile is a pubspec.yaml (F6.2). It is an immutable value. The file is read with the yaml.Node API only (no
// decoding into maps, so aliases are never expanded) and edited by splicing the bytes of the version scalar, so
// comments, quotes and every other byte stay. Knope replaces the whole `version: ...` line instead; see
// testdata/deviations.md.
type pubSpecFile struct {
	path    string
	raw     string
	version semver.Version
	diff    Diff
}

func newPubSpecFile(path, content string) (File, error) {
	_, value, err := parsePubSpec(path, content)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, pubSpecInvalid(path, "YAML did not contain a top-level `version` field")
	}
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return nil, pubSpecInvalid(path, "YAML `version` field was not a string")
	}
	version, err := semver.Parse(value.Value)
	if err != nil {
		return nil, err
	}
	return pubSpecFile{path: path, raw: content, version: version}, nil
}

// pubSpecInvalid is Knope's Error::Deserialize.
func pubSpecInvalid(path, detail string) error {
	return diag.New("Error deserializing " + path + ": " + detail).
		WithCode(codePubSpecDeserialize).
		WithHelp("remeris expects the pubspec.yaml file to be an object with a top level `version` property")
}

// pubSpecEditFailed reports a file whose version cannot be replaced in place. Knope re-serialises such files; this
// refuses to touch them rather than risk corrupting the YAML.
func pubSpecEditFailed(path, detail string) error {
	return diag.New("Could not update " + path + ": " + detail).
		WithCode(codePubSpecSerialize).
		WithHelp("remeris can only replace a version written as a plain or quoted single-line value. " +
			"Write `version: x.y.z` on one line, without an anchor or a tag")
}

// parsePubSpec returns the top-level mapping and the value of its `version` key (nil when there is none). The file
// must hold exactly one YAML document, and that document must be a mapping.
func parsePubSpec(path, content string) (mapping, value *yaml.Node, err error) {
	dec := yaml.NewDecoder(strings.NewReader(content))
	var first *yaml.Node
	documents := 0
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, pubSpecInvalid(path, err.Error())
		}
		if documents == 0 {
			first = &doc
		}
		documents++
	}
	if documents != 1 {
		return nil, nil, pubSpecInvalid(path, "Expected a single YAML document, found "+strconv.Itoa(documents))
	}
	if len(first.Content) != 1 || first.Content[0].Kind != yaml.MappingNode {
		return nil, nil, pubSpecInvalid(path, "YAML was not a mapping")
	}
	mapping = first.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value != keyVersion {
			continue
		}
		if value != nil {
			return nil, nil, pubSpecInvalid(path, "YAML contained more than one top-level `version` field")
		}
		value = mapping.Content[i+1]
	}
	return mapping, value, nil
}

func (f pubSpecFile) Path() string { return f.path }

// Version returns the version read when the file was parsed, or the one SetVersion wrote.
func (f pubSpecFile) Version() (semver.Version, error) { return f.version, nil }

// SetVersion replaces the value of the top-level version key, keeping its quotes, its trailing comment and every
// other byte. The dependency is ignored, as in Knope. The diff is the bare version.
func (f pubSpecFile) SetVersion(v semver.Version, _ *string, _ GoVersioning) (File, error) {
	mapping, value, err := parsePubSpec(f.path, f.raw)
	if err != nil {
		return nil, err
	}
	text := v.String()
	var raw string
	if value == nil {
		raw, err = f.insertVersion(mapping, text)
	} else {
		raw, err = f.spliceVersion(value, text)
	}
	if err != nil {
		return nil, err
	}
	// Whatever was edited must read back as the new version: the guard against writing a corrupted file.
	if _, check, err := parsePubSpec(f.path, raw); err != nil || check == nil || check.Kind != yaml.ScalarNode ||
		check.Value != text {
		return nil, pubSpecEditFailed(f.path, "the edited file does not read back as version "+text)
	}
	out := f
	out.raw = raw
	out.version = v
	out.diff = Diff{}.Add(text) // replaced, not accumulated, like Knope
	return out, nil
}

// spliceVersion replaces the bytes of the scalar value with text.
func (f pubSpecFile) spliceVersion(value *yaml.Node, text string) (string, error) {
	start, end, err := pubSpecScalarSpan(f.raw, value)
	if err != nil {
		return "", pubSpecEditFailed(f.path, err.Error())
	}
	return f.raw[:start] + text + f.raw[end:], nil
}

// insertVersion appends the version at the end of a block mapping that has none. It cannot happen for a file that
// passed newPubSpecFile, which requires the key. Knope re-serialises the document here; this only appends a line.
func (f pubSpecFile) insertVersion(mapping *yaml.Node, text string) (string, error) {
	if mapping.Style&yaml.FlowStyle != 0 {
		return "", pubSpecEditFailed(f.path, "the top-level mapping is written in flow style")
	}
	eol := "\n"
	if strings.Contains(f.raw, crlf) {
		eol = crlf
	}
	raw := f.raw
	if raw != "" && !strings.HasSuffix(raw, "\n") && !strings.HasSuffix(raw, "\r") {
		raw += eol
	}
	return raw + "version: " + text + eol, nil
}

// Write returns the write action, or nil if SetVersion was never called.
func (f pubSpecFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}

// Style bits of yaml.Node that make a scalar's source differ from its value, or hide its start behind a tag.
const pubSpecUnspliceable = yaml.TaggedStyle | yaml.LiteralStyle | yaml.FoldedStyle

// pubSpecScalarSpan returns the byte range of the text to replace for the scalar node n: the whole text of a plain
// scalar, or the text between the quotes of a quoted one. It fails, and the caller must not edit, when the scalar is
// an alias, has an anchor or a tag, is a block scalar, spans lines, or uses escapes: then its source is not the
// simple text the splice assumes.
func pubSpecScalarSpan(src string, n *yaml.Node) (start, end int, err error) {
	switch {
	case n.Kind == yaml.AliasNode:
		return 0, 0, errors.New("the version is an alias")
	case n.Kind != yaml.ScalarNode:
		return 0, 0, errors.New("the version is not a scalar")
	case n.Anchor != "":
		return 0, 0, errors.New("the version has an anchor")
	case n.Style&pubSpecUnspliceable != 0:
		return 0, 0, errors.New("the version has a tag or is a block scalar")
	}
	off, ok := yamlOffset(src, n.Line, n.Column)
	if !ok {
		return 0, 0, errors.New("could not locate the version in the file")
	}
	switch {
	case n.Style&yaml.SingleQuotedStyle != 0:
		return quotedSpan(src, off, '\'', n.Value)
	case n.Style&yaml.DoubleQuotedStyle != 0:
		return quotedSpan(src, off, '"', n.Value)
	}
	if n.Value == "" || !strings.HasPrefix(src[off:], n.Value) {
		return 0, 0, errors.New("the version is not a single-line value")
	}
	return off, off + len(n.Value), nil
}

// quotedSpan returns the range between the quotes of the scalar that starts at off. A single-quoted scalar
// unescapes only two quotes in a row; a double-quoted one with any backslash is refused. The text must decode to want.
func quotedSpan(src string, off int, quote byte, want string) (start, end int, err error) {
	if off >= len(src) || src[off] != quote {
		return 0, 0, errors.New("could not locate the quoted version in the file")
	}
	for i := off + 1; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\n' || c == '\r':
			return 0, 0, errors.New("the quoted version spans several lines")
		case c == '\\' && quote == '"':
			return 0, 0, errors.New("the quoted version contains escapes")
		case c != quote:
		case quote == '\'' && i+1 < len(src) && src[i+1] == '\'':
			i++
		default:
			raw := src[off+1 : i]
			if quote == '\'' {
				raw = strings.ReplaceAll(raw, "''", "'")
			}
			if raw != want {
				return 0, 0, errors.New("could not locate the quoted version in the file")
			}
			return off + 1, i, nil
		}
	}
	return 0, 0, errors.New("the quoted version is not closed")
}

// yamlOffset converts a yaml.v3 position into a byte offset of src. yaml.v3 numbers lines from 1 and columns from 1
// in characters (runes, so a tab or a multi-byte character is one column), counts \r\n as one line break and also
// breaks lines at \r, U+0085, U+2028 and U+2029, and does not count a leading byte order mark.
func yamlOffset(src string, line, col int) (int, bool) {
	i := 0
	if strings.HasPrefix(src, pubspecBOM) {
		i = len(pubspecBOM)
	}
	for l := 1; l < line; l++ {
		next, ok := afterLineBreak(src, i)
		if !ok {
			return 0, false
		}
		i = next
	}
	for c := 1; c < col; c++ {
		if i >= len(src) {
			return 0, false
		}
		_, size := utf8.DecodeRuneInString(src[i:])
		i += size
	}
	return i, true
}

// afterLineBreak returns the offset just after the first line break at or after i.
func afterLineBreak(src string, i int) (int, bool) {
	for ; i < len(src); i++ {
		switch src[i] {
		case '\n':
			return i + 1, true
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				return i + 2, true
			}
			return i + 1, true
		case 0xC2:
			if i+1 < len(src) && src[i+1] == 0x85 {
				return i + 2, true
			}
		case 0xE2:
			if i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9) {
				return i + 3, true
			}
		}
	}
	return 0, false
}
