package handlers

import (
	"errors"
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpresponse"
)

// errorResponse is how one service sentinel is rendered to a client.
type errorResponse struct {
	status  int
	code    string
	message string
}

// errorCase pairs a sentinel with its rendering. Handlers hold an ordered
// slice rather than a map so matching order is deterministic and identical to
// the switch statements these replace.
type errorCase struct {
	err      error
	response errorResponse
}

func badRequest(message string) errorResponse {
	return errorResponse{http.StatusBadRequest, "invalid_request", message}
}

func notFound(message string) errorResponse {
	return errorResponse{http.StatusNotFound, "not_found", message}
}

// unauthorized takes an explicit code because the auth paths distinguish
// "invalid_credentials" from a plain "unauthorized".
func unauthorized(code, message string) errorResponse {
	return errorResponse{http.StatusUnauthorized, code, message}
}

// alsoHandling returns base extended with extra, leaving base untouched — for
// a handler that renders everything a sibling does plus a few of its own.
// base is matched first.
func alsoHandling(base []errorCase, extra ...errorCase) []errorCase {
	combined := make([]errorCase, 0, len(base)+len(extra))
	combined = append(combined, base...)
	return append(combined, extra...)
}

// respondError renders err as the first matching case, or as a 500 carrying
// fallback when nothing matches. It reports whether it wrote a response, so
// callers read as:
//
//	if respondError(w, err, createEventErrors, "failed to create event") {
//		return
//	}
//
// A nil err writes nothing and reports false.
func respondError(w http.ResponseWriter, err error, cases []errorCase, fallback string) bool {
	if err == nil {
		return false
	}
	for _, c := range cases {
		if errors.Is(err, c.err) {
			httpresponse.Error(w, c.response.status, c.response.code, c.response.message)
			return true
		}
	}
	httpresponse.Error(w, http.StatusInternalServerError, "internal_error", fallback)
	return true
}
