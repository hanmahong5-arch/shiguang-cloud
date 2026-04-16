package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/shiguang/shared/aiondb"
	"github.com/shiguang/shared/crypto"
)

// AccountService58 talks to AionCore's PostgreSQL `aion_world_live.account_data`
// table using the NCSoft password hash algorithm. The column layout matches
// the one used by AionNetGate's MSSQL "new account database" code path — the
// hash stored is the uppercase hex digest WITHOUT the "0x" prefix.
type AccountService58 struct {
	db *aiondb.Pool
}

// NewAccountService58 wires the service with a pool.
func NewAccountService58(db *aiondb.Pool) *AccountService58 {
	return &AccountService58{db: db}
}

var nameRegex58 = regexp.MustCompile(`^[A-Za-z0-9_]{4,16}$`)

// Register — INSERT INTO account_data(name, password, email)
func (s *AccountService58) Register(ctx context.Context, name, password, email string) error {
	if !nameRegex58.MatchString(name) {
		return errors.New("name must be 4-16 chars, alphanumeric + underscore")
	}
	if len(password) < 4 || len(password) > 16 {
		return errors.New("password must be 4-16 characters")
	}
	// Check existence first (cleaner error than catching unique violation)
	var existing string
	err := s.db.QueryRow(ctx,
		`SELECT name FROM account_data WHERE name = $1`, name).Scan(&existing)
	if err == nil {
		return ErrAccountExists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existence: %w", err)
	}

	hash := stripPrefix(crypto.NCSoftHash(password))
	_, err = s.db.Exec(ctx,
		`INSERT INTO account_data (name, password, email) VALUES ($1, $2, $3)`,
		name, hash, email)
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

// Login — SELECT password FROM account_data WHERE name = $1
func (s *AccountService58) Login(ctx context.Context, name, password string) error {
	var stored string
	err := s.db.QueryRow(ctx,
		`SELECT password FROM account_data WHERE name = $1`, name).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadCredentials
	}
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	if stored != stripPrefix(crypto.NCSoftHash(password)) {
		return ErrBadCredentials
	}
	return nil
}

// ChangePassword — UPDATE ... WHERE name=$1 AND password=$2
func (s *AccountService58) ChangePassword(ctx context.Context, name, oldPassword, newPassword string) error {
	if len(newPassword) < 4 || len(newPassword) > 16 {
		return errors.New("password must be 4-16 characters")
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE account_data SET password = $3 WHERE name = $1 AND password = $2`,
		name, stripPrefix(crypto.NCSoftHash(oldPassword)), stripPrefix(crypto.NCSoftHash(newPassword)))
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBadCredentials
	}
	return nil
}

// ResetPassword — UPDATE ... WHERE name=$1 AND email=$2, returns new random pw
func (s *AccountService58) ResetPassword(ctx context.Context, name, email string) (string, error) {
	newPw, err := randomPassword(8)
	if err != nil {
		return "", err
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE account_data SET password = $3 WHERE name = $1 AND email = $2`,
		name, email, stripPrefix(crypto.NCSoftHash(newPw)))
	if err != nil {
		return "", fmt.Errorf("update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrEmailMismatch
	}
	return newPw, nil
}

// stripPrefix removes the "0x" prefix from NCSoftHash output so the DB
// stores just the 32 hex chars (matches AionNetGate's "newaccountdatabase" path).
func stripPrefix(hex string) string {
	if len(hex) >= 2 && hex[:2] == "0x" {
		return hex[2:]
	}
	return hex
}

// randomPassword returns a cryptographically random alphanumeric password.
func randomPassword(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"
	buf := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[idx.Int64()]
	}
	return string(buf), nil
}
