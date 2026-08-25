package caldavserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/XiovV/calich/server/internal/httpauth"
)

const pathPrefix = "/dav"

// attachmentsBasePath is the RFC 8607 managed-attachments server URL,
// advertised path-only on the calendar home collection (managed-attachments-server-URL, propfind.go) and
// the base every managed-attachment URI ATTACH emits is built from
// (icalendar.CalDAVTarget's uriPrefix) — also this app's own
// download route (attachment_actions.go's GET handler), so the two always
// agree by construction (#133, ADR-0040).
const attachmentsBasePath = pathPrefix + "/attachments/"

// attachmentDownloadPath is one Attachment's full managed-attachment path —
// the value the CalDAV POST actions' Location header returns.
func attachmentDownloadPath(id string) string {
	return attachmentsBasePath + id
}

// attachmentsURIPrefix returns attachmentsBasePath made absolute with the
// scheme+host string dispatchHandler derived from the request in ctx, so ATTACH's
// URI (plain iCalendar text, unlike a WebDAV href, with no request to
// resolve a bare path against once it's sitting in a client's calendar
// store — #142) is a fully-qualified URL a native CalDAV client can
// actually dereference. Falls back to the path-only form if ctx carries no
// request (e.g. a test calling the Backend directly).
func attachmentsURIPrefix(ctx context.Context) string {
	baseURL, ok := baseURLFromContext(ctx)
	if !ok {
		return attachmentsBasePath
	}
	return baseURL + attachmentsBasePath
}

// Path depths below are load-bearing: go-webdav's PROPFIND dispatch
// classifies a request purely by how many path segments follow Handler.Prefix
// (1 = principal, 2 = calendar-home-set, 3 = calendar, 4 = calendar object —
// see resourceTypeAtPath in its caldav/server.go). It does not parse path
// semantics, so principal and home-set must land at depths 1 and 2 exactly
// (ADR-0023).

func principalPath(userID int64) string {
	return fmt.Sprintf("%s/%d/", pathPrefix, userID)
}

func homeSetPath(userID int64) string {
	return fmt.Sprintf("%s/%d/calendars/", pathPrefix, userID)
}

func calendarPath(userID int64, calendarID string) string {
	return fmt.Sprintf("%s/%d/calendars/%s/", pathPrefix, userID, calendarID)
}

// attendeeCollectionID is the reserved, non-UUID collection id backing the
// synthetic "Invitations" collection a principal's calendar home-set
// carries whenever they're an Attendee (ADR-0046) of at least one Event
// whose Calendar they have no Access to. Unlike every other entry
// ListCalendars returns, no real repository.Calendar row backs this
// collection for this principal — it exists purely to address those
// Attendee-only Events over CalDAV (#163). Real Calendar ids are
// uuid.New().String() values, which never collide with this fixed word.
const attendeeCollectionID = "attendee"

// attendeeCollectionName is the Invitations collection's displayname.
const attendeeCollectionName = "Invitations"

func attendeeCollectionPath(userID int64) string {
	return calendarPath(userID, attendeeCollectionID)
}

// calendarIDFromPath extracts {calendarId} from a collection path under
// userID's calendar-home-set, e.g. "/dav/1/calendars/abc-uuid/" -> "abc-uuid".
func calendarIDFromPath(userID int64, path string) (string, error) {
	prefix := homeSetPath(userID)
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("path %q is not under calendar home %q", path, prefix)
	}

	id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if id == "" {
		return "", fmt.Errorf("no calendar id in path %q", path)
	}
	return id, nil
}

// calendarObjectPath returns a series' resource path: {masterId}.ics under
// its calendar's collection (ADR-0025).
func calendarObjectPath(userID int64, calendarID, masterID string) string {
	return fmt.Sprintf("%s%s.ics", calendarPath(userID, calendarID), masterID)
}

// calendarObjectIDFromPath extracts {calendarId} and {masterId} from an
// object path under userID's calendar home, e.g.
// "/dav/1/calendars/abc/def.ics" -> ("abc", "def").
func calendarObjectIDFromPath(userID int64, path string) (calendarID, masterID string, err error) {
	prefix := homeSetPath(userID)
	if !strings.HasPrefix(path, prefix) {
		return "", "", fmt.Errorf("path %q is not under calendar home %q", path, prefix)
	}

	rest := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("no calendar object in path %q", path)
	}

	const suffix = ".ics"
	if !strings.HasSuffix(parts[1], suffix) {
		return "", "", fmt.Errorf("calendar object path %q does not end in %q", path, suffix)
	}

	return parts[0], strings.TrimSuffix(parts[1], suffix), nil
}

func userIDFromContext(ctx context.Context) (int64, error) {
	userID, ok := httpauth.UserIDFromContext(ctx)
	if !ok {
		return 0, fmt.Errorf("no authenticated user in context")
	}
	return userID, nil
}
