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
// The reason phrase is the one of the Rust http crate (see reasonPhrase). A status code without a phrase
// prints as "499 <unknown status code>", as reqwest does.
//
// The URL is printed with [url.URL.Redacted], so a password in it shows as xxxxx. reqwest prints the URL as is;
// remeris does not leak a secret into a message.
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
	status := strconv.Itoa(resp.StatusCode) + " " + reasonPhrase(resp.StatusCode)
	msg := "HTTP status " + kind + " error (" + status + ")"
	if resp.Request != nil && resp.Request.URL != nil {
		msg += " for url (" + resp.Request.URL.Redacted() + ")"
	}
	return msg, true
}

// phraseOverrides are the 4xx and 5xx codes whose reason phrase in the Rust http crate (src/status.rs) differs
// from Go's http.StatusText. The crate has no phrase for 509 or 427, where Go has none either.
var phraseOverrides = map[int]string{
	http.StatusRequestEntityTooLarge:        "Payload Too Large",
	http.StatusRequestURITooLong:            "URI Too Long",
	http.StatusRequestedRangeNotSatisfiable: "Range Not Satisfiable",
}

// reasonPhrase returns the http crate's canonical reason for code, or "<unknown status code>".
func reasonPhrase(code int) string {
	if text, ok := phraseOverrides[code]; ok {
		return text
	}
	if text := http.StatusText(code); text != "" {
		return text
	}
	return "<unknown status code>"
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
	return NewDecodeErrorFor(service, service, activity, cause)
}

// NewDecodeErrorFor is [NewDecodeError] for a service that Knope names differently in the message and in the help.
// Gitea's message says "Gitea" and its help says "the Gitea instance at {host}".
func NewDecodeErrorFor(service, helpService, activity string, cause error) *diag.Error {
	return diag.New("Trouble decoding the response from " + service + " while " + activity + ": " + cause.Error()).
		WithCode(codeAPIResponse).
		WithHelp("Failure to decode a response from " + helpService +
			" is probably a bug. Please report it at " + issuesURL).
		WithCause(cause)
}
