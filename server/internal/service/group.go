package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/XiovV/calendar/server/internal/repository"
)

// GroupService creates and manages Groups — named sets of a Workspace's
// Members, callable only by that Workspace's Owner or Admin (ADR-0045).
type GroupService struct {
	groups     *repository.GroupRepository
	workspaces *repository.WorkspaceRepository
}

func NewGroupService(groups *repository.GroupRepository, workspaces *repository.WorkspaceRepository) *GroupService {
	return &GroupService{groups: groups, workspaces: workspaces}
}

var (
	// ErrInvalidGroupName is returned by Create and Rename when name is empty.
	ErrInvalidGroupName = errors.New("group name must not be empty")

	// ErrGroupMemberNotInWorkspace is returned by AddMember when targetUserID
	// isn't a Member of the Group's own Workspace — a Group can only contain
	// Members of its own Workspace (ADR-0045).
	ErrGroupMemberNotInWorkspace = errors.New("user is not a member of this workspace")
)

// Create makes a new Group named name inside workspaceID, callable only by
// its Owner or Admin (ADR-0045).
func (s *GroupService) Create(ctx context.Context, actorUserID, workspaceID int64, name string) (repository.Group, error) {
	if err := requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, workspaceID); err != nil {
		return repository.Group{}, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Group{}, ErrInvalidGroupName
	}

	group, err := s.groups.Create(ctx, workspaceID, name)
	if err != nil {
		return repository.Group{}, fmt.Errorf("create group: %w", err)
	}
	return group, nil
}

// ListByWorkspace returns every Group belonging to workspaceID, callable by
// any Member of it.
func (s *GroupService) ListByWorkspace(ctx context.Context, actorUserID, workspaceID int64) ([]repository.Group, error) {
	if _, err := s.workspaces.GetMember(ctx, workspaceID, actorUserID); err != nil {
		return nil, err
	}

	groups, err := s.groups.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return groups, nil
}

// Rename changes groupID's name, callable only by its Workspace's Owner or
// Admin.
func (s *GroupService) Rename(ctx context.Context, actorUserID, groupID int64, name string) (repository.Group, error) {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return repository.Group{}, err
	}
	if err := requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, group.WorkspaceID); err != nil {
		return repository.Group{}, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return repository.Group{}, ErrInvalidGroupName
	}

	renamed, err := s.groups.Rename(ctx, groupID, name)
	if err != nil {
		return repository.Group{}, fmt.Errorf("rename group: %w", err)
	}
	return renamed, nil
}

// Delete removes groupID, callable only by its Workspace's Owner or Admin.
func (s *GroupService) Delete(ctx context.Context, actorUserID, groupID int64) error {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, group.WorkspaceID); err != nil {
		return err
	}

	if err := s.groups.Delete(ctx, groupID); err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	return nil
}

// AddMember adds targetUserID to groupID, callable only by the Group's
// Workspace's Owner or Admin. targetUserID must already be a Member of that
// same Workspace — a Group can only contain Members of its own Workspace
// (ADR-0045).
func (s *GroupService) AddMember(ctx context.Context, actorUserID, groupID, targetUserID int64) error {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, group.WorkspaceID); err != nil {
		return err
	}

	if _, err := s.workspaces.GetMember(ctx, group.WorkspaceID, targetUserID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrGroupMemberNotInWorkspace
		}
		return err
	}

	if err := s.groups.AddMember(ctx, groupID, targetUserID); err != nil {
		return err
	}
	return nil
}

// RemoveMember removes targetUserID from groupID, callable only by the
// Group's Workspace's Owner or Admin.
func (s *GroupService) RemoveMember(ctx context.Context, actorUserID, groupID, targetUserID int64) error {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return err
	}
	if err := requireWorkspaceOwnerOrAdmin(ctx, s.workspaces, actorUserID, group.WorkspaceID); err != nil {
		return err
	}

	if err := s.groups.RemoveMember(ctx, groupID, targetUserID); err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return nil
}

// ListMembers returns every GroupMember of groupID, callable by any Member
// of the Group's Workspace.
func (s *GroupService) ListMembers(ctx context.Context, actorUserID, groupID int64) ([]repository.GroupMember, error) {
	group, err := s.groups.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if _, err := s.workspaces.GetMember(ctx, group.WorkspaceID, actorUserID); err != nil {
		return nil, err
	}

	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	return members, nil
}
