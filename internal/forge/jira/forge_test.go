package jira

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/prompt"
)

const hostExample = "https://example.atlassian.net"

func TestCredentialsAreAskedEmailFirstThenTokenOnlyOnce(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	scripted := prompt.NewScripted(prompt.Answer{Text: testEmail + "\n"}, prompt.Answer{Text: testToken + "\n"})
	store := storeWith(t, nil, scripted)
	f := newForge(srv.URL, store)
	if len(scripted.Asked()) != 0 {
		t.Fatal("New must not ask: a dry run never builds a client")
	}

	var first *Client
	for i := range 3 {
		c, err := f.RequireAuthentication(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		} else if c != first {
			t.Error("the client should be cached")
		}
		if _, err := c.SearchIssues(context.Background(), statusDone); err != nil {
			t.Fatal(err)
		}
	}

	asked := scripted.Asked()
	if len(asked) != 2 || asked[0].Kind != prompt.KindPassword || asked[1].Kind != prompt.KindPassword ||
		asked[0].Message != "Input your email address" || !strings.HasPrefix(asked[1].Message, "No Jira token found") {
		t.Errorf("asked = %+v, want the email, then the token", asked)
	}
	for _, r := range srv.requests() {
		if r.auth != testAuth {
			t.Errorf("Authorization = %q: the newlines of the answers must be trimmed", r.auth)
		}
	}
	for name, want := range map[string]string{"email": testEmail + "\n", "jira_token": testToken + "\n"} {
		got, err := os.ReadFile(filepath.Join(store.Dir, "remeris", name))
		if err != nil || string(got) != want {
			t.Errorf("stored %s = %q, %v", name, got, err)
		}
	}
}

func TestStoredCredentialsAreNotAskedAgain(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	store := storedCredentials(t)
	scripted := prompt.NewScripted()
	store.Prompter = scripted

	c, err := newForge(srv.URL, store).RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchIssues(context.Background(), statusDone); err != nil {
		t.Fatal(err)
	}

	if len(scripted.Asked()) != 0 {
		t.Errorf("asked = %+v", scripted.Asked())
	}
}

func TestOnlyTheTokenIsMissing(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	store := storedCredentials(t)
	if err := os.Remove(filepath.Join(store.Dir, "remeris", "jira_token")); err != nil {
		t.Fatal(err)
	}
	scripted := prompt.NewScripted(prompt.Answer{Text: "new-token"})
	store.Prompter = scripted

	if _, err := newForge(srv.URL, store).RequireAuthentication(context.Background()); err != nil {
		t.Fatal(err)
	}

	asked := scripted.Asked()
	if len(asked) != 1 || !strings.HasPrefix(asked[0].Message, "No Jira token found") {
		t.Errorf("asked = %+v, want only the token", asked)
	}
}

func TestMissingEmailStopsBeforeTheToken(t *testing.T) {
	scripted := prompt.NewScripted(prompt.Answer{Text: "  "})
	f := newForge("http://127.0.0.1:1", storeWith(t, nil, scripted))

	_, err := f.RequireAuthentication(context.Background())

	if err == nil || err.Error() != "No email address was given" {
		t.Errorf("err = %v", err)
	}
	if len(scripted.Asked()) != 1 {
		t.Errorf("asked = %+v: the token must not be asked after a failed email", scripted.Asked())
	}
}

func TestNoEnvironmentOverride(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	env := map[string]string{"JIRA_TOKEN": "env-token", "JIRA_EMAIL": "env@example.com", "EMAIL": "env@example.com"}
	scripted := prompt.NewScripted(prompt.Answer{Text: testEmail}, prompt.Answer{Text: testToken})

	c, err := newForge(srv.URL, storeWith(t, env, scripted)).RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SearchIssues(context.Background(), statusDone); err != nil {
		t.Fatal(err)
	}

	if len(scripted.Asked()) != 2 || srv.requests()[0].auth != testAuth {
		t.Errorf("asked = %+v, requests = %+v", scripted.Asked(), srv.requests())
	}
}

func TestInvalidURLs(t *testing.T) {
	tests := map[string]struct {
		url, wantCode, wantMsg string
	}{
		"empty":     {url: "", wantCode: codeInvalidURL},
		"no scheme": {url: "example.atlassian.net", wantCode: codeInvalidURL},
		"bare path": {url: "/jira", wantCode: codeInvalidURL},
		"space":     {url: "https://example.atlassian.net/a b", wantCode: codeInvalidURL},
		"control":   {url: "https://example.atlassian.net/\x00", wantCode: codeInvalidURL},
		"userinfo": { //nolint:gosec // G101: a fake URL under test
			url:      "https://u:p@example.atlassian.net",
			wantCode: codeInvalidURL,
		},
		"at sign in path":  {url: "https://example.atlassian.net/a@b", wantCode: codeInvalidURL},
		"query":            {url: "https://example.atlassian.net?x=1", wantCode: codeInvalidURL},
		"empty query":      {url: "https://example.atlassian.net?", wantCode: codeInvalidURL},
		"fragment":         {url: "https://example.atlassian.net#x", wantCode: codeInvalidURL},
		"empty fragment":   {url: "https://example.atlassian.net#", wantCode: codeInvalidURL},
		"ftp":              {url: "ftp://example.atlassian.net", wantCode: codeInvalidURL},
		"no host":          {url: "https://", wantCode: codeInvalidURL},
		"plain http":       {url: "http://example.atlassian.net", wantCode: codeInsecureURL},
		"plain http local": {url: "http://192.168.1.10:8080", wantCode: codeInsecureURL},
		"localhost name":   {url: "http://localhost.example.com", wantCode: codeInsecureURL},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scripted := prompt.NewScripted()
			f := newForge(tc.url, storeWith(t, nil, scripted))

			_, err := f.RequireAuthentication(context.Background())

			if err == nil {
				t.Fatal(errNoIssue)
			}
			if got := codeOf(t, err); got != tc.wantCode {
				t.Errorf("code = %q, want %q (err %q)", got, tc.wantCode, err)
			}
			if len(scripted.Asked()) != 0 {
				t.Errorf("a bad URL must not prompt: %+v", scripted.Asked())
			}
			for _, secret := range []string{"p@", ":p", "a b"} {
				if strings.Contains(err.Error(), secret) || strings.Contains(helpOf(err), secret) {
					t.Errorf("the URL is echoed: %q", err)
				}
			}
		})
	}
}

func TestAcceptedURLs(t *testing.T) {
	tests := map[string]struct{ url, want string }{
		"https":              {url: hostExample, want: hostExample},
		"trailing slashes":   {url: hostExample + "///", want: hostExample},
		"context path":       {url: hostExample + "/jira/", want: hostExample + "/jira"},
		"uppercase scheme":   {url: "HTTPS://example.atlassian.net", want: "HTTPS://example.atlassian.net"},
		"http loopback ip":   {url: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		"http localhost":     {url: "http://localhost:8080/", want: "http://localhost:8080"},
		"http loopback ipv6": {url: "http://[::1]:8080", want: "http://[::1]:8080"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := apiBase(tc.url)

			if err != nil || got != tc.want {
				t.Errorf("apiBase = %q, %v, want %q", got, err, tc.want)
			}
		})
	}
}
