package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XiovV/calich/server/internal/repository"
)

// InviteRateLimitError wraps ErrInviteRateLimitExceeded with the actor's own
// configured hourly ceiling (#204, ADR-0058), so the error names the limit
// that was hit rather than a generic refusal — an organizer must never
// believe an invitation went out when it did not, and "try again, some of
// this silently didn't happen" is the failure mode the outbox itself
// (ADR-0060) exists to rule out. Returned by inviteUser/inviteEmail/
// expandGroupMembers alone — the three paths that invite someone not
// already an Attendee — never by a re-send or a cancellation, both of which
// are lifecycle mail for an Event already invited to and stay uncapped.
type InviteRateLimitError struct {
	LimitPerHour int
}

func (e *InviteRateLimitError) Error() string {
	return fmt.Sprintf("invitation rate limit exceeded: at most %d invitations per hour", e.LimitPerHour)
}

func (e *InviteRateLimitError) Unwrap() error {
	return ErrInviteRateLimitExceeded
}

// chargeInviteRateLimit enforces actorUserID's own hourly ceiling on
// brand-new Invitations (#204, ADR-0058) before inviteUser/inviteEmail/
// expandGroupMembers queue one more — counting only what
// OutboxRepository.EnqueueWithActor/EnqueueEmailWithActor wrote them, never
// a re-send or a CANCEL (repository.OutboxMessage.ActorUserID's own
// contract), so lifecycle mail for an Event already invited to never trips
// it. Runs inside the same transaction as the write it's guarding, so
// hitting the ceiling partway through a multi-target invite (a Create
// carrying several targets, or a large Group expansion) fails the whole
// write via withTx's rollback rather than leaving some recipients invited
// and others silently not.
//
// This is a deliberate exception to ADR-0018's general "reads happen
// outside the transaction" convention, on the same grounds
// expandGroupMembers' own caller (AddGroupAttendee) already carries for
// reading Group membership inside its transaction: a count-then-charge read
// outside the write it gates is a TOCTOU gap — two concurrent invites could
// each read the same count and both proceed, both landing past the
// ceiling. Reading and writing the charge together, inside one transaction,
// is what makes the check exact rather than advisory.
func chargeInviteRateLimit(ctx context.Context, outbox *repository.OutboxRepository, actorUserID int64, limitPerHour int) error {
	count, err := outbox.CountByActorSince(ctx, actorUserID, time.Now().Add(-time.Hour))
	if err != nil {
		return fmt.Errorf("count invitations for rate limit: %w", err)
	}
	if count >= limitPerHour {
		return &InviteRateLimitError{LimitPerHour: limitPerHour}
	}
	return nil
}

// addCreateAttendees writes eventID's initial Attendees inside Create's own
// transaction (#187, ADR-0055), reusing inviteUser, expandGroupMembers, and
// inviteEmail — the same checks and the same Group-expansion loop
// AddAttendee, AddGroupAttendee, and AddAttendeeByEmail run, just against
// repos, the transaction-bound repositories, never the EventService's own
// pooled ones — a pooled call here would deadlock against the
// single-connection test database while the transaction holds its one
// connection (txRepos' doc comment).
//
// userIDs are explicit targets and strict, matching inviteUser's own
// checks: an unknown or Disabled User, or one outside workspaceID, fails
// eventID's entire create — the first invalid target found, not collected.
// groupIDs are explicit too and just as strict about the Group id itself —
// unknown, or belonging to another Workspace, fails the create — but each
// valid Group's member expansion stays lenient (expandGroupMembers' own
// policy), since the caller named the Group, not its individual members. A
// User named both individually and via a Group is silently deduplicated,
// same as AddGroupAttendee already tolerates re-inviting someone. emails are
// explicit and strict too, matching inviteEmail's own checks (ADR-0058,
// #200): malformed, naming the Organizer, or naming a Disabled Member all
// fail the create. actorUserID is the creating User — every Attendee this
// call queues an Invitation for is charged against their own hourly
// ceiling (#204, ADR-0058).
func (s *EventService) addCreateAttendees(ctx context.Context, repos txRepos, event repository.Event, workspaceID int64, userIDs, groupIDs []int64, emails []string, actorUserID int64) error {
	for _, targetUserID := range userIDs {
		if _, err := inviteUser(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, workspaceID, targetUserID, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	for _, groupID := range groupIDs {
		group, err := repos.groups.GetByID(ctx, groupID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrGroupNotFound
			}
			return fmt.Errorf("look up attendee group: %w", err)
		}
		if group.WorkspaceID != workspaceID {
			return ErrAttendeeTargetNotInWorkspace
		}

		if _, err := expandGroupMembers(ctx, repos.groups, repos.users, repos.attendees, repos.notifications, repos.outbox, event, group.ID, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	for _, email := range emails {
		if _, err := inviteEmail(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, workspaceID, email, actorUserID, s.inviteRateLimitPerHour); err != nil {
			return err
		}
	}

	return nil
}

// attendeeManagementCalendar resolves eventID and confirms actorUserID holds
// Editor Access (or is the Owner) of its Calendar — AddAttendee and
// RemoveAttendee's shared guard, since Attendee management is a Calendar
// write like any other Event edit (#161), not something an Attendee with no
// Calendar Access could ever do to their own invite. Also returns the
// Calendar the guard resolved, so callers can read its WorkspaceID without a
// second lookup — one Access resolution per invite or removal, not two.
func (s *EventService) attendeeManagementCalendar(ctx context.Context, actorUserID int64, eventID string) (repository.Event, repository.Calendar, error) {
	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return repository.Event{}, repository.Calendar{}, err
	}
	calendar, err := s.requireWritableCalendar(ctx, actorUserID, event.CalendarID)
	if err != nil {
		if errors.Is(err, ErrCalendarNotFound) {
			return repository.Event{}, repository.Calendar{}, repository.ErrNotFound
		}
		return repository.Event{}, repository.Calendar{}, err
	}
	return event, calendar, nil
}

// enqueueReinvitations re-queues a REQUEST Invitation to every one of
// eventID's own current Attendees except those named in exceptUserIDs/
// exceptEmails — Update's ADR-0059 re-send, run whenever contentChanged said
// something an Invitation renders actually changed, and AddAttendee/
// AddGroupAttendee/RemoveAttendee's own re-send for everyone *else*: the
// Attendee list itself is a non-material change that still re-sends (every
// REQUEST renders the row's whole current ATTENDEE list, ADR-0062), but the
// one Attendee just added or removed already gets their own dedicated
// message — a fresh invite or a CANCEL — so they're excluded here to avoid
// sending them a redundant second one. Both maps are nil from Update's own
// call, which excludes nobody. Reuses OutboxRepository.Enqueue/EnqueueEmail,
// exactly like inviteUser/inviteEmail do for a brand-new Attendee: the
// message carries no snapshot of its own (repository.OutboxMessage's
// Method-REQUEST half), since InvitationSender always rebuilds a REQUEST
// from live state — including the row's own Sequence, already bumped (or
// not) by the caller's write before this runs. A no-op when outbox is nil
// (no SMTP configured) or eventID has no other Attendees.
func (s *EventService) enqueueReinvitations(ctx context.Context, repos txRepos, eventID string, exceptUserIDs map[int64]bool, exceptEmails map[string]bool) error {
	if repos.outbox == nil {
		return nil
	}
	attendees, err := repos.attendees.ListByEventID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("list attendees for re-invitation: %w", err)
	}
	for _, a := range attendees {
		if a.UserID != nil {
			if exceptUserIDs[*a.UserID] {
				continue
			}
			if _, err := repos.outbox.Enqueue(ctx, eventID, *a.UserID); err != nil {
				return fmt.Errorf("enqueue re-invitation: %w", err)
			}
			continue
		}
		if exceptEmails[a.Email] {
			continue
		}
		if _, err := repos.outbox.EnqueueEmail(ctx, eventID, a.Email); err != nil {
			return fmt.Errorf("enqueue re-invitation: %w", err)
		}
	}
	return nil
}

// buildCancelSnapshot is enqueueCancellationToUser/enqueueCancellationToEmail's
// shared core: it captures row's current fields plus its own Organizer into
// a repository.OutboxCancelSnapshot right now, rather than a live lookup at
// send time — a CANCEL's purpose is to outlive the row (or the Attendee row)
// it withdraws, unlike a REQUEST. uid is row's iTIP UID: its own id on a
// Master or a standalone Event, its Master's id on an Override
// (icalendar.InvitationToICal's contract).
func (s *EventService) buildCancelSnapshot(ctx context.Context, repos txRepos, row repository.Event, recipientName, recipientEmail string) (repository.OutboxCancelSnapshot, error) {
	uid := row.ID
	if row.ParentID != nil {
		uid = *row.ParentID
	}

	var organizerName, organizerEmail string
	if row.CreatedBy != nil {
		organizer, err := repos.users.GetByID(ctx, *row.CreatedBy)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return repository.OutboxCancelSnapshot{}, fmt.Errorf("look up organizer: %w", err)
		}
		organizerName, organizerEmail = organizer.Name, organizer.Email
	}

	return repository.OutboxCancelSnapshot{
		UID:            uid,
		RecurrenceID:   row.RecurrenceID,
		AllDay:         row.AllDay,
		Tzid:           row.Tzid,
		Start:          row.Start,
		End:            row.End,
		Title:          row.Title,
		Sequence:       row.Sequence,
		OrganizerName:  organizerName,
		OrganizerEmail: organizerEmail,
		RecipientEmail: recipientEmail,
		RecipientName:  recipientName,
	}, nil
}

// enqueueCancellationToUser queues one CANCEL withdrawing row's Invitation
// from a User Attendee, mirroring inviteUser's own split from inviteEmail
// (ADR-0058).
func (s *EventService) enqueueCancellationToUser(ctx context.Context, repos txRepos, row repository.Event, recipientName, recipientEmail string, userID int64) error {
	snapshot, err := s.buildCancelSnapshot(ctx, repos, row, recipientName, recipientEmail)
	if err != nil {
		return err
	}
	if _, err := repos.outbox.EnqueueCancel(ctx, row.ID, userID, snapshot); err != nil {
		return fmt.Errorf("enqueue cancellation: %w", err)
	}
	return nil
}

// enqueueCancellationToEmail queues one CANCEL withdrawing row's Invitation
// from an email-only Attendee, mirroring inviteEmail's own split from
// inviteUser (ADR-0058).
func (s *EventService) enqueueCancellationToEmail(ctx context.Context, repos txRepos, row repository.Event, recipientName, recipientEmail string) error {
	snapshot, err := s.buildCancelSnapshot(ctx, repos, row, recipientName, recipientEmail)
	if err != nil {
		return err
	}
	if _, err := repos.outbox.EnqueueCancelEmail(ctx, row.ID, recipientEmail, snapshot); err != nil {
		return fmt.Errorf("enqueue cancellation: %w", err)
	}
	return nil
}

// enqueueCancellationsForRow queues a CANCEL to every one of row's own
// current Attendees — Delete's per-row fan-out over
// enqueueCancellationToUser/enqueueCancellationToEmail, one per Attendee. A
// no-op when outbox is nil.
func (s *EventService) enqueueCancellationsForRow(ctx context.Context, repos txRepos, row repository.Event) error {
	if repos.outbox == nil {
		return nil
	}
	attendees, err := repos.attendees.ListByEventID(ctx, row.ID)
	if err != nil {
		return fmt.Errorf("list attendees for cancellation: %w", err)
	}
	for _, a := range attendees {
		if a.UserID != nil {
			if err := s.enqueueCancellationToUser(ctx, repos, row, a.Name, a.Email, *a.UserID); err != nil {
				return err
			}
			continue
		}
		if err := s.enqueueCancellationToEmail(ctx, repos, row, a.Name, a.Email); err != nil {
			return err
		}
	}
	return nil
}

// inviteUser is AddAttendee's and Create's shared explicit-target check
// (#187, ADR-0055): targetUserID must exist, not be Disabled (hidden from
// the invite picker, ADR-0037 — from the inviter's perspective they don't
// exist to invite, same as CalendarService.Share), and already be a Member
// of workspaceID, before their attendees row is inserted. Takes the User,
// Workspace, Attendee, and Notification repositories directly rather than
// through EventService or txRepos, so the identical check can run against
// either the pooled repos (AddAttendee, outside any transaction) or the
// transaction-bound ones (Create, inside its own) without duplicating the
// checks themselves.
//
// Writing an invite Notification for targetUserID here, right alongside the
// attendees row, is what makes "an invite Notification is written when an
// Attendee row naming a User is written" (ADR-0061) true regardless of
// which of inviteUser's three callers (AddAttendee, addCreateAttendees,
// expandGroupMembers's sibling) is the one doing the writing. Enqueuing an
// Invitation alongside it, when outbox is non-nil, does the same for
// ADR-0059/ADR-0060: the outbox row commits in the same transaction as the
// attendees row it belongs to, so a failed send never loses the Attendee
// and a rolled-back invite queues nothing. outbox is nil when this
// deployment has no SMTP transport configured (EventService.outbox), in
// which case nothing is queued and the invite still succeeds — and nothing
// is charged against actorUserID's rate limit either (#204, ADR-0058),
// since chargeInviteRateLimit only runs alongside an actual enqueue.
func inviteUser(ctx context.Context, users *repository.UserRepository, workspaces *repository.WorkspaceRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, workspaceID, targetUserID, actorUserID int64, inviteRateLimitPerHour int) (repository.Attendee, error) {
	target, err := users.GetByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Attendee{}, ErrUserNotFound
		}
		return repository.Attendee{}, fmt.Errorf("look up user: %w", err)
	}
	if target.IsDisabled {
		return repository.Attendee{}, ErrUserNotFound
	}

	if _, err := workspaces.GetMember(ctx, workspaceID, targetUserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.Attendee{}, ErrAttendeeTargetNotInWorkspace
		}
		return repository.Attendee{}, fmt.Errorf("get workspace member: %w", err)
	}

	attendee, err := attendees.Add(ctx, event.ID, targetUserID)
	if err != nil {
		return repository.Attendee{}, err
	}
	if _, err := notifications.InsertInvite(ctx, targetUserID, event.ID, event.Title, time.Now()); err != nil {
		return repository.Attendee{}, fmt.Errorf("insert invite notification: %w", err)
	}
	if outbox != nil {
		if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
			return repository.Attendee{}, err
		}
		if _, err := outbox.EnqueueWithActor(ctx, event.ID, targetUserID, actorUserID); err != nil {
			return repository.Attendee{}, fmt.Errorf("enqueue invitation: %w", err)
		}
	}
	return attendee, nil
}

// inviteEmail is AddAttendeeByEmail's and Create's shared typed-address
// check (ADR-0058, #200): rawEmail is validated and folded the same way
// every other login-identifier-shaped input in this app is
// (service.validateEmail), then resolved against workspaceID's Members
// before anything is written. A match writes a User-backed Attendee by
// delegating straight to inviteUser — which is also what makes a Disabled
// Member's address rejected rather than downgraded to an email row: it's
// the same disabled check inviteUser already runs for an explicit user id
// target, inherited for free rather than duplicated. Anything else
// (including the same address belonging to a User in a *different*
// Workspace on this instance — ADR-0058's deliberate "treated as an
// outsider") writes an email-shaped Attendee instead, with no Notification
// (no account to notify) and, when outbox is configured, its own Invitation
// enqueued.
//
// The Event's own Organizer's address is rejected outright, before the
// Member lookup even runs — admitting it is one SetResponse away from an
// Organizer who declined their own meeting (ADR-0055). Takes the User,
// Workspace, Attendee, and Notification repositories directly, same reason
// as inviteUser: the identical check must run against both the pooled repos
// (AddAttendeeByEmail) and the transaction-bound ones (Create). actorUserID
// and inviteRateLimitPerHour are inviteUser's own rate-limit params (#204,
// ADR-0058), threaded through to the inviteUser delegation below and
// applied directly to the email-shaped write.
func inviteEmail(ctx context.Context, users *repository.UserRepository, workspaces *repository.WorkspaceRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, workspaceID int64, rawEmail string, actorUserID int64, inviteRateLimitPerHour int) (repository.AttendeeWithName, error) {
	email, err := validateEmail(rawEmail)
	if err != nil {
		return repository.AttendeeWithName{}, ErrInvalidEmail
	}

	if event.CreatedBy != nil {
		organizer, err := users.GetByID(ctx, *event.CreatedBy)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return repository.AttendeeWithName{}, fmt.Errorf("look up organizer: %w", err)
		}
		if err == nil && organizer.Email == email {
			return repository.AttendeeWithName{}, ErrAttendeeIsOrganizer
		}
	}

	target, err := users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return repository.AttendeeWithName{}, fmt.Errorf("look up user by email: %w", err)
	}
	if err == nil {
		if _, memErr := workspaces.GetMember(ctx, workspaceID, target.ID); memErr == nil {
			attendee, err := inviteUser(ctx, users, workspaces, attendees, notifications, outbox, event, workspaceID, target.ID, actorUserID, inviteRateLimitPerHour)
			if err != nil {
				return repository.AttendeeWithName{}, err
			}
			userID := attendee.UserID
			return repository.AttendeeWithName{EventID: attendee.EventID, UserID: &userID, Response: attendee.Response, CreatedAt: attendee.CreatedAt, Name: target.Name, Email: target.Email}, nil
		} else if !errors.Is(memErr, repository.ErrNotFound) {
			return repository.AttendeeWithName{}, fmt.Errorf("get workspace member: %w", memErr)
		}
		// target exists on this instance but in a different Workspace (or
		// none at all) — an outsider from this Event's Workspace's point of
		// view, so falls through to the email-shaped write below (ADR-0058).
	}

	if outbox == nil {
		return repository.AttendeeWithName{}, ErrAttendeeEmailInvitesUnavailable
	}

	added, err := attendees.AddEmail(ctx, event.ID, email)
	if err != nil {
		return repository.AttendeeWithName{}, err
	}
	if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
		return repository.AttendeeWithName{}, err
	}
	if _, err := outbox.EnqueueEmailWithActor(ctx, event.ID, email, actorUserID); err != nil {
		return repository.AttendeeWithName{}, fmt.Errorf("enqueue invitation: %w", err)
	}
	return added, nil
}

// expandGroupMembers is AddGroupAttendee's and Create's shared Group
// expansion (#162, #187, ADR-0046, ADR-0055): one attendees row per current,
// enabled member of groupID. A Disabled member is silently skipped, same as
// inviteUser refuses to invite one directly (ADR-0037) — so a Group invite
// can never produce an Attendee an individual invite would have refused. A
// member who is already an Attendee of eventID (invited individually, or via
// another Group) is left untouched rather than failing the whole expansion.
// Takes the Group, User, Attendee, and Notification repositories directly,
// same reason as inviteUser above — AddGroupAttendee always calls this with
// transaction-bound repos (reading membership and writing Attendees in the
// same transaction keeps the snapshot honest against a concurrent
// membership change); Create's own call is already inside its own
// transaction. Writes one invite Notification per Attendee row actually
// added, same as inviteUser (ADR-0061) — a member already an Attendee is
// skipped and gets no second Notification. Enqueues one Invitation per
// Attendee row actually added too, same as inviteUser, when outbox is
// non-nil (ADR-0059, ADR-0060) — each one charged against actorUserID's own
// hourly ceiling (#204, ADR-0058), same as an individual invite; a large
// Group hitting the ceiling partway through fails the whole expansion via
// the caller's transaction rollback rather than inviting some members and
// silently not others.
func expandGroupMembers(ctx context.Context, groups *repository.GroupRepository, users *repository.UserRepository, attendees *repository.AttendeeRepository, notifications *repository.NotificationRepository, outbox *repository.OutboxRepository, event repository.Event, groupID, actorUserID int64, inviteRateLimitPerHour int) ([]repository.Attendee, error) {
	members, err := groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}

	added := []repository.Attendee{}
	for _, member := range members {
		target, err := users.GetByID(ctx, member.UserID)
		if err != nil {
			return nil, fmt.Errorf("look up group member: %w", err)
		}
		if target.IsDisabled {
			continue
		}

		attendee, err := attendees.Add(ctx, event.ID, member.UserID)
		if err != nil {
			if errors.Is(err, repository.ErrAlreadyAttendee) {
				continue
			}
			return nil, err
		}
		if _, err := notifications.InsertInvite(ctx, member.UserID, event.ID, event.Title, time.Now()); err != nil {
			return nil, fmt.Errorf("insert invite notification: %w", err)
		}
		if outbox != nil {
			if err := chargeInviteRateLimit(ctx, outbox, actorUserID, inviteRateLimitPerHour); err != nil {
				return nil, err
			}
			if _, err := outbox.EnqueueWithActor(ctx, event.ID, member.UserID, actorUserID); err != nil {
				return nil, fmt.Errorf("enqueue invitation: %w", err)
			}
		}
		added = append(added, attendee)
	}
	return added, nil
}

// AddAttendee invites targetUserID as an Attendee of eventID, callable by
// any caller with Editor Access to (or ownership of) eventID's Calendar
// (#161, ADR-0046). Being an Attendee grants targetUserID visibility to
// eventID alone, with no Calendar Access of their own required — the invite
// itself is the grant. targetUserID must already be a Member of eventID's
// Calendar's own Workspace and not Disabled — inviteUser's shared check.
// Also re-sends the Invitation to every one of eventID's other current
// Attendees, unbumped: the Attendee list is itself a non-material change
// (ADR-0059) — every REQUEST renders the row's whole current list
// (ADR-0062) — and targetUserID already gets their own fresh invite, so
// they're excluded from this re-send.
func (s *EventService) AddAttendee(ctx context.Context, actorUserID int64, eventID string, targetUserID int64) (repository.Attendee, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return repository.Attendee{}, err
	}

	var attendee repository.Attendee
	err = s.withTx(ctx, func(repos txRepos) error {
		var err error
		attendee, err = inviteUser(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, calendar.WorkspaceID, targetUserID, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		return s.enqueueReinvitations(ctx, repos, eventID, map[int64]bool{targetUserID: true}, nil)
	})
	if err != nil {
		return repository.Attendee{}, err
	}
	return attendee, nil
}

// AddAttendeeByEmail invites rawEmail as an Attendee of eventID (ADR-0058,
// #200), callable by any caller with Editor Access to (or ownership of)
// eventID's Calendar, same as AddAttendee. rawEmail is resolved against
// eventID's Calendar's own Workspace before anything is written —
// inviteEmail's shared check — producing a User-backed Attendee on a match
// and an email-shaped one otherwise. Re-sends to every other current
// Attendee, same as AddAttendee (ADR-0059, #201), excluding whichever shape
// the new Attendee actually took.
func (s *EventService) AddAttendeeByEmail(ctx context.Context, actorUserID int64, eventID string, rawEmail string) (repository.AttendeeWithName, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return repository.AttendeeWithName{}, err
	}

	var attendee repository.AttendeeWithName
	err = s.withTx(ctx, func(repos txRepos) error {
		var err error
		attendee, err = inviteEmail(ctx, repos.users, repos.workspaces, repos.attendees, repos.notifications, repos.outbox, event, calendar.WorkspaceID, rawEmail, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		if attendee.UserID != nil {
			return s.enqueueReinvitations(ctx, repos, eventID, map[int64]bool{*attendee.UserID: true}, nil)
		}
		return s.enqueueReinvitations(ctx, repos, eventID, nil, map[string]bool{attendee.Email: true})
	})
	if err != nil {
		return repository.AttendeeWithName{}, err
	}
	return attendee, nil
}

// AddGroupAttendee invites every current member of groupID as an Attendee
// of eventID, callable by any caller with Editor Access to (or ownership
// of) eventID's Calendar (#162, ADR-0046). This is a one-time snapshot
// expansion, not a dynamic Group Share (ADR-0045): it inserts one attendees
// row per member of groupID as it stands right now (expandGroupMembers'
// shared expansion), and a later change to groupID's membership does not
// retroactively add or remove Attendees this call created, since attendees
// rows carry no link back to the Group they were invited through. groupID
// must belong to eventID's Calendar's own Workspace, mirroring AddAttendee's
// target check. Re-sends to every other current Attendee, same as
// AddAttendee, excluding every member actually added this call — a member
// already an Attendee (expandGroupMembers' own lenient skip) is not
// excluded, since they get no dedicated message of their own to make this
// one redundant.
func (s *EventService) AddGroupAttendee(ctx context.Context, actorUserID int64, eventID string, groupID int64) ([]repository.Attendee, error) {
	event, calendar, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return nil, err
	}

	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("look up group: %w", err)
	}
	if group.WorkspaceID != calendar.WorkspaceID {
		return nil, ErrAttendeeTargetNotInWorkspace
	}

	var added []repository.Attendee
	err = s.withTx(ctx, func(repos txRepos) error {
		// Reading membership inside the same transaction as the inserts
		// keeps the snapshot honest: a concurrent membership change can't
		// land between "read members" and "write attendees" and end up
		// reflected in neither snapshot.
		var err error
		added, err = expandGroupMembers(ctx, repos.groups, repos.users, repos.attendees, repos.notifications, repos.outbox, event, group.ID, actorUserID, s.inviteRateLimitPerHour)
		if err != nil {
			return err
		}
		if len(added) == 0 {
			return nil
		}
		exceptUserIDs := make(map[int64]bool, len(added))
		for _, a := range added {
			exceptUserIDs[a.UserID] = true
		}
		return s.enqueueReinvitations(ctx, repos, eventID, exceptUserIDs, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("add group attendees: %w", err)
	}

	return added, nil
}

// RemoveAttendee revokes targetUserID's invite to eventID, callable by any
// caller with Editor Access to (or ownership of) eventID's Calendar (#161,
// ADR-0046). Removing their row revokes their visibility to eventID; what
// happens to their historical response is left undecided by ADR-0046, and
// this simply deletes the row. Withdraws their Invitation with a
// METHOD:CANCEL, addressed to them alone, then re-sends to every remaining
// Attendee, unbumped — the Attendee list changing is itself a non-material
// change (ADR-0059, #201).
func (s *EventService) RemoveAttendee(ctx context.Context, actorUserID int64, eventID string, targetUserID int64) error {
	event, _, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return err
	}

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.attendees.Remove(ctx, eventID, targetUserID); err != nil {
			return err
		}
		if repos.outbox == nil {
			return nil
		}
		target, err := repos.users.GetByID(ctx, targetUserID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("look up removed attendee: %w", err)
		}
		if err == nil {
			if err := s.enqueueCancellationToUser(ctx, repos, event, target.Name, target.Email, targetUserID); err != nil {
				return err
			}
		}
		// targetUserID's own row is already gone from the table by now, so
		// it needs no exclusion here the way AddAttendee's re-send does.
		return s.enqueueReinvitations(ctx, repos, eventID, nil, nil)
	})
}

// RemoveAttendeeByEmail revokes email's Attendee invite to eventID
// (ADR-0058, #200) — the email-shaped counterpart to RemoveAttendee, same
// caller requirement. email is normalized the same way inviteEmail writes
// it, so a differently-cased address still matches the stored row. Withdraws
// their Invitation with a METHOD:CANCEL and re-sends to every remaining
// Attendee, same as RemoveAttendee (ADR-0059, #201).
func (s *EventService) RemoveAttendeeByEmail(ctx context.Context, actorUserID int64, eventID string, email string) error {
	event, _, err := s.attendeeManagementCalendar(ctx, actorUserID, eventID)
	if err != nil {
		return err
	}
	normalized := normalizeEmail(email)

	return s.withTx(ctx, func(repos txRepos) error {
		if err := repos.attendees.RemoveEmail(ctx, eventID, normalized); err != nil {
			return err
		}
		if repos.outbox == nil {
			return nil
		}
		if err := s.enqueueCancellationToEmail(ctx, repos, event, "", normalized); err != nil {
			return err
		}
		return s.enqueueReinvitations(ctx, repos, eventID, nil, nil)
	})
}

// ListAttendees returns every Attendee of eventID with their Name, for
// display — callable by anyone who can see eventID at all (getVisibleEvent):
// existing Calendar Access, or being an Attendee themselves (#161,
// ADR-0046).
func (s *EventService) ListAttendees(ctx context.Context, userID int64, eventID string) ([]repository.AttendeeWithName, error) {
	if _, err := s.getVisibleEvent(ctx, userID, eventID); err != nil {
		return nil, err
	}
	return s.attendees.ListByEventID(ctx, eventID)
}
