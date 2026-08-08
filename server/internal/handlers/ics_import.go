// ics_import.go implements ICS import (#77): POST /api/calendars/import
// accepts a multipart/form-data upload (a "file" part plus a "targets" JSON
// part) and returns the Import summary — the same payload whether dryRun=1
// only previews it or dryRun=0 (the default) actually writes (ADR-0030).
package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/service"
)

// maxImportUploadBytes caps an import upload at 10MB (#77), enforced via
// http.MaxBytesReader so an oversized body is rejected while still
// streaming, not after being buffered whole.
const maxImportUploadBytes = 10 << 20

var importErrors = []errorCase{
	{service.ErrImportUnsupportedFileType, badRequest("uploaded file must be a .ics or .zip file")},
	{service.ErrImportTargetMissing, badRequest("no import target for one of the uploaded files")},
	{service.ErrImportInvalidAction, badRequest(`import target action must be "new", "existing", or "skip"`)},
	{service.ErrImportExistingNotAllowedForZip, badRequest("a zip import may not target an existing calendar")},
	{service.ErrImportMissingCalendarID, badRequest(`import target action "existing" requires a calendarId`)},
	{service.ErrImportTargetSubscribed, forbidden("import target is a subscribed calendar")},
	{service.ErrCalendarNotFound, notFound("calendar not found")},
	{service.ErrInvalidTitle, badRequest("event title must not be empty")},
	{service.ErrInvalidTimeRange, badRequest("event end must be after start")},
	{service.ErrInvalidRecurrenceRule, badRequest("recurrence rule is invalid")},
	{service.ErrInvalidReminderChannel, badRequest(`reminder channel must be "notification" or "email"`)},
}

type importTargetRequest struct {
	Filename   string `json:"filename"`
	Action     string `json:"action"`
	Name       string `json:"name"`
	CalendarID string `json:"calendarId"`
}

// importTargetsRequest is the "targets" multipart part's JSON shape:
// {"entries": [...]} (#77).
type importTargetsRequest struct {
	Entries []importTargetRequest `json:"entries"`
}

// Import serves POST /api/calendars/import?dryRun=1|0: a multipart upload
// (a "file" part carrying a .ics or .zip, and a "targets" part carrying the
// per-file JSON instructions) is parsed and, unless dryRun=1, written
// synchronously in one call (#77).
func (h *CalendarHandler) Import(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	dryRun, ok := parseDryRun(r.URL.Query().Get("dryRun"))
	if !ok {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `dryRun must be "0" or "1"`)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadBytes)
	if err := r.ParseMultipartForm(maxImportUploadBytes); err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "request body must be a multipart/form-data upload no larger than 10MB")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", `a "file" part is required`)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "failed to read uploaded file")
		return
	}

	var targetsReq importTargetsRequest
	if raw := r.FormValue("targets"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &targetsReq); err != nil {
			httpresponse.Error(w, http.StatusBadRequest, "invalid_request", "targets must be valid JSON")
			return
		}
	}
	targets := make([]service.ImportTarget, len(targetsReq.Entries))
	for i, t := range targetsReq.Entries {
		targets[i] = service.ImportTarget{
			Filename:   t.Filename,
			Action:     service.ImportTargetAction(t.Action),
			Name:       t.Name,
			CalendarID: t.CalendarID,
		}
	}

	summary, err := h.imports.Import(r.Context(), userID, header.Filename, data, targets, dryRun)
	if respondError(w, err, importErrors, "failed to import calendar file") {
		return
	}

	httpresponse.JSON(w, http.StatusOK, toImportSummaryResponse(summary))
}

// parseDryRun reads the dryRun query param: "1" is true, "" or "0" is
// false, anything else is invalid.
func parseDryRun(raw string) (dryRun, ok bool) {
	switch raw {
	case "", "0":
		return false, true
	case "1":
		return true, true
	default:
		return false, false
	}
}

type importSummaryResponse struct {
	Files []importFileSummaryResponse `json:"files"`
}

type importFileSummaryResponse struct {
	Filename     string                   `json:"filename"`
	CalendarName string                   `json:"calendarName"`
	CalendarID   string                   `json:"calendarId,omitempty"`
	EventCount   int                      `json:"eventCount"`
	RangeStart   *time.Time               `json:"rangeStart,omitempty"`
	RangeEnd     *time.Time               `json:"rangeEnd,omitempty"`
	Skipped      []skippedGroupResponse   `json:"skipped,omitempty"`
	Adjusted     []adjustedGroupResponse  `json:"adjusted,omitempty"`
	Ignored      ignoredCountsResponse    `json:"ignored"`
	Reminders    reminderCountsResponse   `json:"reminders"`
	Attachments  attachmentCountsResponse `json:"attachments"`
}

type skippedGroupResponse struct {
	Reason  string   `json:"reason"`
	Count   int      `json:"count"`
	Samples []string `json:"samples,omitempty"`
}

type adjustedGroupResponse struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type ignoredCountsResponse struct {
	VTodo     int `json:"vtodo"`
	VJournal  int `json:"vjournal"`
	VFreeBusy int `json:"vfreebusy"`
}

type reminderCountsResponse struct {
	Notification int `json:"notification"`
	Email        int `json:"email"`
}

// attachmentCountsResponse is the four outcomes an ATTACH property can have
// on import (#135, ADR-0040): imported, too large for MAX_ATTACHMENT_SIZE,
// past MAX_ATTACHMENTS_PER_EVENT, or a URI ATTACH ignored outright.
type attachmentCountsResponse struct {
	Imported   int `json:"imported"`
	TooLarge   int `json:"tooLarge"`
	TooMany    int `json:"tooMany"`
	IgnoredURI int `json:"ignoredUri"`
}

func toImportSummaryResponse(summary service.ImportSummary) importSummaryResponse {
	files := make([]importFileSummaryResponse, len(summary.Files))
	for i, f := range summary.Files {
		files[i] = importFileSummaryResponse{
			Filename:     f.Filename,
			CalendarName: f.CalendarName,
			CalendarID:   f.CalendarID,
			EventCount:   f.EventCount,
			RangeStart:   f.RangeStart,
			RangeEnd:     f.RangeEnd,
			Skipped:      toSkippedGroupResponses(f.Skipped),
			Adjusted:     toAdjustedGroupResponses(f.Adjusted),
			Ignored: ignoredCountsResponse{
				VTodo:     f.Ignored.VTodo,
				VJournal:  f.Ignored.VJournal,
				VFreeBusy: f.Ignored.VFreeBusy,
			},
			Reminders: reminderCountsResponse{
				Notification: f.Reminders.Notification,
				Email:        f.Reminders.Email,
			},
			Attachments: attachmentCountsResponse{
				Imported:   f.Attachments.Imported,
				TooLarge:   f.Attachments.TooLarge,
				TooMany:    f.Attachments.TooMany,
				IgnoredURI: f.Attachments.IgnoredURI,
			},
		}
	}
	return importSummaryResponse{Files: files}
}

func toSkippedGroupResponses(groups []service.SkippedGroup) []skippedGroupResponse {
	if len(groups) == 0 {
		return nil
	}
	out := make([]skippedGroupResponse, len(groups))
	for i, g := range groups {
		out[i] = skippedGroupResponse{Reason: g.Reason, Count: g.Count, Samples: g.Samples}
	}
	return out
}

func toAdjustedGroupResponses(groups []service.AdjustedGroup) []adjustedGroupResponse {
	if len(groups) == 0 {
		return nil
	}
	out := make([]adjustedGroupResponse, len(groups))
	for i, g := range groups {
		out[i] = adjustedGroupResponse{Reason: g.Reason, Count: g.Count}
	}
	return out
}
