package config

import "os"

type AuthConfig struct {
	// PasswordResetBaseURL is the back-office URL the reset link points to.
	// The flow appends "?token=<token>" to it. Empty disables the email send —
	// the request still succeeds silently, which is the intended behaviour of
	// POST /auth/forgot-password (see docs/PASSWORD_RESET.md).
	PasswordResetBaseURL string
}

func loadAuthConfig() AuthConfig {
	return AuthConfig{
		PasswordResetBaseURL: os.Getenv("PASSWORD_RESET_BASE_URL"),
	}
}
