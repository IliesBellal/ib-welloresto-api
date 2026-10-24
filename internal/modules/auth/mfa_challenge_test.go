package auth

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// otpMailer ne surcharge que SendOTP ; le reste de mailer.Service reste nil.
type otpMailer struct {
	mailer.Service
	mu   sync.Mutex
	sent []mailer.MfaOTPData
}

func (m *otpMailer) SendOTP(data mailer.MfaOTPData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, data)
}

func (m *otpMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// lockedRedis sérialise l'accès au memRedis (map non thread-safe) : SendMFACode
// envoie le mail dans une goroutine et la rafale appelle le service en parallèle.
type lockedRedis struct {
	mu    sync.Mutex
	inner *memRedis
}

func (l *lockedRedis) Get(ctx context.Context, key string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Get(ctx, key)
}

func (l *lockedRedis) Set(ctx context.Context, key, value string, ttl time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Set(ctx, key, value, ttl)
}

func (l *lockedRedis) Delete(ctx context.Context, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Delete(ctx, key)
}

func buildMFAService(t *testing.T) (*AuthService, sqlmock.Sqlmock, *lockedRedis, *otpMailer) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cache := &lockedRedis{inner: newMemRedis()}
	mail := &otpMailer{}
	svc := &AuthService{repo: NewAuthRepository(db), redis: cache, email: mail}
	return svc, mock, cache, mail
}

func mfaTestUser() *UserLoginRow {
	mfaType := "email_sms"
	return &UserLoginRow{UserID: "2", Token: "tok-2", Email: "ilies@example.com", FirstName: "Ilies", LastName: "BELLAL", MFAType: &mfaType}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition non remplie dans le délai")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func expectStatusUpdate(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET mfa_status`)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectOTPSentUpdate(mock sqlmock.Sqlmock) {
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET mfa_otp_sent_at`)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestStartMFAChallenge_SendsEmailAndStoresOTP(t *testing.T) {
	svc, mock, cache, mail := buildMFAService(t)
	user := mfaTestUser()
	expectStatusUpdate(mock)
	expectOTPSentUpdate(mock)

	svc.StartMFAChallenge(context.Background(), user)

	// SendMFACode envoie le mail dans une goroutine.
	waitFor(t, func() bool { return mail.count() == 1 })
	if _, ok := cache.Get(context.Background(), helpers.GetMFACacheKey(user.Token)); !ok {
		t.Fatal("OTP absent de Redis : VerifyMFA ne pourra pas le valider")
	}
	if got := mail.sent[0].UserEmail; got != user.Email {
		t.Errorf("mail envoyé à %q, attendu %q", got, user.Email)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// Un refresh de page = plusieurs requêtes qui reçoivent mfa_required l'une après
// l'autre ; chaque envoi écraserait le code du précédent.
func TestStartMFAChallenge_ExistingLiveOTP_NoResend(t *testing.T) {
	svc, mock, cache, mail := buildMFAService(t)
	user := mfaTestUser()
	cache.Set(context.Background(), helpers.GetMFACacheKey(user.Token), "123456", models.OTPCacheTTL)
	expectStatusUpdate(mock)
	expectStatusUpdate(mock)

	svc.StartMFAChallenge(context.Background(), user)
	svc.StartMFAChallenge(context.Background(), user)

	if n := mail.count(); n != 0 {
		t.Fatalf("%d mail(s) envoyé(s) alors qu'un code valide existait déjà", n)
	}
	if got, _ := cache.Get(context.Background(), helpers.GetMFACacheKey(user.Token)); got != "123456" {
		t.Errorf("le code en cours a été écrasé : %q", got)
	}
}

func TestStartMFAChallenge_Burst_SendsOnce(t *testing.T) {
	const requests = 8
	svc, mock, _, mail := buildMFAService(t)
	user := mfaTestUser()
	mock.MatchExpectationsInOrder(false)
	for i := 0; i < requests; i++ {
		expectStatusUpdate(mock)
	}
	expectOTPSentUpdate(mock)

	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.StartMFAChallenge(context.Background(), user)
		}()
	}
	wg.Wait()

	waitFor(t, func() bool { return mail.count() >= 1 })
	// Laisse une éventuelle goroutine d'envoi en trop se manifester.
	time.Sleep(50 * time.Millisecond)
	if n := mail.count(); n != 1 {
		t.Fatalf("%d mails pour une rafale de requêtes, attendu 1", n)
	}
}

func TestStartMFAChallenge_NoRedis_NoOp(t *testing.T) {
	svc, _, _, mail := buildMFAService(t)
	svc.redis = nil

	svc.StartMFAChallenge(context.Background(), mfaTestUser())

	if mail.count() != 0 {
		t.Fatal("aucun envoi attendu sans Redis")
	}
}

// Le bouton « Renvoyer le code » de la popup MFA appelle send-verification avec
// mode "mfa" : le code doit être stocké sous la clé lue par VerifyMFA et le mail partir.
func TestSendVerificationCode_MFAMode_UsesMFACircuit(t *testing.T) {
	svc, mock, cache, mail := buildMFAService(t)
	token := "tok-2"

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(pinMinRow("2", token, "212")...))
	expectOTPSentUpdate(mock)

	recipient, err := svc.SendVerificationCode(context.Background(), token, "mfa")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recipient != helpers.MaskEmail("email@ex.com") {
		t.Errorf("recipient = %q, attendu l'email masqué", recipient)
	}

	waitFor(t, func() bool { return mail.count() == 1 })
	if _, ok := cache.Get(context.Background(), helpers.GetMFACacheKey(token)); !ok {
		t.Fatal("OTP absent de la clé MFA : VerifyMFA le rejetterait comme expiré")
	}
	if _, ok := cache.Get(context.Background(), helpers.GetVerificationCacheKey("mfa", token)); ok {
		t.Error("le code ne doit pas être écrit sous la clé de vérification email/tel")
	}
}
