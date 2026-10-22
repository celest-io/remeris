package versionedfile_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	pomPath      = "pom.xml"
	pomOld       = "1.2.3"
	pomOne       = "1.0.0"
	pomNew       = "2.0.0-rc.1+build.5"
	pomOpen      = "<project>\n"
	pomClose     = "</project>\n"
	pomHead      = "  <modelVersion>4.0.0</modelVersion>\n  <artifactId>my-app</artifactId>\n"
	pomMissing   = pomPath + " was missing required property project.version"
	pomXMLCode   = "remeris::maven_pom::xml"
	pomXMLPrefix = "Invalid XML in " + pomPath + ": "
	pomEncCode   = "remeris::maven_pom::encoding"
)

// pomSplice is one input written as pre + old + post. The version element holds old, so the output is
// pre + new + post: the span is known without looking at the code under test.
type pomSplice struct{ pre, old, post string }

func (c pomSplice) in() string { return c.pre + c.old + c.post }

var pomSplices = map[string]pomSplice{
	"knope fixture, no final newline": {
		pre:  pomOpen + pomHead + "  <groupId>com.mycompany.app</groupId>\n  <version>",
		old:  pomOld,
		post: "</version>\n</project>",
	},
	"pom with a plain version": {pre: pomOpen + pomHead + "  <version>", old: pomOld, post: "</version>\n" + pomClose},
	"namespaced project": {
		pre: `<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` +
			" xsi:schemaLocation='http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd'>\n" +
			pomHead + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"prefixed elements": {
		pre:  `<m:project xmlns:m="urn:m">` + "\n  <m:version>",
		old:  pomOld,
		post: "</m:version>\n</m:project>\n",
	},
	"xml declaration and comments around": {
		pre: "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- before -->\n" + pomOpen + "  <!-- <version>9.9.9</version> -->\n" +
			pomHead + "  <version>",
		old:  pomOld,
		post: "</version> <!-- trailing -->\n" + pomClose + "<!-- after -->\n",
	},
	"declaration with single quotes and standalone": {
		pre:  "<?xml version='1.0' encoding='utf-8' standalone='yes'?>\n" + pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"parent version before": {
		pre: pomOpen + "  <parent>\n    <groupId>g</groupId>\n    <version>7.7.7</version>\n  </parent>\n" +
			pomHead + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"parent version after": {
		pre:  pomOpen + pomHead + "  <version>",
		old:  pomOld,
		post: "</version>\n  <parent>\n    <version>7.7.7</version>\n  </parent>\n" + pomClose,
	},
	"dependencies and properties around": {
		pre: pomOpen + "  <properties>\n    <version>8.8.8</version>\n    <java.version>17</java.version>\n  </properties>\n" +
			"  <dependencies>\n    <dependency>\n      <artifactId>a</artifactId>\n      <version>3.3.3</version>\n" +
			"    </dependency>\n  </dependencies>\n" + pomHead + "  <version>",
		old:  pomOld,
		post: "</version>\n  <dependencies><dependency><version>4.4.4</version></dependency></dependencies>\n" + pomClose,
	},
	"first of two version elements": {
		pre:  pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n  <version>5.5.5</version>\n" + pomClose,
	},
	"entities inside": {
		pre: pomOpen + "  <version>", old: "1&#46;2&amp;3", post: "</version>\n" + pomClose,
	},
	"cdata inside": {
		pre: pomOpen + "  <version>", old: "<![CDATA[1.2.3]]>", post: "</version>\n" + pomClose,
	},
	"comment inside": {
		pre: pomOpen + "  <version>", old: "1.2<!-- c -->.3", post: "</version>\n" + pomClose,
	},
	"child element inside": {
		pre: pomOpen + "  <version>", old: "<x>1.2.3</x>", post: "</version>\n" + pomClose,
	},
	"empty element with end tag": {
		pre: pomOpen + "  <version>", old: "", post: "</version>\n" + pomClose,
	},
	"white space inside": {
		pre: pomOpen + "  <version>", old: "\n    1.2.3\n  ", post: "</version>\n" + pomClose,
	},
	"attributes on version": {
		pre:  pomOpen + `  <version a="x>y" b='/'>`,
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"pom with crlf line endings": {
		pre:  "<project>\r\n  <artifactId>a</artifactId>\r\n  <version>",
		old:  pomOld,
		post: "</version>\r\n</project>\r\n",
	},
	"utf-8 bom": {
		pre:  "\xEF\xBB\xBF<?xml version=\"1.0\"?>\n" + pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"tabs": {
		pre:  "<project>\n\t<artifactId>a</artifactId>\n\t<version>",
		old:  pomOld,
		post: "</version>\n</project>\n",
	},
	"non-ascii text elsewhere": {
		pre:  pomOpen + "  <name>Café ☃</name>\n  <!-- ü -->\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"doctype": {
		pre:  "<!DOCTYPE project>\n" + pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"processing instruction": {
		pre:  "<?xml-stylesheet href=\"a.xsl\"?>\n" + pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"latin-1 declaration with high bytes": {
		pre:  "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" + pomOpen + "  <name>Caf\xe9</name>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"us-ascii declaration": {
		pre:  "<?xml version=\"1.0\" encoding=\"us-ascii\"?>\n" + pomOpen + "  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"cdata in project holding a version": {
		pre:  pomOpen + "  <![CDATA[<version>9</version>]]>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"processing instruction holding a version": {
		pre:  pomOpen + "  <?pi <version>9</version>?>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"doctype with an internal subset": {
		pre: "<!DOCTYPE project [\n  <!ENTITY e \"<version>9</version> > x\">\n  <!ENTITY f '>'>\n]>\n" + pomOpen +
			"  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"escaped version tag in an attribute": {
		pre:  pomOpen + `  <x a="&lt;version&gt;9&lt;/version&gt;"/>` + "\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"crlf inside the version text": {
		pre: pomOpen + "  <version>", old: "\r\n    1.2.3\r\n  ", post: "</version>\n" + pomClose,
	},
	"multi-byte element name before the version": {
		pre:  pomOpen + "  <versi\u00f3n>9.9.9</versi\u00f3n>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"bom and latin-1 together": {
		pre: "\xEF\xBB\xBF<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n" + pomOpen +
			"  <name>Caf\xe9</name>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
	"version element on the project line": {
		pre: "<project><version>", old: pomOld, post: "</version></project>",
	},
	"deeply nested elements elsewhere": {
		pre:  pomOpen + "  <x>" + strings.Repeat("<a>", 20000) + strings.Repeat("</a>", 20000) + "</x>\n  <version>",
		old:  pomOld,
		post: "</version>\n" + pomClose,
	},
}

func newPom(content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: pomPath, Format: versionedfile.MavenPom}, content, nil)
}

func mustPom(t testing.TB, content string) versionedfile.File {
	t.Helper()
	f, err := newPom(content)
	if err != nil {
		t.Fatalf("New: %v\n%q", err, content)
	}
	return f
}

// pomContent is the content of the single write action of f.
func pomContent(t testing.TB, f versionedfile.File) string {
	t.Helper()
	acts := f.Write()
	if len(acts) != 1 {
		t.Fatalf("Write() = %v, want one action", acts)
	}
	if acts[0].Kind != action.KindWriteToFile {
		t.Fatalf("Write()[0] is %v", acts[0])
	}
	return acts[0].Content
}

// pomTokens lists the tokens of s, leaving out everything inside project/version.
func pomTokens(t testing.TB, s string) []string {
	t.Helper()
	out, err := pomTokenList(s)
	if err != nil {
		t.Fatalf("tokens of %q: %v", s, err)
	}
	return out
}

func pomTokenList(s string) ([]string, error) {
	s = strings.TrimPrefix(s, "\xEF\xBB\xBF")
	if !utf8.ValidString(s) { // only a single-byte encoding gets here: keep the shape of the tokens
		s = strings.Map(func(r rune) rune {
			if r == utf8.RuneError {
				return 'x'
			}
			return r
		}, s)
	}
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var out []string
	depth, inVersion := 0, false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch tk := tok.(type) {
		case xml.StartElement:
			if depth == 1 && tk.Name.Local == "version" && !inVersion {
				inVersion = true
			}
			depth++
			if !inVersion || depth <= 2 {
				out = append(out, "<"+tk.Name.Local)
			}
		case xml.EndElement:
			depth--
			if !inVersion || depth <= 1 {
				out = append(out, "</"+tk.Name.Local)
			}
			if depth == 1 {
				inVersion = false
			}
		case xml.CharData:
			if !inVersion {
				out = append(out, "text:"+string(bytes.Clone(tk)))
			}
		case xml.Comment:
			if !inVersion {
				out = append(out, "comment:"+string(bytes.Clone(tk)))
			}
		}
	}
}

func TestMavenPomSpliceInvariant(t *testing.T) {
	t.Parallel()
	next := semver.MustParse(pomNew)
	for name, tc := range pomSplices {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := tc.in()
			f := mustPom(t, in)
			if f.Path() != pomPath {
				t.Errorf("Path() = %q", f.Path())
			}
			if f.Write() != nil {
				t.Errorf("Write() before SetVersion = %v, want nil", f.Write())
			}
			out, err := f.SetVersion(next, nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			got := pomContent(t, out)
			if want := tc.pre + pomNew + tc.post; got != want {
				t.Fatalf("splice invariant broken:\n got %q\nwant %q", got, want)
			}
			if f.Write() != nil {
				t.Error("SetVersion changed the receiver")
			}
			again := mustPom(t, got)
			v, err := again.Version()
			if err != nil || v.String() != pomNew {
				t.Errorf("Version() of the output = %v, %v; want %s", v, err, pomNew)
			}
			if a, b := pomTokens(t, in), pomTokens(t, got); strings.Join(a, "\x00") != strings.Join(b, "\x00") {
				t.Errorf("token streams differ:\n in %q\nout %q", a, b)
			}
			// Idempotence: the same version twice is the same as once.
			twice, err := out.SetVersion(next, nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			if pomContent(t, twice) != got {
				t.Error("applying SetVersion twice differs from once")
			}
			if d := twice.Write()[0].Diff; d != "project.version = "+pomNew {
				t.Errorf("diff = %q", d)
			}
		})
	}
}

func TestMavenPomIdentity(t *testing.T) {
	t.Parallel()
	for name, tc := range pomSplices {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := mustPom(t, tc.in())
			cur, err := f.Version()
			if err != nil || cur.String() != tc.old {
				t.Skip("the element text is not the canonical form of a version")
			}
			out, err := f.SetVersion(cur, nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			if got := pomContent(t, out); got != tc.in() {
				t.Errorf("identity broken:\n got %q\nwant %q", got, tc.in())
			}
		})
	}
}

func TestMavenPomVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ content, want string }{
		"knope get_version": {
			content: "\n        <project>\n          <modelVersion>4.0.0</modelVersion>\n" +
				"          <groupId>com.mycompany.app</groupId>\n          <artifactId>my-app</artifactId>\n" +
				"          <version>1.2.3-rc.1</version>\n        </project>\n        ",
			want: "1.2.3-rc.1",
		},
		"parent first": {
			content: "<project><parent><version>9.0.0</version></parent><version>1.0.0</version></project>",
			want:    pomOne,
		},
		"entities":       {content: "<project><version>1&#46;2&#x2E;3</version></project>", want: pomOld},
		"cdata":          {content: "<project><version><![CDATA[1.2.3]]></version></project>", want: pomOld},
		"text and cdata": {content: "<project><version>1.<![CDATA[2]]>.3</version></project>", want: pomOld},
		"comment joins":  {content: "<project><version>1.2<!-- c -->.3</version></project>", want: pomOld},
		"prefixed":       {content: `<p:project xmlns:p="urn:p"><p:version>1.2.3</p:version></p:project>`, want: pomOld},
		"bom":            {content: "\xEF\xBB\xBF<project><version>1.2.3</version></project>", want: pomOld},
		"latin-1": {
			content: "<?xml version=\"1.0\" encoding=\"iso-8859-1\"?><project><n>\xe9</n><version>1.2.3</version></project>",
			want:    pomOld,
		},
		"first duplicate": {
			content: "<project><version>1.2.3</version><version>9.9.9</version></project>", want: pomOld,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, err := mustPom(t, tc.content).Version()
			if err != nil || v.String() != tc.want {
				t.Errorf("Version() = %v, %v; want %s", v, err, tc.want)
			}
		})
	}
}

func TestMavenPomVersionErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"knope error_on_missing_version": "\n<project>\n  <modelVersion>4.0.0</modelVersion>\n  <artifactId>my-app</artifactId>\n</project>\n",
		"only parent version":            "<project><parent><version>1.0.0</version></parent></project>",
		"only dependency version":        "<project><dependencies><dependency><version>1.0.0</version></dependency></dependencies></project>",
		"only properties version":        "<project><properties><version>1.0.0</version></properties></project>",
		"nested deeper":                  "<project><build><plugins><version>1.0.0</version></plugins></build></project>",
		"self-closing":                   "<project><version/></project>",
		"pom with an empty version":      "<project><version></version></project>",
		"blank":                          "<project><version> \n </version></project>",
		"child only":                     "<project><version><x>1.0.0</x></version></project>",
		"root is the version":            "<version>1.0.0</version>",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if strings.HasPrefix(name, "root") {
				_, err := newPom(content)
				if err == nil || err.Error() != pomPath+" was missing required property project" {
					t.Errorf("New error = %v", err)
				}
				return
			}
			v, err := mustPom(t, content).Version()
			if err == nil || err.Error() != pomMissing {
				t.Fatalf("Version() = %v, %v; want %q", v, err, pomMissing)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != "remeris::maven_pom::missing_property" {
				t.Errorf("diagnostic = %v", d)
			}
		})
	}
	t.Run("not a semantic version", func(t *testing.T) {
		t.Parallel()
		for _, text := range []string{" 1.2.3", "1.2.3 ", "one", "1.2"} {
			if _, err := mustPom(t, "<project><version>"+text+"</version></project>").Version(); err == nil {
				t.Errorf("Version() accepted %q", text)
			}
		}
	})
}

func TestMavenPomHighBytesAreNotAVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"latin-1 pre-release": "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><version>1.0.0-\xe9</version></project>",
		"us-ascii build":      "<?xml version=\"1.0\" encoding=\"us-ascii\"?><project><version>1.0.0+\xff</version></project>",
		"latin-1 only":        "<?xml version=\"1.0\" encoding=\"latin1\"?><project><version>\xe9</version></project>",
		"bom and latin-1":     "\xEF\xBB\xBF<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><project><version>1.0.0-\xe9</version></project>",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, err := mustPom(t, content).Version()
			if err == nil {
				t.Fatalf("Version() = %v, want an error", v)
			}
		})
	}
}

func TestMavenPomNewErrors(t *testing.T) {
	t.Parallel()
	missingProject := pomPath + " was missing required property project"
	tests := map[string]struct{ content, message string }{
		"knope error_on_missing_project": {
			content: "\n        <somethingElse>\n          Invalid data  \n        </somethingElse>\n        ",
			message: missingProject,
		},
		"project in another namespace spelling": {content: "<Project/>", message: missingProject},
		"empty file":                            {content: "", message: pomXMLPrefix},
		"white space only":                      {content: " \n\t", message: pomXMLPrefix},
		"declaration only":                      {content: `<?xml version="1.0"?>`, message: pomXMLPrefix},
		"comment only":                          {content: "<!-- x -->", message: pomXMLPrefix},
		"unclosed project":                      {content: "<project><version>1.0.0</version>", message: pomXMLPrefix},
		"mismatched tags":                       {content: "<project><version>1.0.0</versio></project>", message: pomXMLPrefix},
		"text outside root":                     {content: "<project/> junk", message: pomXMLPrefix},
		"text before root":                      {content: "junk <project/>", message: pomXMLPrefix},
		"two roots":                             {content: "<project/><project/>", message: pomXMLPrefix},
		"undefined entity":                      {content: "<project>&nbsp;</project>", message: pomXMLPrefix},
		"bad attribute":                         {content: "<project a=1/>", message: pomXMLPrefix},
		"invalid utf-8":                         {content: "<project>\xff</project>", message: pomXMLPrefix},
		"malformed beats missing project":       {content: "<other><x></other>", message: pomXMLPrefix},
		"not xml":                               {content: "{\"version\": \"1.0.0\"}", message: pomXMLPrefix},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newPom(tc.content)
			if f != nil || err == nil {
				t.Fatalf("New = %v, %v", f, err)
			}
			if tc.message == missingProject {
				if err.Error() != missingProject {
					t.Errorf("message %q", err.Error())
				}
				return
			}
			if !strings.HasPrefix(err.Error(), tc.message) {
				t.Errorf("message %q lacks prefix %q", err.Error(), tc.message)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != pomXMLCode ||
				d.Help() != "remeris expects the pom.xml file to be maven project with a version property" {
				t.Errorf("diagnostic %v", d)
			}
		})
	}
}

func TestMavenPomEncodings(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"utf-16 bom":        "\xFE\xFF\x00<\x00p\x00/\x00>",
		"utf-16le bom":      "\xFF\xFE<\x00p\x00/\x00>\x00",
		"windows-1252":      `<?xml version="1.0" encoding="windows-1252"?><project/>`,
		"shift_jis":         `<?xml version="1.0" encoding='Shift_JIS'?><project/>`,
		"utf-16 declared":   `<?xml version="1.0" encoding="UTF-16"?><project/>`,
		"bom and euc-kr":    "\xEF\xBB\xBF<?xml version=\"1.0\" encoding=\"EUC-KR\"?><project/>",
		"unknown":           `<?xml version="1.0" encoding="nope"?><project/>`,
		"declaration space": "<?xml  version=\"1.0\"\n encoding = \"koi8-r\" ?><project/>",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newPom(content)
			var d diag.Diagnostic
			if f != nil || !errors.As(err, &d) || d.Code() != pomEncCode ||
				!strings.HasPrefix(err.Error(), "Unsupported XML encoding ") {
				t.Fatalf("New = %v, %v", f, err)
			}
		})
	}
}

func TestMavenPomInsert(t *testing.T) {
	t.Parallel()
	const elem = "<version>2.0.0</version>"
	tests := map[string]struct{ in, want string }{
		"after artifactId": {
			in:   "<project>\n  <modelVersion>4.0.0</modelVersion>\n  <artifactId>my-app</artifactId>\n  <packaging>jar</packaging>\n</project>\n",
			want: "<project>\n  <modelVersion>4.0.0</modelVersion>\n  <artifactId>my-app</artifactId>\n  " + elem + "\n  <packaging>jar</packaging>\n</project>\n",
		},
		"after artifactId, tabs": {
			in:   "<project>\n\t<artifactId>a</artifactId>\n</project>",
			want: "<project>\n\t<artifactId>a</artifactId>\n\t" + elem + "\n</project>",
		},
		"after artifactId, crlf": {
			in:   "<project>\r\n  <artifactId>a</artifactId>\r\n</project>\r\n",
			want: "<project>\r\n  <artifactId>a</artifactId>\r\n  " + elem + "\r\n</project>\r\n",
		},
		"after artifactId on one line": {
			in:   "<project><artifactId>a</artifactId></project>",
			want: "<project><artifactId>a</artifactId>\n" + elem + "</project>",
		},
		"parent artifactId is not the project one": {
			in:   "<project>\n  <parent>\n    <artifactId>p</artifactId>\n  </parent>\n</project>\n",
			want: "<project>\n  <parent>\n    <artifactId>p</artifactId>\n  </parent>\n  " + elem + "\n</project>\n",
		},
		"no artifactId, indented": {
			in:   "<project>\n  <modelVersion>4.0.0</modelVersion>\n</project>\n",
			want: "<project>\n  <modelVersion>4.0.0</modelVersion>\n  " + elem + "\n</project>\n",
		},
		"no artifactId, tabs and crlf": {
			in:   "<project>\r\n\t<a/>\r\n\t</project>",
			want: "<project>\r\n\t<a/>\r\n\t\t" + elem + "\r\n\t</project>",
		},
		"no artifactId, one line": {
			in:   "<project><a/></project>",
			want: "<project><a/>" + elem + "</project>",
		},
		"empty project": {
			in:   "<project/>",
			want: "<project>" + elem + "</project>",
		},
		"empty project with space": {
			in:   "<?xml version=\"1.0\"?>\n<project />\n",
			want: "<?xml version=\"1.0\"?>\n<project>" + elem + "</project>\n",
		},
		"prefixed empty project": {
			in:   `<m:project xmlns:m="urn:m"/>`,
			want: `<m:project xmlns:m="urn:m">` + elem + `</m:project>`,
		},
		"self-closing version": {
			in:   "<project>\n  <version/>\n</project>\n",
			want: "<project>\n  " + elem + "\n</project>\n",
		},
		"self-closing version with space": {
			in:   "<project>\n  <version />\n</project>\n",
			want: "<project>\n  " + elem + "\n</project>\n",
		},
		"self-closing version with attribute": {
			in:   "<project><version a='1' /></project>",
			want: "<project><version a='1'>2.0.0</version></project>",
		},
		"self-closing prefixed version": {
			in:   `<m:project xmlns:m="urn:m"><m:version/></m:project>`,
			want: `<m:project xmlns:m="urn:m"><m:version>2.0.0</m:version></m:project>`,
		},
		"bom": {
			in:   "\xEF\xBB\xBF<project>\n  <artifactId>a</artifactId>\n</project>\n",
			want: "\xEF\xBB\xBF<project>\n  <artifactId>a</artifactId>\n  " + elem + "\n</project>\n",
		},
	}
	next := semver.MustParse("2.0.0")
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := mustPom(t, tc.in)
			out, err := f.SetVersion(next, nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			got := pomContent(t, out)
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
			v, err := out.Version()
			if err != nil || v.String() != "2.0.0" {
				t.Errorf("Version() = %v, %v", v, err)
			}
			twice, err := out.SetVersion(next, nil, versionedfile.Standard)
			if err != nil || pomContent(t, twice) != got {
				t.Errorf("not idempotent: %v", err)
			}
		})
	}
}

func TestMavenPomKnopeSetVersion(t *testing.T) {
	t.Parallel()
	// Knope's set_version test re-indents the file; remeris keeps every byte but the version.
	in := "\n        <project>\n          <modelVersion>4.0.0</modelVersion>\n          <groupId>com.mycompany.app</groupId>\n" +
		"          <artifactId>my-app</artifactId>\n          <version>0.1.0-rc.0</version>\n        </project>\n        "
	out, err := mustPom(t, in).SetVersion(semver.MustParse("1.2.3-rc.4"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in, "0.1.0-rc.0", "1.2.3-rc.4", 1)
	if got := pomContent(t, out); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	acts := out.Write()
	w := acts[0]
	if w.Path != pomPath || w.Diff != "project.version = 1.2.3-rc.4" {
		t.Errorf("action %+v", w)
	}
}

func TestMavenPomDependencyUnsupported(t *testing.T) {
	t.Parallel()
	f := mustPom(t, "<project><version>1.0.0</version></project>")
	dep := "x"
	got, err := f.SetVersion(semver.MustParse(pomOld), &dep, versionedfile.Standard)
	if got != nil || err == nil || err.Error() != versionedfile.UnsupportedDependency(pomPath).Error() {
		t.Errorf("SetVersion with a dependency = %v, %v", got, err)
	}
}

func TestMavenPomDiffReplaced(t *testing.T) {
	t.Parallel()
	f := mustPom(t, "<project><version>1.0.0</version></project>")
	a, err := f.SetVersion(semver.MustParse("1.1.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	b, err := a.SetVersion(semver.MustParse("1.2.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if d := b.Write()[0].Diff; d != "project.version = 1.2.0" {
		t.Errorf("diff = %q, want the last version only, as Knope", d)
	}
}

func TestMavenPomLargeFile(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(pomOpen + pomHead)
	b.WriteString("  <dependencies>\n")
	dep := "    <dependency>\n      <groupId>g</groupId>\n      <artifactId>a</artifactId>\n      <version>1.0.0</version>\n" +
		"    </dependency>\n"
	b.Grow(len(dep) * 60000)
	for range 60000 { // about 6 MB
		b.WriteString(dep)
	}
	b.WriteString("  </dependencies>\n  <version>")
	pre := b.String()
	post := "</version>\n" + pomClose
	in := pre + pomOld + post
	f := mustPom(t, in)
	out, err := f.SetVersion(semver.MustParse("9.9.9"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got := pomContent(t, out); got != pre+"9.9.9"+post {
		t.Error("splice invariant broken on a large file")
	}
}

func TestMavenPomDeepNesting(t *testing.T) {
	t.Parallel()
	const depth = 200000
	in := pomOpen + "  <version>1.0.0</version>\n  <x>" + strings.Repeat(
		"<a>",
		depth,
	) + strings.Repeat(
		"</a>",
		depth,
	) + "</x>\n" + pomClose
	out, err := mustPom(t, in).SetVersion(semver.MustParse("1.0.1"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got := pomContent(t, out); got != strings.Replace(in, pomOne, "1.0.1", 1) {
		t.Error("splice invariant broken on a deeply nested file")
	}
}

func FuzzMavenPom(f *testing.F) {
	for _, tc := range pomSplices {
		f.Add(tc.in())
	}
	f.Add("<project/>")
	f.Add("<project><version/></project>")
	f.Add("<project>\r\n<artifactId>a</artifactId>\r\n</project>")
	f.Add("\xEF\xBB\xBF<project><version>1.0.0</version></project>")
	next := semver.MustParse(pomNew)
	f.Fuzz(func(t *testing.T, in string) {
		file, err := newPom(in)
		if err != nil {
			return
		}
		out, err := file.SetVersion(next, nil, versionedfile.Standard)
		if err != nil {
			t.Fatalf("SetVersion failed on a file that parsed: %v\n%q", err, in)
		}
		got := pomContent(t, out)
		v, err := out.Version()
		if err != nil || v.String() != pomNew {
			t.Fatalf("Version() of the output = %v, %v\n in %q\nout %q", v, err, in, got)
		}
		again := mustPom(t, got)
		if v, err := again.Version(); err != nil || v.String() != pomNew {
			t.Fatalf("re-parse reads %v, %v\n in %q\nout %q", v, err, in, got)
		}
		if _, err := file.Version(); err == nil {
			if !isSplice(in, got, pomNew) {
				t.Fatalf("the output is not the input with one range replaced:\n in %q\nout %q", in, got)
			}
			checkReplacedRange(t, in, got)
			a, errA := pomTokenList(in)
			b, errB := pomTokenList(got)
			if errA == nil && errB == nil && strings.Join(a, "\x00") != strings.Join(b, "\x00") {
				t.Fatalf("token streams differ:\n in %q\nout %q", a, b)
			}
		}
		twice, err := out.SetVersion(next, nil, versionedfile.Standard)
		if err != nil || pomContent(t, twice) != got {
			t.Fatalf("not idempotent: %v\n%q", err, got)
		}
	})
}

// isSplice reports whether got is in with one byte range replaced by v: got == in[:a] + v + in[b:].
func isSplice(in, got, v string) bool {
	for i := 0; i+len(v) <= len(got); i++ {
		if got[i:i+len(v)] != v {
			continue
		}
		head, tail := got[:i], got[i+len(v):]
		if len(head)+len(tail) <= len(in) && strings.HasPrefix(in, head) && strings.HasSuffix(in, tail) {
			return true
		}
	}
	return false
}

// checkReplacedRange finds the first depth-1 version start tag of the root with a RawToken walk, which shares no
// code with the scanner, and checks that the output keeps every byte up to the end of that tag and then holds v.
func checkReplacedRange(t *testing.T, in, got string) {
	t.Helper()
	base := 0
	if strings.HasPrefix(in, "\xEF\xBB\xBF") {
		base = 3
	}
	text := in[base:]
	if !utf8.ValidString(text) {
		text = strings.Map(func(r rune) rune {
			if r == utf8.RuneError {
				return 'x'
			}
			return r
		}, text)
	}
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	depth := 0
	for {
		tok, err := dec.RawToken()
		if err != nil {
			t.Fatalf("RawToken on an input that parsed: %v\n%q", err, in)
		}
		switch tk := tok.(type) {
		case xml.StartElement:
			if depth == 1 && tk.Name.Local == "version" {
				off := base + int(dec.InputOffset())
				if in[off-2:off] == "/>" {
					t.Fatalf("a self-closing version was readable: %q", in)
				}
				if got[:off] != in[:off] || !strings.HasPrefix(got[off:], pomNew) {
					t.Fatalf("the replaced range does not start at %d:\n in %q\nout %q", off, in, got)
				}
				return
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}
