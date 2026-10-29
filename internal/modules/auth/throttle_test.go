package auth

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// expectFailedPasswordLogin : identifiants inconnus ou faux (ErrUserNotFound) —
// requête de connexion sans ligne, puis vérification « compte sans lien actif ».
func expectFailedPasswordLogin(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(".").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(".").WillReturnError(sql.ErrNoRows)
}

// withFailedLoginDelay réduit l'attente pour le test et la rétablit ensuite.
func withFailedLoginDelay(t *testing.T, d time.Duration) {
	t.Helper()
	previous := failedLoginDelay
	failedLoginDelay = d
	t.Cleanup(func() { failedLoginDelay = previous })
}

func TestLogin_FailedPasswordLoginIsDelayedNeverLocked(t *testing.T) {
	withFailedLoginDelay(t, 50*time.Millisecond)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	svc := &AuthService{repo: NewAuthRepository(db), redis: newMemRedis()}

	// Bien au-delà de l'ancien seuil : chaque essai part en base (aucun
	// verrouillage) et chaque échec attend le délai.
	for i := 0; i < 15; i++ {
		expectFailedPasswordLogin(mock)
		start := time.Now()
		_, err := svc.Login(context.Background(), LoginRequestPayload{Email: "chef@resto.fr", Password: "faux"}, "", true)
		if !errors.Is(err, models.ErrUserNotFound) {
			t.Fatalf("essai %d : err = %v, want ErrUserNotFound", i+1, err)
		}
		if elapsed := time.Since(start); elapsed < failedLoginDelay {
			t.Fatalf("essai %d : réponse en %v, attendu au moins %v", i+1, elapsed, failedLoginDelay)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_TokenLoginNotDelayed(t *testing.T) {
	withFailedLoginDelay(t, time.Hour) // un délai appliqué ferait échouer le test par timeout
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	svc := &AuthService{repo: NewAuthRepository(db), redis: newMemRedis()}

	mock.ExpectQuery(".").WillReturnError(sql.ErrNoRows)
	if _, err := svc.Login(context.Background(), LoginRequestPayload{}, "tok", false); !errors.Is(err, models.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestWaitAfterFailedLogin_ReleasedWhenClientLeaves(t *testing.T) {
	withFailedLoginDelay(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { waitAfterFailedLogin(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("une requête abandonnée doit libérer l'attente")
	}
}

func TestLoginFrom_FailedAttemptsCappedPerIP(t *testing.T) {
	withFailedLoginDelay(t, 0)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	svc := &AuthService{repo: NewAuthRepository(db), redis: newMemRedis(), failedLogins: newFailedLoginGuard()}
	ctx := context.Background()
	payload := LoginRequestPayload{Email: "chef@resto.fr", Password: "faux"}

	for i := 0; i < failedLoginsPerIP; i++ {
		expectFailedPasswordLogin(mock)
		if _, err := svc.LoginFrom(ctx, "203.0.113.7", payload, "", true); !errors.Is(err, models.ErrUserNotFound) {
			t.Fatalf("essai %d : err = %v, want ErrUserNotFound", i+1, err)
		}
	}
	// Seuil atteint : refus immédiat, sans requête en base.
	if _, err := svc.LoginFrom(ctx, "203.0.113.7", payload, "", true); !errors.Is(err, models.ErrRateLimited) {
		t.Fatalf("après le seuil : err = %v, want ErrRateLimited", err)
	}
	// Le compte n'est pas bloqué : une autre adresse passe.
	expectFailedPasswordLogin(mock)
	if _, err := svc.LoginFrom(ctx, "198.51.100.2", payload, "", true); !errors.Is(err, models.ErrUserNotFound) {
		t.Fatalf("autre adresse : err = %v, want ErrUserNotFound", err)
	}
	// Les connexions par jeton ne sont jamais freinées.
	mock.ExpectQuery(".").WillReturnError(sql.ErrNoRows)
	if _, err := svc.LoginFrom(ctx, "203.0.113.7", LoginRequestPayload{}, "tok", false); !errors.Is(err, models.ErrUserNotFound) {
		t.Fatalf("jeton : err = %v, want ErrUserNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedLoginGuard_SuccessNeverCounts(t *testing.T) {
	g := newFailedLoginGuard()
	now := time.Now()
	for i := 0; i < failedLoginsPerIP*3; i++ {
		if !g.reserve("203.0.113.7", now) {
			t.Fatalf("essai réussi %d refusé", i+1)
		}
		g.release("203.0.113.7", now, now)
	}
}

func TestFailedLoginGuard_ResetsEachWindow(t *testing.T) {
	g := newFailedLoginGuard()
	start := time.Unix(0, 0).Add(failedLoginWindow) // début de fenêtre
	for i := 0; i < failedLoginsPerIP; i++ {
		g.reserve("203.0.113.7", start)
	}
	if g.reserve("203.0.113.7", start.Add(failedLoginWindow-time.Second)) {
		t.Fatal("seuil atteint : refus attendu jusqu'à la fin de la fenêtre")
	}
	if !g.reserve("203.0.113.7", start.Add(failedLoginWindow)) {
		t.Fatal("nouvelle fenêtre : l'adresse doit repasser")
	}
}

func TestVerifyMFA_DiscardsCodeAfterMaxAttempts(t *testing.T) {
	svc, mock, cache, _ := buildMFAService(t)
	ctx := context.Background()
	token := "tok-2"
	key := helpers.GetMFACacheKey(token)
	cache.Set(ctx, key, "123456", models.OTPCacheTTL)

	for i := 1; i <= models.OTPMaxAttempts; i++ {
		mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
			WithArgs(token).
			WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(pinMinRow("2", token, "212")...))
		err := svc.VerifyMFA(ctx, token, "000000")
		want := models.ErrOTPMismatch
		if i == models.OTPMaxAttempts {
			want = models.ErrMFAExpired
		}
		if !errors.Is(err, want) {
			t.Fatalf("essai %d : err = %v, want %v", i, err, want)
		}
	}
	if _, ok := cache.Get(ctx, key); ok {
		t.Fatal("l'OTP doit être retiré après le dernier essai autorisé")
	}

	// Le bon code ne passe plus : il faut en redemander un.
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(pinMinRow("2", token, "212")...))
	if err := svc.VerifyMFA(ctx, token, "123456"); !errors.Is(err, models.ErrMFAExpired) {
		t.Fatalf("bon code après épuisement : err = %v, want ErrMFAExpired", err)
	}
}

func TestConfirmVerification_DiscardsCodeAfterMaxAttempts(t *testing.T) {
	svc, _, cache, _ := buildMFAService(t)
	ctx := context.Background()
	key := helpers.GetVerificationCacheKey("EMAIL", "tok-2")
	cache.Set(ctx, key, "123456", models.OTPCacheTTL)

	for i := 1; i <= models.OTPMaxAttempts; i++ {
		err := svc.ConfirmVerification(ctx, "tok-2", "EMAIL", "000000")
		want := models.ErrOTPMismatch
		if i == models.OTPMaxAttempts {
			want = models.ErrMFAExpired
		}
		if !errors.Is(err, want) {
			t.Fatalf("essai %d : err = %v, want %v", i, err, want)
		}
	}
	if _, ok := cache.Get(ctx, key); ok {
		t.Fatal("le code doit être retiré après le dernier essai autorisé")
	}
	if err := svc.ConfirmVerification(ctx, "tok-2", "EMAIL", "123456"); !errors.Is(err, models.ErrMFAExpired) {
		t.Fatalf("bon code après épuisement : err = %v, want ErrMFAExpired", err)
	}
}

func TestSendVerificationCode_ResendCooldown(t *testing.T) {
	svc, mock, _, mail := buildMFAService(t)
	ctx := context.Background()
	token := "tok-2"

	for i := 0; i < 2; i++ {
		mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
			WithArgs(token).
			WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(pinMinRow("2", token, "212")...))
	}

	if _, err := svc.SendVerificationCode(ctx, token, "EMAIL"); err != nil {
		t.Fatalf("premier envoi : %v", err)
	}
	if _, err := svc.SendVerificationCode(ctx, token, "EMAIL"); !errors.Is(err, models.ErrOTPWaitTime) {
		t.Fatalf("renvoi immédiat : err = %v, want ErrOTPWaitTime", err)
	}
	if mail.count() != 1 {
		t.Fatalf("%d mails envoyés, want 1", mail.count())
	}
}
