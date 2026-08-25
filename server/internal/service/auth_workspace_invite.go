package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/XiovV/calich/server/internal/repository"
)

// inviteTokenTTL is how long a Workspace Invite token is valid for
// (ADR-0044) — shorter than the 30-day refresh token (ADR-0009) because this
// token doesn't extend a Session, it creates one: anyone holding a live link
// can become a Member, or a brand-new User, outright.
const inviteTokenTTL = 7 * 24 * time.Hour

// liveWorkspaceInvite resolves token to the WorkspaceInvite it names, the
// check every Workspace-invite operation shares: the token must match an
// outstanding invite and it must not have expired (ADR-0044).
func (s *AuthService) liveWorkspaceInvite(ctx context.Context, token string) (repository.WorkspaceInvite, error) {
	invite, err := s.workspaceInvites.GetByTokenHash(ctx, hashToken(token))
	if errors.Is(err, repository.ErrNotFound) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	if err != nil {
		return repository.WorkspaceInvite{}, fmt.Errorf("get workspace invite by token: %w", err)
	}

	if time.Now().After(invite.InviteExpiresAt) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}

	return invite, nil
}

// WorkspaceInvitePreview is what the accept-workspace-invite page shows
// before the invitee does anything (ADR-0044): which Workspace they're
// joining, the email the invite names, and whether that email already
// belongs to an account — deciding whether the page should collect a name
// and password (new account) or just ask the invitee to log in (existing
// account).
type WorkspaceInvitePreview struct {
	WorkspaceName string
	Email         string
	UserExists    bool
}

// PreviewWorkspaceInvite resolves a live Workspace Invite token without
// consuming it (ADR-0044), mirroring PreviewInvite for the account-level
// Invite this replaces.
func (s *AuthService) PreviewWorkspaceInvite(ctx context.Context, token string) (WorkspaceInvitePreview, error) {
	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return WorkspaceInvitePreview{}, err
	}

	workspace, err := s.workspaces.GetByID(ctx, invite.WorkspaceID)
	if err != nil {
		return WorkspaceInvitePreview{}, fmt.Errorf("get workspace: %w", err)
	}

	_, err = s.users.GetByEmail(ctx, invite.Email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return WorkspaceInvitePreview{}, fmt.Errorf("check existing user: %w", err)
	}

	return WorkspaceInvitePreview{
		WorkspaceName: workspace.Name,
		Email:         invite.Email,
		UserExists:    err == nil,
	}, nil
}

// liveWorkspaceInviteTx re-resolves invite.ID inside a transaction and
// confirms it's still the same live invite token names — the re-check both
// accept paths need after opening their transaction, since liveWorkspaceInvite
// only proved the invite was live at the time it was first read, not at the
// moment the transaction actually commits.
func liveWorkspaceInviteTx(ctx context.Context, txInvites *repository.WorkspaceInviteRepository, invite repository.WorkspaceInvite, token string) (repository.WorkspaceInvite, error) {
	liveInvite, err := txInvites.GetByID(ctx, invite.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	if err != nil {
		return repository.WorkspaceInvite{}, fmt.Errorf("get workspace invite: %w", err)
	}
	if liveInvite.InviteTokenHash != hashToken(token) || time.Now().After(liveInvite.InviteExpiresAt) {
		return repository.WorkspaceInvite{}, ErrWorkspaceInviteInvalid
	}
	return liveInvite, nil
}

// AcceptWorkspaceInviteNewAccount accepts a live Workspace Invite whose email
// belongs to nobody yet (ADR-0044): it creates a User (name, the invite's
// email, password), adds them as a Member of the inviting Workspace, and
// consumes the invite — all in one transaction — then logs them straight in,
// matching AcceptInvite's shape for the account-level Invite this replaces.
// Fails with ErrWorkspaceInviteInvalid if, by the time the transaction runs,
// a User has already claimed the invite's email — the same kind of race
// Register's first-account count check closes.
func (s *AuthService) AcceptWorkspaceInviteNewAccount(ctx context.Context, token, name, password string) (LoginResult, error) {
	name, err := validateName(name)
	if err != nil {
		return LoginResult{}, err
	}
	if err := validatePassword(password); err != nil {
		return LoginResult{}, err
	}

	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return LoginResult{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	var newUser repository.User
	var inviteWorkspaceID int64
	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txUsers := s.users.WithTx(tx)
		txInvites := s.workspaceInvites.WithTx(tx)

		liveInvite, err := liveWorkspaceInviteTx(ctx, txInvites, invite, token)
		if err != nil {
			return err
		}
		inviteWorkspaceID = liveInvite.WorkspaceID

		if _, err := txUsers.GetByEmail(ctx, liveInvite.Email); err == nil {
			return ErrWorkspaceInviteInvalid
		} else if !errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("check existing user: %w", err)
		}

		newUser, err = txUsers.Create(ctx, name, liveInvite.Email, string(hash), false)
		if err != nil {
			if errors.Is(err, repository.ErrEmailTaken) {
				return ErrEmailTaken
			}
			return fmt.Errorf("create user: %w", err)
		}

		if err := s.workspaces.AddMemberInTx(ctx, tx, liveInvite.WorkspaceID, newUser.ID, repository.WorkspaceRoleMember); err != nil {
			return fmt.Errorf("add workspace member: %w", err)
		}

		// Sweeps any outstanding email-shaped Attendee rows for this address
		// onto newUser (ADR-0058, #203) — the fallback we wrote because we
		// couldn't find their account has no reason to outlive the account
		// appearing. Bound to this same tx so the conversion and the
		// Membership write succeed or fail together.
		if err := s.attendees.WithTx(tx).ConvertEmailAttendeesToUser(ctx, liveInvite.WorkspaceID, liveInvite.Email, newUser.ID); err != nil {
			return fmt.Errorf("convert email attendees: %w", err)
		}

		if err := txInvites.Delete(ctx, liveInvite.ID); err != nil {
			return fmt.Errorf("consume workspace invite: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	// Seeds the three default Calendars into the Workspace the Invite
	// admitted newUser to (#172), private to them under ADR-0045 until they
	// explicitly Share. Run only after the transaction above has committed,
	// for the same reason Register does: EnsureDefaults resolves Workspace
	// membership through the plain (non-tx) repos. A failure here is
	// compensated by deleting newUser outright (cascading their membership
	// row and any partially-seeded Calendars) rather than the Workspace
	// itself, which newUser doesn't own — same "don't leave a half-done
	// account" reasoning as Register, without Register's extra Workspace to
	// clean up.
	if err := s.calendars.EnsureDefaults(ctx, newUser.ID, inviteWorkspaceID); err != nil {
		if cleanupErr := s.users.Delete(ctx, newUser.ID); cleanupErr != nil {
			return LoginResult{}, fmt.Errorf("seed default calendars: %w (cleanup also failed: %v)", err, cleanupErr)
		}
		return LoginResult{}, fmt.Errorf("seed default calendars: %w", err)
	}

	tokens, err := s.issueSession(ctx, newUser.ID, newUser.TokenVersion)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{sessionTokens: tokens, MustChangePassword: false}, nil
}

// AcceptWorkspaceInviteExisting accepts a live Workspace Invite on behalf of
// the already-authenticated userID (ADR-0044): it adds a WorkspaceMember row
// for the inviting Workspace and consumes the invite — no new User row, no
// password step, and every other Workspace userID already belongs to is left
// untouched. Refused with ErrWorkspaceInviteEmailMismatch unless userID's own
// account email matches the invite's exactly, so accepting an invite meant
// for someone else's address isn't possible just by being logged in as
// anybody, and with ErrAlreadyWorkspaceMember if userID already belongs to
// the Workspace.
func (s *AuthService) AcceptWorkspaceInviteExisting(ctx context.Context, userID int64, token string) (repository.Workspace, error) {
	invite, err := s.liveWorkspaceInvite(ctx, token)
	if err != nil {
		return repository.Workspace{}, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return repository.Workspace{}, fmt.Errorf("get user: %w", err)
	}
	if user.Email != invite.Email {
		return repository.Workspace{}, ErrWorkspaceInviteEmailMismatch
	}

	err = s.workspaces.WithTx(ctx, func(tx *sql.Tx) error {
		txInvites := s.workspaceInvites.WithTx(tx)

		liveInvite, err := liveWorkspaceInviteTx(ctx, txInvites, invite, token)
		if err != nil {
			return err
		}

		if err := s.workspaces.AddMemberInTx(ctx, tx, liveInvite.WorkspaceID, userID, repository.WorkspaceRoleMember); err != nil {
			if errors.Is(err, repository.ErrAlreadyMember) {
				return ErrAlreadyWorkspaceMember
			}
			return fmt.Errorf("add workspace member: %w", err)
		}

		// Sweeps any outstanding email-shaped Attendee rows for this address
		// onto userID (ADR-0058, #203) — the fallback we wrote because we
		// couldn't find their account has no reason to outlive the account
		// appearing. Bound to this same tx so the conversion and the
		// Membership write succeed or fail together.
		if err := s.attendees.WithTx(tx).ConvertEmailAttendeesToUser(ctx, liveInvite.WorkspaceID, liveInvite.Email, userID); err != nil {
			return fmt.Errorf("convert email attendees: %w", err)
		}

		if err := txInvites.Delete(ctx, liveInvite.ID); err != nil {
			return fmt.Errorf("consume workspace invite: %w", err)
		}

		return nil
	})
	if err != nil {
		return repository.Workspace{}, err
	}

	return s.workspaces.GetByID(ctx, invite.WorkspaceID)
}
