package deposit

import (
	"aviator/backend/internal/auth"
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
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

// CreateDeposit godoc
// @Summary Create a deposit
// @Description Creates a deposit request for the authenticated user.
// @Tags deposits
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body CreateRequest true "Deposit details"
// @Success 201 {object} Deposit
// @Failure 400 {string} string "Invalid request or deposit"
// @Failure 401 {string} string "Unauthorized"
// @Router /api/deposits [post]
func (h *Handler) Create(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := auth.UserIDFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
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
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	deposit, err := h.service.Create(
		r.Context(),
		userID,
		req,
	)
	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(
		deposit,
	)
}

// ListDeposits godoc
// @Summary List deposits
// @Description Returns deposits for the authenticated user.
// @Tags deposits
// @Security BearerAuth
// @Produce json
// @Success 200 {array} Deposit
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Failed to load deposits"
// @Router /api/deposits [get]
func (h *Handler) List(
	w http.ResponseWriter,
	r *http.Request,
) {
	userID, ok := auth.UserIDFromContext(
		r.Context(),
	)
	if !ok {
		http.Error(
			w,
			"unauthorized",
			http.StatusUnauthorized,
		)
		return
	}

	deposits, err := h.service.List(
		r.Context(),
		userID,
	)
	if err != nil {
		http.Error(
			w,
			"failed to load deposits",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	_ = json.NewEncoder(w).Encode(
		deposits,
	)
}
