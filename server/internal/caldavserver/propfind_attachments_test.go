package caldavserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const propfindManagedAttachmentsURL = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:managed-attachments-server-URL/></d:prop>
</d:propfind>`

const propfindAttachmentLimits = `<?xml version="1.0" encoding="UTF-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:max-attachment-size/><c:max-attachments-per-resource/></d:prop>
</d:propfind>`

func TestPropfind_ManagedAttachmentsServerURL_OnCalendarHome_IsPathOnlyHref(t *testing.T) {
	env := newTestCalDAVEnv(t)

	homePath := homeSetPath(env.userID)
	resp := propfind(t, env.srv, homePath, "admin@example.com", env.appPasswordSecret, "0", propfindManagedAttachmentsURL)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if !strings.Contains(body, "managed-attachments-server-URL") {
		t.Fatalf("expected managed-attachments-server-URL in response:\n%s", body)
	}
	if strings.Contains(body, "<managed-attachments-server-URL") && strings.Contains(body, "404") {
		t.Fatalf("expected managed-attachments-server-URL to be served with a 200 status, got:\n%s", body)
	}

	wantHref := fmt.Sprintf("<href xmlns=\"DAV:\">%s</href>", attachmentsBasePath)
	if !strings.Contains(body, wantHref) {
		t.Fatalf("expected a path-only href %q, got:\n%s", wantHref, body)
	}
	// Path-only: never a scheme or authority — the client substitutes those
	// itself (RFC 8607, ADR-0040).
	if strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Fatalf("expected no scheme/authority in the href, got:\n%s", body)
	}
}

func TestPropfind_ManagedAttachmentsServerURL_NotAdvertisedOnCalendarCollection(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindManagedAttachmentsURL)
	defer resp.Body.Close()

	body := readBody(t, resp)
	if strings.Contains(body, attachmentsBasePath) {
		t.Fatalf("expected managed-attachments-server-URL to be scoped to the calendar home collection, not a calendar collection, got:\n%s", body)
	}
}

func TestPropfind_AttachmentLimits_OnCalendarCollection_ReflectConfig(t *testing.T) {
	env := newTestCalDAVEnv(t)

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "admin@example.com", env.appPasswordSecret, "0", propfindAttachmentLimits)
	defer resp.Body.Close()

	body := readBody(t, resp)
	wantSize := fmt.Sprintf("<max-attachment-size xmlns=\"urn:ietf:params:xml:ns:caldav\">%s</max-attachment-size>", strconv.FormatInt(testMaxAttachmentSize, 10))
	if !strings.Contains(body, wantSize) {
		t.Fatalf("expected %q in response:\n%s", wantSize, body)
	}
	wantCount := fmt.Sprintf("<max-attachments-per-resource xmlns=\"urn:ietf:params:xml:ns:caldav\">%s</max-attachments-per-resource>", strconv.Itoa(testMaxAttachmentsPerEvent))
	if !strings.Contains(body, wantCount) {
		t.Fatalf("expected %q in response:\n%s", wantCount, body)
	}
}

func TestPropfind_AttachmentLimits_StrangerWithNoAccess_NotAdvertised(t *testing.T) {
	env := newTestCalDAVEnv(t)

	stranger, err := env.users.Create(context.Background(), "stranger", "stranger@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	created, err := env.appPasswordService.Create(context.Background(), stranger.ID, "Test device")
	if err != nil {
		t.Fatalf("create stranger app password: %v", err)
	}

	path := calendarPath(env.userID, env.calendarID)
	resp := propfind(t, env.srv, path, "stranger@example.com", created.Secret, "0", propfindAttachmentLimits)
	defer resp.Body.Close()

	if resp.StatusCode == 207 {
		body := readBody(t, resp)
		if strings.Contains(body, strconv.FormatInt(testMaxAttachmentSize, 10)) {
			t.Fatalf("expected no access to hide attachment limits, got:\n%s", body)
		}
	}
}
