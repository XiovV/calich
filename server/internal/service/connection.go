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
	"time"

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
	// ErrInvalidDisconnectDisposition is returned by Disconnect when the
	// disposition names neither DisconnectKeep nor DisconnectDelete — there
	// is no default (#295, mirroring ADR-0037's account-deletion rule):
	// guessing "delete" silently destroys Calendars that may hold local
	// colours, Default reminders, Shares and Events created here, and may be
	// the User's last copy if they are leaving the Provider.
	ErrInvalidDisconnectDisposition = errors.New(`disposition must be "keep" or "delete"`)
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
// googleClient (google.go). The Calendar picker (#286, connection_picker.go)
// lives here too rather than in its own service — it's the same Connection,
// the same googleClient, and the same "is this account still usable" guard.
type ConnectionService struct {
	connections *repository.ConnectionRepository
	states      connectStateCodec
	google      *googleClient
	// calendars is what ImportCalendars hands a picked calendar to, to
	// become a Linked Calendar (#286) — the same CreateSubscribed write path
	// SubscribeService uses to give a brand new Calendar a Source at
	// creation time.
	calendars *CalendarService
	// events is what a Refresh reads a Linked Calendar's existing series
	// from and reconciles a fetch's mapped result into (#287, #288) — the same
	// ListSeriesByCalendar/ListStoredReminders/ReconcileSubscribedSeries
	// SubscribeService's own doRefresh drives, since "what a Calendar
	// already stores" and "apply this reconciled result" don't care which
	// Source kind produced the incoming side.
	events *EventService
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
	// now is a Refresh's clock (#287, #288) — always time.Now outside a
	// test, mirroring SubscribeService's own now field.
	now func() time.Time
	// connectionRefreshInterval is the poller cadence a Delta Refresh
	// reschedules against (#288) — config.Config.ConnectionRefreshInterval,
	// or DefaultConnectionRefreshInterval when unset.
	connectionRefreshInterval time.Duration
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

// withGoogleEndpoints overrides the four URLs googleClient calls, in place
// of Google's real ones — the other half of the same test seam
// withGoogleHTTPClient provides the transport for.
func withGoogleEndpoints(authorizeURL, tokenURL, userinfoURL, calendarListURL string) ConnectionOption {
	return func(s *ConnectionService) {
		s.google.authorizeURL = authorizeURL
		s.google.tokenURL = tokenURL
		s.google.userinfoURL = userinfoURL
		s.google.calendarListURL = calendarListURL
	}
}

// withGoogleEventsURL overrides events.list's own base URL (#287), in place
// of Google's real one — withGoogleEndpoints' sibling, kept separate since
// events.list is scoped to one calendar and every other endpoint isn't.
func withGoogleEventsURL(eventsURL string) ConnectionOption {
	return func(s *ConnectionService) { s.google.eventsURL = eventsURL }
}

// withConnectionNow overrides a Refresh's clock (#288) — tests use it to
// prove a failed Delta Refresh reschedules next_refresh_at with backoff.
func withConnectionNow(now func() time.Time) ConnectionOption {
	return func(s *ConnectionService) { s.now = now }
}

// withConnectionRefreshInterval overrides the Delta Refresh poll cadence
// (#288), in place of DefaultConnectionRefreshInterval.
func withConnectionRefreshInterval(d time.Duration) ConnectionOption {
	return func(s *ConnectionService) { s.connectionRefreshInterval = d }
}

// NewConnectionService builds a ConnectionService. configured is
// config.Config.GoogleConfigured() — computed once by the caller (graph.go)
// rather than re-derived here from clientID/clientSecret/encryptionKey, so
// there is exactly one place that decides whether the Google Provider is
// usable, and Settings' Connect button (which reads the same GoogleConfigured
// call) can never disagree with what Connect/Callback actually refuse.
func NewConnectionService(connections *repository.ConnectionRepository, states connectStateCodec, calendars *CalendarService, events *EventService, clientID, clientSecret, encryptionKey string, configured bool, opts ...ConnectionOption) *ConnectionService {
	s := &ConnectionService{
		connections:   connections,
		states:        states,
		google:        newGoogleClient(clientID, clientSecret),
		calendars:     calendars,
		events:        events,
		encryptionKey: encryptionKey,
		configured:    configured,
		now:           time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.connectionRefreshInterval <= 0 {
		s.connectionRefreshInterval = DefaultConnectionRefreshInterval
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

// AccountEmailsByIDs resolves connectionIDs' account Emails, keyed by id —
// the sidebar's join (#286) that groups Linked Calendars under one heading
// per Connection, since a Calendar's own Source carries only the
// Connection's id. Never a Connection's tokens, which this doesn't even
// select for.
func (s *ConnectionService) AccountEmailsByIDs(ctx context.Context, connectionIDs []int64) (map[int64]string, error) {
	connections, err := s.connections.ListByIDs(ctx, connectionIDs)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	emails := make(map[int64]string, len(connections))
	for id, c := range connections {
		emails[id] = c.AccountEmail
	}
	return emails, nil
}

// StatusesByIDs resolves connectionIDs' current Status, keyed by id (#291)
// — CalendarHandler's own batched join for surfacing "this Linked
// Calendar's Connection needs reconnecting" on every affected row without a
// query per Calendar, mirroring AccountEmailsByIDs' own shape and batching.
func (s *ConnectionService) StatusesByIDs(ctx context.Context, connectionIDs []int64) (map[int64]repository.ConnectionStatus, error) {
	connections, err := s.connections.ListByIDs(ctx, connectionIDs)
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	statuses := make(map[int64]repository.ConnectionStatus, len(connections))
	for id, c := range connections {
		statuses[id] = c.Status
	}
	return statuses, nil
}

// Disposition choices Disconnect accepts (#295). There is no default:
// deletion is unrecoverable and the mirror may be the User's last copy, so
// the choice is always explicit.
const (
	// DisconnectKeep leaves every Linked Calendar in place as an ordinary
	// owned Calendar — the Source dropped (a cascade of removing the
	// Connection row) and the Provider ids cleared from its Events.
	DisconnectKeep = "keep"
	// DisconnectDelete removes every Linked Calendar this Connection
	// produced, and with each one its Events.
	DisconnectDelete = "delete"
)

// DisconnectCalendarImpact is one Linked Calendar a Disconnect would touch
// (#295): enough for the confirmation to name what a "delete" disposition
// costs, and to say so explicitly when other people hold a Share.
type DisconnectCalendarImpact struct {
	ID         string
	Name       string
	ShareCount int
}

// DisconnectImpact is every Linked Calendar a Connection produced, across
// every Workspace (#295) — what the disconnect confirmation renders before
// the User picks a disposition.
type DisconnectImpact struct {
	LinkedCalendars []DisconnectCalendarImpact
}

// DisconnectImpact reports what disconnecting userID's Connection id would
// affect (#295): every Linked Calendar it produced and how many Shares each
// carries. ErrConnectionNotFound if the id doesn't name one of theirs.
func (s *ConnectionService) DisconnectImpact(ctx context.Context, userID, id int64) (DisconnectImpact, error) {
	if _, err := s.connections.GetByID(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return DisconnectImpact{}, ErrConnectionNotFound
		}
		return DisconnectImpact{}, fmt.Errorf("get connection: %w", err)
	}

	links, err := s.calendars.ListConnectionLinks(ctx, id)
	if err != nil {
		return DisconnectImpact{}, fmt.Errorf("list linked calendars: %w", err)
	}

	impact := DisconnectImpact{LinkedCalendars: make([]DisconnectCalendarImpact, 0, len(links))}
	for _, link := range links {
		shareCount, err := s.calendars.ShareCount(ctx, link.CalendarID)
		if err != nil {
			return DisconnectImpact{}, err
		}
		impact.LinkedCalendars = append(impact.LinkedCalendars, DisconnectCalendarImpact{
			ID: link.CalendarID, Name: link.CalendarName, ShareCount: shareCount,
		})
	}
	return impact, nil
}

// Disconnect removes userID's Connection with the given id, applying
// disposition to every Linked Calendar it produced first (#295):
//
//   - DisconnectKeep clears the Provider ids from each Calendar's Events,
//     leaving ordinary owned Calendars behind. The Source itself is dropped
//     by the ON DELETE CASCADE on removing the Connection row — so "keep"'s
//     only explicit work is the Event cleanup.
//   - DisconnectDelete removes each Linked Calendar outright, cascading to
//     its Events.
//
// The per-Calendar steps run before the Connection row is removed (once it's
// gone, ListConnectionLinks can no longer find them) and are each
// idempotent, so a failure partway through is safely re-runnable rather than
// leaving a half-applied disposition that corrupts anything.
func (s *ConnectionService) Disconnect(ctx context.Context, userID, id int64, disposition string) error {
	if disposition != DisconnectKeep && disposition != DisconnectDelete {
		return ErrInvalidDisconnectDisposition
	}

	if _, err := s.connections.GetByID(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("get connection: %w", err)
	}

	links, err := s.calendars.ListConnectionLinks(ctx, id)
	if err != nil {
		return fmt.Errorf("list linked calendars: %w", err)
	}
	for _, link := range links {
		switch disposition {
		case DisconnectKeep:
			if err := s.events.ClearProviderIdentity(ctx, link.CalendarID); err != nil {
				return fmt.Errorf("clear provider identity for calendar %s: %w", link.CalendarID, err)
			}
		case DisconnectDelete:
			if err := s.calendars.Delete(ctx, userID, link.CalendarID); err != nil {
				return fmt.Errorf("delete linked calendar %s: %w", link.CalendarID, err)
			}
		}
	}

	if err := s.connections.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrConnectionNotFound
		}
		return fmt.Errorf("disconnect: %w", err)
	}
	return nil
}
