// google.go is the Google half of the Provider seam (#285, ADR-0050, #286,
// #287, #288): OAuth token exchange (including a stored refresh_token minting
// a fresh access token, since one is never relied on across requests), the
// connected account's own Email, the calendarList the Calendar picker
// offers, a Linked Calendar's events.list — a complete listing for a Full
// Refresh, or only what changed since a syncToken for a Delta Refresh — and
// events.patch, the one Write-back call this app ever makes (#290,
// ADR-0075). Insert and instances are later tickets' (#292, #291), added to
// googleClient rather than beside it, so every Google call keeps going
// through the one overridable httpClient (#285's testing decisions) — a test
// points this at an httptest.Server serving canned JSON in place of Google,
// never a mocked fetcher.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	googleAuthorizeURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL        = "https://oauth2.googleapis.com/token"
	googleUserinfoURL     = "https://www.googleapis.com/oauth2/v2/userinfo"
	googleCalendarListURL = "https://www.googleapis.com/calendar/v3/users/me/calendarList"
	// googleEventsURL is events.list's own base, with one calendar's id
	// appended per call (eventsURLFor) — there is no single fixed URL the way
	// there is for calendarList, since events.list is scoped to one calendar.
	googleEventsURL = "https://www.googleapis.com/calendar/v3/calendars"
)

// googleCalendarListPageSize is the page size requested of calendarList.list
// (#286) — Google's own default and maximum both differ from this, but
// asking for a full page every time means an account with more calendars
// than most self-hosted Users will ever have still costs a small, bounded
// number of requests rather than one per few calendars.
const googleCalendarListPageSize = 250

// googleScopes is the exact scope set requested (#285, ADR-0052): Calendar
// events read/write plus calendar-list read — deliberately never the broad
// `.../auth/calendar` scope, which additionally permits creating/deleting
// calendars and editing their ACLs, none of which this app does. openid and
// email are what resolves the connected account's own address afterward,
// without a separate userinfo scope grant.
var googleScopes = []string{
	"openid",
	"email",
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/calendar.calendarlist.readonly",
}

// ErrGoogleAuthFailed covers everything that can go wrong exchanging a code
// or resolving the account's Email: Google rejecting the request, an
// unreachable endpoint, or an answer missing what was asked for.
var ErrGoogleAuthFailed = errors.New("google rejected the authorization")

// ErrGoogleCalendarListFailed covers everything that can go wrong asking
// Google which calendars a Connection's account can see (#286): an expired
// or revoked access token, an unreachable endpoint, or a malformed response.
var ErrGoogleCalendarListFailed = errors.New("could not list this connection's calendars from google")

// ErrGoogleTokenRefreshFailed covers everything that can go wrong minting a
// fresh access token from a Connection's stored refresh_token (#287): a
// revoked grant, an unreachable endpoint, or a malformed response.
var ErrGoogleTokenRefreshFailed = errors.New("could not refresh this connection's google access token")

// ErrGoogleEventsFailed covers everything that can go wrong fetching a
// Linked Calendar's events.list (#287): an expired or revoked access token,
// the calendar having vanished at Google, an unreachable endpoint, or a
// malformed response.
var ErrGoogleEventsFailed = errors.New("could not fetch this linked calendar's events from google")

// errGoogleSyncTokenExpired is Google's 410 GONE on a Delta Refresh request
// whose syncToken it has invalidated (#288, ADR-0053) — routinely, on any
// ACL change to a calendar shared into the connected account. It is not a
// failure: the caller catches it and falls back to a Full Refresh
// reconciled against the stored rows, preserving row ids. Never surfaced to
// classifyGoogleError, which would wrongly sort it as needs-attention.
var errGoogleSyncTokenExpired = errors.New("google sync token expired")

// googleHTTPError wraps one of the sentinels above with the status code
// Google's response actually carried, so a caller (classifyGoogleError,
// connection_refresh.go) can sort ADR-0053's needs-attention/retrying split
// without parsing an error string. Every other Google-calling method in
// this file predates that need and keeps reporting its status code as
// message text only; listEventChanges and refreshAccessToken are the first
// callers to need the classification, so they are the first to carry it
// structurally.
type googleHTTPError struct {
	sentinel   error
	statusCode int
}

func (e *googleHTTPError) Error() string {
	return fmt.Sprintf("%s: status %d", e.sentinel, e.statusCode)
}

func (e *googleHTTPError) Unwrap() error { return e.sentinel }

// googleClient is every HTTP call this app makes to Google, behind one
// overridable client and one overridable set of endpoint URLs.
type googleClient struct {
	clientID, clientSecret                                          string
	authorizeURL, tokenURL, userinfoURL, calendarListURL, eventsURL string
	httpClient                                                      *http.Client
}

func newGoogleClient(clientID, clientSecret string) *googleClient {
	return &googleClient{
		clientID:        clientID,
		clientSecret:    clientSecret,
		authorizeURL:    googleAuthorizeURL,
		tokenURL:        googleTokenURL,
		userinfoURL:     googleUserinfoURL,
		calendarListURL: googleCalendarListURL,
		eventsURL:       googleEventsURL,
		httpClient:      http.DefaultClient,
	}
}

// authorizeURLFor builds the URL the browser is sent to, to consent (#285).
// access_type=offline and prompt=consent together are what guarantee a
// refresh_token comes back even when the User is re-authorizing an account
// they'd already granted before — Google otherwise omits it on a repeat
// consent, and reconnecting a Connection needs one every time.
func (c *googleClient) authorizeURLFor(redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(googleScopes, " "))
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return c.authorizeURL + "?" + q.Encode()
}

// googleTokens is what a successful code exchange returns.
type googleTokens struct {
	AccessToken  string
	RefreshToken string
	// Scope is exactly what Google granted, echoed back space-separated —
	// not merely what googleScopes requested (ADR-0052's "the scopes
	// requested are ... not the broad calendar scope" is enforced by never
	// asking for more; this records what was actually received).
	Scope string
}

// exchangeCode trades an authorization code for tokens. redirectURI must be
// byte-identical to the one the authorize request used — Google rejects the
// exchange otherwise.
func (c *googleClient) exchangeCode(ctx context.Context, code, redirectURI string) (googleTokens, error) {
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokens{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return googleTokens{}, fmt.Errorf("%w: %v", ErrGoogleAuthFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
		return googleTokens{}, fmt.Errorf("%w: token endpoint responded %d", ErrGoogleAuthFailed, resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return googleTokens{}, fmt.Errorf("%w: %v", ErrGoogleAuthFailed, err)
	}

	// Google omits refresh_token on a repeat consent unless prompt=consent
	// forced the screen (which authorizeURLFor always sets) — an absent one
	// here means the exchange itself is broken, not a User who declined
	// anything, since consent was already granted by the time code exists.
	if body.RefreshToken == "" {
		return googleTokens{}, fmt.Errorf("%w: no refresh token in the response", ErrGoogleAuthFailed)
	}

	return googleTokens{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Scope: body.Scope}, nil
}

// grantsRequiredScopes reports whether Scope — what Google actually granted —
// covers every scope googleScopes requested. Google's granular-consent
// screen lets a User approve sign-in while denying Calendar access
// specifically; the token exchange still succeeds and still returns a
// refresh token in that case, so this is the only place that would ever
// notice before a later Calendar call started failing.
func (t googleTokens) grantsRequiredScopes() bool {
	granted := make(map[string]bool, len(googleScopes))
	for _, field := range strings.Fields(t.Scope) {
		granted[field] = true
	}
	for _, required := range googleScopes {
		if !granted[required] {
			return false
		}
	}
	return true
}

// fetchAccountEmail resolves the authorized account's own Email — what a
// Connection displays and what its (user, provider, account) identity is
// keyed on (#285, ADR-0052) — and whether Google itself has verified it.
func (c *googleClient) fetchAccountEmail(ctx context.Context, accessToken string) (email string, verified bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userinfoURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrGoogleAuthFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
		return "", false, fmt.Errorf("%w: userinfo endpoint responded %d", ErrGoogleAuthFailed, resp.StatusCode)
	}

	var body struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrGoogleAuthFailed, err)
	}
	if body.Email == "" {
		return "", false, fmt.Errorf("%w: userinfo endpoint returned no email", ErrGoogleAuthFailed)
	}

	return body.Email, body.VerifiedEmail, nil
}

// googleCalendarListEntry is one row of a calendarList.list response (#286):
// the account's own calendars, the ones it's subscribed to, and the ones
// other people shared to it — Google draws no distinction between the three
// in this listing, which is exactly why the Calendar picker can show them
// all from one call.
type googleCalendarListEntry struct {
	// ID is the Provider's own id for this calendar — "primary" for the
	// account's own default calendar, an opaque address for every other one.
	// Stored as ExternalCalendarID on the Source a picked row's Linked
	// Calendar gets.
	ID string
	// Summary is Google's own name for the calendar. SummaryOverride is set
	// only once the account holder has locally renamed it in Google's own
	// UI — when present, it's what Google's own sidebar shows in its place,
	// so it wins over Summary here for the same reason.
	Summary, SummaryOverride string
	// BackgroundColor is the calendar's own displayed colour, already a hex
	// value — Google's per-calendar colorId maps to one of these, so this
	// app never needs the id-to-hex table itself.
	BackgroundColor string
	// AccessRole is what Google's ACL grants this account on the calendar:
	// "owner"/"writer" can write to it there, "reader"/"freeBusyReader"
	// cannot. Surfaced to the picker as a read-only badge (#286) — it is
	// deliberately not what decides this app's own Source Mode, since
	// write-back doesn't exist yet (ADR-0052, ADR-0075) and every Source
	// this ticket creates is read-only regardless.
	AccessRole string
	// Selected mirrors Google's own sidebar checkbox for this calendar —
	// the picker's default checked state (#286).
	Selected bool
}

// writable reports whether Google's own ACL lets this account write to the
// calendar — the picker's read-only badge is the inverse of this, never a
// claim about what this app itself will let a User do (see AccessRole).
func (e googleCalendarListEntry) writable() bool {
	return e.AccessRole == "owner" || e.AccessRole == "writer"
}

// displayName is SummaryOverride when the account holder has set one at
// Google, Summary otherwise — mirroring which one Google's own UI shows.
func (e googleCalendarListEntry) displayName() string {
	if e.SummaryOverride != "" {
		return e.SummaryOverride
	}
	return e.Summary
}

// listCalendarList returns every calendar accessToken's account can see —
// its own, the ones it's subscribed to, and the ones shared to it (#286) —
// paginating through Google's own page size so an account with many
// calendars still gets a complete listing rather than a truncated first
// page.
func (c *googleClient) listCalendarList(ctx context.Context, accessToken string) ([]googleCalendarListEntry, error) {
	var entries []googleCalendarListEntry
	pageToken := ""

	for {
		q := url.Values{}
		q.Set("maxResults", strconv.Itoa(googleCalendarListPageSize))
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.calendarListURL+"?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("build calendar list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrGoogleCalendarListFailed, err)
		}

		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
			resp.Body.Close()
			return nil, fmt.Errorf("%w: calendar list endpoint responded %d", ErrGoogleCalendarListFailed, resp.StatusCode)
		}

		var body struct {
			Items []struct {
				ID              string `json:"id"`
				Summary         string `json:"summary"`
				SummaryOverride string `json:"summaryOverride"`
				BackgroundColor string `json:"backgroundColor"`
				AccessRole      string `json:"accessRole"`
				Selected        bool   `json:"selected"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrGoogleCalendarListFailed, decodeErr)
		}

		for _, item := range body.Items {
			entries = append(entries, googleCalendarListEntry{
				ID:              item.ID,
				Summary:         item.Summary,
				SummaryOverride: item.SummaryOverride,
				BackgroundColor: item.BackgroundColor,
				AccessRole:      item.AccessRole,
				Selected:        item.Selected,
			})
		}

		if body.NextPageToken == "" {
			break
		}
		pageToken = body.NextPageToken
	}

	return entries, nil
}

// getCalendarListEntry fetches one calendar's own calendarList.get entry
// (#290, ADR-0075) — cheaper than paginating listCalendarList's whole
// listing when all a caller needs is one calendar's current AccessRole,
// which is what a Linked Calendar's Refresh re-reads on every cycle to
// derive its Source's Mode: writability is Google's ACL to revoke at any
// time, not a fact fixed at import.
func (c *googleClient) getCalendarListEntry(ctx context.Context, accessToken, calendarID string) (googleCalendarListEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.calendarListURL+"/"+url.PathEscape(calendarID), nil)
	if err != nil {
		return googleCalendarListEntry{}, fmt.Errorf("build calendar list entry request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return googleCalendarListEntry{}, fmt.Errorf("%w: %v", ErrGoogleCalendarListFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
		return googleCalendarListEntry{}, &googleHTTPError{sentinel: ErrGoogleCalendarListFailed, statusCode: resp.StatusCode}
	}

	var item struct {
		ID              string `json:"id"`
		Summary         string `json:"summary"`
		SummaryOverride string `json:"summaryOverride"`
		BackgroundColor string `json:"backgroundColor"`
		AccessRole      string `json:"accessRole"`
		Selected        bool   `json:"selected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return googleCalendarListEntry{}, fmt.Errorf("%w: %v", ErrGoogleCalendarListFailed, err)
	}

	return googleCalendarListEntry{
		ID:              item.ID,
		Summary:         item.Summary,
		SummaryOverride: item.SummaryOverride,
		BackgroundColor: item.BackgroundColor,
		AccessRole:      item.AccessRole,
		Selected:        item.Selected,
	}, nil
}

// googleRefreshedTokens is what refreshAccessToken returns — just the new
// access token, since Google only mints a new refresh_token on this grant
// type when the old one was revoked for security reasons (rare enough that
// this app treats the stored refresh_token as durable until a Connection is
// reconnected, a later ticket's concern).
type googleRefreshedTokens struct {
	AccessToken string
}

// refreshAccessToken mints a fresh access token from refreshToken (#287) —
// never relied on across requests without this, since an access token
// lasts about an hour and a Full Refresh may run long after the Connection
// was made. A revoked or expired refreshToken fails here with a 400/401,
// which classifyGoogleError sorts as needs-attention (connection_refresh.go).
func (c *googleClient) refreshAccessToken(ctx context.Context, refreshToken string) (googleRefreshedTokens, error) {
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.clientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleRefreshedTokens{}, fmt.Errorf("build token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return googleRefreshedTokens{}, fmt.Errorf("%w: %v", ErrGoogleTokenRefreshFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
		return googleRefreshedTokens{}, &googleHTTPError{sentinel: ErrGoogleTokenRefreshFailed, statusCode: resp.StatusCode}
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return googleRefreshedTokens{}, fmt.Errorf("%w: %v", ErrGoogleTokenRefreshFailed, err)
	}
	if body.AccessToken == "" {
		return googleRefreshedTokens{}, fmt.Errorf("%w: no access token in the response", ErrGoogleTokenRefreshFailed)
	}

	return googleRefreshedTokens{AccessToken: body.AccessToken}, nil
}

// googleEventsPageSize is the page size requested of events.list (#287) —
// Google's own documented maximum, so a Linked Calendar with many Events
// still costs a small, bounded number of requests.
const googleEventsPageSize = 2500

// eventListParams builds events.list's own query parameters, shared by the
// Full and Delta Refresh call paths so ADR-0053's "the query parameters on
// every incremental request must be identical to those on the initial full
// sync" holds by construction rather than by convention (#288). No time
// window is requested (ADR-0053: any window would be locked in for the life
// of the cursor, and a calendar app that cannot show last year's meeting is
// broken in a way a slow first sync is not); singleEvents=false preserves a
// recurring Event as one Master plus its Overrides/Exceptions, which this
// app's model needs. showDeleted's default of false is left alone — an
// incremental sync always returns a deleted item as status=cancelled
// regardless, and in a full listing a wholly deleted Event is meant to be
// absent, which Full mode's own absence-means-deletion rule handles.
//
// syncToken is empty on a Full Refresh and the stored cursor on a Delta
// Refresh — the only difference between the two requests.
func eventListParams(pageToken, syncToken string) url.Values {
	q := url.Values{}
	q.Set("singleEvents", "false")
	q.Set("maxResults", strconv.Itoa(googleEventsPageSize))
	if syncToken != "" {
		q.Set("syncToken", syncToken)
	}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	return q
}

// eventsURLFor builds events.list's URL for one calendar (#287) — Google
// requires the calendar's own id in the path, unescaped except for the
// standard percent-encoding a "primary" or an opaque group-calendar address
// needs.
func (c *googleClient) eventsURLFor(calendarID string) string {
	return c.eventsURL + "/" + url.PathEscape(calendarID) + "/events"
}

// googleEventJSON is one Events.list item's wire shape, decoded directly
// off Google's response before toGoogleEvent narrows it to what
// google_mapper.go actually consumes.
type googleEventJSON struct {
	ID                string                    `json:"id"`
	ETag              string                    `json:"etag"`
	Status            string                    `json:"status"`
	Summary           string                    `json:"summary"`
	Description       string                    `json:"description"`
	Location          string                    `json:"location"`
	ColorID           string                    `json:"colorId"`
	Start             googleEventDateTimeJSON   `json:"start"`
	End               googleEventDateTimeJSON   `json:"end"`
	RecurringEventID  string                    `json:"recurringEventId"`
	OriginalStartTime *googleEventDateTimeJSON  `json:"originalStartTime"`
	Recurrence        []string                  `json:"recurrence"`
	Attendees         []googleAttendeeJSON      `json:"attendees"`
	ConferenceData    *googleConferenceDataJSON `json:"conferenceData"`
}

type googleEventDateTimeJSON struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type googleAttendeeJSON struct {
	Self           bool   `json:"self"`
	Resource       bool   `json:"resource"`
	ResponseStatus string `json:"responseStatus"`
}

type googleConferenceDataJSON struct {
	EntryPoints []googleEntryPointJSON `json:"entryPoints"`
}

type googleEntryPointJSON struct {
	EntryPointType string `json:"entryPointType"`
	URI            string `json:"uri"`
}

// toGoogleEvent narrows googleEventJSON to the googleEvent shape
// google_mapper.go's pure functions consume — the decode boundary and the
// mapper stay separate so the mapper itself takes no encoding/json
// dependency and table-tests against plain struct literals.
func toGoogleEvent(j googleEventJSON) googleEvent {
	e := googleEvent{
		ID:               j.ID,
		ETag:             j.ETag,
		Status:           j.Status,
		Summary:          j.Summary,
		Description:      j.Description,
		Location:         j.Location,
		ColorID:          j.ColorID,
		Start:            toGoogleEventDateTime(j.Start),
		End:              toGoogleEventDateTime(j.End),
		RecurringEventID: j.RecurringEventID,
		Recurrence:       j.Recurrence,
	}
	if j.OriginalStartTime != nil {
		dt := toGoogleEventDateTime(*j.OriginalStartTime)
		e.OriginalStartTime = &dt
	}
	for _, a := range j.Attendees {
		e.Attendees = append(e.Attendees, googleAttendee{Self: a.Self, Resource: a.Resource, ResponseStatus: a.ResponseStatus})
	}
	if j.ConferenceData != nil {
		cd := googleConferenceData{}
		for _, ep := range j.ConferenceData.EntryPoints {
			cd.EntryPoints = append(cd.EntryPoints, googleEntryPoint{EntryPointType: ep.EntryPointType, URI: ep.URI})
		}
		e.ConferenceData = &cd
	}
	return e
}

func toGoogleEventDateTime(j googleEventDateTimeJSON) googleEventDateTime {
	return googleEventDateTime{Date: j.Date, DateTime: j.DateTime, TimeZone: j.TimeZone}
}

// googleEventChanges is one listEventChanges call's whole result (#288):
// every event page the request produced, flattened, plus the fresh
// nextSyncToken Google hands back only on the final page — the cursor the
// next Delta Refresh presents. Events carries the complete listing on a
// Full Refresh (syncToken empty) and only what changed since the cursor on
// a Delta Refresh, with a deleted item appearing as status=cancelled.
type googleEventChanges struct {
	Events        []googleEvent
	NextSyncToken string
	// Summary is the Provider's own current name for the calendar, echoed at
	// the top level of every events.list response — Full and incremental
	// alike. A Linked Calendar's name follows it until someone renames the
	// Calendar here (#289, ADR-0052's "presentation is local"). Empty when a
	// response omitted it, in which case Refresh leaves the stored shadow
	// alone.
	Summary string
}

// listEventChanges fetches calendarID's events (#287, #288, ADR-0053):
// with syncToken empty it is a Full Refresh — the complete listing, no time
// window, paginated through Google's own page size; with syncToken set it is
// a Delta Refresh — only what changed since that cursor. Either way it
// paginates to the end and returns the nextSyncToken from the final page.
//
// A 410 GONE on a Delta Refresh (Google having invalidated the cursor,
// routinely on an ACL change) is returned as errGoogleSyncTokenExpired for
// the caller to recover from with a Full Refresh — never a *googleHTTPError,
// which classifyGoogleError would wrongly sort as needs-attention. Grouping
// instances under their Master and mapping to a domain SeriesWrite is
// google_mapper.go's job, kept free of any encoding/json or net/http
// dependency so it table-tests directly.
func (c *googleClient) listEventChanges(ctx context.Context, accessToken, calendarID, syncToken string) (googleEventChanges, error) {
	var result googleEventChanges
	pageToken := ""

	for {
		q := eventListParams(pageToken, syncToken)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.eventsURLFor(calendarID)+"?"+q.Encode(), nil)
		if err != nil {
			return googleEventChanges{}, fmt.Errorf("build events list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return googleEventChanges{}, fmt.Errorf("%w: %v", ErrGoogleEventsFailed, err)
		}

		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
			resp.Body.Close()
			if resp.StatusCode == http.StatusGone && syncToken != "" {
				return googleEventChanges{}, errGoogleSyncTokenExpired
			}
			return googleEventChanges{}, &googleHTTPError{sentinel: ErrGoogleEventsFailed, statusCode: resp.StatusCode}
		}

		var body struct {
			Items         []googleEventJSON `json:"items"`
			Summary       string            `json:"summary"`
			NextPageToken string            `json:"nextPageToken"`
			NextSyncToken string            `json:"nextSyncToken"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return googleEventChanges{}, fmt.Errorf("%w: %v", ErrGoogleEventsFailed, decodeErr)
		}

		for _, item := range body.Items {
			result.Events = append(result.Events, toGoogleEvent(item))
		}
		if body.NextSyncToken != "" {
			result.NextSyncToken = body.NextSyncToken
		}
		if body.Summary != "" {
			result.Summary = body.Summary
		}

		if body.NextPageToken == "" {
			break
		}
		pageToken = body.NextPageToken
	}

	return result, nil
}

// ErrGoogleWriteBackFailed covers everything that can go wrong pushing a
// Write-back PATCH (#290, ADR-0075): an expired or revoked access token, the
// calendar or event having vanished at Google, a stale etag (412), or an
// unreachable endpoint.
var ErrGoogleWriteBackFailed = errors.New("could not push this event's changes to google")

// googleEventPatchSourceJSON carries Event URL (ADR-0063) onto Google's own
// `source` field — the closest fit Google's Event resource has to "a link to
// more information about this event": a title plus a URL, rendered in
// Google's own UI as "via <title>". Set only when the URL is present and
// parses as http/https, mirroring CONTEXT.md's "offered as a click-through
// only when it is http or https" rule for the same field — a non-web scheme
// (message://, a bare feed vendor scheme) is never sent, since Google's own
// field has no use for a link nothing there can open either.
type googleEventPatchSourceJSON struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// googleEventPatchBody is the wire shape of a Write-back PATCH request body
// (#290, ADR-0075) — and the load-bearing type this ticket exists to build:
// it has a field for exactly title, description, location, start, end,
// recurrence and Event URL, and no field for attendees, conferenceData,
// visibility, or anything else this app doesn't model. There is no
// constructor that takes a whole repository.Event and no method that lets a
// caller set a field this struct doesn't declare — buildGooglePatch is the
// only function that produces one, and it reads a fixed, named set of
// arguments, never an Event value it could forward wholesale. That is what
// makes "a whole-Event replace is impossible to express at the Provider
// seam" true by construction rather than by convention: there is no
// events.update call in this file, and this type could not serialize one if
// there were.
type googleEventPatchBody struct {
	Summary     string                  `json:"summary"`
	Description string                  `json:"description"`
	Location    string                  `json:"location"`
	Start       googleEventDateTimeJSON `json:"start"`
	End         googleEventDateTimeJSON `json:"end"`
	Recurrence  []string                `json:"recurrence,omitempty"`
	// Source is deliberately not ",omitempty": a nil pointer must marshal to
	// an explicit "source":null, the PATCH request that clears Event URL at
	// Google, rather than an absent key, which Google reads as "leave
	// whatever's there alone" — the same reason Summary/Description/Location
	// above carry no omitempty of their own.
	Source *googleEventPatchSourceJSON `json:"source"`
}

// encodeGoogleEventDateTime is decodeGoogleTime's inverse (#290, ADR-0075):
// an all-day boundary becomes a bare Date, a timed one becomes DateTime plus
// TimeZone. tzid nil (a Floating Event) has no Google equivalent to round-trip
// through — encoded as an absolute UTC instant (a "Z"-suffixed DateTime, no
// TimeZone), the closest Google concept there is, rather than refusing the
// push outright; this app's own Floating Event predates the timezone model
// (ADR-0019) and a Linked Calendar's own Events always carry a concrete Anchor
// zone from the Provider in practice.
func encodeGoogleEventDateTime(t time.Time, allDay bool, tzid *string) googleEventDateTimeJSON {
	if allDay {
		return googleEventDateTimeJSON{Date: t.UTC().Format("2006-01-02")}
	}
	if tzid == nil {
		return googleEventDateTimeJSON{DateTime: t.UTC().Format("2006-01-02T15:04:05Z")}
	}
	loc, err := time.LoadLocation(*tzid)
	if err != nil {
		// An unrecognized zone name can't be round-tripped by name; falling
		// back to the UTC instant is lossy on the wall-clock but never wrong
		// about the moment in time, and TimeZone is deliberately left empty
		// rather than sent as a string Google itself cannot resolve either.
		return googleEventDateTimeJSON{DateTime: t.UTC().Format("2006-01-02T15:04:05Z")}
	}
	return googleEventDateTimeJSON{DateTime: t.In(loc).Format("2006-01-02T15:04:05-07:00"), TimeZone: *tzid}
}

// encodeGoogleRecurrence is parseGoogleRecurrence's (plus
// parseGoogleExdateValue's) inverse for exactly the two lines this app ever
// writes back: one RRULE line carrying rrule verbatim (ADR-0016 stores it as
// opaque text already in Google's own RFC 5545 shape, so it never needs
// translating), and one EXDATE line per stored Exception, so a Master edit's
// recurrence array — which Google replaces wholesale, never merges — doesn't
// silently drop cancellations this app already reconciled in from a previous
// Refresh. tzid anchors the EXDATE values exactly as the Master's own
// boundaries are anchored (encodeGoogleEventDateTime's own rule); an all-day
// series' exdates are encoded as bare VALUE=DATE tokens instead. Returns nil
// for a non-recurring write (rrule empty and no exdates), which
// buildGooglePatch reads as "omit recurrence from the patch entirely" — never
// as "clear whatever Google has": the empty-non-recurring case only reaches
// here for an Event this app never modeled as recurring to begin with, and
// this ticket doesn't turn a recurring Master back into a plain Event over
// Write-back.
func encodeGoogleRecurrence(rrule string, exdates []time.Time, allDay bool, tzid *string) []string {
	if rrule == "" && len(exdates) == 0 {
		return nil
	}

	lines := make([]string, 0, 1+len(exdates))
	if rrule != "" {
		lines = append(lines, "RRULE:"+rrule)
	}
	for _, exdate := range exdates {
		lines = append(lines, encodeGoogleExdateLine(exdate, allDay, tzid))
	}
	return lines
}

// encodeGoogleExdateLine renders one EXDATE content line in the same shape
// parseGoogleExdateValue accepts back: VALUE=DATE for an all-day series, a
// bare "Z"-suffixed instant for an absolute one (tzid nil or "Etc/UTC"), and
// a TZID-parameterized local wall-clock value otherwise.
func encodeGoogleExdateLine(t time.Time, allDay bool, tzid *string) string {
	if allDay {
		return "EXDATE;VALUE=DATE:" + t.UTC().Format("20060102")
	}
	if tzid == nil || *tzid == "Etc/UTC" {
		return "EXDATE:" + t.UTC().Format("20060102T150405Z")
	}
	loc, err := time.LoadLocation(*tzid)
	if err != nil {
		return "EXDATE:" + t.UTC().Format("20060102T150405Z")
	}
	return "EXDATE;TZID=" + *tzid + ":" + t.In(loc).Format("20060102T150405")
}

// googlePatchSource builds Event URL's Source mapping (#290, ADR-0075),
// nil when eventURL is empty or isn't http/https — see
// googleEventPatchSourceJSON's own doc comment for why.
func googlePatchSource(eventTitle, eventURL string) *googleEventPatchSourceJSON {
	if eventURL == "" {
		return nil
	}
	if !strings.HasPrefix(eventURL, "http://") && !strings.HasPrefix(eventURL, "https://") {
		return nil
	}
	title := eventTitle
	if title == "" {
		title = eventURL
	}
	return &googleEventPatchSourceJSON{Title: title, URL: eventURL}
}

// buildGooglePatch is the one function that produces a googleEventPatchBody
// (#290, ADR-0075) — the field-scoped patch compiler ADR-0075 requires be
// enforced at the Provider seam, not by convention at each call site. Its
// argument list is exactly ADR-0075's allow-list (title, start, end,
// all-day, Anchor zone, recurrence rule plus its Exdates, description,
// location, Event URL) and nothing a caller could use to smuggle an
// Attendee, conferenceData, or a visibility change through: those fields
// don't exist on this function's signature, so there is nothing to pass even
// by mistake.
func buildGooglePatch(title string, start, end time.Time, allDay bool, tzid *string, rrule string, exdates []time.Time, description, location, eventURL string) googleEventPatchBody {
	return googleEventPatchBody{
		Summary:     title,
		Description: description,
		Location:    location,
		Start:       encodeGoogleEventDateTime(start, allDay, tzid),
		End:         encodeGoogleEventDateTime(end, allDay, tzid),
		Recurrence:  encodeGoogleRecurrence(rrule, exdates, allDay, tzid),
		Source:      googlePatchSource(title, eventURL),
	}
}

// patchEvent pushes patch to eventID on calendarID via events.patch (#290,
// ADR-0075) — never events.update, which this file has no method for at
// all. Always sends sendUpdates=none (ADR-0075's "guests this app cannot
// see": this app doesn't model Attendees on a Linked Calendar's Event, so it
// must never trigger Google's own guest-notification email on their behalf).
// ifMatchEtag, when non-nil, is sent as the If-Match precondition header
// against the Provider's own per-instance validator; a stale one answers
// 412, returned as a *googleHTTPError for the caller to classify like any
// other failure (bounded conflict retry is #291's own ticket, not this
// one's). Returns the updated event Google's own response describes, for the
// caller to apply as though it were a Refresh result (ADR-0075, ADR-0076) —
// principally its fresh etag, so this app's own push doesn't come back as a
// change on the next Delta Refresh.
func (c *googleClient) patchEvent(ctx context.Context, accessToken, calendarID, eventID string, ifMatchEtag *string, patch googleEventPatchBody) (googleEventJSON, error) {
	body, err := json.Marshal(patch)
	if err != nil {
		return googleEventJSON{}, fmt.Errorf("marshal event patch: %w", err)
	}

	q := url.Values{}
	q.Set("sendUpdates", "none")
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.eventsURLFor(calendarID)+"/"+url.PathEscape(eventID)+"?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return googleEventJSON{}, fmt.Errorf("build event patch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if ifMatchEtag != nil {
		req.Header.Set("If-Match", `"`+*ifMatchEtag+`"`)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return googleEventJSON{}, fmt.Errorf("%w: %v", ErrGoogleWriteBackFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // draining for keep-alive reuse; the response carries nothing we need on failure
		return googleEventJSON{}, &googleHTTPError{sentinel: ErrGoogleWriteBackFailed, statusCode: resp.StatusCode}
	}

	var updated googleEventJSON
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return googleEventJSON{}, fmt.Errorf("%w: %v", ErrGoogleWriteBackFailed, err)
	}
	return updated, nil
}
