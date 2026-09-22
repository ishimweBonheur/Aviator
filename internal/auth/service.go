package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db        *pgxpool.Pool
	jwtSecret string
}

type User struct {
	ID       int64
	Username string
	Email    string
}

func NewService(db *pgxpool.Pool, jwtSecret string) *Service {
	return &Service{
		db:        db,
		jwtSecret: jwtSecret,
	}
}

func (s *Service) Register(
	ctx context.Context,
	username string,
	email string,
	password string,
) (*User, error) {

	if username == "" {
		return nil, errors.New("username is required")
	}

	if email == "" {
		return nil, errors.New("email is required")
	}

	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var user User

	err = s.db.QueryRow(
		ctx,
		`
		INSERT INTO users (
			username,
			email,
			password_hash
		)
		VALUES ($1, $2, $3)
		RETURNING id, username, email
		`,
		username,
		email,
		string(passwordHash),
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

func (s *Service) Login(
	ctx context.Context,
	email string,
	password string,
) (string, *User, error) {

	var (
		user         User
		passwordHash string
		status       string
	)

	err := s.db.QueryRow(
		ctx,
		`
		SELECT
			id,
			username,
			email,
			password_hash,
			status
		FROM users
		WHERE email = $1
		`,
		email,
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&passwordHash,
		&status,
	)

	if err != nil {
		return "", nil, errors.New("invalid email or password")
	}

	if status != "ACTIVE" {
		return "", nil, errors.New("user account is not active")
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	); err != nil {
		return "", nil, errors.New("invalid email or password")
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{
			"user_id":  user.ID,
			"username": user.Username,
			"exp":      time.Now().Add(24 * time.Hour).Unix(),
			"iat":      time.Now().Unix(),
		},
	)

	tokenString, err := token.SignedString(
		[]byte(s.jwtSecret),
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	return tokenString, &user, nil
}
