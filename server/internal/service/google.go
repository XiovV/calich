// google.go is the Google half of the Provider seam (#285, ADR-0050, #286):
// OAuth token exchange, the connected account's own Email, and the
// calendarList the Calendar picker offers. Event list, patch, insert and
// instances are later tickets', added to googleClient rather than beside it,
// so every Google call keeps going through the one overridable httpClient
// (#285's testing decisions) — a test points this at an httptest.Server
// serving canned JSON in place of Google, never a mocked fetcher.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	googleAuthorizeURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL        = "https://oauth2.googleapis.com/token"
	googleUserinfoURL     = "https://www.googleapis.com/oauth2/v2/userinfo"
	googleCalendarListURL = "https://www.googleapis.com/calendar/v3/users/me/calendarList"
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

// googleClient is every HTTP call this app makes to Google, behind one
// overridable client and one overridable set of endpoint URLs.
type googleClient struct {
	clientID, clientSecret                               string
	authorizeURL, tokenURL, userinfoURL, calendarListURL string
	httpClient                                           *http.Client
}

func newGoogleClient(clientID, clientSecret string) *googleClient {
	return &googleClient{
		clientID:        clientID,
		clientSecret:    clientSecret,
		authorizeURL:    googleAuthorizeURL,
		tokenURL:        googleTokenURL,
		userinfoURL:     googleUserinfoURL,
		calendarListURL: googleCalendarListURL,
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
