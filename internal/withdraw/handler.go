package withdrawal

import (
	"aviator/backend/internal/auth"
	"aviator/backend/internal/httpapi"
	"encoding/json"
	"net/http"
)

type Handler struct {
	service *Service
}

func NewHandler(
	service *Service,
) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) Handle(
	w http.ResponseWriter,
	r *http.Request,
) {
	switch r.Method {
	case http.MethodPost:
		h.Create(w, r)

	case http.MethodGet:
		h.List(w, r)

	default:
		httpapi.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

// CreateWithdrawal godoc
// @Summary Create a withdrawal
// @Description Creates a withdrawal request for the authenticated user.
// @Tags withdrawals
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body CreateRequest true "Withdrawal details"
// @Success 201 {object} CreateResult
// @Failure 400 {string} string "Invalid request or withdrawal"
// @Failure 401 {string} string "Unauthorized"
// @Failure 409 {string} string "Insufficient balance"
// @Router /api/withdrawals [post]
func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := auth.UserIDFromContext(
		r.Context(),
	)
	if !ok {
		httpapi.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	var req CreateRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		httpapi.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	result, err := h.service.Create(
		r.Context(),
		userID,
		req,
	)
	if err != nil {
		httpapi.ServiceError(w, err)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(
		result,
	)
}

// ListWithdrawals godoc
// @Summary List withdrawals
// @Description Returns withdrawals for the authenticated user.
// @Tags withdrawals
// @Security BearerAuth
// @Produce json
// @Success 200 {array} Withdrawal
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Failed to load withdrawals"
// @Router /api/withdrawals [get]
func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := auth.UserIDFromContext(
		r.Context(),
	)
	if !ok {
		httpapi.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	withdrawals, err := h.service.List(
		r.Context(),
		userID,
	)
	if err != nil {
		httpapi.Error(
			w,
			"failed to load withdrawals",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	_ = json.NewEncoder(w).Encode(
		withdrawals,
	)
}
