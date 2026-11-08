package users

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type sentMail struct {
	fromName, fromEmail, to, subject, template string
	data                                       map[string]interface{}
}

type linkMailerSpy struct{ sent []sentMail }

func (m *linkMailerSpy) SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{}) {
	d, _ := data.(map[string]interface{})
	m.sent = append(m.sent, sentMail{fromName, fromEmail, to, subject, templateName, d})
}

type fixedMerchantName string

func (n fixedMerchantName) Resolve(_ context.Context, _, _ string) mailer.MerchantBrand {
	return mailer.NewMerchantBrand(string(n), "", "", "")
}

var findLinkTargetSQL = regexp.QuoteMeta(`WHERE LOWER(email) = LOWER(?) AND enabled = TRUE`)

func newLinkByEmailService(t *testing.T) (*UsersService, sqlmock.Sqlmock, *linkMailerSpy, context.Context) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc := NewUsersService(NewUserRepository(db), nil, nil, nil, nil)
	spy := &linkMailerSpy{}
	svc.SetLinkNotifier(spy, fixedMerchantName("Le Petit Zinc"))
	ctx := middleware.WithUser(context.Background(), &auth.UserLoginRow{UserID: "admin_1", MerchantID: "merchant_1"})
	return svc, mock, spy, ctx
}

// Un e-mail sans compte se termine comme un succès : rien n'est écrit, rien n'est
// envoyé, et la réponse ne trahit pas l'absence de compte.
func TestLinkMerchantUserByEmailUnknownAccountIsSilentSuccess(t *testing.T) {
	svc, mock, spy, ctx := newLinkByEmailService(t)

	mock.ExpectQuery(findLinkTargetSQL).WithArgs("nobody@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "email"}))

	if err := svc.LinkMerchantUserByEmail(ctx, MerchantUserLinkByEmailRequest{Email: "nobody@example.com"}); err != nil {
		t.Fatalf("LinkMerchantUserByEmail() error = %v, want nil", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("mail envoyé pour un compte inexistant : %+v", spy.sent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// Même résultat pour un compte déjà rattaché : pas de 409 qui révélerait le compte.
func TestLinkMerchantUserByEmailAlreadyLinkedIsSilentSuccess(t *testing.T) {
	svc, mock, spy, ctx := newLinkByEmailService(t)

	// Espaces rognés ; la casse est comparée par LOWER() côté SQL.
	mock.ExpectQuery(findLinkTargetSQL).WithArgs("Sarah@Example.com").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "email"}).AddRow("user_7", "Sarah", "sarah@example.com"))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM users_rights`)).WithArgs("merchant_1", "user_7").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	if err := svc.LinkMerchantUserByEmail(ctx, MerchantUserLinkByEmailRequest{Email: " Sarah@Example.com "}); err != nil {
		t.Fatalf("LinkMerchantUserByEmail() error = %v, want nil", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("mail envoyé pour un compte déjà rattaché : %+v", spy.sent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// Deux comptes pour une même adresse : on ne devine pas lequel rattacher.
func TestLinkMerchantUserByEmailAmbiguousIsSilentSuccess(t *testing.T) {
	svc, mock, spy, ctx := newLinkByEmailService(t)

	mock.ExpectQuery(findLinkTargetSQL).WithArgs("dup@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "email"}).
			AddRow("user_1", "A", "dup@example.com").AddRow("user_2", "B", "dup@example.com"))

	if err := svc.LinkMerchantUserByEmail(ctx, MerchantUserLinkByEmailRequest{Email: "dup@example.com"}); err != nil {
		t.Fatalf("LinkMerchantUserByEmail() error = %v, want nil", err)
	}
	if len(spy.sent) != 0 {
		t.Fatalf("mail envoyé malgré l'ambiguïté : %+v", spy.sent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestLinkMerchantUserByEmailRejectsInvalidAddress(t *testing.T) {
	svc, mock, _, ctx := newLinkByEmailService(t)

	for _, email := range []string{"", "   ", "pas-un-mail"} {
		err := svc.LinkMerchantUserByEmail(ctx, MerchantUserLinkByEmailRequest{Email: email})
		if !errors.Is(err, models.ErrInvalidInput) {
			t.Errorf("email %q : error = %v, want %v", email, err, models.ErrInvalidInput)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("aucune requête SQL n'était attendue : %v", err)
	}
}

func TestNotifyMerchantLinkedSendsSecurityMailToAccountHolder(t *testing.T) {
	svc, _, spy, ctx := newLinkByEmailService(t)

	svc.notifyMerchantLinkedTo(ctx, "merchant_1", linkTarget{UserID: "user_7", FirstName: "Sarah", Email: "sarah@example.com"})

	if len(spy.sent) != 1 {
		t.Fatalf("mails envoyés = %d, want 1", len(spy.sent))
	}
	got := spy.sent[0]
	if got.to != "sarah@example.com" || got.template != "merchant_user_linked.html" || got.fromEmail != mailer.SecurityEmail {
		t.Errorf("mail inattendu : %+v", got)
	}
	if !strings.Contains(got.subject, "Le Petit Zinc") {
		t.Errorf("l'objet ne nomme pas l'établissement : %q", got.subject)
	}
	if got.data["MerchantName"] != "Le Petit Zinc" || got.data["FirstName"] != "Sarah" {
		t.Errorf("données du mail inattendues : %+v", got.data)
	}
	// Le rendu réel du template doit aboutir avec ces données.
	if _, err := mailer.RenderTemplate(got.template, got.data); err != nil {
		t.Errorf("le template ne se rend pas avec les données envoyées : %v", err)
	}
}

func TestNotifyMerchantLinkedSkipsAccountWithoutEmail(t *testing.T) {
	svc, _, spy, ctx := newLinkByEmailService(t)
	svc.notifyMerchantLinkedTo(ctx, "merchant_1", linkTarget{UserID: "user_7", Email: "  "})
	if len(spy.sent) != 0 {
		t.Fatalf("mail envoyé sans adresse : %+v", spy.sent)
	}
}

// La route répond 202 avec le même message, quoi qu'il arrive au compte.
func TestLinkMerchantUserByEmailHandlerAnswersGenericMessage(t *testing.T) {
	svc, mock, _, ctx := newLinkByEmailService(t)
	mock.ExpectQuery(findLinkTargetSQL).WithArgs("nobody@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "first_name", "email"}))

	h := &UsersHandler{svc: svc}
	req := httptest.NewRequest(http.MethodPost, "/users/link-by-email", strings.NewReader(`{"email":"nobody@example.com"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.LinkMerchantUserByEmail(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Si un compte existe") {
		t.Errorf("message générique absent : %s", rec.Body.String())
	}
}
