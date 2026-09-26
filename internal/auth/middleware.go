package auth

import (
	"aviator/backend/internal/httpapi"
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const userIDKey contextKey = "user_id"

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		authHeader := r.Header.Get("Authorization")

		if authHeader == "" {
			httpapi.Error(
				w,
				"authorization required",
				http.StatusUnauthorized,
			)
			return
		}

		// Accept:
		// Authorization: Bearer <token>
		// and, for Swagger compatibility:
		// Authorization: <token>
		tokenString := authHeader

		parts := strings.SplitN(authHeader, " ", 2)

		if len(parts) == 2 {
			if !strings.EqualFold(parts[0], "Bearer") {
				httpapi.Error(
					w,
					"invalid authorization header",
					http.StatusUnauthorized,
				)
				return
			}

			tokenString = parts[1]
		}

		if tokenString == "" {
			httpapi.Error(
				w,
				"invalid authorization token",
				http.StatusUnauthorized,
			)
			return
		}

		token, err := jwt.Parse(
			tokenString,
			func(token *jwt.Token) (interface{}, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, errors.New("unexpected signing method")
				}

				return []byte(s.jwtSecret), nil
			},
		)

		if err != nil || !token.Valid {
			httpapi.Error(
				w,
				"invalid or expired token",
				http.StatusUnauthorized,
			)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)

		if !ok {
			httpapi.Error(
				w,
				"invalid token claims",
				http.StatusUnauthorized,
			)
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)

		if !ok {
			httpapi.Error(
				w,
				"invalid user id",
				http.StatusUnauthorized,
			)
			return
		}

		userID := int64(userIDFloat)
		var status string
		if err := s.db.QueryRow(r.Context(), "SELECT status FROM users WHERE id=$1", userID).Scan(&status); err != nil {
			httpapi.Error(w, "account unavailable", http.StatusUnauthorized)
			return
		}
		if status != "ACTIVE" {
			httpapi.Error(w, "account is not active", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(
			r.Context(),
			userIDKey,
			userID,
		)

		next.ServeHTTP(
			w,
			r.WithContext(ctx),
		)
	})
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey).(int64)

	return userID, ok
}

// RequireAdmin checks the current database role on every request, so revocation
// takes effect for existing sessions as well as new logins.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := UserIDFromContext(r.Context())
		if !ok {
			httpapi.Error(w, "authorization required", 401)
			return
		}
		var role, status string
		if err := s.db.QueryRow(r.Context(), "SELECT role,status FROM users WHERE id=$1", id).Scan(&role, &status); err != nil {
			httpapi.Error(w, "account unavailable", 503)
			return
		}
		if role != "ADMIN" || status != "ACTIVE" {
			httpapi.Error(w, "administrator access required", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
