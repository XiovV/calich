package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/XiovV/calich/server/internal/repository"
)

// fakeGoogleServer stands in for Google's token, userinfo, and calendarList
// endpoints (#285/#286's testing decisions: a real local test server serving
// canned Provider JSON, not a mocked fetcher). refreshToken/email/scope are
// what the token/userinfo responses carry; tokenStatus/userinfoStatus let a
// test force a non-200 (a denied consent, a broken exchange); verifiedEmail
// overrides the userinfo response's verified_email, nil meaning true.
// calendarListItems/calendarListStatus are the picker's own fixture (#286):
// nil items renders an empty calendarList, calendarListStatus forces a
// non-200 (an expired access token). eventsByCalendar/eventsStatus/
// eventsPageSize are Full Refresh's own fixture (#287): eventsByCalendar
// keys one calendar's raw Events.list items by its external id,
// eventsStatus forces a non-200, and eventsPageSize — when set — serves
// eventsByCalendar a few items at a time via pageToken/nextPageToken, so a
// test can assert listEvents actually follows pagination rather than
// trusting a single page.
type fakeGoogleServer struct {
	*httptest.Server
	refreshToken, email, scope  string
	tokenStatus, userinfoStatus int
	verifiedEmail               *bool
	calendarListItems           []map[string]any
	calendarListStatus          int
	eventsByCalendar            map[string][]map[string]any
	eventsStatus                int
	eventsPageSize              int
	// eventsFailFirstNCalls forces the first N calls to /events to answer
	// 401 regardless of the token presented, then serve normally — a
	// Connection's cached access_token having actually expired at Google
	// (#287), which doRefresh's own retry-once-with-a-fresh-token path is
	// what a test forcing this exercises.
	eventsFailFirstNCalls, eventsCallCount int
	// nextSyncToken is the cursor /events hands back on the final page of
	// every listing (#288) — Full and Delta alike. Defaults to a non-empty
	// value so a Full Refresh always has one to store.
	nextSyncToken string
	// eventsDeltaByToken keys a Delta Refresh's changed items by the
	// syncToken the request presented, so a test can stage "since token-1,
	// this one series changed". syncTokenExpired instead makes any
	// syncToken-bearing request answer 410 GONE (a cursor Google invalidated,
	// routinely on an ACL change).
	eventsDeltaByToken map[string][]map[string]any
	syncTokenExpired   bool
	// lastSyncTokenSeen is the syncToken query param the last /events request
	// carried, "" for a Full Refresh — a test asserts the Delta path actually
	// sends the stored cursor.
	lastSyncTokenSeen string
	// eventsSummaryByCalendar keys events.list's own top-level "summary" field
	// by calendar id (#289) — the Provider's own current name for the
	// calendar, echoed on every response (Full and Delta alike), which a
	// Linked Calendar's name tracks until it's renamed here. A calendar
	// absent from this map gets no "summary" field at all, mirroring a
	// response that omitted it.
	eventsSummaryByCalendar map[string]string
}

func newFakeGoogleServer(t *testing.T) *fakeGoogleServer {
	t.Helper()

	f := &fakeGoogleServer{refreshToken: "1/fake-refresh-token", email: "someone@gmail.com", scope: "openid email https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/calendar.calendarlist.readonly", nextSyncToken: "sync-token-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if f.tokenStatus != 0 && f.tokenStatus != http.StatusOK {
			w.WriteHeader(f.tokenStatus)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access-token",
			"refresh_token": f.refreshToken,
			"scope":         f.scope,
			"expires_in":    3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if f.userinfoStatus != 0 && f.userinfoStatus != http.StatusOK {
			w.WriteHeader(f.userinfoStatus)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-access-token" {
			t.Fatalf("expected userinfo request to carry the exchanged access token, got %q", r.Header.Get("Authorization"))
		}
		verifiedEmail := true
		if f.verifiedEmail != nil {
			verifiedEmail = *f.verifiedEmail
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"email": f.email, "verified_email": verifiedEmail})
	})
	mux.HandleFunc("/calendarList", func(w http.ResponseWriter, r *http.Request) {
		if f.calendarListStatus != 0 && f.calendarListStatus != http.StatusOK {
			w.WriteHeader(f.calendarListStatus)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-access-token" {
			t.Fatalf("expected calendarList request to carry the exchanged access token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": f.calendarListItems})
	})
	mux.HandleFunc("/{calendarId}/events", func(w http.ResponseWriter, r *http.Request) {
		f.eventsCallCount++
		if f.eventsFailFirstNCalls > 0 && f.eventsCallCount <= f.eventsFailFirstNCalls {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.eventsStatus != 0 && f.eventsStatus != http.StatusOK {
			w.WriteHeader(f.eventsStatus)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fake-access-token" {
			t.Fatalf("expected events request to carry the exchanged access token, got %q", r.Header.Get("Authorization"))
		}
		if got := r.URL.Query().Get("singleEvents"); got != "false" {
			t.Fatalf("expected singleEvents=false (ADR-0053: no time window, preserve Master+instances), got %q", got)
		}

		calendarID := r.PathValue("calendarId")
		syncToken := r.URL.Query().Get("syncToken")
		f.lastSyncTokenSeen = syncToken

		var items []map[string]any
		if syncToken != "" {
			if f.syncTokenExpired {
				w.WriteHeader(http.StatusGone)
				return
			}
			items = f.eventsDeltaByToken[syncToken]
		} else {
			items = f.eventsByCalendar[calendarID]
		}

		start := 0
		if pageToken := r.URL.Query().Get("pageToken"); pageToken != "" {
			parsed, err := strconv.Atoi(pageToken)
			if err != nil {
				t.Fatalf("unexpected pageToken %q: %v", pageToken, err)
			}
			start = parsed
		}

		pageSize := f.eventsPageSize
		if pageSize <= 0 || start+pageSize >= len(items) {
			pageSize = len(items) - start
		}
		page := items[start : start+pageSize]

		body := map[string]any{"items": page}
		if start+pageSize < len(items) {
			body["nextPageToken"] = strconv.Itoa(start + pageSize)
		} else if f.nextSyncToken != "" {
			body["nextSyncToken"] = f.nextSyncToken
		}
		if summary, ok := f.eventsSummaryByCalendar[calendarID]; ok {
			body["summary"] = summary
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newTestConnectionService(t *testing.T, google *fakeGoogleServer) (*ConnectionService, *AuthService, int64) {
	t.Helper()

	g := newTestGraph(t)

	user, err := g.UserRepo.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	connections := repository.NewConnectionRepository(g.DB)
	svc := NewConnectionService(connections, g.Auth, g.Calendars, g.Events, "test-client-id", "test-client-secret", "test-encryption-key", true,
		withGoogleHTTPClient(google.Client()),
		withGoogleEndpoints(google.URL+"/authorize", google.URL+"/token", google.URL+"/userinfo", google.URL+"/calendarList"),
		withGoogleEventsURL(google.URL),
	)

	return svc, g.Auth, user.ID
}

func TestConnectionService_Connect_BuildsAuthorizeURLCarryingASignedState(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)

	authorizeURL, err := svc.Connect(userID, "https://calendar.example.com/api/connections/google/callback")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := parsed.Query()

	if got := q.Get("client_id"); got != "test-client-id" {
		t.Fatalf("expected client_id %q, got %q", "test-client-id", got)
	}
	if got := q.Get("redirect_uri"); got != "https://calendar.example.com/api/connections/google/callback" {
		t.Fatalf("expected redirect_uri to round-trip unchanged, got %q", got)
	}
	if got := q.Get("access_type"); got != "offline" {
		t.Fatalf("expected access_type=offline, got %q", got)
	}
	if got := q.Get("prompt"); got != "consent" {
		t.Fatalf("expected prompt=consent, got %q", got)
	}

	scopes := strings.Fields(q.Get("scope"))
	for _, want := range []string{"https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.calendarlist.readonly"} {
		if !slices.Contains(scopes, want) {
			t.Fatalf("expected scope to include %q, got %q", want, scopes)
		}
	}
	// The broad scope permits creating/deleting calendars and editing ACLs
	// (ADR-0050) — asking for it would overstate what this app does.
	if slices.Contains(scopes, "https://www.googleapis.com/auth/calendar") {
		t.Fatalf("expected the broad calendar scope never to be requested, got %q", scopes)
	}

	state := q.Get("state")
	if state == "" {
		t.Fatalf("expected a non-empty state")
	}
	stateUserID, err := auth.ParseConnectState(state)
	if err != nil {
		t.Fatalf("parse connect state: %v", err)
	}
	if stateUserID != userID {
		t.Fatalf("expected state to carry user id %d, got %d", userID, stateUserID)
	}
}

func TestConnectionService_Connect_RefusesWhenGoogleNotConfigured(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, userID := newTestConnectionService(t, google)
	svc.configured = false

	if _, err := svc.Connect(userID, "https://calendar.example.com/callback"); err != ErrGoogleNotConfigured {
		t.Fatalf("expected ErrGoogleNotConfigured, got %v", err)
	}
}

func TestConnectionService_Callback_CreatesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	redirectURI := "https://calendar.example.com/api/connections/google/callback"
	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	connection, err := svc.Callback(ctx, "auth-code", state, redirectURI)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	if connection.UserID != userID {
		t.Fatalf("expected connection for user %d, got %d", userID, connection.UserID)
	}
	if connection.Provider != repository.ProviderGoogle {
		t.Fatalf("expected provider %q, got %q", repository.ProviderGoogle, connection.Provider)
	}
	if connection.AccountEmail != "someone@gmail.com" {
		t.Fatalf("expected account email %q, got %q", "someone@gmail.com", connection.AccountEmail)
	}
	if connection.Status != repository.ConnectionStatusLive {
		t.Fatalf("expected status %q, got %q", repository.ConnectionStatusLive, connection.Status)
	}
	// The refresh token is never stored in the clear (#285, ADR-0052).
	if connection.RefreshToken == "1/fake-refresh-token" {
		t.Fatalf("expected the stored refresh token to be encrypted, got the raw value")
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one connection, got %d", len(list))
	}
}

func TestConnectionService_Callback_ReconnectingSameAccountReusesTheRow(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()
	redirectURI := "https://calendar.example.com/api/connections/google/callback"

	state1, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	first, err := svc.Callback(ctx, "auth-code-1", state1, redirectURI)
	if err != nil {
		t.Fatalf("first callback: %v", err)
	}

	google.refreshToken = "1/fake-refresh-token-2"
	state2, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	second, err := svc.Callback(ctx, "auth-code-2", state2, redirectURI)
	if err != nil {
		t.Fatalf("second callback: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("expected reconnect to reuse connection id %d, got %d", first.ID, second.ID)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one connection after reconnecting, got %d", len(list))
	}
}

func TestConnectionService_Callback_RefusesDisabledAccount(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	// The account is disabled after Connect issued the state but before
	// Callback runs — the window a User spends on Google's own consent
	// screen (#285).
	if _, err := auth.users.SetDisabled(ctx, userID, true); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	if _, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback"); err != ErrConnectAccountNotActive {
		t.Fatalf("expected ErrConnectAccountNotActive, got %v", err)
	}
}

func TestConnectionService_Callback_RefusesAccountThatMustChangePassword(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, _ := newTestConnectionService(t, google)
	ctx := context.Background()

	user, err := auth.users.Create(ctx, "user-b", "user-b@example.com", "hash", true)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	state, err := auth.IssueConnectState(user.ID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback"); err != ErrConnectAccountNotActive {
		t.Fatalf("expected ErrConnectAccountNotActive, got %v", err)
	}
}

// TestConnectionService_Callback_RefusesInsufficientScopes covers Google's
// granular-consent screen: a User can approve sign-in while denying Calendar
// access specifically, and the token exchange still succeeds with a
// refresh token either way (#285) — Callback must notice before ever
// showing the Connection as live.
func TestConnectionService_Callback_RefusesInsufficientScopes(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.scope = "openid email" // calendar scopes denied
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected insufficient granted scopes to fail")
	}
}

func TestConnectionService_Callback_RefusesUnverifiedEmail(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.verifiedEmail = boolPtr(false)
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected an unverified google email to fail")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestConnectionService_Callback_InvalidStateFails(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, _ := newTestConnectionService(t, google)

	if _, err := svc.Callback(context.Background(), "auth-code", "not-a-real-state", "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected an invalid state to fail")
	}
}

func TestConnectionService_Callback_TokenExchangeFailureIsSurfaced(t *testing.T) {
	google := newFakeGoogleServer(t)
	google.tokenStatus = http.StatusBadRequest
	svc, auth, userID := newTestConnectionService(t, google)

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}

	if _, err := svc.Callback(context.Background(), "auth-code", state, "https://calendar.example.com/callback"); err == nil {
		t.Fatalf("expected a token exchange failure to be surfaced")
	}
}

func TestConnectionService_Disconnect_RemovesConnection(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, auth, userID := newTestConnectionService(t, google)
	ctx := context.Background()

	state, err := auth.IssueConnectState(userID)
	if err != nil {
		t.Fatalf("issue connect state: %v", err)
	}
	connection, err := svc.Callback(ctx, "auth-code", state, "https://calendar.example.com/callback")
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	if err := svc.Disconnect(ctx, userID, connection.ID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no connections after disconnect, got %d", len(list))
	}
}

func TestConnectionService_Disconnect_NotFound(t *testing.T) {
	google := newFakeGoogleServer(t)
	svc, _, userID := newTestConnectionService(t, google)

	if err := svc.Disconnect(context.Background(), userID, 999); err != ErrConnectionNotFound {
		t.Fatalf("expected ErrConnectionNotFound, got %v", err)
	}
}
