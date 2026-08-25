package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/XiovV/calich/server/internal/httpresponse"
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

func forbidden(message string) errorResponse {
	return errorResponse{http.StatusForbidden, "forbidden", message}
}

// conflict takes an explicit code, mirroring unauthorized — a 409 covers
// more than one kind of "the request conflicts with the resource's current
// state", so callers still need to say which.
func conflict(code, message string) errorResponse {
	return errorResponse{http.StatusConflict, code, message}
}

// unauthorized takes an explicit code because the auth paths distinguish
// "invalid_credentials" from a plain "unauthorized".
func unauthorized(code, message string) errorResponse {
	return errorResponse{http.StatusUnauthorized, code, message}
}

// rateLimited renders AuthRateLimiter's ErrAuthRateLimitExceeded (#240,
// ADR-0070) — always the same code and message regardless of which bucket
// tripped or whether the submitted Email names a real account, so a 429
// here can never become a User-enumeration oracle.
func rateLimited(message string) errorResponse {
	return errorResponse{http.StatusTooManyRequests, "rate_limited", message}
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

// decodeJSON decodes r's body as JSON into a zero-valued T, writing a 400
// and reporting false if it isn't valid JSON — the shared "decode or 400"
// read every handler with a JSON request body starts with, mirroring
// respondError's bool-return convention:
//
//	req, ok := decodeJSON[createEventRequest](w, r)
//	if !ok {
//		return
//	}
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return v, false
	}
	return v, true
}
