// import.go orchestrates ICS import (#77, ADR-0030): detecting a .zip vs a
// single .ics upload, resolving each file's target Calendar, converting
// icalendar.ParseImportFile's output into ImportSeries writes, and building
// the Import summary — the same payload for a dry run and a real run, since
// it is the user's only view of a deliberately lossy translation.
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/attachmentstore"
	"github.com/XiovV/calich/server/internal/icalendar"
	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrImportUnsupportedFileType is returned when the uploaded file's name
	// doesn't end ".ics" or ".zip".
	ErrImportUnsupportedFileType = errors.New("uploaded file must be a .ics or .zip file")
	// ErrImportTargetMissing is returned when a file inside the upload has no
	// matching entry in targets.
	ErrImportTargetMissing = errors.New("no import target for file")
	// ErrImportInvalidAction is returned when a target names an action other
	// than "new", "existing", or "skip".
	ErrImportInvalidAction = errors.New(`import target action must be "new", "existing", or "skip"`)
	// ErrImportExistingNotAllowedForZip is returned when a .zip's target
	// names action "existing" — a .zip always creates one new Calendar per
	// entry (ADR-0030).
	ErrImportExistingNotAllowedForZip = errors.New("a zip import may not target an existing calendar")
	// ErrImportMissingCalendarID is returned when action "existing" names no
	// CalendarID to write into.
	ErrImportMissingCalendarID = errors.New(`import target action "existing" requires a calendarId`)
	// ErrImportTargetSubscribed is returned when action "existing" names a
	// Subscribed Calendar — its Events are written only by Refresh's bypass
	// (ADR-0032), so import cannot target it even though EventService's own
	// guard would already refuse the later write.
	ErrImportTargetSubscribed = errors.New("import target is a subscribed calendar")
)

// ImportTargetAction is how one Calendar file's contents map onto a
// destination Calendar (ADR-0030).
type ImportTargetAction string

const (
	ImportTargetNew      ImportTargetAction = "new"
	ImportTargetExisting ImportTargetAction = "existing"
	ImportTargetSkip     ImportTargetAction = "skip"
)

// ImportTarget is the caller's per-file instruction: create a new Calendar
// (optionally naming it), write into an existing one, or skip the file
// entirely. Filename matches an ImportFile's Name (for a .zip, the archive
// entry name; for a single .ics, the uploaded filename).
type ImportTarget struct {
	Filename   string
	Action     ImportTargetAction
	Name       string
	CalendarID string
}

// ImportFile is one file to import: its name and raw bytes. A .ics upload
// is one ImportFile; a .zip upload is one ImportFile per .ics archive
// entry.
type ImportFile struct {
	Name string
	Data []byte
}

// ExtractFiles splits an uploaded file into the ImportFiles it holds: a
// .zip is unzipped entry by entry, keeping only ".ics" entries; a single
// .ics is one ImportFile. isZip tells the caller whether targets may use
// action "existing" (ADR-0030: only a single-file .ics import may target an
// existing Calendar).
func ExtractFiles(filename string, data []byte) (files []ImportFile, isZip bool, err error) {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, true, fmt.Errorf("open zip: %w", err)
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".ics") {
				continue
			}
			entry, err := readZipEntry(f)
			if err != nil {
				return nil, true, err
			}
			files = append(files, ImportFile{Name: f.Name, Data: entry})
		}
		return files, true, nil
	case strings.HasSuffix(lower, ".ics"):
		return []ImportFile{{Name: filename, Data: data}}, false, nil
	default:
		return nil, false, fmt.Errorf("%w: %q", ErrImportUnsupportedFileType, filename)
	}
}

// readZipEntry reads one .zip archive entry's whole content into memory —
// small enough to be safe since the archive itself is already capped at
// maxImportUploadBytes before it reaches here.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read zip entry %q: %w", f.Name, err)
	}
	return data, nil
}

// ReminderCounts breaks a count out by Channel (ADR-0030: the email figure
// must be visible before the user confirms, since it means the scheduler
// will really send mail).
type ReminderCounts struct {
	Notification int
	Email        int
}

func (c *ReminderCounts) add(reminders []repository.Reminder) {
	for _, r := range reminders {
		switch r.Channel {
		case "notification":
			c.Notification++
		case "email":
			c.Email++
		}
	}
}

// SkippedGroup is one reason import declined to write some series, the
// count of series it applied to, and a few sample titles.
type SkippedGroup struct {
	Reason  string
	Count   int
	Samples []string
}

// AdjustedGroup is one way import quietly altered series it did write, and
// how many series it applied to.
type AdjustedGroup struct {
	Reason string
	Count  int
}

const maxSkippedSamples = 5

const (
	AdjustedFloatingDowngrade = "unresolvable timezone downgraded to a Floating Event"
	AdjustedAlarmsDropped     = "unsupported reminder dropped"
	AdjustedZeroLengthAllDay  = "zero-length all-day event given a one-day duration"
)

// AdjustedZeroLengthTimed states the duration a zero-length timed Event was
// given, read off the constant that gives it rather than repeating the
// figure, so the two can never disagree — the Import summary is part of the
// contract, not decoration (ADR-0030).
var AdjustedZeroLengthTimed = fmt.Sprintf("zero-length event given a %d-minute duration",
	int(icalendar.ZeroLengthTimedDuration/time.Minute))

// Skip reasons for a series icalendar decoded cleanly and this app still
// cannot store (#228), continuing the vocabulary icalendar's own skip
// reasons start for a series that couldn't be decoded at all. Each names
// what about the series has no place in this app's model, since the Import
// summary is where a user learns why an Event they can see in their file
// isn't in their Calendar (ADR-0030).
const (
	SkipMissingTitle              = "missing title"
	SkipEndBeforeStart            = "end before start"
	SkipUnsupportedRecurrenceRule = "unsupported recurrence rule"
	SkipUnsupportedReminder       = "unsupported reminder"
	SkipUnsupportedColor          = "unsupported color"
	SkipUnstorable                = "cannot be stored"
)

// dropUnstorableSeries removes from writes — and, index for index, from
// parsed.Series — every series this app cannot store, recording each on
// parsed.Skipped with the reason it was dropped, and returns what survives.
// One VEVENT this app has no way to model must cost its own series and
// nothing more (#228): the file's other Events import, and the Import
// summary accounts for what was left out, which ADR-0030 makes part of the
// feature rather than a diagnostic. Keeping parsed in step with writes is
// what lets fillFileSummary go on reading the two by the same index, and
// what lets a Refresh tell "present in the feed but unstorable" apart from
// "gone from the feed" (ADR-0033) — hence the series' own UID on each
// record.
func dropUnstorableSeries(parsed *icalendar.ParsedFile, writes []SeriesWrite) []SeriesWrite {
	kept := writes[:0]
	keptSeries := parsed.Series[:0]
	for i := range writes {
		if err := validateSeriesWrite(&writes[i]); err != nil {
			parsed.Skipped = append(parsed.Skipped, icalendar.SkippedSeries{
				Reason: skipReason(err),
				Title:  strings.TrimSpace(writes[i].Title),
				UID:    parsed.Series[i].UID,
			})
			continue
		}
		kept = append(kept, writes[i])
		keptSeries = append(keptSeries, parsed.Series[i])
	}
	parsed.Series = keptSeries
	return kept
}

// skipReason names, for the Import summary, what a write failed validation
// on. An error with no reason of its own still reports one rather than
// nothing, since a skip the summary can't explain is exactly the silence
// ADR-0030 exists to prevent.
func skipReason(err error) string {
	switch {
	case errors.Is(err, ErrInvalidTitle):
		return SkipMissingTitle
	case errors.Is(err, ErrInvalidTimeRange):
		return SkipEndBeforeStart
	case errors.Is(err, ErrInvalidRecurrenceRule):
		return SkipUnsupportedRecurrenceRule
	case errors.Is(err, ErrInvalidReminderChannel):
		return SkipUnsupportedReminder
	case errors.Is(err, ErrInvalidEventColor):
		return SkipUnsupportedColor
	default:
		return SkipUnstorable
	}
}

// IgnoredCounts tallies components a Calendar file may carry that this app
// doesn't model at all (ADR-0030).
type IgnoredCounts struct {
	VTodo, VJournal, VFreeBusy int
}

// AttachmentCounts tallies the four outcomes an inline/URI ATTACH can have
// on import (#135, ADR-0040): produced by the parse, so the same figures
// appear in the dry-run preview and after a real run.
type AttachmentCounts struct {
	// Imported is how many distinct inline ATTACH values became Attachments.
	Imported int
	// TooLarge is how many distinct inline ATTACH values exceeded
	// MAX_ATTACHMENT_SIZE and were skipped.
	TooLarge int
	// TooMany is how many distinct inline ATTACH values were skipped for
	// arriving past MAX_ATTACHMENTS_PER_EVENT on their series.
	TooMany int
	// IgnoredURI is how many distinct URI ATTACH values were seen and never
	// fetched (ADR-0040: a link is not an Attachment).
	IgnoredURI int
}

// FileSummary is one imported file's contribution to the Import summary
// (ADR-0030): its proposed (or resolved) destination Calendar, how many
// series it contributed, the date range they span, and everything import
// skipped, adjusted, or ignored within it.
type FileSummary struct {
	Filename     string
	CalendarName string
	// CalendarID is the destination Calendar's id. Empty on a dry run for
	// action "new", since nothing has been created yet.
	CalendarID string
	// EventCount is the number of series (not including Overrides) this
	// file contributed.
	EventCount           int
	RangeStart, RangeEnd *time.Time
	Skipped              []SkippedGroup
	Adjusted             []AdjustedGroup
	Ignored              IgnoredCounts
	Reminders            ReminderCounts
	Attachments          AttachmentCounts
}

// ImportSummary is the whole upload's Import summary — the same payload for
// dryRun=1 and dryRun=0 (ADR-0030).
type ImportSummary struct {
	Files []FileSummary
}

// ImportService orchestrates ICS import: it owns the attachmentstore.Store
// directly (mirroring AttachmentService.Upload's pattern) so an inline
// ATTACH's bytes land on disk before EventService.ImportSeries opens the
// transaction that rows them (ADR-0040), delegating everything else — the
// actual Event write and Calendar creation — to EventService and
// CalendarService.
type ImportService struct {
	events    *EventService
	calendars *CalendarService
	// attachments is where saveAttachments writes an inline ATTACH's bytes
	// before EventService.ImportSeries rows them (ADR-0040).
	attachments *attachmentstore.Store
	// maxAttachmentSize and maxAttachmentsPerEvent are
	// MAX_ATTACHMENT_SIZE/MAX_ATTACHMENTS_PER_EVENT, threaded into
	// icalendar.ParseImportFile so the parse enforces the same caps a live
	// upload would (#135).
	maxAttachmentSize      int64
	maxAttachmentsPerEvent int
}

func NewImportService(events *EventService, calendars *CalendarService, attachments *attachmentstore.Store, maxAttachmentSize int64, maxAttachmentsPerEvent int) *ImportService {
	return &ImportService{events: events, calendars: calendars, attachments: attachments, maxAttachmentSize: maxAttachmentSize, maxAttachmentsPerEvent: maxAttachmentsPerEvent}
}

// importColorRotation is the default Calendar color a new import-created
// Calendar gets when its file carries no X-APPLE-CALENDAR-COLOR, cycling so
// a whole folder of imported Calendars isn't monochrome. Independent of
// CalendarService's defaultCalendars seed colors and the frontend's Swatch
// hexes — the lists are allowed to drift (ADR-0029).
var importColorRotation = []string{
	"#12809CFF", // peacock
	"#E2483DFF", // tomato
	"#6B9071FF", // sage
	"#8E44ADFF", // grape
	"#D4A017FF", // amber
	"#3B6E8FFF", // slate
}

// preparedFile is one file's fully validated, not-yet-written state: parsed
// content, the SeriesWrite it produces, and its proposed destination —
// everything Import needs to know before it creates a single Calendar row
// or calls ImportSeries.
type preparedFile struct {
	file    ImportFile
	target  ImportTarget
	parsed  *icalendar.ParsedFile
	writes  []SeriesWrite
	summary FileSummary
	// color is the proposed color for a "new" target's Calendar, computed
	// up front so create (pass two) needn't recompute the rotation.
	color string
}

// Import validates every target and every file's parsed content — including
// the same title-trim/field checks EventService.ImportSeries itself applies
// — before writing anything, so a bad target or a bad series later in a
// multi-file upload can never leave earlier files already committed, and a
// dry run reports exactly the errors a real run would hit (ADR-0030). Only
// once every file has passed validation does it create Calendars and call
// EventService.ImportSeries — once per file, since ImportSeries is scoped to
// one destination Calendar and a .zip's entries each get their own.
func (s *ImportService) Import(ctx context.Context, userID, workspaceID int64, filename string, data []byte, targets []ImportTarget, dryRun bool) (ImportSummary, error) {
	files, isZip, err := ExtractFiles(filename, data)
	if err != nil {
		return ImportSummary{}, err
	}

	targetByFilename := make(map[string]ImportTarget, len(targets))
	for _, t := range targets {
		targetByFilename[t.Filename] = t
	}

	rotation := 0
	var prepared []preparedFile
	for _, file := range files {
		target, ok := targetByFilename[file.Name]
		if !ok {
			return ImportSummary{}, fmt.Errorf("%w: %q", ErrImportTargetMissing, file.Name)
		}
		if err := validateTarget(target, isZip); err != nil {
			return ImportSummary{}, err
		}
		if target.Action == ImportTargetSkip {
			continue
		}

		parsed, err := icalendar.ParseImportFile(bytes.NewReader(file.Data), s.maxAttachmentSize, s.maxAttachmentsPerEvent)
		if err != nil {
			return ImportSummary{}, fmt.Errorf("parse %q: %w", file.Name, err)
		}

		writes := make([]SeriesWrite, len(parsed.Series))
		for i, series := range parsed.Series {
			writes[i] = seriesWriteFromImported(series)
		}
		writes = dropUnstorableSeries(parsed, writes)

		fileSummary, color, err := s.proposeTarget(ctx, userID, file.Name, target, parsed, &rotation)
		if err != nil {
			return ImportSummary{}, err
		}

		prepared = append(prepared, preparedFile{file: file, target: target, parsed: parsed, writes: writes, summary: fileSummary, color: color})
	}

	var summary ImportSummary
	for _, p := range prepared {
		calendarID := p.summary.CalendarID
		if p.target.Action == ImportTargetNew && !dryRun {
			calendar, err := s.calendars.Create(ctx, userID, workspaceID, uuid.NewString(), CalendarWrite{Name: p.summary.CalendarName, Color: p.color})
			if err != nil {
				return ImportSummary{}, fmt.Errorf("create calendar %q: %w", p.summary.CalendarName, err)
			}
			calendarID = calendar.ID
			p.summary.CalendarID = calendar.ID
		}

		if !dryRun && len(p.writes) > 0 {
			if err := s.saveAttachments(p.parsed.Series, p.writes); err != nil {
				return ImportSummary{}, fmt.Errorf("save attachments for %q: %w", p.file.Name, err)
			}
			if _, err := s.events.ImportSeries(ctx, userID, calendarID, p.writes); err != nil {
				return ImportSummary{}, fmt.Errorf("import %q: %w", p.file.Name, err)
			}
		}

		fillFileSummary(&p.summary, p.parsed, p.writes)
		summary.Files = append(summary.Files, p.summary)
	}

	return summary, nil
}

// saveAttachments writes every series' decoded inline Attachments to disk
// and fills in writes' Attachments field with the saved id/size a moment
// before EventService.ImportSeries rows them — ADR-0040's ordering
// ("bytes before commit") holds because this runs, and returns, before
// ImportSeries ever opens its transaction. series and writes are the same
// length and index-aligned (both built 1:1 from parsed.Series by Import), so
// a dry run — which never calls this — writes nothing to disk. A failure
// partway through leaves whatever was already saved for the sweeper to
// reclaim (#132), matching ADR-0040's "do not add a bespoke rollback path".
func (s *ImportService) saveAttachments(series []icalendar.ImportedSeries, writes []SeriesWrite) error {
	for i, sr := range series {
		if len(sr.Attachments) == 0 {
			continue
		}
		attachments := make([]AttachmentWrite, len(sr.Attachments))
		for j, ia := range sr.Attachments {
			id := uuid.NewString()
			size, err := s.attachments.Save(id, bytes.NewReader(ia.Data))
			if err != nil {
				return fmt.Errorf("save attachment: %w", err)
			}
			attachments[j] = AttachmentWrite{ID: id, Filename: ia.Filename, ContentType: ia.ContentType, SizeBytes: size}
		}
		writes[i].Attachments = attachments
	}
	return nil
}

// validateTarget checks target's action is one Import understands and, for
// "existing", that it names a CalendarID and isn't inside a .zip (ADR-0030:
// only a single-file .ics import may target an existing Calendar).
func validateTarget(target ImportTarget, isZip bool) error {
	switch target.Action {
	case ImportTargetNew, ImportTargetSkip:
	case ImportTargetExisting:
		if isZip {
			return fmt.Errorf("%w: %q", ErrImportExistingNotAllowedForZip, target.Filename)
		}
		if target.CalendarID == "" {
			return fmt.Errorf("%w: %q", ErrImportMissingCalendarID, target.Filename)
		}
	default:
		return fmt.Errorf("%w: %q", ErrImportInvalidAction, target.Filename)
	}
	return nil
}

// proposeTarget resolves target's destination Calendar without writing
// anything: fetching it for "existing" (so an unknown CalendarID fails
// before any file is written, not partway through), or computing the
// name/color a "new" Calendar will get if this Import goes on to create one.
// name comes from the target's own name override, then the file's
// X-WR-CALNAME, then a filename-derived name; color from the file's
// X-APPLE-CALENDAR-COLOR, else the next rotation color. Returns the
// proposed color separately from summary since FileSummary has no color
// field of its own — only the later Create call needs it.
func (s *ImportService) proposeTarget(ctx context.Context, userID int64, filename string, target ImportTarget, parsed *icalendar.ParsedFile, rotation *int) (FileSummary, string, error) {
	summary := FileSummary{Filename: filename}

	if target.Action == ImportTargetExisting {
		calendar, err := s.calendars.Get(ctx, userID, target.CalendarID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return FileSummary{}, "", fmt.Errorf("%w: %q", ErrCalendarNotFound, target.CalendarID)
			}
			return FileSummary{}, "", err
		}
		if calendar.Source != nil {
			return FileSummary{}, "", fmt.Errorf("%w: %q", ErrImportTargetSubscribed, target.CalendarID)
		}
		summary.CalendarName = calendar.Name
		summary.CalendarID = calendar.ID
		return summary, "", nil
	}

	name := target.Name
	if name == "" {
		name = parsed.CalendarName
	}
	if name == "" {
		name = strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	}
	summary.CalendarName = name

	color, ok := NormalizeColor(parsed.Color)
	if !ok {
		color = importColorRotation[*rotation%len(importColorRotation)]
		*rotation++
	}

	return summary, color, nil
}

// fillFileSummary computes the parts of a file's summary that don't depend
// on whether the Calendar was actually written to: event count, date range,
// skipped/adjusted/ignored counts, and reminder counts — the same for a dry
// run and a real run (ADR-0030).
func fillFileSummary(summary *FileSummary, parsed *icalendar.ParsedFile, writes []SeriesWrite) {
	summary.EventCount = len(parsed.Series)
	summary.Ignored = IgnoredCounts{
		VTodo:     parsed.IgnoredVTodo,
		VJournal:  parsed.IgnoredVJournal,
		VFreeBusy: parsed.IgnoredVFreeBusy,
	}

	var floatingDowngrades, droppedAlarms, zeroLengthTimed, zeroLengthAllDay int
	for i, series := range parsed.Series {
		floatingDowngrades += series.FloatingDowngrades
		droppedAlarms += series.DroppedAlarms
		zeroLengthTimed += series.ZeroLengthTimed
		zeroLengthAllDay += series.ZeroLengthAllDay
		summary.Attachments.Imported += len(series.Attachments)
		summary.Attachments.TooLarge += series.AttachmentsTooLarge
		summary.Attachments.TooMany += series.AttachmentsTooMany
		summary.Attachments.IgnoredURI += series.AttachmentsIgnoredURI

		extendRange(summary, writes[i].Start, writes[i].End)
		summary.Reminders.add(writes[i].Reminders)
		for _, o := range writes[i].Overrides {
			extendRange(summary, o.Start, o.End)
			summary.Reminders.add(o.Reminders)
		}
	}
	if floatingDowngrades > 0 {
		summary.Adjusted = append(summary.Adjusted, AdjustedGroup{Reason: AdjustedFloatingDowngrade, Count: floatingDowngrades})
	}
	if droppedAlarms > 0 {
		summary.Adjusted = append(summary.Adjusted, AdjustedGroup{Reason: AdjustedAlarmsDropped, Count: droppedAlarms})
	}
	if zeroLengthTimed > 0 {
		summary.Adjusted = append(summary.Adjusted, AdjustedGroup{Reason: AdjustedZeroLengthTimed, Count: zeroLengthTimed})
	}
	if zeroLengthAllDay > 0 {
		summary.Adjusted = append(summary.Adjusted, AdjustedGroup{Reason: AdjustedZeroLengthAllDay, Count: zeroLengthAllDay})
	}

	summary.Skipped = skippedGroups(parsed.Skipped)
}

func extendRange(summary *FileSummary, start, end time.Time) {
	if summary.RangeStart == nil || start.Before(*summary.RangeStart) {
		s := start
		summary.RangeStart = &s
	}
	if summary.RangeEnd == nil || end.After(*summary.RangeEnd) {
		e := end
		summary.RangeEnd = &e
	}
}

// skippedGroups collapses ParseImportFile's per-series skip records into one
// entry per reason, each carrying a count and a few sample titles.
func skippedGroups(skipped []icalendar.SkippedSeries) []SkippedGroup {
	if len(skipped) == 0 {
		return nil
	}

	order := make([]string, 0, 4)
	byReason := make(map[string]*SkippedGroup, 4)
	for _, s := range skipped {
		g, ok := byReason[s.Reason]
		if !ok {
			g = &SkippedGroup{Reason: s.Reason}
			byReason[s.Reason] = g
			order = append(order, s.Reason)
		}
		g.Count++
		if len(g.Samples) < maxSkippedSamples && s.Title != "" {
			g.Samples = append(g.Samples, s.Title)
		}
	}

	groups := make([]SkippedGroup, len(order))
	for i, reason := range order {
		groups[i] = *byReason[reason]
	}
	return groups
}

// seriesWriteFromImported converts one decoded series into the
// EventService.ImportSeries writes it takes, discarding the file's own UID
// (ADR-0030) — ImportSeries mints its own id.
func seriesWriteFromImported(imported icalendar.ImportedSeries) SeriesWrite {
	overrides := make([]OverrideWrite, len(imported.Overrides))
	for i, o := range imported.Overrides {
		overrides[i] = OverrideWrite{
			RecurrenceID: o.RecurrenceID,
			Title:        o.Title,
			Description:  o.Description,
			Location:     o.Location,
			URL:          o.URL,
			Start:        o.Start,
			End:          o.End,
			AllDay:       o.AllDay,
			Tzid:         o.Tzid,
			Reminders:    o.Reminders,
			Color:        o.Color,
		}
	}

	return SeriesWrite{
		Title:       imported.Master.Title,
		Description: imported.Master.Description,
		Location:    imported.Master.Location,
		URL:         imported.Master.URL,
		Start:       imported.Master.Start,
		End:         imported.Master.End,
		AllDay:      imported.Master.AllDay,
		Tzid:        imported.Master.Tzid,
		Rrule:       imported.Rrule,
		Reminders:   imported.Master.Reminders,
		Exdates:     imported.Exdates,
		Overrides:   overrides,
		Color:       imported.Master.Color,
	}
}
