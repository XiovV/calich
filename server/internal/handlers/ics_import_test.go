package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

const importTestICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
END:VEVENT
END:VCALENDAR
`

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// importRequest builds a multipart/form-data POST to /api/calendars/import
// carrying a "file" part (named filename, containing data) and a "targets"
// part carrying targetsJSON verbatim.
func importRequest(t *testing.T, baseURL, accessToken, workspaceID, path, filename string, data []byte, targetsJSON string) *http.Response {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if targetsJSON != "" {
		if err := w.WriteField("targets", targetsJSON); err != nil {
			t.Fatalf("write targets field: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+path, &body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Workspace-Id", workspaceID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestCalendarHandler_Import_DryRun_DoesNotCreateCalendar(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := importRequest(t, baseURL, accessToken, workspaceID, "/api/calendars/import?dryRun=1", "invite.ics", []byte(crlf(importTestICS)),
		`{"entries":[{"filename":"invite.ics","action":"new"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var summary importSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(summary.Files) != 1 {
		t.Fatalf("expected 1 file summary, got %+v", summary.Files)
	}
	if summary.Files[0].CalendarName != "Imported Calendar" {
		t.Fatalf("expected calendar name from X-WR-CALNAME, got %q", summary.Files[0].CalendarName)
	}
	if summary.Files[0].CalendarID != "" {
		t.Fatalf("expected no calendar id on a dry run, got %q", summary.Files[0].CalendarID)
	}
	if summary.Files[0].EventCount != 1 {
		t.Fatalf("expected 1 event, got %d", summary.Files[0].EventCount)
	}

	listResp, err := authenticatedGetWithWorkspace(baseURL+"/api/calendars/", accessToken, workspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()
	var calendars []calendarResponse
	if err := json.NewDecoder(listResp.Body).Decode(&calendars); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(calendars) != 0 {
		t.Fatalf("expected no calendars created on a dry run, got %+v", calendars)
	}
}

func TestCalendarHandler_Import_RealRun_CreatesCalendarAndEvent(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := importRequest(t, baseURL, accessToken, workspaceID, "/api/calendars/import", "invite.ics", []byte(crlf(importTestICS)),
		`{"entries":[{"filename":"invite.ics","action":"new"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var summary importSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Files[0].CalendarID == "" {
		t.Fatalf("expected a created calendar id, got %+v", summary.Files[0])
	}

	listResp, err := authenticatedGetWithWorkspace(baseURL+"/api/calendars/", accessToken, workspaceID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()
	var calendars []calendarResponse
	if err := json.NewDecoder(listResp.Body).Decode(&calendars); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(calendars) != 1 || calendars[0].ID != summary.Files[0].CalendarID {
		t.Fatalf("expected the imported calendar to exist, got %+v", calendars)
	}
}

func TestCalendarHandler_Import_MissingFilePart(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("targets", `{}`); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/calendars/import", &body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Workspace-Id", workspaceID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Import_InvalidDryRunValue(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := importRequest(t, baseURL, accessToken, workspaceID, "/api/calendars/import?dryRun=maybe", "invite.ics", []byte(crlf(importTestICS)), `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCalendarHandler_Import_Zip_RejectsExistingTarget(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	createResp := createCalendar(t, baseURL, accessToken, workspaceID, "11111111-1111-1111-1111-111111111111", "Personal", "#12809CFF")
	createResp.Body.Close()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("a.ics")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte(crlf(importTestICS))); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	resp := importRequest(t, baseURL, accessToken, workspaceID, "/api/calendars/import?dryRun=1", "export.zip", buf.Bytes(),
		`{"entries":[{"filename":"a.ics","action":"existing","calendarId":"11111111-1111-1111-1111-111111111111"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}

// importTestICSWithAttach carries one inline ATTACH, base64("hello world").
const importTestICSWithAttach = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Imported Calendar
BEGIN:VEVENT
UID:foreign-uid-attach
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
ATTACH;ENCODING=BASE64;VALUE=BINARY;FMTTYPE=text/plain;FILENAME=notes.txt:aGVsbG8gd29ybGQ=
END:VEVENT
END:VCALENDAR
`

// TestCalendarHandler_Import_RealRun_IngestsInlineAttachment exercises #135
// over HTTP: the Import summary's "attachments" counts round-trip through
// the JSON wire shape (ADR-0030). newCalendarTestServer wires no
// /api/events routes, so the imported Event/Attachment row themselves are
// checked at the service layer (TestImportService_RealRun_WritesInlineAttachment).
func TestCalendarHandler_Import_RealRun_IngestsInlineAttachment(t *testing.T) {
	baseURL, accessToken, workspaceID := newCalendarTestServer(t)

	resp := importRequest(t, baseURL, accessToken, workspaceID, "/api/calendars/import", "invite.ics", []byte(crlf(importTestICSWithAttach)),
		`{"entries":[{"filename":"invite.ics","action":"new"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var summary importSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Files[0].Attachments.Imported != 1 {
		t.Fatalf("expected 1 imported attachment in the summary, got %+v", summary.Files[0].Attachments)
	}
	if summary.Files[0].Attachments.TooLarge != 0 || summary.Files[0].Attachments.TooMany != 0 || summary.Files[0].Attachments.IgnoredURI != 0 {
		t.Fatalf("expected the other three attachment counts to be 0, got %+v", summary.Files[0].Attachments)
	}
}

func TestCalendarHandler_Import_RequiresAuth(t *testing.T) {
	baseURL, _, _ := newCalendarTestServer(t)

	resp := importRequest(t, baseURL, "", "", "/api/calendars/import?dryRun=1", "invite.ics", []byte(crlf(importTestICS)),
		`{"entries":[{"filename":"invite.ics","action":"new"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
