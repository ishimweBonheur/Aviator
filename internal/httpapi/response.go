package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"log"
	"net/http"
	"strings"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func Error(w http.ResponseWriter, message string, status int) {
	JSON(w, status, map[string]string{"error": message})
}

// ServiceError exposes validation conflicts but never SQL/driver diagnostics.
func ServiceError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "23505" {
			Error(w, "this operation already exists", 409)
			return
		}
		log.Printf("database request failed: %v", err)
		Error(w, "request could not be completed", 500)
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "failed to") || strings.Contains(msg, "transaction") || strings.Contains(msg, "connection") || strings.Contains(msg, "SQL") || strings.Contains(msg, "context") {
		log.Printf("request failed: %v", err)
		Error(w, "request could not be completed", 500)
		return
	}
	status := http.StatusConflict
	if strings.Contains(msg, "not found") {
		status = 404
	}
	if strings.Contains(msg, "invalid") || strings.Contains(msg, "must") || strings.Contains(msg, "unsupported") {
		status = 400
	}
	Error(w, msg, status)
}
