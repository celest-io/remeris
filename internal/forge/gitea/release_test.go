package gitea

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const (
	sha       = "0123456789abcdef0123456789abcdef01234567"
	codebergH = "https://codeberg.org"

	tagV1          = "v1"
	tagV120        = "v1.2.0"
	ver120         = "1.2.0"
	bodyNotes      = "notes"
	hostExample    = "https://gitea.example.com"
	codeAPIRequest = "remeris::api_request_error"
	msgDecodeEOF   = "Trouble decoding the response from Gitea while creating a release: unexpected end of JSON input"
)

func TestCreateReleaseBody(t *testing.T) {
	tests := map[string]struct {
		in   ReleaseInput
		want string
	}{
		"default": {
			in: ReleaseInput{TagName: tagV120, Name: "1.2.0 (2026-01-01)", Body: "## Features\n\n- x", TargetCommitish: sha},
			want: `{"tag_name":"v1.2.0","name":"1.2.0 (2026-01-01)","body":"## Features\n\n- x","prerelease":false,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + sha + `"}`,
		},
		"named package": {
			in: ReleaseInput{TagName: "foo/v1.2.0", Name: "foo 1.2.0", Body: bodyNotes, TargetCommitish: sha},
			want: `{"tag_name":"foo/v1.2.0","name":"foo 1.2.0","body":"notes","prerelease":false,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + sha + `"}`,
		},
		"prerelease": {
			in: ReleaseInput{
				TagName:         "v1.2.0-rc.1",
				Name:            "1.2.0-rc.1",
				Body:            bodyNotes,
				Prerelease:      true,
				TargetCommitish: sha,
			},
			want: `{"tag_name":"v1.2.0-rc.1","name":"1.2.0-rc.1","body":"notes","prerelease":true,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + sha + `"}`,
		},
		"empty body asks Gitea to generate the notes": {
			in:   ReleaseInput{TagName: tagV120, Name: ver120, TargetCommitish: sha},
			want: `{"tag_name":"v1.2.0","name":"1.2.0","prerelease":false,"generate_release_notes":true,"draft":false,"target_commitish":"` + sha + `"}`,
		},
		"html characters are not escaped": {
			in: ReleaseInput{TagName: tagV120, Name: ver120, Body: "a <b> & c"},
			want: `{"tag_name":"v1.2.0","name":"1.2.0","body":"a <b> & c","prerelease":false,` +
				`"generate_release_notes":false,"draft":false}`,
		},
		"without the commit": {
			in:   ReleaseInput{TagName: tagV120, Name: ver120, Body: bodyNotes},
			want: `{"tag_name":"v1.2.0","name":"1.2.0","body":"notes","prerelease":false,"generate_release_notes":false,"draft":false}`,
		},
		"neither body nor commit": {
			in:   ReleaseInput{TagName: tagV120, Name: ver120},
			want: `{"tag_name":"v1.2.0","name":"1.2.0","prerelease":false,"generate_release_notes":true,"draft":false}`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusCreated, `{"url":"u","upload_url":"x"}`))
			c := newClient(t, srv.URL)

			if err := c.CreateRelease(context.Background(), tc.in); err != nil {
				t.Fatal(err)
			}

			got := srv.requests()
			if len(got) != 1 {
				t.Fatalf("requests = %+v", got)
			}
			r := got[0]
			if r.body != tc.want {
				t.Errorf("body = %s\nwant   %s", r.body, tc.want)
			}
			if r.method != http.MethodPost || r.uri != releasesPath {
				t.Errorf("request = %s %s", r.method, r.uri)
			}
			if r.auth != "token "+testToken {
				t.Errorf("Authorization = %q", r.auth)
			}
			if strings.Contains(r.uri, "access_token") || strings.Contains(r.uri, testToken) {
				t.Errorf("the token is in the URL: %q", r.uri)
			}
		})
	}
}

func TestCreateReleaseDecodesLeniently(t *testing.T) {
	tests := map[string]string{
		"full":                `{"url":"u","upload_url":"x"}`,
		"older Gitea":         `{"id":1,"url":"u"}`,
		"empty object":        `{}`,
		"array":               `[]`,
		"null":                `null`,
		"scalar":              `"ok"`,
		"with surrounding ws": " {\"id\":1}\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusCreated, body))
			c := newClient(t, srv.URL)

			if err := c.CreateRelease(context.Background(), ReleaseInput{TagName: tagV1, Name: "1"}); err != nil {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestCreateReleaseErrors(t *testing.T) {
	tests := map[string]struct {
		status   int
		body     string
		wantMsg  string
		wantCode string
	}{
		"client error": {
			status: http.StatusConflict, body: "release exists",
			wantMsg: "Trouble communicating with {host} while creating a release: " +
				"HTTP status client error (409 Conflict) for url ({host}" + releasesPath + ") with body: release exists",
			wantCode: codeAPIRequest,
		},
		"server error": {
			status: http.StatusInternalServerError, body: "boom",
			wantMsg: "Trouble communicating with {host} while creating a release: " +
				"HTTP status server error (500 Internal Server Error) for url ({host}" + releasesPath + ") with body: boom",
			wantCode: codeAPIRequest,
		},
		"invalid JSON": {
			status: http.StatusCreated, body: `{"url":`,
			wantMsg:  msgDecodeEOF,
			wantCode: codeAPIResponse,
		},
		"empty body": {
			status: http.StatusCreated, body: "",
			wantMsg:  msgDecodeEOF,
			wantCode: codeAPIResponse,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(tc.status, tc.body))
			c := newClient(t, srv.URL)

			err := c.CreateRelease(context.Background(), ReleaseInput{TagName: tagV1, Name: "1"})

			if err == nil {
				t.Fatal("want an error")
			}
			if want := strings.ReplaceAll(tc.wantMsg, "{host}", srv.URL); err.Error() != want {
				t.Errorf("message = %q\nwant      %q", err.Error(), want)
			}
			if got := codeOf(t, err); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(helpOf(err), testToken) {
				t.Error("the token is in the error")
			}
		})
	}
}

func TestDescribeRelease(t *testing.T) {
	tests := map[string]struct {
		host string
		in   ReleaseInput
		want string
	}{
		"release with notes": {
			host: codebergH,
			in:   ReleaseInput{TagName: "v1.1.0", Name: "1.1.0 (2026-01-01)", Body: "## Features\n\n- x", TargetCommitish: sha},
			want: "Would create a release on Gitea [https://codeberg.org] with name 1.1.0 (2026-01-01) and tag v1.1.0 at commit " +
				sha + " and body:\n## Features\n\n- x",
		},
		"prerelease, generated notes, unknown commit": {
			host: codebergH + "/",
			in:   ReleaseInput{TagName: "foo/v1.2.0-rc.1", Name: "foo 1.2.0-rc.1", Prerelease: true},
			want: "Would create a prerelease on Gitea [https://codeberg.org/] with name foo 1.2.0-rc.1 and tag foo/v1.2.0-rc.1 " +
				"and autogenerated body",
		},
		"host as written, even when invalid": {
			host: "gitea.example.com",
			in:   ReleaseInput{TagName: tagV1, Name: "1"},
			want: "Would create a release on Gitea [gitea.example.com] with name 1 and tag v1 and autogenerated body",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := DescribeRelease(Config{Owner: "o", Repo: "r", Host: tc.host}, tc.in)
			if got != tc.want {
				t.Errorf("line = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

// leak is the password of the hosts below. It must never be printed.
const leak = "pw-SENTINEL-71c3"

func TestDisplayHostRemovesCredentials(t *testing.T) {
	tests := map[string]struct {
		host string
		want string
	}{
		"plain":                     {host: codebergH, want: codebergH},
		"path prefix":               {host: "https://example.com/gitea/", want: "https://example.com/gitea/"},
		"user and password":         {host: "https://alice:" + leak + "@gitea.example.com", want: hostExample},
		"user only":                 {host: "https://alice@gitea.example.com:3000/x", want: "https://gitea.example.com:3000/x"},
		"empty user, password":      {host: "https://:" + leak + "@gitea.example.com", want: hostExample},
		"escaped characters":        {host: "http://a%40b:p%2Fw@localhost:3000", want: "http://localhost:3000"},
		"no scheme":                 {host: "alice:" + leak + "@gitea.example.com", want: redactedHost},
		"unparseable":               {host: "https://alice:" + leak + "@[::1", want: redactedHost},
		"user looks like host:port": {host: "https://gitea.example.com:" + leak + "@host", want: "https://host"},
		"invalid port":              {host: "https://alice:" + leak + "@host:port", want: redactedHost},
		"at sign without userinfo":  {host: "https://gitea.example.com/a@b", want: redactedHost},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := DisplayHost(tc.host)
			if got != tc.want {
				t.Errorf("DisplayHost(%q) = %q, want %q", tc.host, got, tc.want)
			}
			if strings.Contains(got, leak) {
				t.Errorf("the password is printed: %q", got)
			}
			if line := DescribeRelease(Config{Host: tc.host}, ReleaseInput{}); strings.Contains(line, leak) {
				t.Errorf("the password is in the dry run line: %q", line)
			}
		})
	}
}
