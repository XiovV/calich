package caldavserver

import (
	"net/http"
	"strings"
	"testing"
)

// TestGetCalendarObject_AttachURI_IsAbsolute verifies #142's fix: a native
// CalDAV client resolves ATTACH's URI as plain iCalendar text with no
// request to fall back on, unlike managed-attachments-server-URL's WebDAV
// href, so it must already carry a scheme and host rather than a bare path.
func TestGetCalendarObject_AttachURI_IsAbsolute(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	getResp := getObject(t, env, path)
	defer getResp.Body.Close()
	body := readBody(t, getResp)

	wantURI := env.srv.URL + attachmentDownloadPath(managedID)
	if !strings.Contains(body, "ATTACH") || !strings.Contains(body, wantURI) {
		t.Fatalf("expected ATTACH to carry the absolute URI %q, got:\n%s", wantURI, body)
	}
}

// TestGetCalendarObject_AttachURI_HonorsForwardedHeaders verifies the
// reverse-proxy path: X-Forwarded-Proto/X-Forwarded-Host, when present,
// take precedence over the request's own scheme and Host (#142).
func TestGetCalendarObject_AttachURI_HonorsForwardedHeaders(t *testing.T) {
	env := newTestCalDAVEnv(t)
	master := createTestMaster(t, env, "evt-1")
	path := calendarObjectPath(env.userID, env.calendarID, master.ID)

	addResp := postAction(t, env, path, "action=attachment-add", "text/plain", "notes.txt", "hello world")
	managedID := addResp.Header.Get("Cal-Managed-ID")
	addResp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, env.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build GET request: %v", err)
	}
	req.SetBasicAuth("admin", env.appPasswordSecret)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "calendar.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)

	wantURI := "https://calendar.example.com" + attachmentDownloadPath(managedID)
	if !strings.Contains(body, wantURI) {
		t.Fatalf("expected ATTACH to carry the forwarded-header URI %q, got:\n%s", wantURI, body)
	}
}
