package httpx

import (
	"net/http"
	"strconv"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	codeAPIRequest  = "remeris::api_request_error"
	codeAPIResponse = "remeris::api_response_error"
	issuesURL       = "https://github.com/celest-io/remeris"
)

// StatusError renders a failed response the way reqwest's Display of a status error does, for example
//
//	HTTP status client error (404 Not Found) for url (https://api.github.com/repos/o/r)
//
// It returns false when the status is neither 4xx nor 5xx (reqwest's error_for_status only fails for those).
func StatusError(resp *http.Response) (string, bool) {
	var kind string
	switch {
	case resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError:
		kind = "client"
	case resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600:
		kind = "server"
	default:
		return "", false
	}
	status := strconv.Itoa(resp.StatusCode)
	if text := http.StatusText(resp.StatusCode); text != "" {
		status += " " + text
	}
	msg := "HTTP status " + kind + " error (" + status + ")"
	if resp.Request != nil && resp.Request.URL != nil {
		msg += " for url (" + resp.Request.URL.Redacted() + ")"
	}
	return msg, true
}

// NewAPIRequestError is Knope's ApiRequestError: a request to service failed while doing activity.
// err is the already rendered reason. The message is
//
//	Trouble communicating with {service} while {activity}: {err}
func NewAPIRequestError(service, activity, err string) *diag.Error {
	return diag.New("Trouble communicating with " + service + " while " + activity + ": " + err).
		WithCode(codeAPIRequest).
		WithHelp("There was a problem communicating with " + service +
			", this may be a network issue or a permissions issue.")
}

// NewTransportError wraps an error returned by the HTTP client (no response was received).
// The cause stays reachable with errors.Is and errors.As.
func NewTransportError(service, activity string, cause error) *diag.Error {
	return NewAPIRequestError(service, activity, cause.Error()).WithCause(cause)
}

// NewStatusError wraps a 4xx or 5xx response whose body has been read into body:
//
//	Trouble communicating with {service} while {activity}: {StatusError} with body: {body}
//
// It returns nil when resp is not an error status, like reqwest's error_for_status.
func NewStatusError(service, activity string, resp *http.Response, body string) *diag.Error {
	status, failed := StatusError(resp)
	if !failed {
		return nil
	}
	return NewAPIRequestError(service, activity, status+" with body: "+body)
}

// NewDecodeError reports a response from service that could not be decoded:
//
//	Trouble decoding the response from {service} while {activity}: {cause}
//
// service is "GitHub" or, for Gitea, "the Gitea instance at {host}". Callers may replace the code
// with WithCode (Knope used github::api_response_error and gitea::api_response_error).
func NewDecodeError(service, activity string, cause error) *diag.Error {
	return diag.New("Trouble decoding the response from " + service + " while " + activity + ": " + cause.Error()).
		WithCode(codeAPIResponse).
		WithHelp("Failure to decode a response from " + service +
			" is probably a bug. Please report it at " + issuesURL).
		WithCause(cause)
}
