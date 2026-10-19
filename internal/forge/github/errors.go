package github

import (
	"errors"
	"io"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v92/github"

	"github.com/celest-io/remeris/internal/httpx"
)

// WrapError turns an error returned by go-github into the diagnostic Knope shows (spec F16.0):
//
//	Trouble communicating with GitHub while {activity}: {status error} with body: {body}
//
// for an error response from GitHub, including the rate-limit errors, and
//
//	Trouble communicating with GitHub while {activity}: {error}
//
// for anything else, such as a connection failure. The body is the raw response text, as Knope prints it, or
// GitHub's message when the body is gone. activity completes "while ...", for example "creating a release".
// A nil err gives nil.
func WrapError(activity string, err error) error {
	if err == nil {
		return nil
	}
	var resp *http.Response
	var message string
	var (
		errResp *gh.ErrorResponse
		rate    *gh.RateLimitError
		abuse   *gh.AbuseRateLimitError
	)
	switch {
	case errors.As(err, &errResp):
		resp, message = errResp.Response, errResp.Message
	case errors.As(err, &rate):
		resp, message = rate.Response, rate.Message
	case errors.As(err, &abuse):
		resp, message = abuse.Response, abuse.Message
	}
	if resp != nil {
		if d := httpx.NewStatusError(service, activity, resp, responseBody(resp, message)); d != nil {
			return d.WithCause(err)
		}
	}
	return httpx.NewTransportError(service, activity, err)
}

// responseBody reads the body that go-github put back after parsing the error, and puts it back again. It
// falls back to message when the body is empty or gone.
func responseBody(resp *http.Response, message string) string {
	if resp.Body == nil {
		return message
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(string(data)))
	if err != nil || len(data) == 0 {
		return message
	}
	return string(data)
}
