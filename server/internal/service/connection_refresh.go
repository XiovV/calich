// connection_refresh.go implements Refresh for a Linked Calendar (#287,
// #288, ADR-0053): fetching a Connection-kind Source's events and
// reconciling them into ordinary Events, so recurrence expansion, Anchor
// zones, the all-day lane and overlap layout all work for free.
//
// Two modes, an explicit argument at every call site (RefreshMode), never
// inferred from whether a cursor happens to be present:
//
//   - Full: the complete listing is fetched and reconciled by ReconcileSeries
//     — a series the listing no longer carries has genuinely been deleted and
//     is tombstoned. The first Refresh after ImportCalendars, and recovery
//     after a 410 GONE cursor expiry.
//   - Delta: a stored cursor is presented and only changes come back;
//     reconciled by ReconcileDelta, in which tombstone-by-absence is
//     structurally unreachable — absence means unchanged, and a deletion is
//     applied only where the Provider states one explicitly.
//
// RefreshLinked runs from the Connection poller (poller.go) on a ~15-minute
// cadence and from the sidebar's manual "Refresh" action; FullRefresh is the
// mode-fixed wrapper ImportCalendars' initial import uses.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/XiovV/calich/server/internal/repository"
)

// RefreshMode is Refresh's most important input (#288, ADR-0053): it decides
// whether a series absent from the fetch may be tombstoned. It is passed
// explicitly by whoever schedules the Refresh — never derived inside the
// reconciler from cursor state, so "cursor missing due to a storage failure"
// and "cursor deliberately absent" can never silently diverge into different
// tombstone behaviour.
type RefreshMode int

const (
	// RefreshModeFull reconciles a complete listing; absence means deletion.
	RefreshModeFull RefreshMode = iota
	// RefreshModeDelta reconciles only what the Provider reports as changed
	// since the cursor; absence means unchanged, and tombstone-by-absence is
	// structurally unreachable (ReconcileDelta).
	RefreshModeDelta
)

// ErrRefreshNotLinked is returned by RefreshLinked on a Calendar that
// carries no Connection-kind Source — there is nothing to fetch (#287),
// mirroring ErrRefreshNotSubscribed's own guard for a Subscription.
var ErrRefreshNotLinked = errors.New("calendar is not a linked calendar")

// requireConnectionSource returns calendar's Source if it's a Connection
// (#287, ADR-0052), ErrRefreshNotLinked otherwise.
func requireConnectionSource(calendar repository.Calendar) (*repository.Source, error) {
	if calendar.Source == nil || calendar.Source.Kind != repository.SourceKindConnection {
		return nil, ErrRefreshNotLinked
	}
	return calendar.Source, nil
}

// classifyGoogleError sorts a failed Full Refresh into ADR-0053's two
// classes: needs-attention (400/401/403/404 — reconnecting or the calendar
// having vanished at Google needs a human) or retrying (timeouts, 5xx, DNS —
// expected to clear on its own). 400 is here because a revoked or expired
// refresh_token fails the OAuth refresh grant with exactly that status
// (RFC 6749's invalid_grant) — refreshAccessToken's own doc comment already
// says as much. ErrConnectionNotFound is its own case rather than a
// *googleHTTPError: the Connection row itself having vanished (a concurrent
// Disconnect) is never a Google-side failure a retry could clear. Every
// other failure (a build error, a decode error, a plain network failure
// with no response at all) falls to retrying, matching
// classifyRefreshError's own default.
func classifyGoogleError(err error) (class, message string) {
	if errors.Is(err, ErrConnectionNotFound) {
		return ErrorClassNeedsAttention, err.Error()
	}
	var httpErr *googleHTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.statusCode {
		case 400, 401, 403, 404:
			return ErrorClassNeedsAttention, err.Error()
		}
	}
	return ErrorClassRetrying, err.Error()
}

// isGoogleAccessTokenExpired reports whether err is Google's 401 for a bad
// or expired access token — the one status doRefresh treats as "the
// cached token needs replacing", as opposed to 403/404 (a real permission
// or not-found problem a fresh token wouldn't fix).
func isGoogleAccessTokenExpired(err error) bool {
	var httpErr *googleHTTPError
	return errors.As(err, &httpErr) && httpErr.statusCode == http.StatusUnauthorized
}

// mintAccessToken mints a fresh access token from conn's stored
// refresh_token and caches it back onto the Connection row (#287) — the
// fallback doRefresh (and SendWriteBack) reaches for only once conn's cached
// access_token has actually failed against Google, never unconditionally.
// Caching what this mints is what makes repository.Connection.
// UpdateAccessToken's own stated purpose real: "a Refresh moments later
// within the same access token's lifetime doesn't have to mint another". A
// failure to cache it is logged rather than propagated — it costs an extra
// mint on the next call, never correctness — but is worth knowing about,
// since every future Refresh would otherwise silently pay that cost forever.
//
// A refresh_token Google no longer honours — expired, or a User revoking
// access at their Google account directly — fails here with the exact same
// classifyGoogleError-needs-attention shape a bad grant always has (#291,
// ADR-0075's "including ADR-0051's seven-day Testing-status trap"). That is
// recorded onto the Connection itself as Expired, so
// EventService.requireLiveConnection can refuse new edits on its Linked
// Calendars before ever queuing another push against a grant that's gone —
// logged rather than propagated on its own failure to write, mirroring the
// access-token cache above: the caller's own error is what matters, not
// whether this side-effect landed.
func (s *ConnectionService) mintAccessToken(ctx context.Context, userID int64, conn repository.Connection) (string, error) {
	refreshToken, err := decryptRefreshToken(s.encryptionKey, conn.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}

	tokens, err := s.google.refreshAccessToken(ctx, refreshToken)
	if err != nil {
		if class, _ := classifyGoogleError(err); class == ErrorClassNeedsAttention {
			if statusErr := s.connections.UpdateStatus(ctx, userID, conn.ID, repository.ConnectionStatusExpired); statusErr != nil {
				log.Printf("record connection expired (connection=%d): %v", conn.ID, statusErr)
			}
		}
		return "", err
	}

	if err := s.connections.UpdateAccessToken(ctx, userID, conn.ID, tokens.AccessToken); err != nil {
		log.Printf("cache refreshed google access token (connection=%d): %v", conn.ID, err)
	}

	return tokens.AccessToken, nil
}

// FullRefresh is RefreshLinked fixed to Full mode — ImportCalendars' initial
// import uses it, where there is no cursor yet and nothing to reconcile
// against.
func (s *ConnectionService) FullRefresh(ctx context.Context, userID int64, calendarID string) (RefreshResult, error) {
	return s.RefreshLinked(ctx, userID, calendarID, RefreshModeFull)
}

// RefreshLinked brings calendarID's Linked Calendar up to date with its
// Provider calendar in the given mode (#287, #288, ADR-0053), then records
// the outcome once, uniformly, whether the attempt succeeded or failed:
//
//   - success → last_synced_at, the fresh cursor, and a staggered
//     next_refresh_at are stored, and any prior failure clears.
//   - failure → the classified error and a backoff-scheduled next_refresh_at
//     are stored; the Events, cursor and last_synced_at a prior success
//     produced are left exactly as they were, and the Connection is never
//     disabled (ADR-0033).
func (s *ConnectionService) RefreshLinked(ctx context.Context, userID int64, calendarID string, mode RefreshMode) (RefreshResult, error) {
	if !s.configured {
		return RefreshResult{}, ErrGoogleNotConfigured
	}

	calendar, err := s.calendars.Get(ctx, userID, calendarID)
	if err != nil {
		return RefreshResult{}, err
	}
	source, err := requireConnectionSource(calendar)
	if err != nil {
		return RefreshResult{}, err
	}

	result, outcome, refreshErr := s.doRefresh(ctx, userID, calendar, *source, mode)
	now := s.now().UTC()

	if refreshErr != nil {
		failureCount := source.FailureCount + 1
		class, message := classifyGoogleError(refreshErr)
		next := nextRefreshTime(now, calendar.ID, backoffInterval(s.connectionRefreshInterval, failureCount), s.connectionRefreshInterval)
		if err := s.calendars.RecordConnectionRefreshFailure(ctx, userID, calendarID, class, message, failureCount, next); err != nil {
			return RefreshResult{}, fmt.Errorf("record refresh failure: %w", err)
		}
		return RefreshResult{}, refreshErr
	}

	next := nextRefreshTime(now, calendar.ID, s.connectionRefreshInterval, s.connectionRefreshInterval)
	if err := s.calendars.RecordConnectionRefreshSuccess(ctx, userID, calendarID, repository.ConnectionRefreshSuccess{
		SyncedAt: now, Cursor: outcome.cursor, NextRefreshAt: next,
		Name: outcome.name, FeedName: outcome.feedName, Mode: outcome.mode,
	}); err != nil {
		// The fetch and reconcile already committed; only the cursor and the
		// poll schedule failed to persist. That is a correctness non-event —
		// with no stored cursor the next cycle is a Full Refresh, which is
		// safe (ADR-0053) — but it must be logged, since nothing else will
		// notice, and next_refresh_at not advancing means the poller retries
		// this row on its next tick.
		log.Printf("linked calendar refresh (calendar=%s): stored the Events but failed to persist the cursor/schedule (%v); next cycle will be a full refresh", calendarID, err)
		return RefreshResult{}, fmt.Errorf("record refresh success: %w", err)
	}

	return result, nil
}

// RefreshModeForCursor is the one rule every scheduling site uses to choose
// between Full and Delta (#288, ADR-0053): Delta once a cursor exists, Full
// otherwise. Shared so the poller and the manual-refresh handler cannot
// drift apart — and so the choice, made from cursor presence here, is the
// only place that reads cursor state to decide a mode. Everything downstream
// takes the mode as an explicit argument.
func RefreshModeForCursor(hasCursor bool) RefreshMode {
	if hasCursor {
		return RefreshModeDelta
	}
	return RefreshModeFull
}

// connectionSyncOutcome is what one doRefresh call learned about the
// Provider calendar's own cursor and name, for RefreshLinked to persist once
// the attempt is known to have succeeded — mirroring
// SubscribeService.refreshSyncOutcome's own split. name/feedName are what
// RefreshLinked should store as the Calendar's displayed Name and its
// tracking shadow after this attempt (#289, ADR-0052's "presentation is
// local"): resolveFollowedField applied to the Provider's own calendar
// summary, reused verbatim from ADR-0032's mechanism.
type connectionSyncOutcome struct {
	cursor   *string
	name     string
	feedName *string
	// mode is this cycle's freshly re-read Write-back writability (#290,
	// ADR-0075) — Google's own current AccessRole on the calendar, not
	// whatever the Source's Mode happened to be before this attempt. Always
	// set on a successful doRefresh, mirroring how cursor/name/feedName are
	// always resolved to *something* rather than left for the caller to
	// default.
	mode repository.SourceMode
}

// doRefresh performs one fetch-and-reconcile attempt against calendarID's
// Provider calendar in mode, without writing anything to the Source row
// itself — RefreshLinked does that once, uniformly, mirroring
// SubscribeService's own doRefresh split.
//
// The fetch is tried first with the Connection's own cached access_token —
// never relied on as durable, but usually still good, especially moments
// after Connect/Callback minted it or a prior Refresh cached one (#287) —
// and only on a 401 is a fresh one minted and the fetch retried exactly
// once. A 410 GONE on a Delta fetch (Google having invalidated the cursor,
// routinely on an ACL change) is not a failure: it is logged and the whole
// attempt re-runs in Full mode against the stored rows, matched by
// ExternalUID, so row ids are preserved and no CalDAV client re-downloads a
// collection in which nothing changed (ADR-0053).
func (s *ConnectionService) doRefresh(ctx context.Context, userID int64, calendar repository.Calendar, source repository.Source, mode RefreshMode) (RefreshResult, connectionSyncOutcome, error) {
	conn, err := s.connections.GetByID(ctx, userID, *source.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return RefreshResult{}, connectionSyncOutcome{}, ErrConnectionNotFound
		}
		return RefreshResult{}, connectionSyncOutcome{}, fmt.Errorf("get connection: %w", err)
	}

	cursor := ""
	if mode == RefreshModeDelta {
		if source.Cursor == nil {
			// The poller only picks Delta when the Source holds a cursor, and
			// the manual-refresh handler does the same — reaching here with
			// none is a caller bug, not a case to paper over by silently
			// running Full (which is a different, possibly destructive, mode).
			return RefreshResult{}, connectionSyncOutcome{}, fmt.Errorf("delta refresh requested for calendar %s with no stored cursor", calendar.ID)
		}
		cursor = *source.Cursor
	}

	accessToken := ""
	if conn.AccessToken != nil {
		accessToken = *conn.AccessToken
	}

	changes, err := s.google.listEventChanges(ctx, accessToken, *source.ExternalCalendarID, cursor)
	if isGoogleAccessTokenExpired(err) {
		accessToken, err = s.mintAccessToken(ctx, userID, conn)
		if err != nil {
			return RefreshResult{}, connectionSyncOutcome{}, err
		}
		changes, err = s.google.listEventChanges(ctx, accessToken, *source.ExternalCalendarID, cursor)
	}
	if errors.Is(err, errGoogleSyncTokenExpired) {
		log.Printf("linked calendar delta refresh (calendar=%s): cursor expired, falling back to full refresh", calendar.ID)
		return s.doRefresh(ctx, userID, calendar, source, RefreshModeFull)
	}
	if err != nil {
		return RefreshResult{}, connectionSyncOutcome{}, err
	}

	var result ReconcileResult
	var summary ReconcileSummary
	var droppedCount int

	switch mode {
	case RefreshModeDelta:
		deltaChanges, deletions, mapping := mapGoogleEventChanges(changes.Events)
		droppedCount = countDropped(mapping)
		result, summary, err = reconcileDeltaAgainstStored(ctx, s.events, userID, calendar.ID, deltaChanges, deletions)
		// An instance-only change the mapper couldn't anchor (no series to
		// merge into, no RECURRENCE-ID) is present-but-unmappable, exactly as
		// in Full mode — counted, never an absence.
		result.SkippedCount += len(mapping.OrphanExternalUIDs)
	case RefreshModeFull:
		incoming, mapping := mapGoogleEvents(changes.Events)
		droppedCount = countDropped(mapping)
		unparseable := make(map[string]bool, len(mapping.OrphanExternalUIDs))
		for _, uid := range mapping.OrphanExternalUIDs {
			unparseable[uid] = true
		}
		result, summary, err = reconcileAgainstStored(ctx, s.events, userID, calendar.ID, incoming, unparseable, true)
	default:
		// An unrecognised RefreshMode must never fall through to Full's
		// tombstoning path by accident — that is the whole reason the mode is
		// an explicit type rather than a bool (ADR-0053).
		return RefreshResult{}, connectionSyncOutcome{}, fmt.Errorf("unknown refresh mode %d for calendar %s", mode, calendar.ID)
	}
	if err != nil {
		return RefreshResult{}, connectionSyncOutcome{}, err
	}

	// Persist the fresh cursor when the Provider gave one; otherwise keep
	// whatever was stored (Google occasionally omits nextSyncToken on a page
	// it need not repeat — nil-ing a still-valid cursor would force a
	// needless Full Refresh next cycle). A genuinely absent cursor here still
	// only costs a Full Refresh, which is safe, so it is logged rather than
	// treated as a failure (ADR-0053).
	cursorToStore := source.Cursor
	if changes.NextSyncToken != "" {
		cursorToStore = &changes.NextSyncToken
	} else if source.Cursor == nil {
		log.Printf("linked calendar refresh (calendar=%s): provider returned no cursor; next cycle will be a full refresh", calendar.ID)
	}

	// changes.Summary is "" whenever this response omitted it — treated by
	// resolveFollowedField as "the Provider supplied nothing this round",
	// leaving both the displayed Name and its shadow exactly as they were
	// (#289, ADR-0032).
	newName, newFeedName := resolveFollowedField(calendar.Name, source.FeedName, changes.Summary)

	// Writability is per-Calendar, derived from Google's own accessRole and
	// re-read on every Refresh (#290, ADR-0075) — never fixed at import time,
	// since a calendar merely shared into the connected account, or one whose
	// Owner revokes write access later, is exactly the kind of thing an ACL
	// change silently flips. A failure to re-read it here doesn't fail the
	// whole Refresh, which already fetched and reconciled real Events
	// successfully: it's logged and the Source's existing Mode carries
	// forward unchanged, mirroring this function's own cursor-storage
	// leniency below.
	writeMode := source.Mode
	if entry, err := s.google.getCalendarListEntry(ctx, accessToken, *source.ExternalCalendarID); err != nil {
		log.Printf("linked calendar refresh (calendar=%s): could not re-read access role, keeping mode %q: %v", calendar.ID, source.Mode, err)
	} else if entry.writable() {
		writeMode = repository.SourceModeWritable
	} else {
		writeMode = repository.SourceModeReadOnly
	}

	return RefreshResult{
			Created:                summary.Created,
			Updated:                summary.Updated,
			Tombstoned:             summary.Tombstoned,
			Unparseable:            result.SkippedCount,
			NoOp:                   result.NoOpCount,
			DroppedRecurrenceLines: droppedCount,
		}, connectionSyncOutcome{
			cursor: cursorToStore, name: newName, feedName: newFeedName, mode: writeMode,
		}, nil
}

// countDropped totals a mapping summary's dropped-recurrence-line groups
// (#287) — RDATE/EXRULE lines this app's model has no home for, surfaced
// rather than silently lost.
func countDropped(mapping GoogleMappingSummary) int {
	count := 0
	for _, g := range mapping.Dropped {
		count += g.Count
	}
	return count
}
