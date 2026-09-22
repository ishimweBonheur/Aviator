package auth

import (
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
			http.Error(
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
				http.Error(
					w,
					"invalid authorization header",
					http.StatusUnauthorized,
				)
				return
			}

			tokenString = parts[1]
		}

		if tokenString == "" {
			http.Error(
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
			http.Error(
				w,
				"invalid or expired token",
				http.StatusUnauthorized,
			)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)

		if !ok {
			http.Error(
				w,
				"invalid token claims",
				http.StatusUnauthorized,
			)
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)

		if !ok {
			http.Error(
				w,
				"invalid user id",
				http.StatusUnauthorized,
			)
			return
		}

		userID := int64(userIDFloat)

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
