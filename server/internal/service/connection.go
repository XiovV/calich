// connection.go implements Connect a Google account (#285): a User
// authorizes one Google account and sees it listed as a Connection, showing
// its Email and whether it is live, expired or revoked. No Linked Calendar
// exists yet — that's the Calendar picker's, a later ticket's.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrGoogleNotConfigured is returned by Connect/Callback when the
	// self-hoster hasn't supplied Google OAuth credentials and a Connections
	// encryption key (config.Config.GoogleConfigured, ADR-0051) — the
	// Provider is absent from the UI entirely in that case, so reaching
	// either method at all means a stale client or a direct API call.
	ErrGoogleNotConfigured = errors.New("google provider is not configured on this instance")
	// ErrConnectionNotFound is returned by Disconnect for an id that doesn't
	// name a Connection belonging to the caller.
	ErrConnectionNotFound = errors.New("connection not found")
	// ErrConnectCallbackInvalidState is returned by Callback when state
	// doesn't parse as one Connect issued — expired, tampered, or replayed
	// past its ten-minute window.
	ErrConnectCallbackInvalidState = errors.New("invalid or expired connect callback")
	// ErrConnectAccountNotActive is returned by Callback when the User named
	// by state has since become Disabled, or still must change their
	// password, between Connect and Google's redirect back — the same two
	// checks RequireEnabledUser/RequireActiveUser enforce on every other
	// authenticated route, applied here by hand since Callback sits outside
	// RequireAuth entirely (it has no Authorization header to authenticate).
	ErrConnectAccountNotActive = errors.New("account is not active")
)

// connectStateCodec is AuthService's own signed-state pair (IssueConnectState/
// ParseConnectState) plus the two account-status checks Callback applies by
// hand (MustChangePassword/IsDisabled, both httpauth's ordinary middleware
// enforces — unavailable here since Callback carries no Authorization
// header to authenticate). Taken as a narrow interface, satisfied by
// *AuthService, so ConnectionService doesn't depend on AuthService itself.
type connectStateCodec interface {
	IssueConnectState(userID int64) (string, error)
	ParseConnectState(state string) (int64, error)
	MustChangePassword(ctx context.Context, userID int64) (bool, error)
	IsDisabled(ctx context.Context, userID int64) (bool, error)
}

// ConnectionService orchestrates Connect a Google account: it owns no
// storage beyond the repository, delegating the OAuth calls themselves to
// googleClient (google.go).
type ConnectionService struct {
	connections *repository.ConnectionRepository
	states      connectStateCodec
	google      *googleClient
	// encryptionKey is config.Config.ConnectionsEncryptionKey — a refresh
	// token is encrypted under it before Upsert and never stored raw
	// (ADR-0052).
	encryptionKey string
	// configured mirrors config.Config.GoogleConfigured: Connect/Callback
	// refuse outright when false, while List/Disconnect — plain reads and a
	// delete against this app's own database — work regardless, since an
	// existing Connection shouldn't become unreachable just because the
	// self-hoster later unset their OAuth credentials.
	configured bool
}

// ConnectionOption configures a ConnectionService beyond
// NewConnectionService's required arguments — a test-only seam, mirroring
// SubscribeOption's WithHTTPClient (#285's testing decisions). Unexported:
// the Graph-level GraphOptions of the same shape (graph.go's
// WithGoogleHTTPClient/WithGoogleEndpoints) are what every caller outside
// this package actually uses.
type ConnectionOption func(*ConnectionService)

// withGoogleHTTPClient overrides the client googleClient makes every Google
// call with, in place of http.DefaultClient. Tests use it to point at an
// httptest.Server serving canned Provider JSON.
func withGoogleHTTPClient(client *http.Client) ConnectionOption {
	return func(s *ConnectionService) { s.google.httpClient = client }
}

// withGoogleEndpoints overrides the three URLs googleClient calls, in place
// of Google's real ones — the other half of the same test seam
// withGoogleHTTPClient provides the transport for.
func withGoogleEndpoints(authorizeURL, tokenURL, userinfoURL string) ConnectionOption {
	return func(s *ConnectionService) {
		s.google.authorizeURL = authorizeURL
		s.google.tokenURL = tokenURL
		s.google.userinfoURL = userinfoURL
	}
}

// NewConnectionService builds a ConnectionService. configured is
// config.Config.GoogleConfigured() — computed once by the caller (graph.go)
// rather than re-derived here from clientID/clientSecret/encryptionKey, so
// there is exactly one place that decides whether the Google Provider is
// usable, and Settings' Connect button (which reads the same GoogleConfigured
// call) can never disagree with what Connect/Callback actually refuse.
func NewConnectionService(connections *repository.ConnectionRepository, states connectStateCodec, clientID, clientSecret, encryptionKey string, configured bool, opts ...ConnectionOption) *ConnectionService {
	s := &ConnectionService{
		connections:   connections,
		states:        states,
		google:        newGoogleClient(clientID, clientSecret),
		encryptionKey: encryptionKey,
		configured:    configured,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Connect returns the URL to send userID's browser to, to consent and
// authorize a Connection to their Google account. redirectURI is derived
// from the current request
// (handlers.ConnectionHandler) rather than fixed in config, since a
// self-hosted instance's own public origin is exactly what varies per
// deployment (ADR-0051) — it must be byte-identical to the one Callback
// later passes to the token exchange.
func (s *ConnectionService) Connect(userID int64, redirectURI string) (string, error) {
	if !s.configured {
		return "", ErrGoogleNotConfigured
	}

	state, err := s.states.IssueConnectState(userID)
	if err != nil {
		return "", fmt.Errorf("issue connect state: %w", err)
	}

	return s.google.authorizeURLFor(redirectURI, state), nil
}

// Callback completes the round trip Connect started: recovers which User
// initiated it from state, exchanges code for tokens, resolves the
// authorized account's Email, and upserts the Connection — reusing the same
// row if this account was already connected (#285, ADR-0052), never
// creating a duplicate. redirectURI must match the one Connect built its
// authorize URL with.
func (s *ConnectionService) Callback(ctx context.Context, code, state, redirectURI string) (repository.Connection, error) {
	if !s.configured {
		return repository.Connection{}, ErrGoogleNotConfigured
	}

	userID, err := s.states.ParseConnectState(state)
	if err != nil {
		return repository.Connection{}, fmt.Errorf("%w: %v", ErrConnectCallbackInvalidState, err)
	}

	// The state was minted while the User was active; re-check now, since a
	// Disable or a forced password change could have landed during the time
	// they spent on Google's own consent screen (#285).
	if disabled, err := s.states.IsDisabled(ctx, userID); err != nil {
		return repository.Connection{}, fmt.Errorf("check account status: %w", err)
	} else if disabled {
		return repository.Connection{}, ErrConnectAccountNotActive
	}
	if mustChangePassword, err := s.states.MustChangePassword(ctx, userID); err != nil {
		return repository.Connection{}, fmt.Errorf("check account status: %w", err)
	} else if mustChangePassword {
		return repository.Connection{}, ErrConnectAccountNotActive
	}

	tokens, err := s.google.exchangeCode(ctx, code, redirectURI)
	if err != nil {
		return repository.Connection{}, err
	}

	if !tokens.grantsRequiredScopes() {
		return repository.Connection{}, fmt.Errorf("%w: granted scopes %q do not cover what this app requested", ErrGoogleAuthFailed, tokens.Scope)
	}

	email, verifiedEmail, err := s.google.fetchAccountEmail(ctx, tokens.AccessToken)
	if err != nil {
		return repository.Connection{}, err
	}
	if !verifiedEmail {
		return repository.Connection{}, fmt.Errorf("%w: google account's email is not verified", ErrGoogleAuthFailed)
	}

	encryptedRefreshToken, err := encryptRefreshToken(s.encryptionKey, tokens.RefreshToken)
	if err != nil {
		return repository.Connection{}, fmt.Errorf("encrypt refresh token: %w", err)
	}

	var accessToken *string
	if tokens.AccessToken != "" {
		accessToken = &tokens.AccessToken
	}

	connection, err := s.connections.Upsert(ctx, userID, repository.ProviderGoogle, email, repository.ConnectionFields{
		AccessToken:  accessToken,
		RefreshToken: encryptedRefreshToken,
		Scopes:       tokens.Scope,
		Status:       repository.ConnectionStatusLive,
	})
	if err != nil {
		return repository.Connection{}, fmt.Errorf("upsert connection: %w", err)
	}

	return connection, nil
}

// List returns userID's Connections — never their tokens, which the caller
// (handlers.ConnectionHandler) never renders regardless.
func (s *ConnectionService) List(ctx context.Context, userID int64) ([]repository.Connection, error) {
	connections, err := s.connections.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	return connections, nil
}

// Disconnect removes userID's Connection with the given id. No Linked
// Calendar exists yet to have a disposition for (#285) — that question is
// the Calendar picker's, once it exists.
func (s *ConnectionService) Disconnect(ctx context.Context, userID, id int64) error {
	if err := s.connections.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("disconnect: %w", err)
	}
	return nil
}
