package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calendar/server/internal/attachmentstore"
	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// attachmentTestServer is Attachments' REST fixture (#132, ADR-0040): an
// Owner's Calendar shared as Editor to one User and Viewer to another, a
// stranger with no Access, a Master Event to attach to, and a router
// carrying the Event and Attachment routes over real HTTP.
type attachmentTestServer struct {
	baseURL                                             string
	ownerToken, editorToken, viewerToken, strangerToken string
	masterID                                            string
	// overrideID is a real Override of masterID — RecurrenceID at
	// 2026-01-08T09:00:00Z — for exercising ErrAttachmentOnOverride.
	overrideID string
}

func newAttachmentTestServer(t *testing.T, maxAttachmentSize int64, maxAttachmentsPerEvent int) attachmentTestServer {
	t.Helper()
	ctx := context.Background()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	sessions := repository.NewSessionRepository(sqlDB)
	auth := service.NewAuthService(users, sessions, []byte("test-secret"), "owner", "hunter2")
	if _, _, err := auth.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	calendarRepo := repository.NewCalendarRepository(sqlDB)
	shareRepo := repository.NewCalendarShareRepository(sqlDB)
	calendars := service.NewCalendarService(calendarRepo, shareRepo, users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB))
	appPasswords := service.NewAppPasswordService(repository.NewAppPasswordRepository(sqlDB), users)
	accounts := service.NewAccountService(sqlDB, users, sessions, calendarRepo, shareRepo, calendars, appPasswords)

	if _, err := accounts.Create(ctx, "editor", "temp-password"); err != nil {
		t.Fatalf("create editor: %v", err)
	}
	if _, err := accounts.Create(ctx, "viewer", "temp-password"); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if _, err := accounts.Create(ctx, "stranger", "temp-password"); err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	ownerLogin, err := auth.Login(ctx, "owner", "hunter2")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	editorLogin, err := auth.Login(ctx, "editor", "temp-password")
	if err != nil {
		t.Fatalf("editor login: %v", err)
	}
	viewerLogin, err := auth.Login(ctx, "viewer", "temp-password")
	if err != nil {
		t.Fatalf("viewer login: %v", err)
	}
	strangerLogin, err := auth.Login(ctx, "stranger", "temp-password")
	if err != nil {
		t.Fatalf("stranger login: %v", err)
	}

	ownerID, err := auth.Authenticate(ctx, ownerLogin.AccessToken)
	if err != nil {
		t.Fatalf("authenticate owner: %v", err)
	}

	cal, err := calendars.Create(ctx, ownerID, "cal-1", service.CalendarWrite{Name: "Family", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	if _, err := calendars.Share(ctx, ownerID, cal.ID, "editor", repository.RoleEditor); err != nil {
		t.Fatalf("share editor: %v", err)
	}
	if _, err := calendars.Share(ctx, ownerID, cal.ID, "viewer", repository.RoleViewer); err != nil {
		t.Fatalf("share viewer: %v", err)
	}

	attachmentRepo := repository.NewAttachmentRepository(sqlDB)
	events := service.NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendars, users, attachmentRepo)
	master, err := events.Create(ctx, ownerID, "evt-1", service.EventWrite{CalendarID: cal.ID, Title: "Standup", Rrule: "FREQ=WEEKLY", Start: mustParseTestTime(t, "2026-01-01T09:00:00Z"), End: mustParseTestTime(t, "2026-01-01T10:00:00Z")})
	if err != nil {
		t.Fatalf("create master event: %v", err)
	}
	recurrenceID := mustParseTestTime(t, "2026-01-08T09:00:00Z")
	override, err := events.Create(ctx, ownerID, "evt-1-override", service.EventWrite{
		CalendarID:   cal.ID,
		Title:        "Standup (moved)",
		Start:        mustParseTestTime(t, "2026-01-08T09:30:00Z"),
		End:          mustParseTestTime(t, "2026-01-08T10:30:00Z"),
		ParentID:     &master.ID,
		RecurrenceID: &recurrenceID,
	})
	if err != nil {
		t.Fatalf("create override event: %v", err)
	}

	store := attachmentstore.New(t.TempDir())
	attachments := service.NewAttachmentService(attachmentRepo, repository.NewEventRepository(sqlDB), calendars, events, store, maxAttachmentsPerEvent)
	eventHandler := NewEventHandler(events)
	attachmentHandler := NewAttachmentHandler(attachments, maxAttachmentSize)

	r := chi.NewRouter()
	r.Route("/api/events", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Get("/{id}", eventHandler.Get)
		r.Post("/{id}/attachments", attachmentHandler.Upload)
		r.Get("/{id}/attachments/{attachmentId}", attachmentHandler.Download)
		r.Delete("/{id}/attachments/{attachmentId}", attachmentHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return attachmentTestServer{
		baseURL:       srv.URL,
		ownerToken:    ownerLogin.AccessToken,
		editorToken:   editorLogin.AccessToken,
		viewerToken:   viewerLogin.AccessToken,
		strangerToken: strangerLogin.AccessToken,
		masterID:      master.ID,
		overrideID:    override.ID,
	}
}

func mustParseTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	tm, err := parseEventTime(value, false)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return tm
}

// uploadAttachment builds a multipart/form-data POST carrying a single
// "file" part, mirroring importRequest (ics_import_test.go).
func uploadAttachment(t *testing.T, baseURL, accessToken, eventID, filename, contentType string, data []byte) *http.Response {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="file"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatalf("create form part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write form part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/events/"+eventID+"/attachments", &body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestAttachmentHandler_UploadListDownloadDelete(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	resp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "agenda.pdf", "application/pdf", []byte("hello world"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}
	var created attachmentWire
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Filename != "agenda.pdf" || created.SizeBytes != int64(len("hello world")) {
		t.Fatalf("unexpected attachment: %+v", created)
	}

	// The Event's own GET carries the Attachment — there is no separate
	// list endpoint (ADR-0040).
	getResp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID, s.ownerToken)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	defer getResp.Body.Close()
	var event eventResponse
	if err := json.NewDecoder(getResp.Body).Decode(&event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if len(event.Attachments) != 1 || event.Attachments[0].ID != created.ID {
		t.Fatalf("expected the event to carry the uploaded attachment, got %+v", event.Attachments)
	}

	downloadResp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, s.ownerToken)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", downloadResp.StatusCode)
	}
	body, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("unexpected download body: %q", body)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	deleteReq.Header.Set("Authorization", "Bearer "+s.ownerToken)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", deleteResp.StatusCode)
	}

	againResp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, s.ownerToken)
	if err != nil {
		t.Fatalf("download after delete: %v", err)
	}
	defer againResp.Body.Close()
	if againResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", againResp.StatusCode)
	}
}

// TestAttachmentHandler_Download_CarriesRequiredHeaders is the acceptance
// criterion's own wording: every download response must carry the three
// headers, with a test asserting them.
func TestAttachmentHandler_Download_CarriesRequiredHeaders(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	uploadResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "notes.txt", "text/plain", []byte("hi"))
	defer uploadResp.Body.Close()
	var created attachmentWire
	if err := json.NewDecoder(uploadResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	resp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, s.ownerToken)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="notes.txt"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
}

// TestAttachmentHandler_UploadedHTMLDownloadsRatherThanRenders is the
// acceptance criterion covering stored XSS: an uploaded .html file must
// come back as a download (the three headers above), never with a
// Content-Type/disposition a browser would render inline.
func TestAttachmentHandler_UploadedHTMLDownloadsRatherThanRenders(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	uploadResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "page.html", "text/html", []byte("<script>alert(1)</script>"))
	defer uploadResp.Body.Close()
	var created attachmentWire
	if err := json.NewDecoder(uploadResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	resp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, s.ownerToken)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="page.html"` {
		t.Fatalf("Content-Disposition = %q, want an attachment disposition", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
}

func TestAttachmentHandler_Upload_ViewerForbidden(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	resp := uploadAttachment(t, s.baseURL, s.viewerToken, s.masterID, "x.txt", "text/plain", []byte("x"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
}

func TestAttachmentHandler_Upload_EditorSucceeds(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	resp := uploadAttachment(t, s.baseURL, s.editorToken, s.masterID, "x.txt", "text/plain", []byte("x"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}
}

func TestAttachmentHandler_Upload_StrangerGetsNotFound(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	resp := uploadAttachment(t, s.baseURL, s.strangerToken, s.masterID, "x.txt", "text/plain", []byte("x"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}
}

func TestAttachmentHandler_Download_ViewerAllowed(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	uploadResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "x.txt", "text/plain", []byte("x"))
	defer uploadResp.Body.Close()
	var created attachmentWire
	if err := json.NewDecoder(uploadResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	resp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, s.viewerToken)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected a Viewer to be able to download, got %d", resp.StatusCode)
	}
}

func TestAttachmentHandler_Delete_ViewerForbidden(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	uploadResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "x.txt", "text/plain", []byte("x"))
	defer uploadResp.Body.Close()
	var created attachmentWire
	if err := json.NewDecoder(uploadResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, s.baseURL+"/api/events/"+s.masterID+"/attachments/"+created.ID, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.viewerToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestAttachmentHandler_Upload_OverSizeLimitRejectedWithoutWritingAFile is
// the acceptance criterion's own wording.
func TestAttachmentHandler_Upload_OverSizeLimitRejectedWithoutWritingAFile(t *testing.T) {
	s := newAttachmentTestServer(t, 10, 10)

	resp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "big.bin", "application/octet-stream", []byte("this is more than ten bytes"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	getResp, err := authenticatedGet(s.baseURL+"/api/events/"+s.masterID, s.ownerToken)
	if err != nil {
		t.Fatalf("get event: %v", err)
	}
	defer getResp.Body.Close()
	var event eventResponse
	if err := json.NewDecoder(getResp.Body).Decode(&event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(event.Attachments) != 0 {
		t.Fatalf("expected no attachment written, got %+v", event.Attachments)
	}
}

func TestAttachmentHandler_Upload_ExceedingMaxPerEventRejected(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 1)

	firstResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "a.txt", "text/plain", []byte("a"))
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected first upload to succeed, got %d", firstResp.StatusCode)
	}

	secondResp := uploadAttachment(t, s.baseURL, s.ownerToken, s.masterID, "b.txt", "text/plain", []byte("b"))
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(secondResp.Body)
		t.Fatalf("expected 400, got %d: %s", secondResp.StatusCode, body)
	}
}

func TestAttachmentHandler_Upload_OnOverrideRejected(t *testing.T) {
	s := newAttachmentTestServer(t, 25<<20, 10)

	resp := uploadAttachment(t, s.baseURL, s.ownerToken, s.overrideID, "x.txt", "text/plain", []byte("x"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}
