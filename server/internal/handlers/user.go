package handlers

import (
	"net/http"

	"github.com/XiovV/calendar/server/internal/httpauth"
	"github.com/XiovV/calendar/server/internal/httpresponse"
	"github.com/XiovV/calendar/server/internal/repository"
	"github.com/XiovV/calendar/server/internal/service"
)

// UserHandler serves the User directory (#113): any authenticated caller,
// not just an Admin, may see who else has an account, to pick a Share
// recipient.
type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

type userDirectoryResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func toUserDirectoryResponse(u repository.User) userDirectoryResponse {
	return userDirectoryResponse{ID: u.ID, Name: u.Name, Email: u.Email}
}

// Directory serves GET /api/users: every enabled User besides the caller.
func (h *UserHandler) Directory(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpauth.UserIDFromContext(r.Context())
	if !ok {
		httpresponse.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	users, err := h.users.Directory(r.Context(), userID)
	if err != nil {
		httpresponse.Error(w, http.StatusInternalServerError, "internal_error", "failed to list users")
		return
	}

	response := make([]userDirectoryResponse, len(users))
	for i, u := range users {
		response[i] = toUserDirectoryResponse(u)
	}

	httpresponse.JSON(w, http.StatusOK, response)
}
