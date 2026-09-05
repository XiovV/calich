// connection_refresh.go implements Full Refresh for a Linked Calendar (#287,
// ADR-0053): fetching a Connection-kind Source's complete event listing and
// reconciling it into ordinary Events, so recurrence expansion, Anchor
// zones, the all-day lane, and overlap layout all work for free. Read-only,
// synchronous, and one-shot — no cursor, no poller wiring, and no
// conditional-GET short-circuit, all of which are #288's Delta Refresh and
// Connection poller to add. FullRefresh runs once, right after
// ImportCalendars creates a Linked Calendar's Source (connection_picker.go),
// and is reachable again later through the sidebar's existing manual
// "Refresh" action once that action is wired to a Connection-kind Calendar.
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/XiovV/calich/server/internal/repository"
)

// ErrRefreshNotLinked is returned by FullRefresh on a Calendar that carries
// no Connection-kind Source — there is nothing to fetch (#287), mirroring
// ErrRefreshNotSubscribed's own guard for a Subscription.
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
// or expired access token — the one status doFullRefresh treats as "the
// cached token needs replacing", as opposed to 403/404 (a real permission
// or not-found problem a fresh token wouldn't fix).
func isGoogleAccessTokenExpired(err error) bool {
	var httpErr *googleHTTPError
	return errors.As(err, &httpErr) && httpErr.statusCode == http.StatusUnauthorized
}

// mintAccessToken mints a fresh access token from conn's stored
// refresh_token and caches it back onto the Connection row (#287) — the
// fallback doFullRefresh reaches for only once conn's cached access_token
// has actually failed against Google, never unconditionally. Caching what
// this mints is what makes repository.Connection.UpdateAccessToken's own
// stated purpose real: "a Refresh moments later within the same access
// token's lifetime doesn't have to mint another". A failure to cache it is
// logged rather than propagated — it costs an extra mint on the next call,
// never correctness — but is worth knowing about, since every future
// Refresh would otherwise silently pay that cost forever.
func (s *ConnectionService) mintAccessToken(ctx context.Context, userID int64, conn repository.Connection) (string, error) {
	refreshToken, err := decryptRefreshToken(s.encryptionKey, conn.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}

	tokens, err := s.google.refreshAccessToken(ctx, refreshToken)
	if err != nil {
		return "", err
	}

	if err := s.connections.UpdateAccessToken(ctx, userID, conn.ID, tokens.AccessToken); err != nil {
		log.Printf("cache refreshed google access token (connection=%d): %v", conn.ID, err)
	}

	return tokens.AccessToken, nil
}

// FullRefresh brings calendarID's Linked Calendar up to date with its
// Provider calendar (#287, ADR-0053's Full mode): the complete event
// listing is fetched — no time window, paginating through every page — and
// mapped to a domain SeriesWrite per series (google_mapper.go), then
// reconciled against what's already stored, series by series, keyed by
// ExternalUID (ReconcileSeries), and applied via
// EventService.ReconcileSubscribedSeries — the same bypass Subscribe's
// initial import and its own Refresh use (ADR-0032). A series this fetch's
// listing names but cannot itself resolve (an orphaned recurring instance,
// google_mapper.go's own defensive case) is folded into ReconcileSeries'
// unparseableUIDs, so it is left untouched rather than tombstoned — ADR-
// 0053's "present but unparseable is never a reason to tombstone" applies
// here exactly as it does to a Subscription's own unparseable series.
func (s *ConnectionService) FullRefresh(ctx context.Context, userID int64, calendarID string) (RefreshResult, error) {
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

	result, refreshErr := s.doFullRefresh(ctx, userID, calendar, *source)
	if refreshErr != nil {
		failureCount := source.FailureCount + 1
		class, message := classifyGoogleError(refreshErr)
		if err := s.calendars.RecordConnectionRefreshFailure(ctx, userID, calendarID, class, message, failureCount); err != nil {
			return RefreshResult{}, fmt.Errorf("record refresh failure: %w", err)
		}
		return RefreshResult{}, refreshErr
	}

	if err := s.calendars.RecordConnectionRefreshSuccess(ctx, userID, calendarID, s.now().UTC()); err != nil {
		return RefreshResult{}, fmt.Errorf("record refresh success: %w", err)
	}

	return result, nil
}

// doFullRefresh performs one fetch-and-reconcile attempt against
// calendarID's Provider calendar, without writing anything to the Source
// row itself — FullRefresh does that once, uniformly, whether this call
// succeeded or failed, mirroring SubscribeService's own doRefresh split.
// listEvents is tried first with the Connection's own cached access_token —
// never relied on as durable, but usually still good, especially moments
// after Connect/Callback minted it or a prior Refresh cached one (#287) —
// and only on a 401 is a fresh one minted and the fetch retried exactly
// once. Importing N calendars off one Connection therefore costs at most
// one token mint total, not one per calendar.
func (s *ConnectionService) doFullRefresh(ctx context.Context, userID int64, calendar repository.Calendar, source repository.Source) (RefreshResult, error) {
	conn, err := s.connections.GetByID(ctx, userID, *source.ConnectionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return RefreshResult{}, ErrConnectionNotFound
		}
		return RefreshResult{}, fmt.Errorf("get connection: %w", err)
	}

	accessToken := ""
	if conn.AccessToken != nil {
		accessToken = *conn.AccessToken
	}

	events, err := s.google.listEvents(ctx, accessToken, *source.ExternalCalendarID)
	if isGoogleAccessTokenExpired(err) {
		accessToken, err = s.mintAccessToken(ctx, userID, conn)
		if err != nil {
			return RefreshResult{}, err
		}
		events, err = s.google.listEvents(ctx, accessToken, *source.ExternalCalendarID)
	}
	if err != nil {
		return RefreshResult{}, err
	}

	incoming, mapping := mapGoogleEvents(events)
	unparseable := make(map[string]bool, len(mapping.OrphanExternalUIDs))
	for _, uid := range mapping.OrphanExternalUIDs {
		unparseable[uid] = true
	}

	result, summary, err := reconcileAgainstStored(ctx, s.events, userID, calendar.ID, incoming, unparseable)
	if err != nil {
		return RefreshResult{}, err
	}

	droppedCount := 0
	for _, g := range mapping.Dropped {
		droppedCount += g.Count
	}

	return RefreshResult{
		Created:                summary.Created,
		Updated:                summary.Updated,
		Tombstoned:             summary.Tombstoned,
		Unparseable:            result.SkippedCount,
		NoOp:                   result.NoOpCount,
		DroppedRecurrenceLines: droppedCount,
	}, nil
}
