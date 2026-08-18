package caldavserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calich/server/internal/service"
)

func report(t *testing.T, srv *httptest.Server, path, username, password, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest("REPORT", srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build REPORT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Depth", "1")
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("REPORT %s: %v", path, err)
	}
	return resp
}

const syncCollectionInitial = `<?xml version="1.0" encoding="UTF-8"?>
<d:sync-collection xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:sync-token/>
  <d:sync-level>1</d:sync-level>
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
</d:sync-collection>`

func syncCollectionWithToken(token string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<d:sync-collection xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:sync-token>%s</d:sync-token>
  <d:sync-level>1</d:sync-level>
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
</d:sync-collection>`, token)
}

func extractSyncToken(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<sync-token")
	if start == -1 {
		t.Fatalf("no sync-token in response:\n%s", body)
	}
	rest := body[start:]
	open := strings.Index(rest, ">") + 1
	end := strings.Index(rest, "</sync-token>")
	if end == -1 {
		t.Fatalf("malformed sync-token in response:\n%s", body)
	}
	return rest[open:end]
}

func TestSyncCollection_InitialSync_ReturnsAllLiveObjects(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	resp := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	wantHref := calendarObjectPath(env.userID, env.calendarID, created.ID)
	if !strings.Contains(body, wantHref) {
		t.Fatalf("expected response to contain href %q, got:\n%s", wantHref, body)
	}
	if !strings.Contains(body, "<sync-token") {
		t.Fatalf("expected a sync-token, got:\n%s", body)
	}
}

func TestSyncCollection_UnchangedCalendar_ReturnsNothingNew(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	initial := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	initialBody := readBody(t, initial)
	initial.Body.Close()
	token := extractSyncToken(t, initialBody)

	resp := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionWithToken(token))
	defer resp.Body.Close()
	body := readBody(t, resp)
	if strings.Contains(body, "<href>") {
		t.Fatalf("expected no changed objects when nothing mutated since the last sync, got:\n%s", body)
	}
	if !strings.Contains(body, ">"+token+"</sync-token>") {
		t.Fatalf("expected the sync-token to stay %q when nothing changed, got:\n%s", token, body)
	}
}

func TestSyncCollection_AfterMutation_ReturnsOnlyTheChangedObject(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	if _, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	initial := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	initialBody := readBody(t, initial)
	initial.Body.Close()
	token := extractSyncToken(t, initialBody)

	created2, err := env.eventService.Create(ctx, env.userID, "evt-2", service.EventWrite{CalendarID: env.calendarID, Title: "Lunch", Start: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}

	resp := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionWithToken(token))
	defer resp.Body.Close()
	body := readBody(t, resp)

	wantHref := calendarObjectPath(env.userID, env.calendarID, created2.ID)
	if !strings.Contains(body, wantHref) {
		t.Fatalf("expected response to contain the new object's href %q, got:\n%s", wantHref, body)
	}
	unwantedHref := calendarObjectPath(env.userID, env.calendarID, "evt-1")
	if strings.Contains(body, unwantedHref) {
		t.Fatalf("expected the unchanged object %q to be absent, got:\n%s", unwantedHref, body)
	}
}

func TestSyncCollection_AfterDelete_ReportsDeletedHrefAsNotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	initial := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	initialBody := readBody(t, initial)
	initial.Body.Close()
	token := extractSyncToken(t, initialBody)

	if err := env.eventService.Delete(ctx, env.userID, created.ID); err != nil {
		t.Fatalf("delete event: %v", err)
	}

	resp := report(t, env.srv, path, "admin@example.com", env.appPasswordSecret, syncCollectionWithToken(token))
	defer resp.Body.Close()
	body := readBody(t, resp)

	wantHref := calendarObjectPath(env.userID, env.calendarID, created.ID)
	if !strings.Contains(body, wantHref) {
		t.Fatalf("expected response to contain the deleted object's href %q, got:\n%s", wantHref, body)
	}
	if !strings.Contains(body, "404 Not Found") {
		t.Fatalf("expected the deleted object reported with a 404 status, got:\n%s", body)
	}
}

// TestSyncCollection_AfterCalDAVDelete_ReportsDeletedHrefAsNotFound exercises
// the same removal reporting as
// TestSyncCollection_AfterDelete_ReportsDeletedHrefAsNotFound, but through
// the actual CalDAV DELETE endpoint (#67) rather than calling
// EventService.Delete directly, matching #67's "tests over the HTTP seam"
// requirement.
func TestSyncCollection_AfterCalDAVDelete_ReportsDeletedHrefAsNotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	created, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{CalendarID: env.calendarID, Title: "Standup", Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	collectionPath := calendarPath(env.userID, env.calendarID)
	initial := report(t, env.srv, collectionPath, "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	initialBody := readBody(t, initial)
	initial.Body.Close()
	token := extractSyncToken(t, initialBody)

	client := newTestCalDAVClient(t, env)
	objectPath := calendarObjectPath(env.userID, env.calendarID, created.ID)
	if err := client.RemoveAll(ctx, objectPath); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	resp := report(t, env.srv, collectionPath, "admin@example.com", env.appPasswordSecret, syncCollectionWithToken(token))
	defer resp.Body.Close()
	body := readBody(t, resp)

	if !strings.Contains(body, objectPath) {
		t.Fatalf("expected response to contain the deleted object's href %q, got:\n%s", objectPath, body)
	}
	if !strings.Contains(body, "404 Not Found") {
		t.Fatalf("expected the deleted object reported with a 404 status, got:\n%s", body)
	}
}

// TestSyncCollection_ObjectWithAttachment_MatchesGetAttachAndETag is #215's
// regression test: a sync-collection REPORT asking for calendar-data used to
// build its objects from Masters SyncSince never hydrated Attachments onto,
// so ATTACH was missing and the ETag computed over the object disagreed with
// the one GET reports for the same resource — leaving an incrementally
// syncing client with a copy missing the file, under an ETag no other CalDAV
// operation ever returns (ADR-0025, ADR-0040).
func TestSyncCollection_ObjectWithAttachment_MatchesGetAttachAndETag(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	objectPath := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, objectPath, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	getResp := getObject(t, env, objectPath)
	getBody := readBody(t, getResp)
	getETag := strings.Trim(getResp.Header.Get("ETag"), `"`)
	getResp.Body.Close()
	wantURI := env.srv.URL + attachmentDownloadPath(managedID)
	if !strings.Contains(getBody, wantURI) {
		t.Fatalf("expected GET to carry ATTACH %q, got:\n%s", wantURI, getBody)
	}
	if getETag == "" {
		t.Fatalf("expected GET to report an ETag, got none")
	}

	resp := report(t, env.srv, calendarPath(env.userID, env.calendarID), "admin@example.com", env.appPasswordSecret, syncCollectionInitial)
	defer resp.Body.Close()
	body := readBody(t, resp)

	if !strings.Contains(body, wantURI) {
		t.Fatalf("expected sync-collection calendar-data to carry the same ATTACH %q as GET, got:\n%s", wantURI, body)
	}
	if !strings.Contains(body, ">&#34;"+getETag+"&#34;<") && !strings.Contains(body, `>"`+getETag+`"<`) {
		t.Fatalf("expected sync-collection to report GET's ETag %q, got:\n%s", getETag, body)
	}
}
