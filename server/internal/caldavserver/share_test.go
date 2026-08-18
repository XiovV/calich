// share_test.go covers #101's CalDAV surface for a Calendar reached through
// a Share (ADR-0034): it appears in the accessor's own calendar-home-set at
// their own principal path (ADR-0035), advertises read-only privileges for
// a Viewer and read-write for an Editor, refuses a Viewer's writes outright
// rather than merely discouraging them, and — since neither principal has
// Reminders of their own by default (ADR-0064) — serves byte-identical
// objects to every principal with Access until one of them sets a Reminder.
package caldavserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
	"github.com/XiovV/calich/server/internal/service"
)

// rawPutAsUser is rawPut generalized to any principal's credentials, so a
// shared user distinct from "admin" can be exercised.
func rawPutAsUser(t *testing.T, env testCalDAVEnv, path, username, secret string, cal *ical.Calendar, ifMatch string) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		t.Fatalf("encode calendar: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, env.srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", ical.MIMEType)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	req.SetBasicAuth(username, secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

// rawDeleteAsUser is rawDelete generalized to any principal's credentials.
func rawDeleteAsUser(t *testing.T, env testCalDAVEnv, path, username, secret, ifMatch string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, env.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build DELETE request: %v", err)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	req.SetBasicAuth(username, secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func newTestCalDAVClientAs(t *testing.T, env testCalDAVEnv, username, secret string) *caldav.Client {
	t.Helper()
	httpClient := webdav.HTTPClientWithBasicAuth(env.srv.Client(), username, secret)
	client, err := caldav.NewClient(httpClient, env.srv.URL)
	if err != nil {
		t.Fatalf("new caldav client: %v", err)
	}
	return client
}

func TestSharedCalendar_AppearsInAccessorsOwnHomeSet(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	homeSetPath := fmt.Sprintf("/dav/%d/calendars/", editorID)
	resp := propfind(t, env.srv, homeSetPath, "editor@example.com", editorSecret, "1", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantCollection := fmt.Sprintf("/dav/%d/calendars/%s/", editorID, env.calendarID)
	if !strings.Contains(body, wantCollection) {
		t.Fatalf("expected the shared calendar to appear at the accessor's own principal path %q, got:\n%s", wantCollection, body)
	}
	if strings.Contains(body, fmt.Sprintf("/dav/%d/calendars/%s/", env.userID, env.calendarID)) {
		t.Fatalf("expected the accessor's home-set to name only their own path, not the owner's, got:\n%s", body)
	}
}

func TestSharedCalendar_ViewerPrivilegeSetIsReadOnly(t *testing.T) {
	env := newTestCalDAVEnv(t)
	viewerID, viewerSecret := env.addSharedUser(t, "viewer", repository.RoleViewer)

	path := calendarPath(viewerID, env.calendarID)
	resp := propfind(t, env.srv, path, "viewer@example.com", viewerSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "<write") {
		t.Fatalf("expected a Viewer's privilege set to omit write, got:\n%s", body)
	}
	if !strings.Contains(body, "<read") {
		t.Fatalf("expected a Viewer's privilege set to still grant read, got:\n%s", body)
	}
}

func TestSharedCalendar_EditorPrivilegeSetIsReadWrite(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	path := calendarPath(editorID, env.calendarID)
	resp := propfind(t, env.srv, path, "editor@example.com", editorSecret, "0", propfindCurrentUserPrivilegeSet)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "<write") {
		t.Fatalf("expected an Editor's privilege set to still grant write, got:\n%s", body)
	}
}

func TestSharedCalendar_ViewerPutIsForbidden(t *testing.T) {
	env := newTestCalDAVEnv(t)
	viewerID, viewerSecret := env.addSharedUser(t, "viewer", repository.RoleViewer)

	master := repository.Event{
		ID:        "viewer-attempt",
		Title:     "Should not be created",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, _, err := icalendar.SeriesToICal(master, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}

	path := calendarObjectPath(viewerID, env.calendarID, master.ID)
	resp := rawPutAsUser(t, env, path, "viewer@example.com", viewerSecret, cal, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected a Viewer's PUT to be refused with 403, got %d", resp.StatusCode)
	}

	if _, err := env.eventService.Get(context.Background(), env.userID, master.ID); err == nil {
		t.Fatalf("expected the Viewer's PUT to not have created the event")
	}
}

func TestSharedCalendar_ViewerDeleteIsForbidden(t *testing.T) {
	env := newTestCalDAVEnv(t)
	viewerID, viewerSecret := env.addSharedUser(t, "viewer", repository.RoleViewer)

	created, err := env.eventService.Create(context.Background(), env.userID, "owner-evt", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarObjectPath(viewerID, env.calendarID, created.ID)
	resp := rawDeleteAsUser(t, env, path, "viewer@example.com", viewerSecret, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected a Viewer's DELETE to be refused with 403, got %d", resp.StatusCode)
	}

	if _, err := env.eventService.Get(context.Background(), env.userID, created.ID); err != nil {
		t.Fatalf("expected the event to still exist after the refused delete: %v", err)
	}
}

func TestSharedCalendar_EditorCanCreateModifyDelete(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	master := repository.Event{
		ID:        "editor-evt",
		Title:     "Standup",
		Start:     time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	cal, _, err := icalendar.SeriesToICal(master, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	path := calendarObjectPath(editorID, env.calendarID, master.ID)

	putResp := rawPutAsUser(t, env, path, "editor@example.com", editorSecret, cal, "")
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK && putResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected the Editor's create to succeed, got %d", putResp.StatusCode)
	}

	stored, err := env.eventService.Get(context.Background(), env.userID, master.ID)
	if err != nil {
		t.Fatalf("expected the owner to see the editor's created event: %v", err)
	}

	updated := stored
	updated.Title = "Standup (edited by editor)"
	updatedCal, _, err := icalendar.SeriesToICal(updated, nil, icalendar.CalDAVTarget(attachmentsBasePath))
	if err != nil {
		t.Fatalf("seriesToICal: %v", err)
	}
	editResp := rawPutAsUser(t, env, path, "editor@example.com", editorSecret, updatedCal, "")
	editResp.Body.Close()
	if editResp.StatusCode != http.StatusOK && editResp.StatusCode != http.StatusNoContent && editResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected the Editor's edit to succeed, got %d", editResp.StatusCode)
	}

	stored, err = env.eventService.Get(context.Background(), env.userID, master.ID)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	if stored.Title != "Standup (edited by editor)" {
		t.Fatalf("expected the edit to take effect, got %+v", stored)
	}

	delResp := rawDeleteAsUser(t, env, path, "editor@example.com", editorSecret, "")
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected the Editor's delete to succeed, got %d", delResp.StatusCode)
	}

	if _, err := env.eventService.Get(context.Background(), env.userID, master.ID); err == nil {
		t.Fatalf("expected the event to be gone after the editor's delete")
	}
}

func TestSharedCalendar_ObjectBytesIdenticalForOwnerAndEditor(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	created, err := env.eventService.Create(context.Background(), env.userID, "shared-evt", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	ownerClient := newTestCalDAVClient(t, env)
	ownerObj, err := ownerClient.GetCalendarObject(context.Background(), calendarObjectPath(env.userID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("owner GetCalendarObject: %v", err)
	}

	editorClient := newTestCalDAVClientAs(t, env, "editor@example.com", editorSecret)
	editorObj, err := editorClient.GetCalendarObject(context.Background(), calendarObjectPath(editorID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("editor GetCalendarObject: %v", err)
	}

	var ownerBuf, editorBuf bytes.Buffer
	if err := ical.NewEncoder(&ownerBuf).Encode(ownerObj.Data); err != nil {
		t.Fatalf("encode owner object: %v", err)
	}
	if err := ical.NewEncoder(&editorBuf).Encode(editorObj.Data); err != nil {
		t.Fatalf("encode editor object: %v", err)
	}

	if ownerBuf.String() != editorBuf.String() {
		t.Fatalf("expected identical object bytes for owner and editor, got:\nowner:\n%s\neditor:\n%s", ownerBuf.String(), editorBuf.String())
	}
	if ownerObj.ETag != editorObj.ETag {
		t.Fatalf("expected identical ETag for owner and editor, got %q vs %q", ownerObj.ETag, editorObj.ETag)
	}
}

// TestSharedCalendar_ObjectServesEachPrincipalsOwnReminders is #210's CalDAV
// boundary (ADR-0064): a GET of a shared Calendar object carries the
// requesting principal's own Reminders as VALARMs, not the Calendar Owner's
// — the Owner's reminder on a shared Event reaches nobody else's phone, the
// Editor's own reminder (SetReminders, not the Event write) never touches
// the Owner's object, and the two principals' bytes (and ETags) diverge
// exactly where their Reminders do.
func TestSharedCalendar_ObjectServesEachPrincipalsOwnReminders(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	created, err := env.eventService.Create(context.Background(), env.userID, "shared-evt", service.EventWrite{
		CalendarID: env.calendarID, Title: "Bin day",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := env.eventService.SetReminders(context.Background(), env.userID, created.ID, []repository.Reminder{{OffsetMinutes: 10, Channel: "notification"}}); err != nil {
		t.Fatalf("owner set reminders: %v", err)
	}

	editorClient := newTestCalDAVClientAs(t, env, "editor@example.com", editorSecret)
	before, err := editorClient.GetCalendarObject(context.Background(), calendarObjectPath(editorID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("editor GetCalendarObject before own reminders: %v", err)
	}
	var beforeBuf bytes.Buffer
	if err := ical.NewEncoder(&beforeBuf).Encode(before.Data); err != nil {
		t.Fatalf("encode object before own reminders: %v", err)
	}
	if strings.Contains(beforeBuf.String(), "VALARM") {
		t.Fatalf("expected the editor with no Reminders of their own to see no VALARM, got:\n%s", beforeBuf.String())
	}

	if _, err := env.eventService.SetReminders(context.Background(), editorID, created.ID, []repository.Reminder{{OffsetMinutes: 120, Channel: "email"}}); err != nil {
		t.Fatalf("editor set reminders: %v", err)
	}

	editorAfter, err := editorClient.GetCalendarObject(context.Background(), calendarObjectPath(editorID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("editor GetCalendarObject after own reminders: %v", err)
	}
	var editorAfterBuf bytes.Buffer
	if err := ical.NewEncoder(&editorAfterBuf).Encode(editorAfter.Data); err != nil {
		t.Fatalf("encode editor object after own reminders: %v", err)
	}
	if !strings.Contains(editorAfterBuf.String(), "TRIGGER:-PT7200S") {
		t.Fatalf("expected the editor's own reminder (120 minutes) as a VALARM, got:\n%s", editorAfterBuf.String())
	}
	if beforeBuf.String() == editorAfterBuf.String() {
		t.Fatalf("expected the editor's own reminders to change their object bytes")
	}
	if before.ETag == editorAfter.ETag {
		t.Fatalf("expected the editor's own reminders to change their ETag")
	}

	ownerClient := newTestCalDAVClient(t, env)
	ownerObj, err := ownerClient.GetCalendarObject(context.Background(), calendarObjectPath(env.userID, env.calendarID, created.ID))
	if err != nil {
		t.Fatalf("owner GetCalendarObject: %v", err)
	}
	var ownerBuf bytes.Buffer
	if err := ical.NewEncoder(&ownerBuf).Encode(ownerObj.Data); err != nil {
		t.Fatalf("encode owner object: %v", err)
	}
	if !strings.Contains(ownerBuf.String(), "TRIGGER:-PT600S") {
		t.Fatalf("expected the owner to still see their own reminder (10 minutes), unaffected by the editor's, got:\n%s", ownerBuf.String())
	}
	if ownerBuf.String() == editorAfterBuf.String() {
		t.Fatalf("expected the owner's object to diverge from the editor's — each carries only their own Reminders")
	}
	if ownerObj.ETag == editorAfter.ETag {
		t.Fatalf("expected the owner's ETag to diverge from the editor's")
	}
}

// A User with Access to a shared Calendar still cannot address it under
// another principal's path (ADR-0035's prefix-check isolation guarantee) —
// only their own path resolves it, even though they can read the Calendar
// itself.
func TestSharedCalendar_CannotAddressOwnersPrincipalPath(t *testing.T) {
	env := newTestCalDAVEnv(t)
	_, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "editor@example.com", editorSecret, "0", propfindDisplayName)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, "200 OK") {
		t.Fatalf("expected the editor to be refused addressing the owner's principal path, got %d:\n%s", resp.StatusCode, body)
	}
}

// sync-collection behaviour is unchanged for a shared Calendar (#101's
// acceptance criteria): an Editor addressing it at their own path gets the
// same live-object listing and sync-token behaviour any owner would.
func TestSharedCalendar_SyncCollectionWorksAtAccessorsOwnPath(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	created, err := env.eventService.Create(context.Background(), env.userID, "sync-evt", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	path := calendarPath(editorID, env.calendarID)
	resp := report(t, env.srv, path, "editor@example.com", editorSecret, syncCollectionInitial)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	wantHref := calendarObjectPath(editorID, env.calendarID, created.ID)
	if !strings.Contains(body, wantHref) {
		t.Fatalf("expected response to contain href %q, got:\n%s", wantHref, body)
	}
	if !strings.Contains(body, "<sync-token") {
		t.Fatalf("expected a sync-token, got:\n%s", body)
	}
}

func TestSharedCalendar_CTagUnchangedForSharedAccess(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, _ := env.addSharedUser(t, "editor", repository.RoleEditor)

	if _, err := env.eventService.Create(context.Background(), env.userID, "ctag-evt", service.EventWrite{
		CalendarID: env.calendarID, Title: "Standup",
		Start: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	ownerCtag, err := env.eventService.CalendarCTag(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("owner ctag: %v", err)
	}
	editorCtag, err := env.eventService.CalendarCTag(context.Background(), editorID, env.calendarID)
	if err != nil {
		t.Fatalf("editor ctag: %v", err)
	}
	if ownerCtag != editorCtag {
		t.Fatalf("expected the CTag to be identical regardless of the accessing principal, got owner=%d editor=%d", ownerCtag, editorCtag)
	}
}

// Rename/recolour are Owner-only management operations (ADR-0034) that stay
// unavailable to an Editor over CalDAV, consistent with ADR-0032 already
// keeping them out of PROPPATCH for a Subscribed Calendar.
func TestSharedCalendar_EditorCannotRename(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	path := calendarPath(editorID, env.calendarID)
	resp := proppatch(t, env.srv, path, "editor@example.com", editorSecret, proppatchSetDisplayName("Renamed by editor"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "403 Forbidden") {
		t.Fatalf("expected an Editor's rename attempt to be refused with a 403 Forbidden propstat (Owner-only, ADR-0034), got:\n%s", body)
	}

	cal, err := env.calendarService.Get(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if cal.Name == "Renamed by editor" {
		t.Fatalf("expected the rename to not have taken effect")
	}
}

// An Editor's (or Viewer's) recolour attempt no longer means "rename or
// recolour, Owner-only" as one rule (ADR-0038 amends ADR-0034 here): it
// succeeds, but writes the Editor's own colour override rather than the
// Calendar's own colour, which stays the Owner's.
func TestSharedCalendar_EditorRecolor_WritesOwnOverride_NotTheCalendarsColor(t *testing.T) {
	env := newTestCalDAVEnv(t)
	editorID, editorSecret := env.addSharedUser(t, "editor", repository.RoleEditor)

	path := calendarPath(editorID, env.calendarID)
	resp := proppatch(t, env.srv, path, "editor@example.com", editorSecret, proppatchSetCalendarColor("#654321"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "200 OK") {
		t.Fatalf("expected the Editor's colour override to succeed with a 200 OK propstat, got:\n%s", body)
	}

	ownerCal, err := env.calendarService.Get(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if ownerCal.Color != "#12809CFF" {
		t.Fatalf("expected the Owner's own colour to stay unchanged, got %q", ownerCal.Color)
	}

	editorView, err := env.calendarService.AccessWithColor(context.Background(), editorID, env.calendarID)
	if err != nil {
		t.Fatalf("resolve editor's access: %v", err)
	}
	if editorView.Color != "#654321FF" {
		t.Fatalf("expected the Editor's resolved colour to reflect their override, got %q", editorView.Color)
	}
}
