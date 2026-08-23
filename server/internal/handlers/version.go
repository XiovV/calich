package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/XiovV/calich/server/internal/version"
)

// Version reports the build label this binary was linked with (#256,
// ADR-0072). Public and unauthenticated like Health, and deliberately a
// route of its own rather than a field on it: Health answers "is this
// process serving requests right now" and is volatile by design, while
// this answers "what code is this" and is constant for the process's
// whole life.
func Version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"version": version.Version})
}
