package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/celest-io/remeris/internal/prompt"
)

// blobForms are the ways a server or a proxy may echo the Basic blob.
func blobForms() []string {
	return []string{
		strings.TrimRight(testBlob, "="), url.QueryEscape(testBlob), url.PathEscape(testBlob),
		strings.ReplaceAll(url.QueryEscape(testBlob), "%3D", "%3d"),
	}
}

// TestSecretAcrossTheCutIsScrubbed puts the token and the blob across the 2 KiB limit of the body of an error: the
// cut must not leave a prefix of a secret.
func TestSecretAcrossTheCutIsScrubbed(t *testing.T) {
	for name, secret := range map[string]string{"token": testToken, "blob": testBlob} {
		for _, shift := range []int{1, len(secret) / 2, len(secret) - 1} {
			t.Run(fmt.Sprintf("%s at %d", name, shift), func(t *testing.T) {
				body := strings.Repeat("x", maxErrorBody-shift) + secret + strings.Repeat("y", 100)
				srv := newServer(t, replying(http.StatusBadRequest, body))

				err := search(newClient(t, srv.URL))

				if err == nil {
					t.Fatal(errNoIssue)
				}
				msg := err.Error()
				for _, prefix := range []string{secret[:4], secret[:len(secret)/2]} {
					if strings.Contains(msg, prefix) {
						t.Errorf("a prefix %q of the %s is in the message", prefix, name)
					}
				}
				if !strings.HasSuffix(msg, "...") {
					t.Errorf("the body should be cut: %d bytes", len(msg))
				}
			})
		}
	}
}

func TestCutKeepsRunes(t *testing.T) {
	srv := newServer(t, replying(http.StatusBadRequest, strings.Repeat("é", maxErrorBody)))

	err := search(newClient(t, srv.URL))

	if err == nil || !utf8.ValidString(err.Error()) {
		t.Errorf("the message is not valid UTF-8: %q", err)
	}
}

// TestEscapedFormsOfASpecialTokenNeverLeak echoes a token that needs escaping in every form.
func TestEscapedFormsOfASpecialTokenNeverLeak(t *testing.T) {
	const token = "a/b+c d&e=f%gé" //nolint:gosec // G101: a sentinel, not a credential
	forms := []string{
		token, url.QueryEscape(token), url.PathEscape(token),
		strings.ToLower(url.QueryEscape(token)), strings.ToLower(url.PathEscape(token)),
	}
	srv := newServer(t, replying(http.StatusBadGateway, strings.Join(forms, " | ")))
	scripted := prompt.NewScripted(prompt.Answer{Text: testEmail}, prompt.Answer{Text: token})
	c, err := newForge(srv.URL, storeWith(t, nil, scripted)).RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = search(c)

	if err == nil {
		t.Fatal(errNoIssue)
	}
	for _, form := range forms {
		if strings.Contains(err.Error(), form) {
			t.Errorf("the form %q of the token is in %q", form, err)
		}
	}
}
