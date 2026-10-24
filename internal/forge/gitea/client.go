package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	// maxBody caps what is read from a response.
	maxBody = 32 << 20
)

// Client talks to the API of one Gitea instance, sending the token of the user in the Authorization header. Get one
// from [Forge.RequireAuthentication]. It is safe for use by several goroutines.
type Client struct {
	cfg Config
	// base is the API base URL, "{host}/api/v1".
	base  string
	token string
	// scrub removes the token from every error.
	scrub *httpx.Scrubber
	http  *http.Client
}

// Config returns the `[gitea]` settings of the client.
func (c *Client) Config() Config { return c.cfg }

// do sends one request and decodes the JSON response into out, which may be nil to ignore the body.
//
// path is relative to the API base and starts with a slash. body, when not nil, is sent as JSON. activity
// completes the "while ..." of the errors, for example "creating a release". Every error is a diagnostic
// without the token in it.
func (c *Client) do(
	ctx context.Context, method, path string, query url.Values, body, out any, activity string,
) error {
	u, err := url.Parse(c.base + path)
	if err != nil {
		return c.WrapError(activity, err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	var payload io.Reader
	if body != nil {
		// Like serde_json, do not escape <, > and & (release notes hold them).
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return c.WrapError(activity, err)
		}
		payload = bytes.NewReader(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return c.WrapError(activity, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", httpx.UserAgent)
	req.Header.Set("Authorization", "token "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return c.WrapError(activity, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// As in Knope, a body that cannot be read is an empty body in a status error.
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if d := httpx.NewStatusError(c.cfg.Host, activity, resp, string(data)); d != nil {
		return c.scrub.Diag(d)
	}
	if readErr != nil {
		return c.WrapError(activity, readErr)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return c.scrub.Diag(httpx.NewDecodeErrorFor(service, "the Gitea instance at "+c.cfg.Host, activity, err).
			WithCode(codeAPIResponse))
	}
	return nil
}

// WrapError turns an error of a request into the diagnostic Knope shows (spec F17):
//
//	Trouble communicating with {host} while {activity}: {error}
//
// where host is the host as written in the configuration. An error that is already a diagnostic, such as the refusal
// of a redirect, is kept. The token is removed from the messages. A nil err gives nil.
func (c *Client) WrapError(activity string, err error) error {
	if err == nil {
		return nil
	}
	var d *diag.Error
	if errors.As(err, &d) {
		return c.scrub.Diag(d)
	}
	return c.scrub.Diag(httpx.NewTransportError(c.cfg.Host, activity, err))
}
