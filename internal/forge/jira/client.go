package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	// maxBody caps what is read from a response.
	maxBody = 32 << 20
	// maxErrorBody caps the part of a failed response that is shown in the error.
	maxErrorBody = 2 << 10
	// maxResults is the page size of a search, which is also the default of Jira. Like Knope, only the first page is
	// used.
	maxResults = 50

	// The activities complete "Problem communicating with Jira while ..." (Knope's wording).
	activityIssues      = "querying for issues"
	activityTransitions = "getting transitions"
	activityTransition  = "transitioning issue"

	codeAPIRequest  = "remeris::jira::api_request_error"
	codeDeserialize = "remeris::jira::deserialize"
	codeTransition  = "remeris::issues::jira::transition"
	codeIssueKey    = "remeris::jira::invalid_issue_key"
)

// Issue is an issue of a search.
type Issue struct {
	Key     string
	Summary string
}

// Transition is a move of an issue to another status.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client talks to the API of one Jira site, sending the credentials of the user as a Basic Authorization header. Get
// one from [Forge.RequireAuthentication]. It is safe for use by several goroutines.
type Client struct {
	cfg Config
	// base is the URL of the site without trailing slashes.
	base string
	// scrub removes the token and the Basic blob from every error.
	scrub *httpx.Scrubber
	http  *http.Client
}

// Config returns the `[jira]` settings of the client.
func (c *Client) Config() Config { return c.cfg }

// SearchIssues returns the issues of the project that are in status, the first page of the search (spec F19.3).
// The status and the project are quoted in the JQL, so a status such as `In Progress` works and a hostile value
// cannot add a clause.
func (c *Client) SearchIssues(ctx context.Context, status string) ([]Issue, error) {
	body := map[string]any{
		"jql":        "status = " + quoteJQL(status) + " AND project = " + quoteJQL(c.cfg.Project),
		"fields":     []string{"summary"},
		"maxResults": maxResults,
	}
	var resp struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", body, &resp, activityIssues); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(resp.Issues))
	for _, i := range resp.Issues {
		issues = append(issues, Issue{Key: i.Key, Summary: i.Fields.Summary})
	}
	return issues, nil
}

// Transitions lists the transitions that are open for the issue.
func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	path, err := transitionsPath(key)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Transitions []Transition `json:"transitions"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &resp, activityTransitions); err != nil {
		return nil, err
	}
	return resp.Transitions, nil
}

// TransitionIssue moves the issue to status: it lists the transitions of the issue, takes the one whose name is
// status (an exact match) and applies it. No such transition is an error with the code
// remeris::issues::jira::transition, and nothing is changed.
func (c *Client) TransitionIssue(ctx context.Context, key, status string) error {
	path, err := transitionsPath(key)
	if err != nil {
		return err
	}
	transitions, err := c.Transitions(ctx, key)
	if err != nil {
		return err
	}
	var found *Transition
	for i := range transitions {
		if transitions[i].Name == status {
			found = &transitions[i]
			break
		}
	}
	if found == nil {
		return diag.New("The specified transition name was not found in the Jira project").
			WithCode(codeTransition).
			WithHelp("The `transition` field in TransitionJiraIssue must correspond to a valid transition in the Jira project")
	}
	body := map[string]any{"transition": map[string]string{"id": found.ID}}
	return c.do(ctx, http.MethodPost, path, body, nil, activityTransition)
}

// transitionsPath is the path of the transitions of an issue. The key is escaped as one path segment, so that it
// cannot reach another endpoint.
func transitionsPath(key string) (string, error) {
	if key == "" || key == "." || key == ".." {
		return "", diag.New("The Jira issue key is not valid").WithCode(codeIssueKey)
	}
	return "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions", nil
}

// quoteJQL writes s as a JQL string literal.
func quoteJQL(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// do sends one request and decodes the JSON response into out, which may be nil to ignore the body.
//
// path is relative to the site and starts with a slash. body, when not nil, is sent as JSON. activity completes the
// "while ..." of the errors. Every error is a diagnostic without the credentials in it.
func (c *Client) do(ctx context.Context, method, path string, body, out any, activity string) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return c.WrapError(activity, err)
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, payload)
	if err != nil {
		return c.WrapError(activity, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", httpx.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return c.WrapError(activity, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return c.statusError(activity, resp, data)
	}
	switch {
	case readErr != nil:
		return c.WrapError(activity, readErr)
	case len(data) > maxBody:
		return c.WrapError(activity, errors.New("the response is larger than "+strconv.Itoa(maxBody>>20)+" MiB"))
	case out == nil:
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return c.scrub.Diag(diag.New("Could not deserialize response from Jira into JSON: " + err.Error()).
			WithCode(codeDeserialize).WithCause(err))
	}
	return nil
}

// statusError is the diagnostic for a response that is not a success. Knope never looks at the status.
func (c *Client) statusError(activity string, resp *http.Response, body []byte) error {
	if len(body) > maxErrorBody {
		body = append(body[:maxErrorBody:maxErrorBody], "..."...)
	}
	inner, ok := httpx.StatusError(resp)
	if !ok {
		inner = "unexpected HTTP status " + resp.Status
	}
	d := apiError(activity, inner+" with body: "+string(body))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		d = d.WithHelp("Check the email address and the API token: delete the email and jira_token files in the " +
			"remeris config directory to be asked for them again.")
	}
	return c.scrub.Diag(d)
}

// WrapError turns an error of a request into the diagnostic Knope shows (spec F19):
//
//	Problem communicating with Jira while {activity}: {error}
//
// An error that is already a diagnostic, such as the refusal of a redirect, is kept. The credentials are removed from
// the messages. A nil err gives nil.
func (c *Client) WrapError(activity string, err error) error {
	if err == nil {
		return nil
	}
	var d *diag.Error
	if errors.As(err, &d) {
		return c.scrub.Diag(d)
	}
	return c.scrub.Diag(apiError(activity, err.Error()).WithCause(err))
}

func apiError(activity, inner string) *diag.Error {
	return diag.New("Problem communicating with Jira while " + activity + ": " + inner).WithCode(codeAPIRequest)
}
