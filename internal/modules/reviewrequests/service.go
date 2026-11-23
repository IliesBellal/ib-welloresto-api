package reviewrequests

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/models"
)

// Mailer : envoi synchrone de la demande d'avis (brevo_mailer.SendReviewRequest).
type Mailer interface {
	SendReviewRequest(to string, data mailer.ReviewRequestEmailData) (string, error)
}

// BrandResolver : identité de l'établissement dans ses mails (merchantbrand).
type BrandResolver interface {
	Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand
}

type store interface {
	ListCandidates(ctx context.Context, from, to time.Time, limit int) ([]candidate, error)
	IsOptedOut(ctx context.Context, merchantID, emailHash string) (bool, error)
	RequestedSince(ctx context.Context, merchantID, emailHash string, since time.Time) (bool, error)
	Insert(ctx context.Context, row requestRow, now time.Time) (bool, error)
	Finish(ctx context.Context, orderID, status, messageID string, sentAt *time.Time) error
	GetByToken(ctx context.Context, token string) (*tokenInfo, error)
	SetOptOut(ctx context.Context, merchantID, emailHash string, optedOut bool, now time.Time) error
	GetReviewSettings(ctx context.Context, merchantID string) (name, reviewURL string, err error)
}

type Service struct {
	repo    store
	mail    Mailer // nil : envoi impossible
	brands  BrandResolver
	baseURL string // adresse publique de ScanNOrder (page de désinscription)
	log     *zap.Logger
	now     func() time.Time
}

func NewService(repo *Repository, mail Mailer, brands BrandResolver, scannorderBaseURL string, log *zap.Logger) *Service {
	return &Service{
		repo: repo, mail: mail, brands: brands,
		baseURL: strings.TrimRight(scannorderBaseURL, "/"),
		log:     log, now: time.Now,
	}
}

// tokenPattern : jeton du lien de désinscription (32 octets en base64url).
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,64}$`)

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// normalizeEmail : adresse en minuscules sans espaces, "" si invalide.
func normalizeEmail(raw string) string {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || strings.ContainsAny(e, " \t\r\n<>") {
		return ""
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || !strings.Contains(e[strings.LastIndex(e, "@")+1:], ".") {
		return ""
	}
	return e
}

// emailHash : seule trace de l'adresse conservée (review_requests, désinscriptions).
func emailHash(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func (s *Service) unsubscribeURL(token string) string {
	return s.baseURL + "/desinscription/avis/" + url.PathEscape(token)
}

// quietHours : vrai hors de la plage d'envoi, à l'heure de l'établissement.
func quietHours(now time.Time, timezone string) bool {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc, _ = time.LoadLocation("Europe/Paris")
	}
	if loc == nil {
		loc = time.UTC
	}
	h := now.In(loc).Hour()
	return h < QuietHoursEnd || h >= QuietHoursStart
}

// Run envoie les demandes dues. Une demande en échec n'est pas retentée (le
// client ne doit jamais en recevoir deux) ; une commande hors plage horaire
// attend le passage suivant.
func (s *Service) Run(ctx context.Context) (RunResult, error) {
	var res RunResult
	if s.mail == nil || s.baseURL == "" {
		return res, ErrNotConfigured
	}
	now := s.now()
	candidates, err := s.repo.ListCandidates(ctx, now.Add(-ReviewWindow), now.Add(-ReviewDelay), batchSize)
	if err != nil {
		return res, err
	}
	for _, c := range candidates {
		if quietHours(now, c.Timezone) {
			continue
		}
		status, err := s.handle(ctx, c, now)
		switch {
		case err != nil:
			res.Failed++
			s.log.Warn("demande d'avis non envoyée", zap.String("order_id", c.OrderID), zap.String("merchant_id", c.MerchantID), zap.Error(err))
		case status == StatusSent:
			res.Sent++
		case status != "":
			res.Skipped++
		}
	}
	return res, nil
}

// handle traite une commande et renvoie le statut écrit ("" si une autre
// instance l'a prise).
func (s *Service) handle(ctx context.Context, c candidate, now time.Time) (string, error) {
	row := requestRow{MerchantID: c.MerchantID, OrderID: c.OrderID}
	email := normalizeEmail(c.Email)
	if email == "" {
		row.Status, row.EmailHash = StatusSkippedInvalid, emailHash(strings.ToLower(strings.TrimSpace(c.Email)))
		return s.skip(ctx, row, now)
	}
	row.EmailHash = emailHash(email)

	optedOut, err := s.repo.IsOptedOut(ctx, c.MerchantID, row.EmailHash)
	if err != nil {
		return "", err
	}
	if optedOut {
		row.Status = StatusSkippedOptOut
		return s.skip(ctx, row, now)
	}
	recent, err := s.repo.RequestedSince(ctx, c.MerchantID, row.EmailHash, now.Add(-FrequencyCap))
	if err != nil {
		return "", err
	}
	if recent {
		row.Status = StatusSkippedRecent
		return s.skip(ctx, row, now)
	}

	if row.Token, err = newToken(); err != nil {
		return "", err
	}
	row.Status = StatusPending
	// La ligne précède l'envoi : une ligne restée « pending » (arrêt brutal)
	// n'est jamais renvoyée.
	inserted, err := s.repo.Insert(ctx, row, now)
	if err != nil || !inserted {
		return "", err
	}

	data := mailer.ReviewRequestEmailData{
		Brand:          s.brand(ctx, c.MerchantID, c.MerchantName),
		MerchantName:   c.MerchantName,
		CustomerName:   c.FirstName,
		ReviewURL:      c.ReviewURL,
		UnsubscribeURL: s.unsubscribeURL(row.Token),
	}
	messageID, sendErr := s.mail.SendReviewRequest(email, data)
	if sendErr != nil {
		if err := s.repo.Finish(ctx, c.OrderID, StatusFailed, "", nil); err != nil {
			s.log.Error("demande d'avis : statut failed non écrit", zap.String("order_id", c.OrderID), zap.Error(err))
		}
		return "", fmt.Errorf("envoi: %w", sendErr)
	}
	sentAt := s.now()
	if err := s.repo.Finish(ctx, c.OrderID, StatusSent, messageID, &sentAt); err != nil {
		// Mail parti, ligne restée pending : jamais renvoyé, compté comme envoyé.
		s.log.Error("demande d'avis envoyée mais statut non écrit", zap.String("order_id", c.OrderID), zap.Error(err))
	}
	return StatusSent, nil
}

func (s *Service) skip(ctx context.Context, row requestRow, now time.Time) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	row.Token = token
	inserted, err := s.repo.Insert(ctx, row, now)
	if err != nil || !inserted {
		return "", err
	}
	return row.Status, nil
}

func (s *Service) brand(ctx context.Context, merchantID, name string) mailer.MerchantBrand {
	if s.brands != nil {
		return s.brands.Resolve(ctx, merchantID, name)
	}
	return mailer.NewMerchantBrand(name, "", "", "")
}

// SendTest envoie à l'utilisateur connecté la demande telle que ses clients la
// recevront, marquée « test ». Rien n'est enregistré.
func (s *Service) SendTest(ctx context.Context, merchantID, to string) error {
	if s.mail == nil || s.baseURL == "" {
		return ErrNotConfigured
	}
	to = normalizeEmail(to)
	if to == "" {
		return ErrMissingRecipient
	}
	name, reviewURL, err := s.repo.GetReviewSettings(ctx, merchantID)
	if err != nil {
		return err
	}
	if reviewURL == "" {
		return ErrMissingReviewURL
	}
	_, err = s.mail.SendReviewRequest(to, mailer.ReviewRequestEmailData{
		Brand:          s.brand(ctx, merchantID, name),
		MerchantName:   name,
		ReviewURL:      reviewURL,
		UnsubscribeURL: s.unsubscribeURL("exemple"),
		TestNotice:     "E-mail de test : vos clients reçoivent le même, avec leur prénom, environ 1 h 30 après la clôture de leur commande ScanNOrder.",
	})
	return err
}

func (s *Service) byToken(ctx context.Context, token string) (*tokenInfo, error) {
	if !tokenPattern.MatchString(token) {
		return nil, models.ErrNotFound
	}
	return s.repo.GetByToken(ctx, token)
}

// Info : établissement et état de la désinscription, pour la page publique.
func (s *Service) Info(ctx context.Context, token string) (*PublicInfo, error) {
	info, err := s.byToken(ctx, token)
	if err != nil {
		return nil, err
	}
	brand := s.brand(ctx, info.MerchantID, info.MerchantName)
	return &PublicInfo{
		MerchantName: info.MerchantName,
		LogoURL:      brand.LogoURL,
		Color:        brand.Color,
		TextColor:    brand.TextColor,
		OptedOut:     info.OptedOut,
	}, nil
}

// SetOptOut désinscrit (ou réinscrit, pour annuler une erreur) le client des
// demandes d'avis de l'établissement du jeton.
func (s *Service) SetOptOut(ctx context.Context, token string, optedOut bool) (*PublicInfo, error) {
	info, err := s.byToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetOptOut(ctx, info.MerchantID, info.EmailHash, optedOut, s.now()); err != nil {
		return nil, err
	}
	return s.Info(ctx, token)
}

// IsConfigError : erreur de configuration (journalisée une fois par passage).
func IsConfigError(err error) bool {
	return errors.Is(err, ErrNotConfigured)
}
