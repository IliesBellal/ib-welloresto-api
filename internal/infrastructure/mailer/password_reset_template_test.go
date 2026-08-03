package mailer

import (
	"strings"
	"testing"
)

// TestRenderPasswordResetTemplate guards the "mot de passe oublié" email: a
// typo in a field name renders silently as an empty string, producing a mail
// with a dead button. See docs/PASSWORD_RESET.md.
func TestRenderPasswordResetTemplate(t *testing.T) {
	const resetURL = "https://backoffice.example.com/reset-password?token=abc123"

	html, err := RenderTemplate("password_reset.html", PasswordResetMailData{
		EmailBaseData: EmailBaseData{
			BrandName:    "Wello Resto",
			BrandLogoURL: BrandLogoURL,
			SupportEmail: SupportEmail,
			Year:         2026,
		},
		FirstName: "Camille",
		ResetURL:  resetURL,
		ExpiresIn: 30,
	})
	if err != nil {
		t.Fatalf("RenderTemplate() error = %v", err)
	}

	for _, want := range []string{
		resetURL,      // the link itself
		"Camille",     // greeting
		"30 minutes",  // expiry
		"Wello Resto", // branding
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered email is missing %q", want)
		}
	}

	// A field the template does not know about renders as "<no value>";
	// catching it here beats discovering it in a user's inbox.
	if strings.Contains(html, "<no value>") {
		t.Error("rendered email contains \"<no value>\" — a template field name is wrong")
	}
}
