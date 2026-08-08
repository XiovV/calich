package caldavserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// parseCalendarBody decodes resp's body as an iCalendar object, for tests
// that need to mutate a GET's VEVENTs and PUT them back.
func parseCalendarBody(t *testing.T, resp *http.Response) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(resp.Body).Decode()
	if err != nil {
		t.Fatalf("decode calendar object: %v", err)
	}
	return cal
}

// postAction issues a raw POST against a calendar object resource's
// ?action= query, carrying body as the raw file content with contentType
// and a filename Content-Disposition (RFC 8607's upload shape, distinct
// from the REST API's multipart one — #133, ADR-0040).
func postAction(t *testing.T, env testCalDAVEnv, path, query, contentType, filename, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, env.srv.URL+path+"?"+query, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build POST request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if filename != "" {
		req.Header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}
	req.SetBasicAuth("admin", env.appPasswordSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s?%s: %v", path, query, err)
	}
	return resp
}

// createTestMaster is the fixture every test in this file starts from: a
// non-recurring Master under env.calendarID.
func createTestMaster(t *testing.T, env testCalDAVEnv, id string) repository.Event {
	t.Helper()
	master, err := env.eventService.Create(context.Background(), env.userID, id, service.EventWrite{
		CalendarID: env.calendarID,
		Title:      "Standup",
		Start:      time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	return master
}

func TestAttachmentAdd_RidAbsent_Returns201WithManagedIDLocationAndETag(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	resp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	managedID := resp.Header.Get("Cal-Managed-ID")
	if managedID == "" {
		t.Fatalf("expected Cal-Managed-ID header")
	}
	if loc := resp.Header.Get("Location"); loc != attachmentDownloadPath(managedID) {
		t.Fatalf("expected Location %q, got %q", attachmentDownloadPath(managedID), loc)
	}
	if resp.Header.Get("ETag") == "" {
		t.Fatalf("expected a fresh ETag header")
	}
}

func TestAttachmentAdd_RidM_TreatedAsSeries(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	resp := postAction(t, env, path, "action=attachment-add&rid=M", "text/plain", "notes.txt", "hello world")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestAttachmentAdd_RidSpecificRecurrenceID_RejectedWithPreconditionError(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	resp := postAction(t, env, path, "action=attachment-add&rid=2026-06-08T09:00:00Z", "text/plain", "notes.txt", "hello world")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 Precondition Failed, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestAttachmentUpdate_ReplacesBytes(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "version one")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	updateResp := postAction(t, env, path, "action=attachment-update&managed-id="+managedID, "text/plain", "notes.txt", "version two")
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", updateResp.StatusCode, readBody(t, updateResp))
	}
	if got := updateResp.Header.Get("Cal-Managed-ID"); got != managedID {
		t.Fatalf("expected the managed-id to stay %q, got %q", managedID, got)
	}

	_, file, err := env.attachmentService.Download(context.Background(), env.userID, managedID)
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read attachment: %v", err)
	}
	if string(got) != "version two" {
		t.Fatalf("expected replaced bytes %q, got %q", "version two", got)
	}
}

func TestAttachmentUpdate_RidPresent_Rejected(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "version one")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	// Even rid=M — the RFC's own "master" token — must be rejected on
	// attachment-update: managed-id alone already names one Attachment
	// unambiguously (ADR-0040).
	resp := postAction(t, env, path, "action=attachment-update&managed-id="+managedID+"&rid=M", "text/plain", "notes.txt", "version two")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 Precondition Failed, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
}

func TestAttachmentRemove_Returns204_AndATTACHDisappears(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	getBefore := getObject(t, env, path)
	bodyBefore := readBody(t, getBefore)
	getBefore.Body.Close()
	if !strings.Contains(bodyBefore, managedID) {
		t.Fatalf("expected ATTACH carrying %q before removal, got:\n%s", managedID, bodyBefore)
	}

	removeResp := postAction(t, env, path, "action=attachment-remove&managed-id="+managedID, "", "", "")
	defer removeResp.Body.Close()
	if removeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", removeResp.StatusCode, readBody(t, removeResp))
	}

	getAfter := getObject(t, env, path)
	bodyAfter := readBody(t, getAfter)
	getAfter.Body.Close()
	if strings.Contains(bodyAfter, managedID) {
		t.Fatalf("expected ATTACH carrying %q to disappear after removal, got:\n%s", managedID, bodyAfter)
	}
}

func getObject(t *testing.T, env testCalDAVEnv, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build GET request: %v", err)
	}
	req.SetBasicAuth("admin", env.appPasswordSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func TestAttachmentAdd_SerializesOntoMasterAndEveryOverride(t *testing.T) {
	env := newTestCalDAVEnv(t)
	ctx := context.Background()

	master, err := env.eventService.Create(ctx, env.userID, "evt-1", service.EventWrite{
		CalendarID: env.calendarID,
		Title:      "Standup",
		Rrule:      "FREQ=WEEKLY",
		Start:      time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create master: %v", err)
	}
	recurrenceID := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	if _, err := env.eventService.Create(ctx, env.userID, "evt-1-override", service.EventWrite{
		CalendarID:   env.calendarID,
		Title:        "Standup (moved)",
		Start:        time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 8, 10, 30, 0, 0, time.UTC),
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	}); err != nil {
		t.Fatalf("create override: %v", err)
	}

	path := calendarObjectPath(env.userID, env.calendarID, master.ID)
	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()
	if managedID == "" {
		t.Fatalf("expected a managed-id")
	}

	getResp := getObject(t, env, path)
	body := readBody(t, getResp)
	getResp.Body.Close()

	if count := strings.Count(body, "MANAGED-ID="+managedID); count != 2 {
		t.Fatalf("expected ATTACH on both the master and the override VEVENT (2 occurrences of MANAGED-ID=%s), got %d in:\n%s", managedID, count, body)
	}
}

func TestPut_OmittingATTACH_DoesNotDeleteAttachments(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	// PutSeries (service/event.go) updates the master row in place rather
	// than delete-and-recreate, so a PUT that carries no ATTACH at all must
	// leave the Attachment row untouched (ADR-0040's load-bearing claim,
	// pinned here).
	getResp := getObject(t, env, path)
	cal := parseCalendarBody(t, getResp)
	getResp.Body.Close()
	for _, ev := range cal.Events() {
		ev.Props.Del("ATTACH")
	}

	putResp := rawPut(t, env, path, cal, "")
	if putResp.StatusCode != http.StatusNoContent && putResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected PUT to succeed, got %d: %s", putResp.StatusCode, readBody(t, putResp))
	}
	putResp.Body.Close()

	after := getObject(t, env, path)
	bodyAfter := readBody(t, after)
	after.Body.Close()
	if !strings.Contains(bodyAfter, managedID) {
		t.Fatalf("expected the Attachment to survive a PUT omitting ATTACH, got:\n%s", bodyAfter)
	}
}

func TestPut_ForeignManagedID_DoesNotCreateAnAttachment(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	getResp := getObject(t, env, path)
	cal := parseCalendarBody(t, getResp)
	getResp.Body.Close()

	events := cal.Events()
	if len(events) == 0 {
		t.Fatalf("expected a VEVENT")
	}
	fakeManagedID := "not-a-real-attachment-id"
	attachProp := ical.NewProp(ical.PropAttach)
	attachProp.SetValueType(ical.ValueURI)
	attachProp.Value = attachmentDownloadPath(fakeManagedID)
	attachProp.Params.Set("MANAGED-ID", fakeManagedID)
	events[0].Props.Add(attachProp)

	putResp := rawPut(t, env, path, cal, "")
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusNoContent && putResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected PUT to succeed (unmodeled ATTACH is dropped per ADR-0026), got %d", putResp.StatusCode)
	}

	updatedMaster, _, err := env.eventService.GetSeries(context.Background(), env.userID, master.ID)
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if len(updatedMaster.Attachments) != 0 {
		t.Fatalf("expected no Attachment to be created from a foreign MANAGED-ID, got %d", len(updatedMaster.Attachments))
	}
}
