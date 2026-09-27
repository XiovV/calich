package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/XiovV/calich/server/internal/repository"
)

var (
	// ErrBookingLinkPaused is returned by Book when the link isn't accepting
	// bookings right now — explicitly Paused, no SMTP configured, or the
	// host has lost Owner/Editor Access to the Book-into Calendar since the
	// link was last saved (ADR-0087) — collapsed into the one answer isPaused
	// already renders for Get/Slots, so a stranger never learns which.
	ErrBookingLinkPaused = errors.New("this booking link is not accepting bookings")
	// ErrInvalidVisitorName is returned by Book when the visitor's name is
	// empty — the booking form's only other required field besides email
	// (ADR-0087).
	ErrInvalidVisitorName = errors.New("name is required")
	// ErrSlotTaken is returned by Book when start no longer names a
	// bookable slot once re-derived inside the write transaction — either
	// another visitor just took it, or it was never a real slot to begin
	// with (stale client state, a slot past the booking horizon, ...). The
	// caller re-fetches slots and tries again (ADR-0087's "the loser is
	// told the time was just taken and shown fresh slots").
	ErrSlotTaken = errors.New("that time was just taken")
	// ErrInvalidCancelToken is returned by Cancel when token doesn't parse
	// or verify against a signature this instance minted — a forged,
	// truncated, or otherwise tampered cancel link (#327, ADR-0087).
	ErrInvalidCancelToken = errors.New("invalid or expired cancel link")
	// ErrBookingAlreadyStarted is returned by Cancel when the booked Event's
	// own start is no longer in the future — a stale link cannot delete
	// something that already happened (#327, ADR-0087).
	ErrBookingAlreadyStarted = errors.New("this booking has already started and can no longer be cancelled")
)

// bookingCancelCodec is AuthService's own signed-token pair
// (IssueBookingCancelToken/ParseBookingCancelToken, #327, ADR-0087) —
// PublicBookingService's own narrow seam onto it, mirroring
// connectStateCodec's relationship to ConnectionService.
type bookingCancelCodec interface {
	IssueBookingCancelToken(eventID string) (string, error)
	ParseBookingCancelToken(token string) (string, error)
}

// PublicBookingLink is what a stranger holding a Booking Link's URL is told
// (#324, ADR-0084, ADR-0087) — deliberately narrower than repository.
// BookingLink: no id, no Book-into Calendar, no Conflict set, nothing that
// isn't shown on the page itself. Visibility itself is never exposed either
// — Paused collapses "explicitly Paused", "no SMTP configured" and "lost
// Book-into Access" into the one answer a visitor ever needs (ADR-0087: "A
// link behaves as Paused ... and Settings says why" — the why is for the
// host, in Settings, not for a stranger).
type PublicBookingLink struct {
	HostName           string
	HostTimezone       string
	Title              string
	DurationMinutes    int
	Location           string
	Description        string
	BookingHorizonDays int
	Paused             bool
}

// PublicBookingService serves the public Booking Link page (#324, ADR-0084,
// ADR-0087) — the app's first unauthenticated surface that touches calendar
// data. Every method resolves (handle, slug) itself rather than taking an
// id, and every failure to resolve is repository.ErrNotFound: an unknown
// Handle, an unknown Slug, and a reserved-word Handle are indistinguishable,
// so the namespace cannot enumerate Users (ADR-0084's Consequences). A
// Private link is not folded into that: it resolves and renders exactly
// like a Public one when reached by its own direct URL — only the derived
// index (#325) omits it.
type PublicBookingService struct {
	users        *repository.UserRepository
	links        *repository.BookingLinkRepository
	schedules    *repository.AvailabilityScheduleRepository
	calendars    *CalendarService
	bookingLinks *BookingLinkService
	// events is Book's own write path (#326, ADR-0087) — EventService.Create
	// already does everything a booking needs: an Access-checked write onto
	// the Book-into Calendar, an email-shaped Attendee invite (and its
	// Invitation, when outbox is configured), and — for a writable Linked
	// Calendar — a queued Write-back push. Get and Slots above never touch
	// it.
	events *EventService
	// eventsRepo is Cancel's own read of the booked Event (#327, ADR-0087):
	// a signed cancel link names an Event, not a (handle, slug), so there is
	// no host Session to resolve it through the way every other method on
	// this service does — this is a direct, unauthorized-by-design lookup,
	// with the token's own signature the only gate.
	eventsRepo *repository.EventRepository
	// outbox queues the visitor's booking confirmation (carrying the signed
	// cancel link) and the host's cancellation notice (#327, ADR-0087) —
	// both a BOOKING_NOTICE row, never sent inline (ADR-0060).
	outbox *repository.OutboxRepository
	// cancelTokens mints and verifies Cancel's own signed link.
	cancelTokens bookingCancelCodec
	// smtpConfigured mirrors config.Config.SMTPConfigured() — publishing a
	// Booking Link requires SMTP (ADR-0087), so every link behaves as
	// Paused when this instance has none, regardless of what it's set to.
	smtpConfigured bool
}

func NewPublicBookingService(users *repository.UserRepository, links *repository.BookingLinkRepository, schedules *repository.AvailabilityScheduleRepository, calendars *CalendarService, bookingLinks *BookingLinkService, events *EventService, eventsRepo *repository.EventRepository, outbox *repository.OutboxRepository, cancelTokens bookingCancelCodec, smtpConfigured bool) *PublicBookingService {
	return &PublicBookingService{
		users:          users,
		links:          links,
		schedules:      schedules,
		calendars:      calendars,
		bookingLinks:   bookingLinks,
		events:         events,
		eventsRepo:     eventsRepo,
		outbox:         outbox,
		cancelTokens:   cancelTokens,
		smtpConfigured: smtpConfigured,
	}
}

// Get resolves (handle, slug) into what the public page renders.
func (s *PublicBookingService) Get(ctx context.Context, handle, slug string) (PublicBookingLink, error) {
	link, user, err := s.resolve(ctx, handle, slug)
	if err != nil {
		return PublicBookingLink{}, err
	}

	schedule, err := s.schedules.GetByID(ctx, link.AvailabilityScheduleID, link.UserID)
	if err != nil {
		return PublicBookingLink{}, fmt.Errorf("get availability schedule: %w", err)
	}

	paused, err := s.isPaused(ctx, link)
	if err != nil {
		return PublicBookingLink{}, err
	}

	return PublicBookingLink{
		HostName:           user.Name,
		HostTimezone:       schedule.Tzid,
		Title:              link.Title,
		DurationMinutes:    link.DurationMinutes,
		Location:           link.Location,
		Description:        link.Description,
		BookingHorizonDays: link.BookingHorizonDays,
		Paused:             paused,
	}, nil
}

// Slots resolves (handle, slug) and derives its slots for (year, month),
// reusing the exact same derivation the authenticated /slots route runs
// (BookingLinkService.DeriveSlotsForLinkAndMonth) — the whole point of #323
// deriving server-side (ADR-0085's "the public page has no Session, so slot
// derivation runs on the backend"). A Paused link (explicitly, no SMTP, or a
// lost Book-into Access) always answers no slots, without distinguishing why
// — ADR-0087's Access re-check happens here, at read time, not only at
// Create/Update.
func (s *PublicBookingService) Slots(ctx context.Context, handle, slug string, year int, month time.Month, now time.Time) ([]time.Time, error) {
	if month < 1 || month > 12 {
		return nil, ErrInvalidMonth
	}

	link, _, err := s.resolve(ctx, handle, slug)
	if err != nil {
		return nil, err
	}

	paused, err := s.isPaused(ctx, link)
	if err != nil {
		return nil, err
	}
	if paused {
		return []time.Time{}, nil
	}

	return s.bookingLinks.DeriveSlotsForLinkAndMonth(ctx, link, year, month, now)
}

// BookingRequest is Book's input: the booking form's only two fields, both
// required (ADR-0087), plus the slot the visitor picked from Slots' own
// answer.
type BookingRequest struct {
	Start        time.Time
	VisitorName  string
	VisitorEmail string
}

// Book resolves (handle, slug) and confirms req.Start into a Busy Event on
// the Book-into Calendar (#326, ADR-0087): the visitor becomes an
// email-shaped Attendee (ADR-0058) and, once the write commits, receives the
// ordinary Invitation (ADR-0059) — EventService.Create's own attendee-invite
// path does both, unchanged. Access to the Book-into Calendar and SMTP are
// re-checked here via isPaused, exactly as Get and Slots already do, rather
// than trusted from whenever the link was last saved. The slot itself is
// re-derived a second time, inside Create's own write transaction
// (EventWrite.PreCommitCheck) — so a slot that was free when isPaused ran a
// moment ago but is taken by the time this transaction actually opens is
// still caught, and two visitors racing the same slot can never both win.
func (s *PublicBookingService) Book(ctx context.Context, handle, slug string, req BookingRequest, now time.Time, baseURL string) (repository.Event, error) {
	link, host, err := s.resolve(ctx, handle, slug)
	if err != nil {
		return repository.Event{}, err
	}

	paused, err := s.isPaused(ctx, link)
	if err != nil {
		return repository.Event{}, err
	}
	if paused {
		return repository.Event{}, ErrBookingLinkPaused
	}

	name := strings.TrimSpace(req.VisitorName)
	if name == "" {
		return repository.Event{}, ErrInvalidVisitorName
	}
	email, err := validateEmail(req.VisitorEmail)
	if err != nil {
		return repository.Event{}, err
	}

	schedule, err := s.schedules.GetByID(ctx, link.AvailabilityScheduleID, link.UserID)
	if err != nil {
		return repository.Event{}, fmt.Errorf("get availability schedule: %w", err)
	}

	start := req.Start
	end := start.Add(time.Duration(link.DurationMinutes) * time.Minute)
	tzid := schedule.Tzid

	write := EventWrite{
		CalendarID:     link.BookIntoCalendarID,
		Title:          bookingEventTitle(link.Title, name),
		Start:          start,
		End:            end,
		Busy:           true,
		Tzid:           &tzid,
		Description:    link.Description,
		Location:       link.Location,
		AttendeeEmails: []string{email},
		// A Linked Calendar is a legal Book-into target (ADR-0087) — Create's
		// own guard otherwise refuses any Attendee on a Connection Source
		// (ADR-0052); this is the one caller that opts back in, since the
		// visitor's row stays local-only regardless (EventWrite's own doc
		// comment on this field explains why that's still consistent with
		// ADR-0052's reasoning).
		AllowAttendeesOnConnectionSource: true,
		// The slot re-derivation this method's own doc comment describes,
		// run through BookingLinkService.SlotAvailable against tx-bound
		// Event/Task repositories built from this very transaction's *sql.Tx
		// — never s.bookingLinks' own pooled ones, which would read outside
		// it and reopen the race this exists to close.
		PreCommitCheck: func(ctx context.Context, tx *sql.Tx) error {
			available, err := s.bookingLinks.SlotAvailable(ctx, tx, schedule, link, start, now)
			if err != nil {
				return fmt.Errorf("re-derive slot: %w", err)
			}
			if !available {
				return ErrSlotTaken
			}
			return nil
		},
	}

	event, err := s.events.Create(ctx, link.UserID, uuid.NewString(), write)
	if err != nil {
		if errors.Is(err, ErrSlotTaken) {
			return repository.Event{}, ErrSlotTaken
		}
		if errors.Is(err, ErrCalendarNotFound) || errors.Is(err, ErrCalendarReadOnly) {
			// The host lost Access to the Book-into Calendar (or it stopped
			// resolving) between isPaused's own check above and this write
			// landing — rare, but answered exactly as isPaused already
			// would have (ADR-0087).
			return repository.Event{}, ErrBookingLinkPaused
		}
		return repository.Event{}, fmt.Errorf("create booking event: %w", err)
	}

	// The confirmation mail carrying the signed cancel link (#327,
	// ADR-0087) — queued through the outbox (ADR-0060) rather than sent
	// inline, alongside (never instead of) the ordinary Invitation
	// EventService.Create's own attendee-invite path already queued above.
	// The Event already committed by this point, so a failure here (an
	// unexpected DB error, not a delivery failure — the outbox Worker's own
	// retry/backoff owns those) surfaces as a 500 to the visitor even though
	// the booking itself stands; a rare gap accepted rather than adding a
	// second transaction spanning two independent writes.
	token, err := s.cancelTokens.IssueBookingCancelToken(event.ID)
	if err != nil {
		return repository.Event{}, fmt.Errorf("issue booking cancel token: %w", err)
	}
	cancelURL := baseURL + "/cancel-booking?token=" + url.QueryEscape(token)
	subject, body := bookingConfirmationNotice(host.Name, link.Title, link.Location, start, end, tzid, cancelURL)
	if _, err := s.outbox.EnqueueBookingNotice(ctx, event.ID, email, subject, body); err != nil {
		return repository.Event{}, fmt.Errorf("enqueue booking confirmation: %w", err)
	}

	return event, nil
}

// bookingConfirmationNotice renders the visitor's booking confirmation
// (#327, ADR-0087): what carries the signed cancel link.
func bookingConfirmationNotice(hostName, title, location string, start, end time.Time, tzid, cancelURL string) (subject, body string) {
	subject = fmt.Sprintf("Booking confirmed: %s", title)
	body = fmt.Sprintf("Your booking with %s is confirmed.\n\n%s\n%s\n", hostName, title, formatBookingWindow(start, end, tzid))
	if location != "" {
		body += location + "\n"
	}
	body += fmt.Sprintf("\nNeed to cancel? %s\n", cancelURL)
	return subject, body
}

// bookingCancelledNotice renders the host's cancellation notice (#327,
// ADR-0087) — no Notification is raised for it (CONTEXT.md's three
// producers stay three); this mail is the only trace the host gets.
func bookingCancelledNotice(visitorName, visitorEmail, title string, start, end time.Time, tzid string) (subject, body string) {
	subject = fmt.Sprintf("Cancelled: %s", title)
	body = fmt.Sprintf("%s (%s) cancelled their booking.\n\n%s\n%s\n", visitorName, visitorEmail, title, formatBookingWindow(start, end, tzid))
	return subject, body
}

// formatBookingWindow renders start-end in tzid's own wall-clock, falling
// back to UTC for an unparseable zone rather than failing the whole notice
// over a formatting detail.
func formatBookingWindow(start, end time.Time, tzid string) string {
	loc, err := time.LoadLocation(tzid)
	if err != nil {
		loc = time.UTC
	}
	const layout = "Mon, Jan 2, 2006 15:04"
	return fmt.Sprintf("%s - %s (%s)", start.In(loc).Format(layout), end.In(loc).Format("15:04"), tzid)
}

// Cancel verifies token, refuses a booking that has already started, and
// deletes its Event (#327, ADR-0087) — the visitor's own signed link is the
// only gate here, since a visitor holds no Session to authorize this
// through any other way. Deletion reuses EventService.Delete wholesale
// (keyed on the Event's own CreatedBy as the host): the METHOD:CANCEL to
// the visitor's Attendee row, the write-back delete on a Linked Calendar,
// and the tombstone are all its existing, tested behavior. Idempotent: an
// already-cancelled (or never-existed) Event answers nil, not an error, so
// following the link twice never surfaces one.
func (s *PublicBookingService) Cancel(ctx context.Context, token string, now time.Time) error {
	eventID, err := s.cancelTokens.ParseBookingCancelToken(token)
	if err != nil {
		return ErrInvalidCancelToken
	}

	event, err := s.eventsRepo.GetByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get booked event for cancellation: %w", err)
	}
	if event.CreatedBy == nil {
		return ErrInvalidCancelToken
	}
	hostID := *event.CreatedBy

	if !event.Start.After(now) {
		return ErrBookingAlreadyStarted
	}

	host, err := s.users.GetByID(ctx, hostID)
	if err != nil {
		return fmt.Errorf("get host for cancellation notice: %w", err)
	}

	// Captured before Delete, which removes the Attendee row along with the
	// Event itself.
	var visitorName, visitorEmail string
	if attendees, err := s.events.ListAttendees(ctx, hostID, event.ID); err == nil && len(attendees) > 0 {
		visitorName, visitorEmail = attendees[0].Name, attendees[0].Email
	}

	if err := s.events.Delete(ctx, hostID, event.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Raced with another cancel following the same link — already
			// gone, which is exactly what this call wanted.
			return nil
		}
		return fmt.Errorf("delete cancelled booking event: %w", err)
	}

	subject, body := bookingCancelledNotice(visitorName, visitorEmail, event.Title, event.Start, event.End, hostTzid(event))
	if _, err := s.outbox.EnqueueBookingNotice(ctx, event.ID, host.Email, subject, body); err != nil {
		return fmt.Errorf("enqueue booking cancelled notice: %w", err)
	}
	return nil
}

// hostTzid is the Anchor zone a cancellation notice renders event.Start/End
// in, falling back to UTC for a Floating booking Event — never actually nil
// in practice, since Book always sets one from the Schedule, but formatBookingWindow's
// own fallback would otherwise have to guess at an empty string instead.
func hostTzid(event repository.Event) string {
	if event.Tzid != nil {
		return *event.Tzid
	}
	return "Etc/UTC"
}

// bookingEventTitle is a booking's Event title (ADR-0087's Decision:
// `title = "<link title> — <visitor name>"`).
func bookingEventTitle(linkTitle, visitorName string) string {
	return linkTitle + " — " + visitorName
}

// resolve looks up the Booking Link identified by (handle, slug), collapsing
// every way that can fail into repository.ErrNotFound: handle is reserved
// (#321, ADR-0084 — this namespace consumes service.ReservedHandles
// directly rather than keeping a second list), no User claims handle, or
// that User has no Booking Link with slug. Case-insensitivity for handle is
// the users.handle column's own COLLATE NOCASE (ADR-0084, mirroring Email);
// slug is compared as BookingLinkRepository.GetBySlug already does.
func (s *PublicBookingService) resolve(ctx context.Context, handle, slug string) (repository.BookingLink, repository.User, error) {
	if IsReservedHandle(handle) {
		return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
	}

	user, err := s.users.GetByHandle(ctx, handle)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
		}
		return repository.BookingLink{}, repository.User{}, fmt.Errorf("get user by handle: %w", err)
	}

	link, err := s.links.GetBySlug(ctx, user.ID, slug)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.BookingLink{}, repository.User{}, repository.ErrNotFound
		}
		return repository.BookingLink{}, repository.User{}, fmt.Errorf("get booking link by slug: %w", err)
	}

	return link, user, nil
}

// isPaused reports whether link behaves as Paused (ADR-0087): its own
// Visibility says so, this instance has no SMTP transport configured, or
// its owner has lost Owner or Editor Access to its Book-into Calendar since
// it was last saved — re-checked here, at read time, rather than trusted
// from create/update time, since a Share can be revoked at any point
// afterward.
func (s *PublicBookingService) isPaused(ctx context.Context, link repository.BookingLink) (bool, error) {
	if link.Visibility == "paused" {
		return true, nil
	}
	if !s.smtpConfigured {
		return true, nil
	}

	access, calendar, err := s.calendars.Access(ctx, link.UserID, link.BookIntoCalendarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return true, nil
		}
		return false, fmt.Errorf("resolve book-into calendar access: %w", err)
	}
	if !access.CanWrite() || calendar.WorkspaceID != link.WorkspaceID {
		return true, nil
	}

	return false, nil
}
