package kiosk

// Opérations du ruban marketing (migration 190) : lecture par la borne,
// gestion de la rotation par le back-office. Séparé de service.go, qui porte le
// parcours device et la configuration générale.

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/notification"
	"welloresto-api/internal/utils/dbutils"
)

// NewMarketingMediaID produit l'identifiant d'un média avant son upload.
//
// Il nomme l'objet R2 (r2.GenerateKioskMarketingMediaKey) : le fichier doit donc
// être nommé avant que la ligne n'existe en base. L'AdminHandler le génère,
// uploade, puis le passe à CreateMarketingMedia.
func NewMarketingMediaID() string {
	return helpers.GeneratePrefixedID(helpers.KioskMarketingMediaIDPrefix)
}

// marketingDefaultDuration lit la durée par défaut d'une ligne de réglages, avec
// un repli sur la valeur de la colonne : une ligne antérieure à la migration, ou
// jamais enregistrée, ne doit jamais produire une durée nulle côté borne.
func marketingDefaultDuration(row *KioskSettingsRow) int {
	if row == nil || !validMarketingDuration(row.MarketingDefaultDurationSeconds) {
		return defaultMarketingDurationSeconds
	}
	return row.MarketingDefaultDurationSeconds
}

// notifyMarketingChanged prévient les bornes (et les POS) du merchant qu'un
// réglage ou un média du ruban a changé : elles rechargent GET /kiosk/settings
// et le ruban se met à jour sans redémarrage de l'app.
//
// Volontairement limité aux changements du ruban : diffuser à chaque
// UpdateSettings ferait recharger à une borne des réglages de parcours
// (modes de service, paiement...) en pleine commande.
func (s *Service) notifyMarketingChanged(merchantID string) {
	if s.notificationSvc == nil {
		return
	}
	s.notificationSvc.BroadcastToMerchant(merchantID, map[string]interface{}{
		"type":         notification.WSEventKioskSettingsUpdated,
		"triggered_by": "back_office",
	})
}

// marketingMediaForKiosk construit la rotation envoyée à la borne.
//
// Ne renvoie jamais d'erreur : GET /kiosk/settings porte tout le parcours de
// commande, et un ruban décoratif ne doit pas pouvoir le faire échouer. En cas
// d'erreur de lecture, la borne reçoit une rotation vide (le ruban se replie) et
// l'incident est journalisé.
func (s *Service) marketingMediaForKiosk(ctx context.Context, merchantID string, row *KioskSettingsRow) []MarketingMediaResponse {
	items, err := s.repo.ListMarketingMedia(ctx, merchantID)
	if err != nil {
		logger.FromContext(ctx).Warn("kiosk: list marketing media failed, ribbon left empty",
			zap.String("merchant_id", merchantID), zap.Error(err))
		return []MarketingMediaResponse{}
	}
	return toMarketingMediaResponses(items, marketingDefaultDuration(row))
}

// ListMarketingMedia — rotation complète pour le back-office.
func (s *Service) ListMarketingMedia(ctx context.Context, merchantID string) ([]MarketingMediaResponse, error) {
	row, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListMarketingMedia(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	return toMarketingMediaResponses(items, marketingDefaultDuration(row)), nil
}

// CheckMarketingCapacity refuse tôt quand la rotation est pleine, pour que
// l'AdminHandler n'uploade pas un fichier de 50 Mo qui sera de toute façon
// refusé. CreateMarketingMedia revérifie : ce contrôle-ci n'est qu'une économie.
func (s *Service) CheckMarketingCapacity(ctx context.Context, merchantID string) error {
	count, err := s.repo.CountMarketingMedia(ctx, merchantID)
	if err != nil {
		return err
	}
	if count >= maxMarketingMedia {
		return models.ErrKioskMediaLimitReached
	}
	return nil
}

// CreateMarketingMedia ajoute un média à la rotation. mediaID est pré-généré par
// l'appelant (NewMarketingMediaID) car il nomme déjà le fichier uploadé.
func (s *Service) CreateMarketingMedia(ctx context.Context, merchantID, mediaID, kind, url string, durationSeconds *int) (*MarketingMediaResponse, error) {
	if kind != marketingKindImage && kind != marketingKindVideo {
		return nil, models.ErrKioskMediaInvalid
	}
	if mediaID == "" || strings.TrimSpace(url) == "" {
		return nil, models.ErrKioskMediaInvalid
	}

	// Une durée absente n'est PAS remplacée par une valeur : le média reste sans
	// durée propre et suit celle du merchant. Figer 10 s ici le déconnecterait
	// du réglage global dès sa création.
	customDuration, err := normalizeMarketingDuration(kind, durationSeconds)
	if err != nil {
		return nil, err
	}

	if err := s.CheckMarketingCapacity(ctx, merchantID); err != nil {
		return nil, err
	}

	sortOrder, err := s.repo.NextMarketingSortOrder(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	item := MarketingMediaRow{
		ID:              mediaID,
		MerchantID:      merchantID,
		Kind:            kind,
		URL:             url,
		DurationSeconds: customDuration,
		SortOrder:       sortOrder,
	}
	if err := s.repo.CreateMarketingMedia(ctx, item); err != nil {
		return nil, err
	}

	row, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		// Le média est créé : une erreur de relecture des réglages ne doit pas
		// faire croire à un échec (et pousser à renvoyer le fichier en double).
		logger.FromContext(ctx).Warn("kiosk: read settings after marketing media create failed",
			zap.String("merchant_id", merchantID), zap.Error(err))
		row = nil
	}

	s.notifyMarketingChanged(merchantID)
	resp := toMarketingMediaResponse(item, marketingDefaultDuration(row))
	return &resp, nil
}

// UpdateMarketingMedia règle la durée propre d'un média, ou la retire pour
// qu'il suive à nouveau la durée par défaut.
func (s *Service) UpdateMarketingMedia(ctx context.Context, merchantID, mediaID string, req UpdateMarketingMediaRequest) (*MarketingMediaResponse, error) {
	item, err := s.repo.GetMarketingMedia(ctx, merchantID, mediaID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, models.ErrKioskMediaNotFound
	}

	// Ici le refus est explicite, contrairement à la création : régler la durée
	// d'une vidéo est une action délibérée sur un contrôle que l'interface ne
	// propose pas. L'ignorer en silence masquerait un bug côté appelant.
	if item.Kind == marketingKindVideo && req.DurationSeconds != nil && *req.DurationSeconds != 0 {
		return nil, models.ErrKioskMediaInvalid
	}

	customDuration, err := normalizeMarketingDuration(item.Kind, req.DurationSeconds)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.UpdateMarketingMediaDuration(ctx, merchantID, mediaID, customDuration)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, models.ErrKioskMediaNotFound
	}

	row, err := s.repo.GetKioskSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	s.notifyMarketingChanged(merchantID)
	item.DurationSeconds = customDuration
	resp := toMarketingMediaResponse(*item, marketingDefaultDuration(row))
	return &resp, nil
}

// ResetMarketingMediaDurations retire toutes les durées propres : chaque média
// suit la durée par défaut. Retourne le nombre de médias réinitialisés.
func (s *Service) ResetMarketingMediaDurations(ctx context.Context, merchantID string) (int64, error) {
	reset, err := s.repo.ClearMarketingMediaDurations(ctx, merchantID)
	if err != nil {
		return 0, err
	}
	if reset > 0 {
		s.notifyMarketingChanged(merchantID)
	}
	return reset, nil
}

// ReorderMarketingMedia réécrit l'ordre de la rotation (voir
// mergeMarketingOrder pour le traitement d'une liste partielle ou périmée).
// Transaction : lecture de l'existant et réécriture des rangs doivent voir le
// même état.
func (s *Service) ReorderMarketingMedia(ctx context.Context, merchantID string, orderedIDs []string) error {
	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		items, err := s.repo.ListMarketingMedia(txCtx, merchantID)
		if err != nil {
			return err
		}
		existing := make([]string, 0, len(items))
		for _, item := range items {
			existing = append(existing, item.ID)
		}

		merged, err := mergeMarketingOrder(existing, orderedIDs)
		if err != nil {
			return err
		}
		return s.repo.ReorderMarketingMedia(txCtx, merchantID, merged)
	})
	if err != nil {
		return err
	}

	s.notifyMarketingChanged(merchantID)
	return nil
}

// DeleteMarketingMedia retire un média de la rotation et retourne la ligne
// supprimée, pour que l'AdminHandler puisse effacer le fichier R2 associé. Sans
// cette remontée, chaque suppression laisserait un objet orphelin : la ligne en
// base était le seul endroit portant son URL.
func (s *Service) DeleteMarketingMedia(ctx context.Context, merchantID, mediaID string) (*MarketingMediaRow, error) {
	item, err := s.repo.GetMarketingMedia(ctx, merchantID, mediaID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, models.ErrKioskMediaNotFound
	}

	deleted, err := s.repo.DeleteMarketingMedia(ctx, merchantID, mediaID)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, models.ErrKioskMediaNotFound
	}

	s.notifyMarketingChanged(merchantID)
	return item, nil
}
