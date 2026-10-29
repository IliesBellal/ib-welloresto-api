// Package merchantbrand lit l'identité d'un établissement (nom, logo,
// couleur de marque) pour habiller les mails envoyés en son nom à ses clients
// et à son équipe (cadre « Maison », docs/emails-styles.md).
package merchantbrand

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/infrastructure/mailer"
	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(database *sql.DB) *Repository {
	return &Repository{database: database}
}

// Resolve renvoie l'identité de l'établissement merchantID. name, s'il est
// renseigné, remplace le nom lu en base (c'est aussi le nom d'expéditeur du
// mail). Ne renvoie jamais d'erreur : un établissement introuvable ou une
// lecture en échec donne le cadre par défaut (couleurs de merchant_parameters
// par défaut, sans logo), pour ne jamais bloquer l'envoi d'un mail.
func (r *Repository) Resolve(ctx context.Context, merchantID, name string) mailer.MerchantBrand {
	fallback := mailer.NewMerchantBrand(name, "", "", "")
	if r == nil || r.database == nil || strings.TrimSpace(merchantID) == "" {
		return fallback
	}
	var dbName, logoURL, color, textColor string
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, `
		SELECT COALESCE(m.fullName, ''), COALESCE(m.logo_url, ''),
		       COALESCE(mp.primary_color, ''), COALESCE(mp.text_color_on_primary_color, '')
		FROM merchant m
		LEFT JOIN merchant_parameters mp ON mp.merchant_id = m.id::text
		WHERE m.id::text = ?`, merchantID).Scan(&dbName, &logoURL, &color, &textColor)
	if err != nil {
		if err != sql.ErrNoRows {
			logger.FromContext(ctx).Warn("merchantbrand: lecture de l'identité impossible, cadre par défaut",
				zap.String("merchant_id", merchantID), zap.Error(err))
		}
		return fallback
	}
	if strings.TrimSpace(name) == "" {
		name = dbName
	}
	return mailer.NewMerchantBrand(name, logoURL, color, textColor)
}
