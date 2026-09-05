// connection.go implements Connect a Google account's HTTP surface (#285):
// initiating the OAuth round trip, completing it on Google's redirect back,
// and listing/disconnecting the resulting Connections.
package handlers

import (
	"net/http"
	"time"

	"github.com/XiovV/calich/server/internal/caldavserver"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/httpresponse"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// settingsConnectionsPath is where a User lands after the Google round trip,
// whichever way it ends — the Connections Section, addressable like every
// other Settings Section (ADR-0049).
const settingsConnectionsPath = "/settings/connections"

type ConnectionHandler struct {
	connections *service.ConnectionService
}

func NewConnectionHandler(connections *service.ConnectionService) *ConnectionHandler {
	return &ConnectionHandler{connections: connections}
}

type connectionResponse struct {
	ID           int64     `json:"id"`
	Provider     string    `json:"provider"`
	AccountEmail string    `json:"account_email"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

func toConnectionResponse(c repository.Connection) connectionResponse {
	return connectionResponse{
		ID:           c.ID,
		Provider:     string(c.Provider),
		AccountEmail: c.AccountEmail,
		Status:       string(c.Status),
		CreatedAt:    c.CreatedAt,
	}
}

type connectResponse struct {
	URL string `json:"url"`
}

var connectErrors = []errorCase{
	{service.ErrGoogleNotConfigured, notFound("google provider is not configured on this instance")},
}

var disconnectErrors = []errorCase{
	{service.ErrConnectionNotFound, notFound("connection not found")},
}

// Connect returns the URL to send the browser to, to authorize a Connection
// to a Google account (#285) — a JSON response rather than a redirect itself, since
// the caller is the SPA's own fetch, not a browser navigation; the SPA does
// the navigating (window.location) once it has the URL.
func (h *ConnectionHandler) Connect(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	authorizeURL, err := h.connections.Connect(userID, connectRedirectURI(r))
	if respondError(w, err, connectErrors, "failed to start connecting a google account") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, connectResponse{URL: authorizeURL})
}

// Callback is where Google redirects the browser back to once the User has
// consented or declined (#285) — an unauthenticated top-level navigation
// carrying neither an Authorization header nor a cookie scoped to this path,
// so the User it's acting for comes from the signed "state" parameter
// Connect minted, not from a Session. Either way it ends with a redirect
// back into the SPA at settingsConnectionsPath, so a User watches one
// continuous flow rather than a blank success/failure page.
func (h *ConnectionHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Redirect(w, r, settingsConnectionsPath+"?connect_error=declined", http.StatusFound)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, settingsConnectionsPath+"?connect_error=invalid", http.StatusFound)
		return
	}

	if _, err := h.connections.Callback(r.Context(), code, state, connectRedirectURI(r)); err != nil {
		http.Redirect(w, r, settingsConnectionsPath+"?connect_error=failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, settingsConnectionsPath+"?connected=1", http.StatusFound)
}

func (h *ConnectionHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	connections, err := h.connections.List(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list connections")
		return
	}

	responses := make([]connectionResponse, len(connections))
	for i, c := range connections {
		responses[i] = toConnectionResponse(c)
	}

	httpresponse.JSON(w, http.StatusOK, responses)
}

func (h *ConnectionHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	userID := httpauth.MustUserID(r.Context())

	id, ok := parseInt64Param(w, r, "id")
	if !ok {
		return
	}

	err := h.connections.Disconnect(r.Context(), userID, id)
	if respondError(w, err, disconnectErrors, "failed to disconnect") {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// connectRedirectURI must be byte-identical between the authorize request
// Connect builds and the token exchange Callback performs (Google rejects
// the exchange otherwise), so both derive it the same way, from the request
// that's actually in flight, rather than from fixed config — a self-hosted
// instance's own public origin is exactly what varies per deployment
// (ADR-0051). Built on caldavserver.RequestBaseURL rather than a second copy
// of its proxy-trust logic (X-Forwarded-Proto/X-Forwarded-Host precedence).
func connectRedirectURI(r *http.Request) string {
	return caldavserver.RequestBaseURL(r) + "/api/connections/google/callback"
}
