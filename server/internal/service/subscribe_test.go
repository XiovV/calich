package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/XiovV/calendar/server/internal/db"
	"github.com/XiovV/calendar/server/internal/repository"
)

// newTestSubscribeService returns a SubscribeService plus a real user id,
// wired to a real in-memory SQLite DB (db.OpenInMemory) — the same recipe
// newTestImportService uses. The outbound feed side is a real
// httptest.Server per test, not a mocked fetcher, per #83's acceptance
// criteria.
// opts is applied after the default WithHTTPClient override below, so a
// caller wanting the real address guard (#97, ADR-0032) back — as the
// blocked-address tests do — passes WithHTTPClient(subscribeHTTPClient)
// itself; NewSubscribeService applies options in order, so it wins.
func newTestSubscribeService(t *testing.T, opts ...SubscribeOption) (svc *SubscribeService, events *EventService, calendars *CalendarService, userID, workspaceID int64) {
	t.Helper()

	sqlDB, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	users := repository.NewUserRepository(sqlDB)
	user, err := users.Create(context.Background(), "user-a", "user-a@example.com", "hash", false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	workspaceRepo := repository.NewWorkspaceRepository(sqlDB)
	workspace, err := workspaceRepo.Create(context.Background(), "Test Workspace", user.ID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := workspaceRepo.AddMember(context.Background(), workspace.ID, user.ID, repository.WorkspaceRoleOwner); err != nil {
		t.Fatalf("add workspace member: %v", err)
	}

	calendarSvc := NewCalendarService(repository.NewCalendarRepository(sqlDB), repository.NewCalendarShareRepository(sqlDB), users, repository.NewReminderOverrideRepository(sqlDB), repository.NewCalendarUserColorRepository(sqlDB), workspaceRepo, repository.NewCalendarGroupShareRepository(sqlDB), repository.NewGroupRepository(sqlDB))
	eventSvc := NewEventService(sqlDB, repository.NewEventRepository(sqlDB), repository.NewEventExceptionRepository(sqlDB), repository.NewEventReminderRepository(sqlDB), repository.NewReminderOverrideRepository(sqlDB), repository.NewSyncRepository(sqlDB), calendarSvc, users, repository.NewAttachmentRepository(sqlDB), repository.NewAttendeeRepository(sqlDB), workspaceRepo, repository.NewGroupRepository(sqlDB), repository.NewNotificationRepository(sqlDB), nil, 1000)

	// The address guard (#97, ADR-0032) would otherwise refuse every fetch
	// here: icsServer/redirect servers below are httptest.Server instances on
	// 127.0.0.1, a loopback address the guard exists to block.
	allOpts := append([]SubscribeOption{WithHTTPClient(&http.Client{})}, opts...)
	return NewSubscribeService(eventSvc, calendarSvc, 0, allOpts...), eventSvc, calendarSvc, user.ID, workspace.ID
}

// subscribeFeedICS carries the feed's own name/color, a recurring timed
// series with a VALARM (to prove alarms are dropped, not converted), and an
// all-day series — exercising the "recurring, all-day ... render correctly"
// and "feed's alarms produce no Reminders" acceptance criteria in one feed.
const subscribeFeedICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Team Holidays
X-APPLE-CALENDAR-COLOR:#8E44ADFF
BEGIN:VEVENT
UID:feed-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;COUNT=3
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT10M
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:feed-uid-2
DTSTART;VALUE=DATE:20260701
DTEND;VALUE=DATE:20260702
SUMMARY:Company holiday
END:VEVENT
END:VCALENDAR
`

func crlfSub(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func icsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(body)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSubscribeService_Preview_ProposesNameColorAndCounts(t *testing.T) {
	svc, events, calendars, userID, _ := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	preview, err := svc.Preview(ctx, userID, srv.URL+"/feed.ics")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Name != "Team Holidays" {
		t.Fatalf("expected name from X-WR-CALNAME, got %q", preview.Name)
	}
	if preview.Color != "#8E44ADFF" {
		t.Fatalf("expected color from X-APPLE-CALENDAR-COLOR, got %q", preview.Color)
	}
	if preview.EventCount != 2 {
		t.Fatalf("expected 2 series, got %d", preview.EventCount)
	}
	if preview.RangeStart == nil || preview.RangeEnd == nil {
		t.Fatalf("expected a non-nil range, got %+v", preview)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 0 {
		t.Fatalf("expected preview to create no calendar, got %+v", cals)
	}
	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected preview to write no events, got %+v", all)
	}
}

func TestSubscribeService_Subscribe_Success(t *testing.T) {
	svc, events, calendars, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if calendar.SourceURL == nil || *calendar.SourceURL != srv.URL+"/feed.ics" {
		t.Fatalf("expected SourceURL to be stored, got %+v", calendar.SourceURL)
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar, got %+v", cals)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 series written, got %+v", all)
	}

	byTitle := make(map[string]repository.Event, len(all))
	for _, e := range all {
		byTitle[e.Title] = e
	}

	standup, ok := byTitle["Standup"]
	if !ok {
		t.Fatalf("expected a Standup event, got %+v", all)
	}
	if standup.Rrule == "" {
		t.Fatalf("expected the recurring series to keep its RRULE, got %+v", standup)
	}
	if standup.ExternalUID == nil || *standup.ExternalUID != "feed-uid-1" {
		t.Fatalf("expected ExternalUID feed-uid-1, got %+v", standup.ExternalUID)
	}

	holiday, ok := byTitle["Company holiday"]
	if !ok {
		t.Fatalf("expected a Company holiday event, got %+v", all)
	}
	if !holiday.AllDay {
		t.Fatalf("expected the all-day series to stay all-day, got %+v", holiday)
	}
	if holiday.ExternalUID == nil || *holiday.ExternalUID != "feed-uid-2" {
		t.Fatalf("expected ExternalUID feed-uid-2, got %+v", holiday.ExternalUID)
	}

	// The feed's VALARM must not become a Reminder (ADR-0032: dropped
	// unconditionally at this stage).
	withReminders, err := events.Get(ctx, userID, standup.ID)
	if err != nil {
		t.Fatalf("get standup: %v", err)
	}
	if len(withReminders.Reminders) != 0 {
		t.Fatalf("expected no Reminders from the feed's VALARM, got %+v", withReminders.Reminders)
	}
}

func TestSubscribeService_Subscribe_RedirectFollowed(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect.ics" {
			http.Redirect(w, r, srv.URL+"/actual.ics", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	_, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/redirect.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected the redirect to be followed and both series written, got %+v", all)
	}
}

func TestSubscribeService_Subscribe_BasicAuthFromURLUserinfo(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	parsed.User = url.UserPassword("alice", "s3cret")
	parsed.Path = "/feed.ics"

	_, err = svc.Subscribe(ctx, userID, workspaceID, parsed.String(), "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected Basic auth to succeed and both series written, got %+v", all)
	}
}

func TestSubscribeService_Subscribe_AuthFailure(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err == nil || !errors.Is(err, ErrSubscribeAuthFailed) {
		t.Fatalf("expected ErrSubscribeAuthFailed, got %v", err)
	}
}

func TestSubscribeService_Subscribe_Timeout(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/calendar")
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err == nil || !errors.Is(err, ErrSubscribeFetchFailed) {
		t.Fatalf("expected ErrSubscribeFetchFailed on timeout, got %v", err)
	}
}

func TestSubscribeService_Subscribe_OversizedResponseRejected(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	oversized := strings.Repeat("A", maxSubscribeFetchBytes+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(oversized))
	}))
	t.Cleanup(srv.Close)

	_, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err == nil || !errors.Is(err, ErrSubscribeTooLarge) {
		t.Fatalf("expected ErrSubscribeTooLarge, got %v", err)
	}
}

func TestSubscribeService_Subscribe_MalformedBodyUnparseable(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not an ics file"))
	}))
	t.Cleanup(srv.Close)

	_, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err == nil || !errors.Is(err, ErrSubscribeUnparseable) {
		t.Fatalf("expected ErrSubscribeUnparseable, got %v", err)
	}
}

func TestSubscribeService_Subscribe_InvalidURL(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	for _, raw := range []string{"", "not a url", "ftp://example.com/feed.ics", "example.com/feed.ics"} {
		_, err := svc.Subscribe(ctx, userID, workspaceID, raw, "Name", "#8E44ADFF", false)
		if err == nil || !errors.Is(err, ErrSubscribeInvalidURL) {
			t.Fatalf("url %q: expected ErrSubscribeInvalidURL, got %v", raw, err)
		}
	}
}

// TestSubscribeService_Subscribe_BlocksPrivateAddress is an end-to-end
// check of #97/ADR-0032 using the real, unguarded-by-test-override client:
// a SubscribeService built via NewSubscribeService with no WithHTTPClient
// override must refuse a URL that resolves to loopback, the same way it
// would refuse any other private/link-local address, and must not create a
// Calendar in the process.
func TestSubscribeService_Subscribe_BlocksPrivateAddress(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t, WithHTTPClient(subscribeHTTPClient))
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	_, err := svc.Subscribe(ctx, userID, workspaceID, "http://user:s3cret@127.0.0.1:"+srv.URL[len("http://127.0.0.1:"):]+"/feed.ics", "Name", "#8E44ADFF", false)
	if !errors.Is(err, ErrSubscribeURLBlocked) {
		t.Fatalf("expected ErrSubscribeURLBlocked, got %v", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("expected the password to be masked in the blocked-URL error, got %q", err.Error())
	}

	cals, err := calendars.List(ctx, userID)
	if err != nil {
		t.Fatalf("list calendars: %v", err)
	}
	if len(cals) != 0 {
		t.Fatalf("expected no calendar to be created when the URL is blocked, got %+v", cals)
	}
}

// TestSubscribeService_Refresh_BlocksPrivateAddress covers the "same check
// on every Refresh, not only on create" acceptance criterion: a Calendar
// that already carries a loopback SourceURL (as if its feed moved there, or
// as if the guard were added after it was created) must have its Refresh
// rejected the same way Subscribe would reject it, classified
// needs_attention rather than retrying since no amount of retrying fixes it.
func TestSubscribeService_Refresh_BlocksPrivateAddress(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t, WithHTTPClient(subscribeHTTPClient))
	ctx := context.Background()

	sourceURL := "http://127.0.0.1:9999/feed.ics"
	cal, err := calendars.Create(ctx, userID, workspaceID, "cal-private", CalendarWrite{
		Name: "Private", Color: "#12809CFF", SourceURL: &sourceURL,
	})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err = svc.Refresh(ctx, userID, cal.ID, true)
	if !errors.Is(err, ErrSubscribeURLBlocked) {
		t.Fatalf("expected ErrSubscribeURLBlocked, got %v", err)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.ErrorClass == nil || *got.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected ErrorClassNeedsAttention, got %+v", got.ErrorClass)
	}
}

func TestNormalizeSubscribeURL_WebcalToHTTPS(t *testing.T) {
	got, err := normalizeSubscribeURL("webcal://example.com/feed.ics")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "https://example.com/feed.ics" {
		t.Fatalf("expected webcal:// to normalize to https://, got %q", got)
	}
}

func TestMaskURL_RedactsPassword(t *testing.T) {
	masked := MaskURL("https://alice:s3cret@example.com/feed.ics")
	if strings.Contains(masked, "s3cret") {
		t.Fatalf("expected the password to be redacted, got %q", masked)
	}
	if !strings.Contains(masked, "alice") {
		t.Fatalf("expected the username to survive masking, got %q", masked)
	}

	// No userinfo at all: unchanged.
	plain := MaskURL("https://example.com/feed.ics")
	if plain != "https://example.com/feed.ics" {
		t.Fatalf("expected a URL with no userinfo to be returned unchanged, got %q", plain)
	}
}

// twoSeriesFeed builds a minimal feed with two independent series, letting
// each test vary one series' SUMMARY or DTSTART presence to exercise
// Refresh's reconciliation buckets without repeating the whole VCALENDAR
// structure inline.
func twoSeriesFeed(seriesASummary string, seriesAHasDTStart bool, includeSeriesB bool) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\n")
	b.WriteString("BEGIN:VEVENT\nUID:series-a\n")
	if seriesAHasDTStart {
		b.WriteString("DTSTART:20260601T090000Z\nDTEND:20260601T093000Z\n")
	}
	b.WriteString("SUMMARY:" + seriesASummary + "\nEND:VEVENT\n")
	if includeSeriesB {
		b.WriteString("BEGIN:VEVENT\nUID:series-b\nDTSTART:20260602T090000Z\nDTEND:20260602T093000Z\nSUMMARY:Retro\nEND:VEVENT\n")
	}
	b.WriteString("END:VCALENDAR\n")
	return b.String()
}

func TestSubscribeService_Refresh_RejectsNonSubscribedCalendar(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	cal, err := calendars.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err = svc.Refresh(ctx, userID, cal.ID, true)
	if !errors.Is(err, ErrRefreshNotSubscribed) {
		t.Fatalf("expected ErrRefreshNotSubscribed, got %v", err)
	}
}

func TestSubscribeService_Refresh_ETagShortCircuitsUnchangedFeed(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	const etag = `"v1"`
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	first, err := svc.Refresh(ctx, userID, cal.ID, false)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if first.NotModified {
		t.Fatalf("expected the first refresh (no stored validators yet) to actually fetch, got %+v", first)
	}

	requestsBeforeSecond := requests
	second, err := svc.Refresh(ctx, userID, cal.ID, false)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if !second.NotModified {
		t.Fatalf("expected the second refresh to short-circuit on a 304, got %+v", second)
	}
	if requests != requestsBeforeSecond+1 {
		t.Fatalf("expected exactly one more request for the conditional GET, got %d total", requests)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.LastSyncedAt == nil {
		t.Fatalf("expected LastSyncedAt to be set after a successful refresh")
	}

	// A 304 response carries no ETag of its own (RFC 7232 doesn't require
	// repeating it), so a naive implementation overwrites the stored ETag
	// with nothing here — the regression this guards against left every
	// Refresh after the second one with no validator to send at all.
	requestsBeforeThird := requests
	third, err := svc.Refresh(ctx, userID, cal.ID, false)
	if err != nil {
		t.Fatalf("third refresh: %v", err)
	}
	if !third.NotModified {
		t.Fatalf("expected the third refresh to still short-circuit on a 304, got %+v", third)
	}
	if requests != requestsBeforeThird+1 {
		t.Fatalf("expected exactly one more request for the third conditional GET, got %d total", requests)
	}
}

func TestSubscribeService_Refresh_ForceBypassesConditionalGET(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	const etag = `"v1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	result, err := svc.Refresh(ctx, userID, cal.ID, true)
	if err != nil {
		t.Fatalf("forced refresh: %v", err)
	}
	if result.NotModified {
		t.Fatalf("expected force to bypass the conditional-GET short-circuit, got %+v", result)
	}
}

func TestSubscribeService_Refresh_ContentHashShortCircuitsWhenNoValidators(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	result, err := svc.Refresh(ctx, userID, cal.ID, false)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if !result.NotModified {
		t.Fatalf("expected the content hash to short-circuit an identical body with no validators, got %+v", result)
	}
	if requests != 3 {
		t.Fatalf("expected subscribe plus both refreshes to still fetch the body (no validators to send), got %d requests", requests)
	}
}

func TestSubscribeService_Refresh_ChangedSeriesUpdatedInPlaceUnchangedLeftAlone(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	body := twoSeriesFeed("Standup", true, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(body)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	before, err := events.events.ListMastersByCalendar(ctx, cal.ID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	var standupID string
	for _, m := range before {
		if m.Title == "Standup" {
			standupID = m.ID
		}
	}
	if standupID == "" {
		t.Fatalf("expected a Standup master to have been created, got %+v", before)
	}

	body = twoSeriesFeed("Standup (renamed)", true, true)
	result, err := svc.Refresh(ctx, userID, cal.ID, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected 1 updated series, got %+v", result)
	}
	if result.NoOp != 1 {
		t.Fatalf("expected the untouched series to count as a no-op, got %+v", result)
	}

	updated, err := events.events.GetByID(ctx, standupID)
	if err != nil {
		t.Fatalf("get updated master: %v", err)
	}
	if updated.Title != "Standup (renamed)" {
		t.Fatalf("expected the title to be updated, got %q", updated.Title)
	}
	if updated.ID != standupID {
		t.Fatalf("expected the row id to be kept across the update, got %q", updated.ID)
	}
}

// TestSubscribeService_Refresh_URLChangeAloneUpdatesStoredEvent exercises
// #207's acceptance criterion that a Refresh whose only upstream change is
// a series' URL updates the stored Event rather than leaving it unchanged.
func TestSubscribeService_Refresh_URLChangeAloneUpdatesStoredEvent(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	seriesWithURL := func(url string) string {
		return "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\n" +
			"BEGIN:VEVENT\nUID:series-a\nDTSTART:20260601T090000Z\nDTEND:20260601T093000Z\nSUMMARY:Standup\nURL:" + url + "\nEND:VEVENT\n" +
			"END:VCALENDAR\n"
	}

	body := seriesWithURL("https://example.com/before")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(body)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	masters, err := events.events.ListMastersByCalendar(ctx, cal.ID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	if len(masters) != 1 || masters[0].URL != "https://example.com/before" {
		t.Fatalf("expected the initial URL to be stored, got %+v", masters)
	}
	masterID := masters[0].ID

	body = seriesWithURL("https://example.com/after")
	result, err := svc.Refresh(ctx, userID, cal.ID, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected the URL-only change to be detected as an update, got %+v", result)
	}

	updated, err := events.events.GetByID(ctx, masterID)
	if err != nil {
		t.Fatalf("get updated master: %v", err)
	}
	if updated.URL != "https://example.com/after" {
		t.Fatalf("expected the stored URL to follow the feed's change, got %q", updated.URL)
	}
}

func TestSubscribeService_Refresh_AbsentSeriesTombstoned(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	body := twoSeriesFeed("Standup", true, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(body)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	before, err := events.events.ListMastersByCalendar(ctx, cal.ID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	var retroID string
	for _, m := range before {
		if m.Title == "Retro" {
			retroID = m.ID
		}
	}
	if retroID == "" {
		t.Fatalf("expected a Retro master to have been created, got %+v", before)
	}

	body = twoSeriesFeed("Standup", true, false)
	result, err := svc.Refresh(ctx, userID, cal.ID, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Tombstoned != 1 {
		t.Fatalf("expected 1 tombstoned series, got %+v", result)
	}

	if _, err := events.events.GetByID(ctx, retroID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected the removed series' row to be gone, got %v", err)
	}

	deleted, err := events.sync.DeletedSince(ctx, cal.ID, 0)
	if err != nil {
		t.Fatalf("deleted since: %v", err)
	}
	if len(deleted) != 1 || deleted[0].UID != retroID {
		t.Fatalf("expected a tombstone recording %q, got %+v", retroID, deleted)
	}
}

func TestSubscribeService_Subscribe_SchedulesInitialNextRefresh(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	srv := icsServer(t, subscribeFeedICS)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.NextRefreshAt == nil {
		t.Fatalf("expected NextRefreshAt to be set at subscribe time, got nil")
	}
	// Default interval is 1h (DefaultRefreshInterval), plus a stagger of at
	// most a quarter of it.
	if got.NextRefreshAt.Before(fixedNow.Add(time.Hour)) || got.NextRefreshAt.After(fixedNow.Add(time.Hour+15*time.Minute)) {
		t.Fatalf("expected NextRefreshAt within [1h, 1h15m) of subscribe time, got %v (now=%v)", got.NextRefreshAt, fixedNow)
	}
}

func TestSubscribeService_Refresh_SuccessSchedulesNextRefreshAndClearsPriorFailure(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	srv := icsServer(t, subscribeFeedICS)
	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Seed a prior failure directly through the repository, standing in for
	// an earlier broken poll, to prove a success clears it.
	if err := calendars.RecordRefreshFailure(ctx, userID, cal.ID, repository.RefreshFailure{
		ErrorClass: ErrorClassRetrying, ErrorMessage: "timeout", FailureCount: 3, NextRefreshAt: fixedNow.Add(time.Minute),
	}); err != nil {
		t.Fatalf("seed failure: %v", err)
	}

	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.FailureCount != 0 {
		t.Fatalf("expected a success to reset FailureCount, got %d", got.FailureCount)
	}
	if got.ErrorClass != nil {
		t.Fatalf("expected a success to clear ErrorClass, got %v", *got.ErrorClass)
	}
	if got.NextRefreshAt == nil || !got.NextRefreshAt.After(fixedNow) {
		t.Fatalf("expected NextRefreshAt to move into the future, got %v", got.NextRefreshAt)
	}
}

func TestSubscribeService_Refresh_AuthFailureClassifiedNeedsAttention(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	credentialsValid := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !credentialsValid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	credentialsValid = false
	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err == nil || !errors.Is(err, ErrSubscribeAuthFailed) {
		t.Fatalf("expected ErrSubscribeAuthFailed, got %v", err)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.ErrorClass == nil || *got.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected ErrorClass needs_attention, got %v", got.ErrorClass)
	}
	if got.ErrorMessage == nil || !strings.Contains(*got.ErrorMessage, "rejected the credentials") {
		t.Fatalf("expected ErrorMessage to describe the auth failure, got %v", got.ErrorMessage)
	}
	if got.FailureCount != 1 {
		t.Fatalf("expected FailureCount 1, got %d", got.FailureCount)
	}
}

func TestSubscribeService_Refresh_NotFoundClassifiedNeedsAttention(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	exists := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	exists = false
	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err == nil || !errors.Is(err, ErrSubscribeNotFound) {
		t.Fatalf("expected ErrSubscribeNotFound, got %v", err)
	}

	got, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.ErrorClass == nil || *got.ErrorClass != ErrorClassNeedsAttention {
		t.Fatalf("expected ErrorClass needs_attention, got %v", got.ErrorClass)
	}
}

func TestSubscribeService_Refresh_FeedGoesDown_ClassifiedRetryingAndBacksOff(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(subscribeFeedICS)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	up = false
	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err == nil || !errors.Is(err, ErrSubscribeFetchFailed) {
		t.Fatalf("expected ErrSubscribeFetchFailed, got %v", err)
	}

	afterFirst, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if afterFirst.FailureCount != 1 {
		t.Fatalf("expected FailureCount 1 after the first failure, got %d", afterFirst.FailureCount)
	}
	if afterFirst.ErrorClass == nil || *afterFirst.ErrorClass != ErrorClassRetrying {
		t.Fatalf("expected ErrorClass retrying, got %v", afterFirst.ErrorClass)
	}
	if afterFirst.LastSyncedAt != nil {
		t.Fatalf("expected a failure to leave LastSyncedAt untouched (still nil pre-first-success), got %v", afterFirst.LastSyncedAt)
	}
	firstBackoff := afterFirst.NextRefreshAt.Sub(fixedNow)

	if _, err := svc.Refresh(ctx, userID, cal.ID, false); err == nil {
		t.Fatalf("expected the second refresh to still fail")
	}
	afterSecond, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if afterSecond.FailureCount != 2 {
		t.Fatalf("expected FailureCount 2 after the second failure, got %d", afterSecond.FailureCount)
	}
	secondBackoff := afterSecond.NextRefreshAt.Sub(fixedNow)
	if secondBackoff <= firstBackoff {
		t.Fatalf("expected backoff to grow across consecutive failures: first=%v second=%v", firstBackoff, secondBackoff)
	}

	// The feed recovers: a subsequent manual refresh (bypassing backoff via
	// force) must clear the failure state.
	up = true
	if _, err := svc.Refresh(ctx, userID, cal.ID, true); err != nil {
		t.Fatalf("recovery refresh: %v", err)
	}
	recovered, err := calendars.Get(ctx, userID, cal.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if recovered.FailureCount != 0 {
		t.Fatalf("expected recovery to reset FailureCount, got %d", recovered.FailureCount)
	}
	if recovered.ErrorClass != nil {
		t.Fatalf("expected recovery to clear ErrorClass, got %v", recovered.ErrorClass)
	}
}

func TestSubscribeService_Refresh_UnparseableSeriesLeftAloneNotTombstoned(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	body := twoSeriesFeed("Standup", true, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(body)))
	}))
	t.Cleanup(srv.Close)

	cal, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Feed", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	before, err := events.events.ListMastersByCalendar(ctx, cal.ID)
	if err != nil {
		t.Fatalf("list masters: %v", err)
	}
	var standupID string
	for _, m := range before {
		if m.Title == "Standup" {
			standupID = m.ID
		}
	}
	if standupID == "" {
		t.Fatalf("expected a Standup master to have been created, got %+v", before)
	}

	// series-a now has no DTSTART, so it fails to parse this fetch, but the
	// feed still lists its UID — present but unparseable, not absent.
	body = twoSeriesFeed("Standup", false, true)
	result, err := svc.Refresh(ctx, userID, cal.ID, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Unparseable != 1 {
		t.Fatalf("expected 1 unparseable series, got %+v", result)
	}
	if result.Tombstoned != 0 {
		t.Fatalf("expected the unparseable series to survive, not be tombstoned, got %+v", result)
	}

	still, err := events.events.GetByID(ctx, standupID)
	if err != nil {
		t.Fatalf("expected the unparseable series' existing row to survive: %v", err)
	}
	if still.Title != "Standup" {
		t.Fatalf("expected the existing row untouched, got %q", still.Title)
	}
}

// TestSubscribeService_Subscribe_KeepAlarmsTrue_ReminderKept is #87's core
// acceptance criterion at subscribe time: KeepAlarms on means the feed's
// VALARM becomes a Reminder, matching ICS import's behaviour, rather than
// being dropped as it is by default (TestSubscribeService_Subscribe_Success).
func TestSubscribeService_Subscribe_KeepAlarmsTrue_ReminderKept(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", true)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !calendar.KeepAlarms {
		t.Fatalf("expected KeepAlarms to be stored true, got %+v", calendar)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var standup repository.Event
	for _, e := range all {
		if e.Title == "Standup" {
			standup = e
		}
	}
	if standup.ID == "" {
		t.Fatalf("expected a Standup event, got %+v", all)
	}

	got, err := events.Get(ctx, userID, standup.ID)
	if err != nil {
		t.Fatalf("get standup: %v", err)
	}
	if len(got.Reminders) != 1 {
		t.Fatalf("expected the feed's VALARM to become 1 Reminder, got %+v", got.Reminders)
	}
	if got.Reminders[0].OffsetMinutes != 10 || got.Reminders[0].Channel != "notification" {
		t.Fatalf("expected a 10-minute notification Reminder, got %+v", got.Reminders[0])
	}
}

func TestSubscribeService_UpdateKeepAlarms_RejectsNonSubscribedCalendar(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	cal, err := calendars.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err = svc.UpdateKeepAlarms(ctx, userID, cal.ID, true)
	if !errors.Is(err, ErrRefreshNotSubscribed) {
		t.Fatalf("expected ErrRefreshNotSubscribed, got %v", err)
	}
}

// TestSubscribeService_UpdateKeepAlarms_TurningOffClearsRemindersImmediately
// is #87's other explicit acceptance criterion: turning KeepAlarms off must
// remove Reminders a prior Refresh created from the feed right away, not
// merely stop a future Refresh from adding more.
func TestSubscribeService_UpdateKeepAlarms_TurningOffClearsRemindersImmediately(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", true)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var standupID string
	for _, e := range all {
		if e.Title == "Standup" {
			standupID = e.ID
		}
	}
	before, err := events.Get(ctx, userID, standupID)
	if err != nil {
		t.Fatalf("get standup: %v", err)
	}
	if len(before.Reminders) != 1 {
		t.Fatalf("expected the Reminder to exist before turning KeepAlarms off, got %+v", before.Reminders)
	}

	updated, err := svc.UpdateKeepAlarms(ctx, userID, calendar.ID, false)
	if err != nil {
		t.Fatalf("update keep alarms: %v", err)
	}
	if updated.KeepAlarms {
		t.Fatalf("expected KeepAlarms to be stored false, got %+v", updated)
	}

	after, err := events.Get(ctx, userID, standupID)
	if err != nil {
		t.Fatalf("get standup after turn-off: %v", err)
	}
	if len(after.Reminders) != 0 {
		t.Fatalf("expected turning KeepAlarms off to immediately clear Reminders, got %+v", after.Reminders)
	}
}

// TestSubscribeService_UpdateKeepAlarms_TurningOnTakesEffectOnNextForcedRefresh
// covers the companion criterion: turning KeepAlarms on doesn't retroactively
// add Reminders itself, but a subsequent Refresh that reconciles the series
// picks them up, because Reminders are part of the content diff
// (reconcile.go) — a forced "Refresh now" always reaches that diff even
// against a feed whose bytes haven't changed.
func TestSubscribeService_UpdateKeepAlarms_TurningOnTakesEffectOnNextForcedRefresh(t *testing.T) {
	svc, events, _, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var standupID string
	for _, e := range all {
		if e.Title == "Standup" {
			standupID = e.ID
		}
	}
	before, err := events.Get(ctx, userID, standupID)
	if err != nil {
		t.Fatalf("get standup: %v", err)
	}
	if len(before.Reminders) != 0 {
		t.Fatalf("expected no Reminders before KeepAlarms is turned on, got %+v", before.Reminders)
	}

	if _, err := svc.UpdateKeepAlarms(ctx, userID, calendar.ID, true); err != nil {
		t.Fatalf("update keep alarms: %v", err)
	}

	result, err := svc.Refresh(ctx, userID, calendar.ID, true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.Updated == 0 {
		t.Fatalf("expected the now-differing Reminders to force an update, got %+v", result)
	}

	after, err := events.Get(ctx, userID, standupID)
	if err != nil {
		t.Fatalf("get standup after refresh: %v", err)
	}
	if len(after.Reminders) != 1 {
		t.Fatalf("expected the feed's VALARM to become 1 Reminder after the forced refresh, got %+v", after.Reminders)
	}
}

// --- Name/color follows the publisher until the User touches them (#88, ADR-0032) ---

// renamedFeedICS mirrors subscribeFeedICS's single series but under a
// different X-WR-CALNAME/X-APPLE-CALENDAR-COLOR — a publisher rename.
const renamedFeedICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
X-WR-CALNAME:Company Holidays
X-APPLE-CALENDAR-COLOR:#2ECC71FF
BEGIN:VEVENT
UID:feed-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;COUNT=3
END:VEVENT
END:VCALENDAR
`

// namelessFeedICS mirrors subscribeFeedICS's single series with no
// X-WR-CALNAME/X-APPLE-CALENDAR-COLOR at all — a publisher that stops
// supplying either.
const namelessFeedICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:feed-uid-1
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
SUMMARY:Standup
RRULE:FREQ=WEEKLY;COUNT=3
END:VEVENT
END:VCALENDAR
`

// switchableICSServer serves whatever *body currently points at, so a test
// can change the feed's content between two Refresh calls against the same
// URL.
func switchableICSServer(t *testing.T, body *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(crlfSub(*body)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSubscribeService_Subscribe_FeedNameColorShadowTracksFeedNotUsersChoice(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	// The User picks a name/color different from what the feed proposes —
	// diverging right from Subscribe, same as a later rename would.
	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "My Calendar", "#123456FF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if calendar.Name != "My Calendar" || calendar.Color != "#123456FF" {
		t.Fatalf("expected the User's chosen name/color to be stored, got %+v", calendar)
	}
	if calendar.FeedName == nil || *calendar.FeedName != "Team Holidays" {
		t.Fatalf("expected the shadow to track the feed's own name regardless, got %+v", calendar.FeedName)
	}
	if calendar.FeedColor == nil || *calendar.FeedColor != "#8E44ADFF" {
		t.Fatalf("expected the shadow to track the feed's own color regardless, got %+v", calendar.FeedColor)
	}

	got, err := calendars.Get(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "My Calendar" {
		t.Fatalf("expected the stored Name to survive, got %q", got.Name)
	}
}

func TestSubscribeService_Refresh_UntouchedNameFollowsPublisherRename(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	body := subscribeFeedICS
	srv := switchableICSServer(t, &body)
	ctx := context.Background()

	// Accepting the feed's proposed name/color, as Preview would suggest,
	// means Name/Color start out equal to the shadow — "untouched".
	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	body = renamedFeedICS
	if _, err := svc.Refresh(ctx, userID, calendar.ID, true); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := calendars.Get(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "Company Holidays" {
		t.Fatalf("expected the untouched Name to follow the publisher's rename, got %q", got.Name)
	}
	if got.Color != "#2ECC71FF" {
		t.Fatalf("expected the untouched Color to follow the publisher's recolor, got %q", got.Color)
	}
	if got.FeedName == nil || *got.FeedName != "Company Holidays" {
		t.Fatalf("expected the shadow to track the new name, got %+v", got.FeedName)
	}
	if got.FeedColor == nil || *got.FeedColor != "#2ECC71FF" {
		t.Fatalf("expected the shadow to track the new color, got %+v", got.FeedColor)
	}
}

func TestSubscribeService_Refresh_CustomizedNameNeverOverwritten(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	body := subscribeFeedICS
	srv := switchableICSServer(t, &body)
	ctx := context.Background()

	// The User picks their own name/color at Subscribe time — diverged
	// from the shadow immediately.
	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "My Calendar", "#123456FF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	body = renamedFeedICS
	if _, err := svc.Refresh(ctx, userID, calendar.ID, true); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := calendars.Get(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "My Calendar" {
		t.Fatalf("expected the User's customized Name to survive the publisher's rename, got %q", got.Name)
	}
	if got.Color != "#123456FF" {
		t.Fatalf("expected the User's customized Color to survive the publisher's recolor, got %q", got.Color)
	}
	// The shadow still tracks the feed's latest value even while
	// overridden — the comparison alone is the "overridden" flag, so a
	// later coincidental match would resume tracking.
	if got.FeedName == nil || *got.FeedName != "Company Holidays" {
		t.Fatalf("expected the shadow to still track the new feed name, got %+v", got.FeedName)
	}
	if got.FeedColor == nil || *got.FeedColor != "#2ECC71FF" {
		t.Fatalf("expected the shadow to still track the new feed color, got %+v", got.FeedColor)
	}
}

func TestSubscribeService_Refresh_FeedStopsSupplyingNameColor_DoesNotBlank(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	body := subscribeFeedICS
	srv := switchableICSServer(t, &body)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	body = namelessFeedICS
	if _, err := svc.Refresh(ctx, userID, calendar.ID, true); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := calendars.Get(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.Name != "Team Holidays" {
		t.Fatalf("expected the existing Name to survive a feed that stops supplying one, got %q", got.Name)
	}
	if got.Color != "#8E44ADFF" {
		t.Fatalf("expected the existing Color to survive a feed that stops supplying one, got %q", got.Color)
	}
	if got.FeedName == nil || *got.FeedName != "Team Holidays" {
		t.Fatalf("expected the shadow to survive unchanged too, got %+v", got.FeedName)
	}
	if got.FeedColor == nil || *got.FeedColor != "#8E44ADFF" {
		t.Fatalf("expected the shadow to survive unchanged too, got %+v", got.FeedColor)
	}
}

// --- Editing a Subscription's URL (#88, ADR-0032) ---

func TestSubscribeService_UpdateSourceURL_ReconcilesAgainstNewSourceOnNextRefresh(t *testing.T) {
	svc, events, calendars, userID, workspaceID := newTestSubscribeService(t)
	oldSrv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, oldSrv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The publisher moves the feed to a new host, still serving the same
	// series but with the Standup renamed — proving reconciliation runs
	// against the *new* URL's content on the next Refresh.
	newSrv := icsServer(t, strings.Replace(subscribeFeedICS, "Standup", "Standup (moved)", 1))

	updated, err := svc.UpdateSourceURL(ctx, userID, calendar.ID, newSrv.URL+"/feed.ics")
	if err != nil {
		t.Fatalf("update source url: %v", err)
	}
	if updated.SourceURL == nil || *updated.SourceURL != newSrv.URL+"/feed.ics" {
		t.Fatalf("expected SourceURL updated, got %+v", updated.SourceURL)
	}

	if _, err := svc.Refresh(ctx, userID, calendar.ID, false); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	all, err := events.List(ctx, userID, nil, nil)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var found bool
	for _, e := range all {
		if e.Title == "Standup (moved)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the Refresh to reconcile against the new URL's content, got %+v", all)
	}

	got, err := calendars.Get(ctx, userID, calendar.ID)
	if err != nil {
		t.Fatalf("get calendar: %v", err)
	}
	if got.SourceURL == nil || *got.SourceURL != newSrv.URL+"/feed.ics" {
		t.Fatalf("expected the new SourceURL to persist, got %+v", got.SourceURL)
	}
}

func TestSubscribeService_UpdateSourceURL_RejectsNonSubscribedCalendar(t *testing.T) {
	svc, _, calendars, userID, workspaceID := newTestSubscribeService(t)
	ctx := context.Background()

	cal, err := calendars.Create(ctx, userID, workspaceID, "cal-1", CalendarWrite{Name: "Personal", Color: "#12809CFF"})
	if err != nil {
		t.Fatalf("create calendar: %v", err)
	}

	_, err = svc.UpdateSourceURL(ctx, userID, cal.ID, "https://example.com/feed.ics")
	if !errors.Is(err, ErrRefreshNotSubscribed) {
		t.Fatalf("expected ErrRefreshNotSubscribed, got %v", err)
	}
}

func TestSubscribeService_UpdateSourceURL_RejectsInvalidURL(t *testing.T) {
	svc, _, _, userID, workspaceID := newTestSubscribeService(t)
	srv := icsServer(t, subscribeFeedICS)
	ctx := context.Background()

	calendar, err := svc.Subscribe(ctx, userID, workspaceID, srv.URL+"/feed.ics", "Team Holidays", "#8E44ADFF", false)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	_, err = svc.UpdateSourceURL(ctx, userID, calendar.ID, "not a url")
	if !errors.Is(err, ErrSubscribeInvalidURL) {
		t.Fatalf("expected ErrSubscribeInvalidURL, got %v", err)
	}
}
