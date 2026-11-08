package users

import (
	"context"
	"strings"
	"time"

	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/logger"
)

const (
	merchantLinkedTemplate = "merchant_user_linked.html"
	merchantLinkedSender   = "Wello Resto - Security"
)

// linkMailer est la part de mailer.Service utilisée pour prévenir un utilisateur
// qu'il vient d'être rattaché à un établissement.
type linkMailer interface {
	SendAsync(fromName, fromEmail, to, subject, templateName string, data interface{})
}

// merchantNameResolver donne le nom d'un établissement (merchantbrand.Repository).
type merchantNameResolver interface {
	Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand
}

// SetLinkNotifier branche l'envoi du mail « votre compte a été rattaché à… »
// (late-bound comme SetOnboardingService). Sans lui — tests —, le rattachement
// se fait sans mail.
func (s *UsersService) SetLinkNotifier(m linkMailer, names merchantNameResolver) {
	s.linkMailer = m
	s.merchantNames = names
}

// notifyMerchantLinked prévient par mail le titulaire userID. Best-effort : le
// rattachement est déjà fait, un compte sans e-mail ou une lecture en échec ne
// doit pas le faire échouer.
func (s *UsersService) notifyMerchantLinked(ctx context.Context, merchantID, userID string) {
	if s.linkMailer == nil {
		return
	}
	target, err := s.userRepo.GetLinkTargetByID(ctx, userID)
	if err != nil {
		logger.FromContext(ctx).Warn("link notification: compte illisible ou sans e-mail, mail non envoyé (user " + userID + ")")
		return
	}
	s.notifyMerchantLinkedTo(ctx, merchantID, *target)
}

func (s *UsersService) notifyMerchantLinkedTo(ctx context.Context, merchantID string, target linkTarget) {
	if s.linkMailer == nil || strings.TrimSpace(target.Email) == "" {
		return
	}
	merchantName := ""
	if s.merchantNames != nil {
		merchantName = s.merchantNames.Resolve(ctx, merchantID, "").Name
	}
	if merchantName == "" {
		merchantName = "un établissement"
	}
	s.linkMailer.SendAsync(merchantLinkedSender, mailer.SecurityEmail, target.Email,
		"Votre compte Wello Resto a été rattaché à "+merchantName, merchantLinkedTemplate,
		map[string]interface{}{
			"BrandName":    "Wello Resto",
			"Year":         time.Now().Year(),
			"FirstName":    target.FirstName,
			"MerchantName": merchantName,
		})
}
