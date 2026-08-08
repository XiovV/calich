package caldavserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// testMaxAttachmentSize and testMaxAttachmentsPerEvent are the limits
// newTestCalDAVEnv wires the Backend with — arbitrary but generous, since
// most tests aren't exercising the limits themselves (#133, ADR-0040).
const (
	testMaxAttachmentSize      int64 = 25 << 20
	testMaxAttachmentsPerEvent       = 10
)

// testCalDAVEnv is the full wiring newTestCalDAVServer and its variants share:
// the real repository/service stack against an in-memory DB behind the
// CalDAV Basic-auth middleware (ADR-0023, ADR-0024) — driven over real HTTP.
type testCalDAVEnv struct {
	srv                *httptest.Server
	userID             int64
	calendarID         string
	appPasswordSecret  string
	appPasswordService *service.AppPasswordService
	eventService       *service.EventService
	calendarService    *service.CalendarService
	attachmentService  *service.AttachmentService
	users              *repository.UserRepository
}

func newTestCalDAVEnv(t *testing.T) testCalDAVEnv {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	authService := service.NewAuthService(users, sessions, []byte("test-secret"), "", "")
	user, _, err := authService.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	calendarService := service.NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	const calendarID = "cal-1"
	if _, err := calendarService.Create(context.Background(), user.ID, calendarID, service.CalendarWrite{Name: "Personal", Color: "#12809CFF"}); err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	attachmentRepo := repository.NewAttachmentRepository(sqlDB)
	eventService := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarService, users, attachmentRepo)
	attachmentService := service.NewAttachmentService(attachmentRepo, repository.NewEventRepository(sqlDB), calendarService, eventService, attachmentstore.New(t.TempDir()), testMaxAttachmentsPerEvent)

	appPasswordService := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	created, err := appPasswordService.Create(context.Background(), user.ID, "Test device")
	if err != nil {
		t.Fatalf("create app password: %v", err)
	}

	backend := NewBackend(calendarService, eventService, attachmentService, testMaxAttachmentSize, testMaxAttachmentsPerEvent)
	handler := NewHTTPHandler(backend)

	r := chi.NewRouter()
	r.Route(pathPrefix, func(r chi.Router) {
		r.Use(httpauth.RequireCalDAVAuth(appPasswordService))
		r.Handle("/", handler)
		r.Handle("/*", handler)
	})
	r.With(httpauth.RequireCalDAVAuth(appPasswordService)).Handle("/.well-known/caldav", handler)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return testCalDAVEnv{
		srv:                srv,
		userID:             user.ID,
		calendarID:         calendarID,
		appPasswordSecret:  created.Secret,
		appPasswordService: appPasswordService,
		eventService:       eventService,
		calendarService:    calendarService,
		attachmentService:  attachmentService,
		users:              users,
	}
}

// addSharedUser mints a second User, grants them role on env.calendarID
// (env.userID stays the Owner), and gives them an app password — #101's
// fixture for exercising a shared Calendar's CalDAV surface from the
// accessor's side.
func (env testCalDAVEnv) addSharedUser(t *testing.T, username, role string) (userID int64, appPasswordSecret string) {
	t.Helper()

	other, err := env.users.Create(context.Background(), username, "hash", false)
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}

	if _, err := env.calendarService.Share(context.Background(), env.userID, env.calendarID, username, role); err != nil {
		t.Fatalf("share calendar with %q as %q: %v", username, role, err)
	}

	created, err := env.appPasswordService.Create(context.Background(), other.ID, "Test device")
	if err != nil {
		t.Fatalf("create app password for %q: %v", username, err)
	}

	return other.ID, created.Secret
}

// newTestCalDAVServer wires the real repository/service stack against an
// in-memory DB behind the CalDAV Basic-auth middleware (ADR-0023,
// ADR-0024) — the whole discovery seam, driven over real HTTP.
func newTestCalDAVServer(t *testing.T) (srv *httptest.Server, userID int64, appPasswordSecret string) {
	env := newTestCalDAVEnv(t)
	return env.srv, env.userID, env.appPasswordSecret
}

func newTestCalDAVServerWithService(t *testing.T) (srv *httptest.Server, userID int64, appPasswordSecret string, appPasswordService *service.AppPasswordService) {
	env := newTestCalDAVEnv(t)
	return env.srv, env.userID, env.appPasswordSecret, env.appPasswordService
}

func propfind(t *testing.T, srv *httptest.Server, path, username, password, depth, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest("PROPFIND", srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build PROPFIND request: %v", err)
	}
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Depth", depth)
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND %s: %v", path, err)
	}
	return resp
}

const propfindCurrentUserPrincipal = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:current-user-principal/></d:prop>
</d:propfind>`

const propfindHomeSet = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:calendar-home-set/></d:prop>
</d:propfind>`

const propfindDisplayName = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:displayname/><d:resourcetype/></d:prop>
</d:propfind>`

func TestPropfind_MissingCredentials_Returns401(t *testing.T) {
	srv, _, _ := newTestCalDAVServer(t)

	resp := propfind(t, srv, "/dav/", "", "", "0", propfindCurrentUserPrincipal)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPropfind_WrongAppPassword_Returns401(t *testing.T) {
	srv, _, _ := newTestCalDAVServer(t)

	resp := propfind(t, srv, "/dav/", "admin", "not-the-right-secret", "0", propfindCurrentUserPrincipal)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPropfind_WebLoginPassword_Returns401(t *testing.T) {
	srv, _, _ := newTestCalDAVServer(t)

	// "admin" is the bootstrap default web-login password — it must never be
	// accepted as a CalDAV credential (ADR-0024).
	resp := propfind(t, srv, "/dav/", "admin", "admin", "0", propfindCurrentUserPrincipal)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPropfind_Root_ResolvesCurrentUserPrincipal(t *testing.T) {
	srv, userID, secret := newTestCalDAVServer(t)

	resp := propfind(t, srv, "/dav/", "admin", secret, "0", propfindCurrentUserPrincipal)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantPrincipal := fmt.Sprintf("/dav/%d/", userID)
	if !strings.Contains(body, wantPrincipal) {
		t.Fatalf("expected response to contain principal path %q, got:\n%s", wantPrincipal, body)
	}
}

func TestPropfind_Principal_ResolvesCalendarHomeSet(t *testing.T) {
	srv, userID, secret := newTestCalDAVServer(t)

	principalPath := fmt.Sprintf("/dav/%d/", userID)
	resp := propfind(t, srv, principalPath, "admin", secret, "0", propfindHomeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantHomeSet := fmt.Sprintf("/dav/%d/calendars/", userID)
	if !strings.Contains(body, wantHomeSet) {
		t.Fatalf("expected response to contain calendar-home-set path %q, got:\n%s", wantHomeSet, body)
	}
}

func TestPropfind_HomeSet_ListsOneCollectionPerCalendar(t *testing.T) {
	srv, userID, secret := newTestCalDAVServer(t)

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", userID)
	resp := propfind(t, srv, homeSetPath, "admin", secret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantCollection := fmt.Sprintf("/dav/%d/calendars/cal-1/", userID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected response to list the calendar collection %q, got:\n%s", wantCollection, body)
	}
	if !strings.Contains(body, "Personal") {
		t.Fatalf("expected response to include the calendar's display name %q, got:\n%s", "Personal", body)
	}
}

func TestPropfind_SuccessfulAuth_UpdatesAppPasswordLastUsedAt(t *testing.T) {
	srv, userID, secret, appPasswordService := newTestCalDAVServerWithService(t)

	before, err := appPasswordService.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if before[0].LastUsedAt != nil {
		t.Fatalf("expected last_used_at to be unset before any CalDAV auth, got %v", before[0].LastUsedAt)
	}

	resp := propfind(t, srv, "/dav/", "admin", secret, "0", propfindCurrentUserPrincipal)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}

	after, err := appPasswordService.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list app passwords: %v", err)
	}
	if after[0].LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be set after a successful CalDAV auth")
	}
}

func TestWellKnownCalDAV_RedirectsToPrincipal(t *testing.T) {
	srv, userID, secret := newTestCalDAVServer(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/.well-known/caldav", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.SetBasicAuth("admin", secret)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/caldav: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("expected 308, got %d", resp.StatusCode)
	}

	wantPrincipal := fmt.Sprintf("/dav/%d/", userID)
	if got := resp.Header.Get("Location"); got != wantPrincipal {
		t.Fatalf("expected redirect to %q, got %q", wantPrincipal, got)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(data)
}
