package httpx

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const testURL = "https://x.test/"

func response(t *testing.T, status int, rawURL string) *http.Response {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: status, Request: &http.Request{URL: u}}
}

func TestStatusError(t *testing.T) {
	tests := map[string]struct {
		status int
		url    string
		want   string
		failed bool
	}{
		"client error": {
			404, "https://api.github.com/repos/o/r",
			"HTTP status client error (404 Not Found) for url (https://api.github.com/repos/o/r)", true,
		},
		"server error": {
			500, "https://gitea.example.com/api/v1/x?page=2",
			"HTTP status server error (500 Internal Server Error) for url (https://gitea.example.com/api/v1/x?page=2)", true,
		},
		"unknown server status": {
			599, testURL,
			"HTTP status server error (599 <unknown status code>) for url (https://x.test/)", true,
		},
		"unknown client status": {
			499, testURL,
			"HTTP status client error (499 <unknown status code>) for url (https://x.test/)", true,
		},
		"payload too large": {
			413, testURL,
			"HTTP status client error (413 Payload Too Large) for url (https://x.test/)", true,
		},
		"uri too long": {
			414, testURL,
			"HTTP status client error (414 URI Too Long) for url (https://x.test/)", true,
		},
		"range not satisfiable": {
			416, testURL,
			"HTTP status client error (416 Range Not Satisfiable) for url (https://x.test/)", true,
		},
		"teapot": {
			418, testURL,
			"HTTP status client error (418 I'm a teapot) for url (https://x.test/)", true,
		},
		"password is redacted": {
			401, "https://user:secret@x.test/p",
			"HTTP status client error (401 Unauthorized) for url (https://user:xxxxx@x.test/p)", true,
		},
		"success is not an error":  {200, testURL, "", false},
		"redirect is not an error": {302, testURL, "", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, failed := StatusError(response(t, tt.status, tt.url))
			if got != tt.want || failed != tt.failed {
				t.Errorf("StatusError = %q, %v; want %q, %v", got, failed, tt.want, tt.failed)
			}
		})
	}
}

func TestStatusErrorWithoutRequest(t *testing.T) {
	got, _ := StatusError(&http.Response{StatusCode: 403})
	if want := "HTTP status client error (403 Forbidden)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewStatusError(t *testing.T) {
	err := NewStatusError("GitHub", "creating a release", response(t, 422, "https://api.github.com/x"), `{"message":"bad"}`)
	wantMsg := `Trouble communicating with GitHub while creating a release: ` +
		`HTTP status client error (422 Unprocessable Entity) for url (https://api.github.com/x) with body: {"message":"bad"}`
	if err.Error() != wantMsg {
		t.Errorf("message = %q, want %q", err.Error(), wantMsg)
	}
	if err.Code() != "remeris::api_request_error" {
		t.Errorf("code = %q", err.Code())
	}
	wantHelp := "There was a problem communicating with GitHub, this may be a network issue or a permissions issue."
	if err.Help() != wantHelp {
		t.Errorf("help = %q, want %q", err.Help(), wantHelp)
	}
	if NewStatusError("GitHub", "x", response(t, 200, testURL), "") != nil {
		t.Error("a 200 response should give no error")
	}
}

func TestNewTransportError(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := NewTransportError("the Gitea instance at gitea.example.com", "listing issues", cause)
	want := "Trouble communicating with the Gitea instance at gitea.example.com while listing issues: dial tcp: connection refused"
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Error("the cause should be reachable with errors.Is")
	}
	var d diag.Diagnostic
	if !errors.As(error(err), &d) {
		t.Error("not a diag.Diagnostic")
	}
}

func TestNewDecodeError(t *testing.T) {
	tests := map[string]struct {
		service  string
		wantMsg  string
		wantHelp string
	}{
		"GitHub": {
			"GitHub",
			"Trouble decoding the response from GitHub while getting the release: unexpected EOF",
			"Failure to decode a response from GitHub is probably a bug. Please report it at https://github.com/celest-io/remeris",
		},
		"Gitea": {
			"the Gitea instance at gitea.example.com",
			"Trouble decoding the response from the Gitea instance at gitea.example.com while getting the release: unexpected EOF",
			"Failure to decode a response from the Gitea instance at gitea.example.com is probably a bug. " +
				"Please report it at https://github.com/celest-io/remeris",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cause := errors.New("unexpected EOF")
			err := NewDecodeError(tt.service, "getting the release", cause)
			if err.Error() != tt.wantMsg {
				t.Errorf("message = %q, want %q", err.Error(), tt.wantMsg)
			}
			if err.Help() != tt.wantHelp {
				t.Errorf("help = %q, want %q", err.Help(), tt.wantHelp)
			}
			if err.Code() != "remeris::api_response_error" || !errors.Is(err, cause) {
				t.Errorf("code %q, cause reachable %v", err.Code(), errors.Is(err, cause))
			}
		})
	}
}

func TestRenderedError(t *testing.T) {
	var sb strings.Builder
	diag.Render(&sb, NewAPIRequestError("GitHub", "creating a release", "boom"))
	want := "Error: Trouble communicating with GitHub while creating a release: boom\n" +
		"  help: There was a problem communicating with GitHub, this may be a network issue or a permissions issue.\n"
	if sb.String() != want {
		t.Errorf("rendered %q, want %q", sb.String(), want)
	}
}
