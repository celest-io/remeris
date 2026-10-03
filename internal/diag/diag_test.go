package diag

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestRender(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"plain error": {
			err:  errors.New("boom"),
			want: "Error: boom\n",
		},
		"message only": {
			err:  New("No workflow selected"),
			want: "Error: No workflow selected\n",
		},
		"help": {
			err:  New("Command returned non-zero exit code").WithHelp("Try running it manually."),
			want: "Error: Command returned non-zero exit code\n  help: Try running it manually.\n",
		},
		"multi-line help is indented": {
			err:  New("bad").WithHelp("line one\nline two"),
			want: "Error: bad\n  help: line one\n        line two\n",
		},
		"related error": {
			err:  New("Problem with workflow release").WithRelated(New("No packages to operate on").WithHelp("Add one.")),
			want: "Error: Problem with workflow release\n  ╰─▶ No packages to operate on\n      help: Add one.\n",
		},
		"nested and sibling related errors": {
			err: New("There are problems with the defined workflows").WithRelated(
				New("Problem with workflow a").WithRelated(New("first")),
				New("Problem with workflow b").WithRelated(New("second").WithHelp("h")),
			),
			want: "Error: There are problems with the defined workflows\n" +
				"  ╰─▶ Problem with workflow a\n" +
				"      ╰─▶ first\n" +
				"  ╰─▶ Problem with workflow b\n" +
				"      ╰─▶ second\n" +
				"          help: h\n",
		},
		"related plain error": {
			err:  New("outer").WithRelated(errors.New("inner")),
			want: "Error: outer\n  ╰─▶ inner\n",
		},
		"wrapped diagnostic shows the wrapper only": {
			err:  fmt.Errorf("context: %w", New("inner").WithHelp("hidden")),
			want: "Error: context: inner\n",
		},
		"source and url are not rendered in v1": {
			err:  New("bad").WithURL("https://example.com").WithSource("abc", Label{Start: 0, End: 1, Text: "here"}),
			want: "Error: bad\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			Render(&buf, tc.err)
			if got := buf.String(); got != tc.want {
				t.Errorf("Render() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestErrorBuildersDoNotMutate(t *testing.T) {
	base := New("base")
	_ = base.WithHelp("help").WithCode("c").WithRelated(New("child"))
	if base.Help() != "" || base.Code() != "" || len(base.Related()) != 0 {
		t.Errorf("With* methods changed the receiver: %+v", base)
	}
}

func TestErrorAccessors(t *testing.T) {
	cause := errors.New("cause")
	label := Label{Start: 1, End: 2, Text: "x"}
	err := New("msg").WithCode("code").WithHelp("help").WithURL("url").WithSource("src", label).WithCause(cause)
	var d Diagnostic = err
	if d.Error() != "msg" || d.Code() != "code" || d.Help() != "help" || d.URL() != "url" || d.SourceCode() != "src" {
		t.Errorf("unexpected accessors: %+v", err)
	}
	if got := d.Labels(); len(got) != 1 || got[0] != label {
		t.Errorf("Labels() = %v", got)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the cause")
	}
}

func TestNewfWrapsWithW(t *testing.T) {
	cause := errors.New("cause")
	err := Newf("I/O error: %w", cause)
	if err.Error() != "I/O error: cause" || !errors.Is(err, cause) {
		t.Errorf("Newf() = %v, unwrap %v", err, errors.Unwrap(err))
	}
	if plain := Newf("no %s", "verb"); errors.Unwrap(plain) != nil {
		t.Errorf("Newf without %%w should have no cause, got %v", errors.Unwrap(plain))
	}
}
