package caldavserver

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func downloadAttachment(t *testing.T, env testCalDAVEnv, managedID, username, password string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.srv.URL+attachmentDownloadPath(managedID), nil)
	if err != nil {
		t.Fatalf("build GET request: %v", err)
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", attachmentDownloadPath(managedID), err)
	}
	return resp
}

func TestAttachmentDownload_AuthenticatesAndCarriesSecurityHeaders(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	resp := downloadAttachment(t, env, managedID, "admin@example.com", env.appPasswordSecret)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	body := readBody(t, resp)
	if body != "hello world" {
		t.Fatalf("expected the uploaded bytes back, got %q", body)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff, got %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("expected Content-Security-Policy: sandbox, got %q", got)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("expected a Content-Disposition: attachment, got %q", resp.Header.Get("Content-Disposition"))
	}
}

func TestAttachmentDownload_MissingCredentials_401(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	resp := downloadAttachment(t, env, managedID, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAttachmentDownload_NoAccessToCalendar_NotFound(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	stranger, err := env.users.Create(context.Background(), "stranger", "stranger@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	created, err := env.appPasswordService.Create(context.Background(), stranger.ID, "Test device")
	if err != nil {
		t.Fatalf("create stranger app password: %v", err)
	}

	resp := downloadAttachment(t, env, managedID, "stranger@example.com", created.Secret)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for a User with no Access to the Calendar, got %d", resp.StatusCode)
	}
}

func TestAttachmentAddAndRemove_BumpETagAndCTag(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	ctagBefore, err := env.eventService.CalendarCTag(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("ctag before: %v", err)
	}
	getBefore := getObject(t, env, path)
	etagBefore := getBefore.Header.Get("ETag")
	getBefore.Body.Close()

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	etagAfterAdd := addResp.Header.Get("ETag")
	addResp.Body.Close()

	ctagAfterAdd, err := env.eventService.CalendarCTag(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("ctag after add: %v", err)
	}
	if ctagAfterAdd == ctagBefore {
		t.Fatalf("expected CTag to bump after attachment-add, stayed %d", ctagAfterAdd)
	}
	if etagAfterAdd == etagBefore || etagAfterAdd == "" {
		t.Fatalf("expected a fresh ETag after attachment-add, got %q (was %q)", etagAfterAdd, etagBefore)
	}

	removeResp := postAction(t, env, path, "action=attachment-remove&managed-id="+managedID, "", "", "")
	removeResp.Body.Close()

	ctagAfterRemove, err := env.eventService.CalendarCTag(context.Background(), env.userID, env.calendarID)
	if err != nil {
		t.Fatalf("ctag after remove: %v", err)
	}
	if ctagAfterRemove == ctagAfterAdd {
		t.Fatalf("expected CTag to bump again after attachment-remove, stayed %d", ctagAfterRemove)
	}

	getAfter := getObject(t, env, path)
	etagAfterRemove := getAfter.Header.Get("ETag")
	getAfter.Body.Close()
	if etagAfterRemove == etagAfterAdd {
		t.Fatalf("expected ETag to change after attachment-remove")
	}
}
