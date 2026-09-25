package auth

import (
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"net/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

type RegisterRequest struct {
	Username string `json:"username" example:"bonheur" validate:"required"`
	Email    string `json:"email" example:"bonheur@example.com" validate:"required,email"`
	Password string `json:"password" example:"password123" validate:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" example:"bonheur@example.com" validate:"required,email"`
	Password string `json:"password" example:"password123" validate:"required"`
}

type RegisterResponse struct {
	User *User `json:"user"`
}

type LoginResponse struct {
	Token string `json:"token" example:"JWT_TOKEN"`
	User  *User  `json:"user"`
}

type ErrorResponse struct {
	Error string `json:"error" example:"invalid request body"`
}

// Register creates a new user account.
// @Summary Register a user
// @Description Creates a user account. Username and email are required, and the password must be at least 8 characters.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} RegisterResponse
// @Failure 400 {object} ErrorResponse
// @Router /api/auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	user, err := h.service.Register(
		r.Context(),
		req.Username,
		req.Email,
		req.Password,
	)

	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"user": user,
	})
}

// Login authenticates a user and returns a JWT.
// @Summary Log in
// @Description Authenticates a user with an email and password and returns a JWT.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpapi.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	token, user, err := h.service.Login(
		r.Context(),
		req.Email,
		req.Password,
	)

	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user":  user,
	})
}
