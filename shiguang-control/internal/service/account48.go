package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shiguang/shared/aiondb"
	"github.com/shiguang/shared/crypto"
)

// AccountService48 talks to Beyond 4.8's `al_server_ls.account_data` table.
// Password format is SHA-1 + Base64 (matching Beyond's AccountController).
type AccountService48 struct {
	db *aiondb.Pool
}

func NewAccountService48(db *aiondb.Pool) *AccountService48 {
	return &AccountService48{db: db}
}

func (s *AccountService48) Register(ctx context.Context, name, password, email string) error {
	if !nameRegex58.MatchString(name) { // reuse same constraint
		return errors.New("name must be 4-16 chars, alphanumeric + underscore")
	}
	if len(password) < 4 {
		return errors.New("password must be at least 4 characters")
	}
	var existing string
	err := s.db.QueryRow(ctx,
		`SELECT name FROM account_data WHERE name = $1`, name).Scan(&existing)
	if err == nil {
		return ErrAccountExists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existence: %w", err)
	}

	hash := crypto.SHA1Base64(password)
	_, err = s.db.Exec(ctx,
		`INSERT INTO account_data (name, password, email) VALUES ($1, $2, $3)`,
		name, hash, email)
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

func (s *AccountService48) Login(ctx context.Context, name, password string) error {
	var stored string
	err := s.db.QueryRow(ctx,
		`SELECT password FROM account_data WHERE name = $1`, name).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadCredentials
	}
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	if stored != crypto.SHA1Base64(password) {
		return ErrBadCredentials
	}
	return nil
}

func (s *AccountService48) ChangePassword(ctx context.Context, name, oldPassword, newPassword string) error {
	if len(newPassword) < 4 {
		return errors.New("password must be at least 4 characters")
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE account_data SET password = $3 WHERE name = $1 AND password = $2`,
		name, crypto.SHA1Base64(oldPassword), crypto.SHA1Base64(newPassword))
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBadCredentials
	}
	return nil
}

func (s *AccountService48) ResetPassword(ctx context.Context, name, email string) (string, error) {
	newPw, err := randomPassword(8)
	if err != nil {
		return "", err
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE account_data SET password = $3 WHERE name = $1 AND email = $2`,
		name, email, crypto.SHA1Base64(newPw))
	if err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrEmailMismatch
	}
	return newPw, nil
}
