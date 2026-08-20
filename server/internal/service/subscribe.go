// subscribe.go implements Subscribe (#83, ADR-0032) and Refresh (#85,
// ADR-0033): a User pastes an external .ics URL, the server fetches it
// synchronously, and its Events land in an ordinary Calendar carrying that
// URL as its source. Parsing reuses icalendar.ParseImportFile and
// EventService.ImportSubscribedSeries/ReconcileSubscribedSeries — the same
// pipeline ICS import already uses (ADR-0030), via bypasses of #84's
// Subscribed Calendar write guard — so recurrence, Overrides, Exceptions,
// all-day Events, and Anchor zones all arrive correctly for free. Two
// differences from ordinary import: the feed's VALARMs are kept only when
// the Subscription's KeepAlarms opt-in is on, off by default (ADR-0032,
// #87), and each series' foreign UID is kept as ExternalUID rather than
// discarded, so a Refresh can reconcile by it (ADR-0033).
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrSubscribeInvalidURL is returned when the URL doesn't parse, has no
	// host, or uses a scheme other than http/https/webcal.
	ErrSubscribeInvalidURL = errors.New("subscription URL is invalid")
	// ErrSubscribeFetchFailed covers everything about reaching the feed that
	// isn't a credentials problem: DNS, connection refused, timeout, or a
	// non-2xx status other than 401/403.
	ErrSubscribeFetchFailed = errors.New("failed to fetch the calendar feed")
	// ErrSubscribeAuthFailed is returned when the feed responds 401 or 403.
	ErrSubscribeAuthFailed = errors.New("the calendar feed rejected the credentials")
	// ErrSubscribeNotFound is returned when the feed responds 404 — distinct
	// from ErrSubscribeFetchFailed's other non-2xx statuses because a
	// missing feed needs a human to fix the URL, not a retry (#86,
	// ADR-0033).
	ErrSubscribeNotFound = errors.New("the calendar feed was not found")
	// ErrSubscribeTooLarge is returned when the feed's response body exceeds
	// maxSubscribeFetchBytes.
	ErrSubscribeTooLarge = errors.New("the calendar feed exceeds the 10MB import limit")
	// ErrSubscribeUnparseable is returned when a fetched, correctly-sized body
	// isn't valid iCalendar.
	ErrSubscribeUnparseable = errors.New("the calendar feed could not be parsed as iCalendar")
	// ErrRefreshNotSubscribed is returned when Refresh is called on a
	// Calendar that carries no SourceURL — there is no feed to refresh
	// against (#85, ADR-0033).
	ErrRefreshNotSubscribed = errors.New("calendar is not a subscribed calendar")
	// ErrSubscribeURLBlocked is returned when a Subscription URL resolves to
	// a private, loopback, link-local, or otherwise non-public address,
	// either as typed or after following a redirect. Distinct from
	// ErrSubscribeFetchFailed so the sidebar and API can tell "the operator
	// refused this address" apart from "the feed is temporarily
	// unreachable" (#97, ADR-0032).
	ErrSubscribeURLBlocked = errors.New("subscription URL resolves to a non-public address")
)

const (
	// maxSubscribeFetchBytes matches handlers.maxImportUploadBytes (#77):
	// the same 10MB cap import applies to an uploaded file applies here to a
	// fetched one.
	maxSubscribeFetchBytes = 10 << 20
	subscribeFetchTimeout  = 30 * time.Second
)

// SubscribePreview is what Preview shows a User before they commit: the
// name/color the feed proposes and the shape of what subscribing would
// import, without writing anything.
type SubscribePreview struct {
	Name                 string
	Color                string
	EventCount           int
	RangeStart, RangeEnd *time.Time
}

// SubscribeService orchestrates Subscribe: it owns no storage of its own,
// delegating Calendar creation to CalendarService and the Event write to
// EventService.ImportSubscribedSeries — the same split ImportService uses,
// aimed at the one method allowed to write a Subscribed Calendar's Events
// (ADR-0032).
type SubscribeService struct {
	events    *EventService
	calendars *CalendarService
	// defaultRefreshInterval is how often a Subscription is refreshed when
	// its feed states no cadence of its own (#86, ADR-0033) — the
	// deployment's SUBSCRIPTION_REFRESH_INTERVAL, or DefaultRefreshInterval
	// when unset.
	defaultRefreshInterval time.Duration
	now                    func() time.Time
	// httpClient fetches every feed. Defaults to subscribeHTTPClient, whose
	// Transport blocks private/loopback/link-local addresses (#97,
	// ADR-0032); overridable via WithHTTPClient, which tests use to point at
	// an httptest.Server standing in for a feed on loopback.
	httpClient *http.Client
}

// SubscribeOption configures a SubscribeService beyond NewSubscribeService's
// required arguments.
type SubscribeOption func(*SubscribeService)

// WithHTTPClient overrides the client used to fetch feeds, in place of the
// default address-guarded one. Production has no reason to call this — it
// exists for tests that fetch from an httptest.Server, whose loopback
// address the default client would otherwise refuse to connect to.
func WithHTTPClient(client *http.Client) SubscribeOption {
	return func(s *SubscribeService) { s.httpClient = client }
}

func NewSubscribeService(events *EventService, calendars *CalendarService, defaultRefreshInterval time.Duration, opts ...SubscribeOption) *SubscribeService {
	if defaultRefreshInterval <= 0 {
		defaultRefreshInterval = DefaultRefreshInterval
	}
	s := &SubscribeService{events: events, calendars: calendars, defaultRefreshInterval: defaultRefreshInterval, now: time.Now, httpClient: subscribeHTTPClient}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Preview normalizes and fetches rawURL, parses it, and proposes a name and
// Calendar color, without creating a Calendar or writing any Events — so
// the User can review and edit before committing.
func (s *SubscribeService) Preview(ctx context.Context, userID int64, rawURL string) (SubscribePreview, error) {
	// keepAlarms is irrelevant to a Preview — it never writes Events, so
	// whether Reminders would be kept doesn't change what's shown.
	writes, parsed, _, err := s.fetchAndParse(ctx, rawURL, false)
	if err != nil {
		return SubscribePreview{}, err
	}

	existing, err := s.calendars.List(ctx, userID)
	if err != nil {
		return SubscribePreview{}, fmt.Errorf("list calendars: %w", err)
	}
	name, color := proposeNameColor(parsed, len(existing))

	preview := SubscribePreview{Name: name, Color: color, EventCount: len(writes)}
	for _, w := range writes {
		extendPreviewRange(&preview, w.Start, w.End)
		for _, o := range w.Overrides {
			extendPreviewRange(&preview, o.Start, o.End)
		}
	}
	return preview, nil
}

// Subscribe re-fetches rawURL (Preview and Subscribe are independent
// synchronous fetches — there is no server-side state carried between
// them), creates a Calendar carrying it as SourceURL, and writes every
// parsed series via ImportSubscribedSeries. name/color are validated the
// same way CalendarService.Create validates any other Calendar. keepAlarms
// is the Subscription's opt-in to honouring the feed's VALARMs on both
// Channels, off by default — the dialog offering it must state plainly
// that this includes email alarms the instance will send (#87, ADR-0032).
func (s *SubscribeService) Subscribe(ctx context.Context, userID, workspaceID int64, rawURL, name, color string, keepAlarms bool) (repository.Calendar, error) {
	writes, parsed, normalized, err := s.fetchAndParse(ctx, rawURL, keepAlarms)
	if err != nil {
		return repository.Calendar{}, err
	}

	// The shadow starts at whatever the feed actually supplied, regardless
	// of what name/color the User finally chose in the dialog — the User
	// deliberately picking something else at Subscribe time is exactly the
	// same "diverged from the feed" state a later rename produces, and the
	// two are meant to compare identically (#88, ADR-0032).
	var feedName *string
	if parsed.CalendarName != "" {
		feedName = &parsed.CalendarName
	}
	var feedColor *string
	if normalizedFeedColor, ok := NormalizeColor(parsed.Color); ok {
		feedColor = &normalizedFeedColor
	}

	calendar, err := s.calendars.Create(ctx, userID, workspaceID, uuid.NewString(), CalendarWrite{
		Name:       name,
		Color:      color,
		SourceURL:  &normalized,
		KeepAlarms: keepAlarms,
		FeedName:   feedName,
		FeedColor:  feedColor,
	})
	if err != nil {
		return repository.Calendar{}, err
	}

	if len(writes) > 0 {
		// The just-created Calendar carries the SourceURL that makes it a
		// Subscribed Calendar, so an ordinary ImportSeries would now refuse
		// this write (ADR-0032) — ImportSubscribedSeries is the deliberate
		// bypass for exactly this case.
		if _, err := s.events.ImportSubscribedSeries(ctx, userID, calendar.ID, writes); err != nil {
			return repository.Calendar{}, fmt.Errorf("subscribe %q: %w", MaskURL(normalized), err)
		}
	}

	// Gives the new Subscription its first due time so the poller picks it
	// up without a client ever asking (#86, ADR-0033) — staggered so a batch
	// of Subscriptions created around the same time don't all come due on
	// the same tick.
	interval := effectiveRefreshInterval(s.defaultRefreshInterval, durationSecondsPtr(parsed.RefreshInterval))
	next := nextRefreshTime(s.now().UTC(), calendar.ID, interval, interval)
	if err := s.calendars.ScheduleNextRefresh(ctx, userID, calendar.ID, next); err != nil {
		return repository.Calendar{}, fmt.Errorf("schedule next refresh: %w", err)
	}

	return calendar, nil
}

// UpdateSourceURL changes calendarID's Subscription URL (#88, ADR-0032): a
// publisher moving their feed shouldn't force an unsubscribe-and-restart.
// rawURL is normalized and format-validated exactly as Subscribe's own URL
// is — reachability isn't checked here; the next Refresh (scheduled or
// manual) discovers whether the new URL actually answers, surfaced through
// the same needs_attention/retrying machinery any other feed failure uses
// (ADR-0033). The stored conditional-GET validators are reset alongside
// the URL (CalendarRepository.UpdateSourceURL) so the next Refresh doesn't
// send validators earned from the old feed to whatever now answers the
// new one.
func (s *SubscribeService) UpdateSourceURL(ctx context.Context, userID int64, calendarID, rawURL string) (repository.Calendar, error) {
	calendar, err := s.calendars.Get(ctx, userID, calendarID)
	if err != nil {
		return repository.Calendar{}, err
	}
	if calendar.SourceURL == nil {
		return repository.Calendar{}, ErrRefreshNotSubscribed
	}

	normalized, err := normalizeSubscribeURL(rawURL)
	if err != nil {
		return repository.Calendar{}, err
	}

	updated, err := s.calendars.UpdateSourceURL(ctx, userID, calendarID, normalized)
	if err != nil {
		return repository.Calendar{}, fmt.Errorf("update source url: %w", err)
	}
	return updated, nil
}

// UpdateKeepAlarms changes calendarID's Subscription KeepAlarms setting
// (#87, ADR-0032). Turning it on takes effect at the next Refresh that
// actually reconciles a series — the content diff includes Reminders (see
// reconcile.go), so a forced "Refresh now" picks it up reliably even
// against an otherwise-unchanged feed. Turning it off is not deferred: it
// immediately clears every Reminder a prior Refresh created from the feed,
// since "off" must mean no Reminders exist right now, not merely that no
// more will be added later. A no-op call (the value isn't actually
// changing) still round-trips through the repository rather than short-
// circuiting on calendar.KeepAlarms == keepAlarms, keeping this method's
// behavior independent of whatever staleness the caller's own read of
// calendar happened to have.
func (s *SubscribeService) UpdateKeepAlarms(ctx context.Context, userID int64, calendarID string, keepAlarms bool) (repository.Calendar, error) {
	calendar, err := s.calendars.Get(ctx, userID, calendarID)
	if err != nil {
		return repository.Calendar{}, err
	}
	if calendar.SourceURL == nil {
		return repository.Calendar{}, ErrRefreshNotSubscribed
	}

	updated, err := s.calendars.UpdateKeepAlarms(ctx, userID, calendarID, keepAlarms)
	if err != nil {
		return repository.Calendar{}, fmt.Errorf("update keep alarms: %w", err)
	}

	if !keepAlarms {
		if err := s.events.ClearSubscribedCalendarReminders(ctx, userID, calendarID); err != nil {
			return repository.Calendar{}, fmt.Errorf("clear reminders: %w", err)
		}
	}

	return updated, nil
}

// RefreshResult is what Refresh actually did, for its caller (the HTTP
// handler, later the poller — #86) to report (#85, ADR-0033).
type RefreshResult struct {
	// NotModified is true when the feed's own validators, or (lacking
	// those) a content hash, showed it hasn't changed since the last
	// successful Refresh — no parse, no writes; every other field is zero.
	NotModified bool
	// Created, Updated, and Tombstoned mirror ReconcileSummary.
	Created, Updated, Tombstoned int
	// Unparseable is how many existing series were left untouched because
	// the feed still lists them but this fetch couldn't parse them.
	Unparseable int
	// NoOp is how many series the feed still lists, unchanged from what was
	// already stored.
	NoOp int
}

// Refresh brings calendarID's Subscribed Calendar up to date with its feed
// (#85, ADR-0033). Unless force is true, it starts with a conditional GET
// using the validators stored from the last successful Refresh — if the
// server answers 304, or (for a feed sending neither ETag nor
// Last-Modified) the fetched body hashes the same as last time, it stops
// there: no parse, no writes, only last_synced_at moves. Otherwise it
// parses the feed and reconciles it against what's stored, series by
// series, keyed by ExternalUID (ReconcileSeries), then applies the result
// via EventService.ReconcileSubscribedSeries — the same bypass Subscribe's
// initial import uses (ADR-0032). force bypasses the conditional-GET/
// content-hash short-circuit entirely: manual "Refresh now" always sets it,
// so the action is never a visible no-op even when the server believes
// nothing has changed.
func (s *SubscribeService) Refresh(ctx context.Context, userID int64, calendarID string, force bool) (RefreshResult, error) {
	calendar, err := s.calendars.Get(ctx, userID, calendarID)
	if err != nil {
		return RefreshResult{}, err
	}
	if calendar.SourceURL == nil {
		return RefreshResult{}, ErrRefreshNotSubscribed
	}

	result, syncOutcome, refreshErr := s.doRefresh(ctx, userID, calendar, force)
	now := s.now().UTC()

	if refreshErr != nil {
		// The last good Events, and the validators that produced them, are
		// left exactly as they were (#86, ADR-0033) — only the failure/
		// backoff columns move.
		failureCount := calendar.FailureCount + 1
		base := effectiveRefreshInterval(s.defaultRefreshInterval, calendar.RefreshIntervalSeconds)
		next := nextRefreshTime(now, calendar.ID, backoffInterval(base, failureCount), base)
		class, message := classifyRefreshError(refreshErr)
		if err := s.calendars.RecordRefreshFailure(ctx, userID, calendarID, repository.RefreshFailure{
			ErrorClass: class, ErrorMessage: message, FailureCount: failureCount, NextRefreshAt: next,
		}); err != nil {
			return RefreshResult{}, fmt.Errorf("record refresh failure: %w", err)
		}
		return RefreshResult{}, refreshErr
	}

	intervalSeconds := calendar.RefreshIntervalSeconds
	if syncOutcome.refreshIntervalSeconds != nil {
		intervalSeconds = syncOutcome.refreshIntervalSeconds
	}
	base := effectiveRefreshInterval(s.defaultRefreshInterval, intervalSeconds)
	next := nextRefreshTime(now, calendar.ID, base, base)

	if err := s.calendars.RecordRefreshSuccess(ctx, userID, calendarID, repository.RefreshSuccess{
		SyncedAt: now, ETag: syncOutcome.etag, LastModified: syncOutcome.lastModified, ContentHash: syncOutcome.contentHash,
		NextRefreshAt: next, RefreshIntervalSeconds: intervalSeconds,
		Name: syncOutcome.name, Color: syncOutcome.color, FeedName: syncOutcome.feedName, FeedColor: syncOutcome.feedColor,
	}); err != nil {
		return RefreshResult{}, fmt.Errorf("record refresh success: %w", err)
	}

	return result, nil
}

// refreshSyncOutcome is what one doRefresh call learned about the feed's
// own validators/cadence, for Refresh to persist once the attempt is known
// to have succeeded — doRefresh itself performs no writes to the Calendar
// row (#86, ADR-0033).
type refreshSyncOutcome struct {
	etag, lastModified, contentHash *string
	// refreshIntervalSeconds is set only when this attempt actually parsed
	// the feed and it stated a cadence — nil on a 304/content-hash no-op
	// (nothing was parsed) or a feed stating none, in which case Refresh
	// keeps whatever was already stored.
	refreshIntervalSeconds *int
	// name/color are what Refresh should store as the Calendar's displayed
	// Name/Color after this attempt: a pass-through of calendar's existing
	// values on a NotModified short-circuit (nothing to compare against),
	// or recomputed against the feed's own values otherwise. feedName/
	// feedColor are the shadow those decisions are made from, similarly
	// passed through unchanged when nothing was parsed (#88, ADR-0032).
	name, color         string
	feedName, feedColor *string
}

// doRefresh performs one fetch-and-reconcile attempt against calendar's
// feed and reports what happened, without writing anything back to the
// Calendar row itself — Refresh does that once, uniformly, whether this
// call succeeded or failed (#86, ADR-0033). Unless force is true, it starts
// with a conditional GET using the validators stored from the last
// successful Refresh — if the server answers 304, or (for a feed sending
// neither ETag nor Last-Modified) the fetched body hashes the same as last
// time, it stops there: no parse, no writes. Otherwise it parses the feed
// and reconciles it against what's stored, series by series, keyed by
// ExternalUID (ReconcileSeries), then applies the result via
// EventService.ReconcileSubscribedSeries — the same bypass Subscribe's
// initial import uses (ADR-0032). force bypasses the conditional-GET/
// content-hash short-circuit entirely: manual "Refresh now" always sets it,
// so the action is never a visible no-op even when the server believes
// nothing has changed.
func (s *SubscribeService) doRefresh(ctx context.Context, userID int64, calendar repository.Calendar, force bool) (RefreshResult, refreshSyncOutcome, error) {
	var etag, lastModified *string
	if !force {
		etag, lastModified = calendar.ETag, calendar.LastModified
	}

	fetched, err := s.fetchICSConditional(ctx, *calendar.SourceURL, etag, lastModified)
	if err != nil {
		return RefreshResult{}, refreshSyncOutcome{}, err
	}

	if fetched.NotModified {
		// A 304 response carries no ETag/Last-Modified of its own (RFC 7232
		// doesn't require the server to repeat them), so the validators that
		// earned this 304 are still the ones to send on the next Refresh —
		// overwriting them with the empty response would leave nothing to
		// send and turn every later Refresh into an unconditional fetch.
		return RefreshResult{NotModified: true}, refreshSyncOutcome{
			etag: calendar.ETag, lastModified: calendar.LastModified, contentHash: calendar.ContentHash,
			name: calendar.Name, color: calendar.Color, feedName: calendar.FeedName, feedColor: calendar.FeedColor,
		}, nil
	}

	hash := contentHash(fetched.Data)
	if !force && calendar.ContentHash != nil && *calendar.ContentHash == hash {
		return RefreshResult{NotModified: true}, refreshSyncOutcome{
			etag: fetched.ETag, lastModified: fetched.LastModified, contentHash: &hash,
			name: calendar.Name, color: calendar.Color, feedName: calendar.FeedName, feedColor: calendar.FeedColor,
		}, nil
	}

	// 0, 0: Refresh ignores ATTACH entirely (ADR-0040) — an unattended hourly
	// poller against a URL the instance doesn't control gets no attachment
	// ingestion, unlike a human-driven Import. Passing zero limits means
	// every ATTACH parseSeriesAttachments sees is declined, never accepted.
	parsed, err := icalendar.ParseImportFile(bytes.NewReader(fetched.Data), 0, 0)
	if err != nil {
		return RefreshResult{}, refreshSyncOutcome{}, fmt.Errorf("%w: %s", ErrSubscribeUnparseable, MaskURL(*calendar.SourceURL))
	}

	// dropUnstorableSeries records each series it drops on parsed.Skipped
	// under its own UID, so the unparseable set below covers both a series
	// this fetch couldn't decode and one it decoded but can't store: either
	// way the feed still lists that UID, and ReconcileSeries must leave the
	// stored row alone rather than tombstone it (#228, ADR-0033).
	writes := dropUnstorableSeries(parsed, buildWrites(parsed, calendar.KeepAlarms))
	incoming := make([]IncomingSeries, 0, len(writes))
	for _, w := range writes {
		incoming = append(incoming, IncomingSeries{ExternalUID: w.ExternalUID, Write: w})
	}
	unparseable := make(map[string]bool, len(parsed.Skipped))
	for _, skipped := range parsed.Skipped {
		if skipped.UID != "" {
			unparseable[skipped.UID] = true
		}
	}

	masters, overridesByParent, err := s.events.ListSeriesByCalendar(ctx, userID, calendar.ID)
	if err != nil {
		return RefreshResult{}, refreshSyncOutcome{}, fmt.Errorf("list existing series: %w", err)
	}
	storedReminders, err := s.events.ListStoredReminders(ctx, userID, seriesEventIDs(masters, overridesByParent))
	if err != nil {
		return RefreshResult{}, refreshSyncOutcome{}, fmt.Errorf("list stored reminders: %w", err)
	}
	existing := existingSeriesFromMasters(masters, overridesByParent, storedReminders)

	result := ReconcileSeries(existing, incoming, unparseable)

	summary, err := s.events.ReconcileSubscribedSeries(ctx, userID, calendar.ID, result)
	if err != nil {
		return RefreshResult{}, refreshSyncOutcome{}, err
	}

	newName, newFeedName := resolveFollowedField(calendar.Name, calendar.FeedName, parsed.CalendarName)
	feedColorValue := ""
	if normalized, ok := NormalizeColor(parsed.Color); ok {
		feedColorValue = normalized
	}
	newColor, newFeedColor := resolveFollowedField(calendar.Color, calendar.FeedColor, feedColorValue)

	return RefreshResult{
			Created:     summary.Created,
			Updated:     summary.Updated,
			Tombstoned:  summary.Tombstoned,
			Unparseable: result.SkippedCount,
			NoOp:        result.NoOpCount,
		}, refreshSyncOutcome{
			etag: fetched.ETag, lastModified: fetched.LastModified, contentHash: &hash, refreshIntervalSeconds: durationSecondsPtr(parsed.RefreshInterval),
			name: newName, color: newColor, feedName: newFeedName, feedColor: newFeedColor,
		}, nil
}

// resolveFollowedField applies #88's name/color-follows-publisher rule to
// one field: currentDisplay is what's shown now, currentShadow is the last
// feed value it was compared against, and feedValue is what this fetch
// just supplied ("" meaning the feed didn't supply one this time). It
// returns the new displayed value and the new shadow to store.
//
//   - feedValue == "": the feed supplied nothing this round — leave both
//     alone (ADR-0032: a feed that stops supplying a name/colour must not
//     blank the existing value).
//   - currentShadow is nil, or currentDisplay still equals it: the User
//     hasn't diverged from the feed (or there was nothing to diverge from
//     yet) — track the publisher's latest value in both.
//   - otherwise: the User has customized the displayed value — leave it,
//     but still record what the feed supplied, so a later coincidental
//     match resumes tracking. The comparison above is the entire
//     "overridden" flag; there is no separate one.
func resolveFollowedField(currentDisplay string, currentShadow *string, feedValue string) (display string, shadow *string) {
	if feedValue == "" {
		return currentDisplay, currentShadow
	}
	if currentShadow == nil || currentDisplay == *currentShadow {
		return feedValue, &feedValue
	}
	return currentDisplay, &feedValue
}

// existingSeriesFromMasters converts a Subscribed Calendar's current
// Masters and Overrides (as ListSeriesByCalendar reads them) into
// ReconcileSeries' existing-side input, keyed by ExternalUID. A Master with
// no ExternalUID can't occur in a Subscribed Calendar — every write path
// that populates one sets it — but is skipped defensively rather than
// panicking on the nil dereference.
//
// storedReminders supplies each Master's and Override's Reminders instead of
// m.Reminders/o.Reminders: those are the Owner's *resolved* set (Calendar
// defaults included), and a default never reaches the diff (#220) — only
// what is actually stored on the series can compare equal to what a feed
// sends.
func existingSeriesFromMasters(masters []repository.Event, overridesByParent map[string][]repository.Event, storedReminders map[string][]repository.Reminder) []ExistingSeries {
	existing := make([]ExistingSeries, 0, len(masters))
	for _, m := range masters {
		if m.ExternalUID == nil {
			continue
		}

		content := SeriesWrite{
			Title:       m.Title,
			Description: m.Description,
			Location:    m.Location,
			URL:         m.URL,
			Start:       m.Start,
			End:         m.End,
			AllDay:      m.AllDay,
			Tzid:        m.Tzid,
			Rrule:       m.Rrule,
			Exdates:     m.Exdates,
			Reminders:   storedReminders[m.ID],
		}
		for _, o := range overridesByParent[m.ID] {
			content.Overrides = append(content.Overrides, OverrideWrite{
				RecurrenceID: *o.RecurrenceID,
				Title:        o.Title,
				Description:  o.Description,
				Location:     o.Location,
				URL:          o.URL,
				Start:        o.Start,
				End:          o.End,
				AllDay:       o.AllDay,
				Tzid:         o.Tzid,
				Reminders:    storedReminders[o.ID],
			})
		}

		existing = append(existing, ExistingSeries{MasterID: m.ID, ExternalUID: *m.ExternalUID, Content: content})
	}
	return existing
}

// seriesEventIDs collects every Master and Override id across a Subscribed
// Calendar's series — the set doRefresh needs stored Reminders for.
func seriesEventIDs(masters []repository.Event, overridesByParent map[string][]repository.Event) []string {
	ids := make([]string, 0, len(masters))
	for _, m := range masters {
		ids = append(ids, m.ID)
		for _, o := range overridesByParent[m.ID] {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

// fetchAndParse is Preview and Subscribe's shared prefix: normalize, fetch,
// parse, and build the writes the feed would produce, minus any series this
// app cannot store. Returns the normalized URL alongside so Subscribe
// doesn't need to normalize twice.
func (s *SubscribeService) fetchAndParse(ctx context.Context, rawURL string, keepAlarms bool) ([]SeriesWrite, *icalendar.ParsedFile, string, error) {
	normalized, err := normalizeSubscribeURL(rawURL)
	if err != nil {
		return nil, nil, "", err
	}

	data, err := s.fetchICS(ctx, normalized)
	if err != nil {
		return nil, nil, "", err
	}

	// 0, 0: Subscribe's initial import goes through this same parse, and
	// ADR-0040 draws no exception for it — attachment ingestion is Import's
	// (#135), not Subscribe's.
	parsed, err := icalendar.ParseImportFile(bytes.NewReader(data), 0, 0)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %s", ErrSubscribeUnparseable, MaskURL(normalized))
	}

	// A series the feed states clearly but this app cannot store costs
	// itself and nothing more (#228) — the alternative is a Subscription a
	// publisher can break wholesale with one bad VEVENT nobody subscribing
	// can edit.
	writes := dropUnstorableSeries(parsed, buildWrites(parsed, keepAlarms))

	return writes, parsed, normalized, nil
}

// normalizeSubscribeURL trims rawURL, maps a webcal:// scheme to https://,
// and rejects anything other than http/https/webcal or a URL with no host.
func normalizeSubscribeURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", ErrSubscribeInvalidURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", ErrSubscribeInvalidURL
	}

	switch strings.ToLower(parsed.Scheme) {
	case "webcal":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", ErrSubscribeInvalidURL
	}
	if parsed.Host == "" {
		return "", ErrSubscribeInvalidURL
	}

	return parsed.String(), nil
}

// fetchResult is one fetchICSConditional call's outcome: either the feed
// hadn't changed (NotModified, Data empty) or its full body plus whichever
// response validators it sent, if any (#85, ADR-0033).
type fetchResult struct {
	Data               []byte
	ETag, LastModified *string
	NotModified        bool
}

// fetchICS performs the one synchronous, unconditional GET a Preview or
// Subscribe call makes — Subscribe has no prior state to compare against,
// so there is nothing to send a conditional header for.
func (s *SubscribeService) fetchICS(ctx context.Context, rawURL string) ([]byte, error) {
	result, err := s.fetchICSConditional(ctx, rawURL, nil, nil)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// fetchICSConditional performs the one synchronous GET a Refresh makes:
// HTTP Basic from the URL's userinfo (never left in the request line —
// stripped from req.URL once captured), a subscribeFetchTimeout ceiling
// applied via context (so a caller — e.g. a test — may impose a tighter
// deadline instead), and a maxSubscribeFetchBytes cap enforced by reading
// one byte past it rather than trusting Content-Length, so a feed lying
// about its size can't exhaust memory either. etag/lastModified, if
// non-nil, are sent as If-None-Match/If-Modified-Since; a 304 response
// short-circuits to fetchResult.NotModified with no body read beyond
// draining it. Passing both nil (Preview/Subscribe's case) never asks the
// server to compare anything, so it can't come back 304.
func (s *SubscribeService) fetchICSConditional(ctx context.Context, rawURL string, etag, lastModified *string) (fetchResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fetchResult{}, ErrSubscribeInvalidURL
	}

	ctx, cancel := context.WithTimeout(ctx, subscribeFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchResult{}, ErrSubscribeInvalidURL
	}
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		req.SetBasicAuth(parsed.User.Username(), password)
		req.URL.User = nil
	}
	if etag != nil {
		req.Header.Set("If-None-Match", *etag)
	}
	if lastModified != nil {
		req.Header.Set("If-Modified-Since", *lastModified)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, ErrSubscribeURLBlocked) {
			return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeURLBlocked, MaskURL(rawURL))
		}
		return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeFetchFailed, MaskURL(rawURL))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need
		return fetchResult{NotModified: true}, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeAuthFailed, MaskURL(rawURL))
	}
	if resp.StatusCode == http.StatusNotFound {
		return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeNotFound, MaskURL(rawURL))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fetchResult{}, fmt.Errorf("%w: %s responded %d", ErrSubscribeFetchFailed, MaskURL(rawURL), resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscribeFetchBytes+1))
	if err != nil {
		return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeFetchFailed, MaskURL(rawURL))
	}
	if len(data) > maxSubscribeFetchBytes {
		return fetchResult{}, fmt.Errorf("%w: %s", ErrSubscribeTooLarge, MaskURL(rawURL))
	}

	result := fetchResult{Data: data}
	if v := resp.Header.Get("ETag"); v != "" {
		result.ETag = &v
	}
	if v := resp.Header.Get("Last-Modified"); v != "" {
		result.LastModified = &v
	}
	return result, nil
}

// contentHash is the fallback freshness check for a feed that sends neither
// ETag nor Last-Modified: a hex SHA-256 of the fetched body, compared after
// a full fetch since there's no validator to ask the server to compare for
// us (#85, ADR-0033).
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildWrites converts parsed's series into ImportSeries writes exactly the
// way ordinary import does (seriesWriteFromImported), then applies the two
// differences a Subscription requires: every Reminder is stripped unless
// keepAlarms is true, in which case they're kept faithfully on both
// Channels — matching ICS import's behaviour, per the Subscription's own
// opt-in (#87, ADR-0032) — and ExternalUID is set from the series' foreign
// UID on the Master and every Override (ADR-0033).
func buildWrites(parsed *icalendar.ParsedFile, keepAlarms bool) []SeriesWrite {
	writes := make([]SeriesWrite, len(parsed.Series))
	for i, series := range parsed.Series {
		w := seriesWriteFromImported(series)
		w.ExternalUID = series.UID
		if !keepAlarms {
			w.Reminders = nil
		}
		for j := range w.Overrides {
			w.Overrides[j].ExternalUID = series.UID
			if !keepAlarms {
				w.Overrides[j].Reminders = nil
			}
		}
		writes[i] = w
	}
	return writes
}

// proposeNameColor mirrors ImportService.proposeTarget's fallback shape:
// name from the feed's X-WR-CALNAME (else "" — there is no filename to fall
// back to, so the frontend defaults the field to the URL's host); color
// from X-APPLE-CALENDAR-COLOR, else the next importColorRotation color,
// chosen by the caller's existing Calendar count so back-to-back
// Subscriptions don't all land on the same fallback color.
func proposeNameColor(parsed *icalendar.ParsedFile, existingCalendarCount int) (name, color string) {
	name = parsed.CalendarName

	color, ok := NormalizeColor(parsed.Color)
	if !ok {
		color = importColorRotation[existingCalendarCount%len(importColorRotation)]
	}

	return name, color
}

func extendPreviewRange(preview *SubscribePreview, start, end time.Time) {
	if preview.RangeStart == nil || start.Before(*preview.RangeStart) {
		s := start
		preview.RangeStart = &s
	}
	if preview.RangeEnd == nil || end.After(*preview.RangeEnd) {
		e := end
		preview.RangeEnd = &e
	}
}

// credentialPattern matches a URL's "user:password@" userinfo segment, as a
// fallback for MaskURL when url.Parse can't round-trip rawURL.
var credentialPattern = regexp.MustCompile(`://[^/@\s]*:[^/@\s]*@`)

// MaskURL redacts a Subscription URL's password, if it has one, for
// display: every surface that renders one — the sidebar, tooltips, error
// messages — must never show the raw credential (ADR-0032). A URL with no
// password, or one with no userinfo at all, is returned unchanged.
func MaskURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return credentialPattern.ReplaceAllString(rawURL, "://***:***@")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "••••••")
		}
	}
	return parsed.String()
}
