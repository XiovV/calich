package service

import (
	"context"

	"github.com/XiovV/calendar/server/internal/repository"
)

// UserService is the User directory (#113): who else has an account, open
// to any authenticated caller so they can pick a Share recipient. Unlike
// AccountService, it carries no administrative authority and exposes
// nothing beyond identity — no admin/disabled/must-change-password status.
type UserService struct {
	users *repository.UserRepository
}

func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// Directory returns every enabled User except callerID — the pool a caller
// may share a Calendar with.
func (s *UserService) Directory(ctx context.Context, callerID int64) ([]repository.User, error) {
	return s.users.ListEnabledExcluding(ctx, callerID)
}
