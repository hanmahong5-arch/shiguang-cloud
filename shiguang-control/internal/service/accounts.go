// Package service contains business logic for account management,
// launcher configuration, and admin operations.
//
// Accounts are split into two implementations keyed by the user's chosen
// server line:
//   - AccountService58: writes to AionCore's `account_data` in aion_world_live
//     using the NCSoft password hash algorithm.
//   - AccountService48: writes to Beyond 4.8's `account_data` in al_server_ls
//     using SHA-1 + Base64.
//
// All query paths use pgx parameterized placeholders ($1, $2…) — never
// string.Format — to categorically eliminate SQL injection. This is the
// single most important difference from the legacy AionNetGate code.
package service

import "errors"

// AccountService is the common interface both implementations satisfy.
// The handlers pick one based on the incoming request's "server" field.
type AccountService interface {
	// Register creates a new account. Returns ErrAccountExists if name is taken.
	Register(ctx Context, name, password, email string) error

	// Login verifies credentials; returns ErrBadCredentials on mismatch.
	Login(ctx Context, name, password string) error

	// ChangePassword updates the password atomically. Requires the current
	// password for verification.
	ChangePassword(ctx Context, name, oldPassword, newPassword string) error

	// ResetPassword sets a new random password when the caller's email matches
	// the stored value. Returns the new plain-text password for delivery.
	ResetPassword(ctx Context, name, email string) (newPassword string, err error)
}

// Context is an alias to keep the service interface from pulling in
// context.Context directly — concrete implementations use context.Context.
type Context = ctxAlias

// Sentinel errors. Handlers translate these to HTTP status codes.
var (
	ErrAccountExists  = errors.New("account already exists")
	ErrBadCredentials = errors.New("invalid username or password")
	ErrAccountNotFound = errors.New("account not found")
	ErrEmailMismatch  = errors.New("email does not match account record")
)
