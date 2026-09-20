package cds

// Opérations back-office du module CDS — parc d'écrans, codes d'enrôlement,
// paramètres et rotation marketing. Séparées de service.go, qui porte le
// parcours device (enrôlement, tokens, heartbeat, plateau de commandes) :
// même découpage que le module kiosk entre handler.go et admin_handler.go.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// ---- Parc d'écrans ----

// ListDisplays renvoie le parc, plus le plafond et son usage — le back-office
// doit pouvoir afficher « 3 / 4 écrans » et désactiver le bouton d'ajout
// avant que le restaurateur ne génère un code pour rien (D4).
func (s *Service) ListDisplays(ctx context.Context, merchantID string) (*ListDisplaysResponse, error) {
	rows, err := s.repo.ListDisplaysByMerchant(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	activeCount, err := s.repo.GetActiveDisplayCount(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	displays := make([]DisplayResponse, 0, len(rows))
	for i := range rows {
		displays = append(displays, toDisplayResponse(&rows[i]))
	}

	return &ListDisplaysResponse{
		Displays:   displays,
		ActiveUsed: activeCount,
		ActiveMax:  maxActiveDisplays,
	}, nil
}

func (s *Service) GetDisplay(ctx context.Context, merchantID, displayID string) (*DisplayResponse, error) {
	row, err := s.requireDisplay(ctx, merchantID, displayID)
	if err != nil {
		return nil, err
	}
	resp := toDisplayResponse(row)
	return &resp, nil
}

func toDisplayResponse(row *DisplayRow) DisplayResponse {
	var lastHeartbeat *string
	if row.LastHeartbeatAt != nil {
		formatted := row.LastHeartbeatAt.UTC().Format(time.RFC3339)
		lastHeartbeat = &formatted
	}
	return DisplayResponse{
		DisplayID:       row.ID,
		Name:            row.Name,
		Status:          row.Status,
		AppVersion:      row.AppVersion,
		HardwareModel:   row.HardwareModel,
		OSVersion:       row.OSVersion,
		LastHeartbeatAt: lastHeartbeat,
		LastIP:          row.LastIP,
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Service) UpdateDisplay(ctx context.Context, merchantID, displayID string, req UpdateDisplayRequest) error {
	if err := validateDisplayName(req.Name); err != nil {
		return err
	}
	updated, err := s.repo.UpdateDisplayName(ctx, merchantID, displayID, strings.TrimSpace(req.Name))
	if err != nil {
		return err
	}
	if !updated {
		return models.ErrCDSNotFound
	}
	return nil
}

// RevokeDisplay retire définitivement un écran du parc — la seule action de
// cycle de vie disponible (CDS_DECISIONS.md D15).
//
// Trois effets, dans cet ordre : le statut passe à 'revoked' (l'écran ne peut
// plus rien lire), tous ses refresh tokens sont coupés (il ne peut plus se
// réémettre un access token), et sa connexion WebSocket est fermée
// immédiatement. Sans ce dernier point, l'écran continuerait d'afficher les
// commandes jusqu'à l'expiration naturelle de son access token.
func (s *Service) RevokeDisplay(ctx context.Context, merchantID, displayID string) error {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return err
	}

	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		revoked, err := s.repo.RevokeDisplay(txCtx, merchantID, displayID)
		if err != nil {
			return err
		}
		if !revoked {
			return models.ErrCDSNotFound
		}
		return s.repo.RevokeAllDeviceTokens(txCtx, displayID)
	})
	if err != nil {
		return err
	}

	// Best-effort : le WebSocket n'est qu'un canal temps réel. Si l'écran
	// n'est pas connecté, il découvrira sa révocation au prochain heartbeat.
	if s.notifier != nil {
		s.notifier.CloseCDSConnection(merchantID, displayID)
	}

	return nil
}

// requireDisplay vérifie qu'un écran existe et appartient bien au merchant
// appelant — garde de cloisonnement multi-tenant, posée sur chaque opération
// back-office qui porte un display_id d'URL.
func (s *Service) requireDisplay(ctx context.Context, merchantID, displayID string) (*DisplayRow, error) {
	display, err := s.repo.GetDisplayForMerchant(ctx, merchantID, displayID)
	if err != nil {
		return nil, err
	}
	if display == nil {
		return nil, models.ErrCDSNotFound
	}
	return display, nil
}

// ---- Codes d'enrôlement ----

// ListEnrollmentCodes liste les codes en attente — jamais le code en clair ni
// son hash. Un code ne s'affiche qu'une fois, à sa génération.
func (s *Service) ListEnrollmentCodes(ctx context.Context, merchantID string) (*ListEnrollmentCodesResponse, error) {
	rows, err := s.repo.ListPendingEnrollmentCodes(ctx, merchantID)
	if err != nil {
		return nil, err
	}

	codes := make([]EnrollmentCodeListItem, 0, len(rows))
	for _, row := range rows {
		item := EnrollmentCodeListItem{
			ID:        row.ID,
			Name:      row.DisplayName,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
			ExpiresAt: row.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if row.UsedAt != nil {
			used := row.UsedAt.UTC().Format(time.RFC3339)
			item.UsedAt = &used
		}
		codes = append(codes, item)
	}

	return &ListEnrollmentCodesResponse{Codes: codes}, nil
}

func (s *Service) DeleteEnrollmentCode(ctx context.Context, merchantID, codeID string) error {
	deleted, err := s.repo.DeleteEnrollmentCode(ctx, merchantID, codeID)
	if err != nil {
		return err
	}
	if !deleted {
		return models.ErrCDSNotFound
	}
	return nil
}

// GetAdminPin déchiffre le PIN d'administration pour consultation
// back-office. C'est l'intérêt du chiffrement réversible par rapport à un
// hash : le restaurateur qui a perdu le PIN doit pouvoir le relire.
func (s *Service) GetAdminPin(ctx context.Context, merchantID, displayID string) (*AdminPinResponse, error) {
	display, err := s.requireDisplay(ctx, merchantID, displayID)
	if err != nil {
		return nil, err
	}
	if len(display.AdminPinEncrypted) == 0 {
		return nil, models.ErrCDSNotFound
	}

	pin, err := helpers.Decrypt(display.AdminPinEncrypted)
	if err != nil {
		return nil, fmt.Errorf("cds: decrypt admin pin: %w", err)
	}
	return &AdminPinResponse{AdminPin: pin}, nil
}

// ---- Paramètres ----

func (s *Service) GetSettings(ctx context.Context, merchantID, displayID string) (*SettingsResponse, error) {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return nil, err
	}

	settings, err := s.repo.GetSettings(ctx, displayID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, models.ErrCDSNotFound
	}

	return &SettingsResponse{
		LayoutMode:         settings.LayoutMode,
		PreparingZoneRatio: settings.PreparingZoneRatio,
		OrderTypes:         settings.OrderTypes,
		Channels:           settings.Channels,
		ShowWaitTime:       settings.ShowWaitTime,
		MarketingEnabled:   settings.MarketingEnabled,
	}, nil
}

// validOrderTypes / validChannels — les deux axes du filtre (D2). Validés
// côté serveur et pas seulement dans le back-office : une valeur inconnue ne
// produirait aucune erreur SQL, elle rendrait simplement l'écran vide sans
// que personne ne comprenne pourquoi.
var (
	validOrderTypes = map[string]bool{"IN": true, "TAKE_AWAY": true, "DELIVERY": true}
	validChannels   = map[string]bool{"WELLO_RESTO": true, "UBER_EATS": true, "DELIVEROO": true}
)

func (s *Service) UpdateSettings(ctx context.Context, merchantID, displayID string, req UpdateSettingsRequest) error {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return err
	}

	if req.LayoutMode != nil && *req.LayoutMode != "two_zones" && *req.LayoutMode != "three_zones" {
		return models.ErrCDSSettingsInvalid
	}
	if req.PreparingZoneRatio != nil && (*req.PreparingZoneRatio < 20 || *req.PreparingZoneRatio > 60) {
		return models.ErrCDSSettingsInvalid
	}
	if req.OrderTypes != nil {
		for _, t := range *req.OrderTypes {
			if !validOrderTypes[t] {
				return models.ErrCDSSettingsInvalid
			}
		}
	}
	if req.Channels != nil {
		for _, c := range *req.Channels {
			if !validChannels[c] {
				return models.ErrCDSSettingsInvalid
			}
		}
	}

	return s.repo.UpdateSettings(ctx, displayID, req)
}

// ---- Médias marketing ----

func (s *Service) ListMedia(ctx context.Context, merchantID, displayID string) ([]MediaItemResponse, error) {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return nil, err
	}

	items, err := s.repo.ListMediaItems(ctx, displayID, false)
	if err != nil {
		return nil, err
	}

	resp := make([]MediaItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, MediaItemResponse{
			ID:              item.ID,
			Kind:            item.Kind,
			URL:             item.URL,
			QRPayload:       item.QRPayload,
			DurationSeconds: item.DurationSeconds,
			SortOrder:       item.SortOrder,
		})
	}
	return resp, nil
}

// NewMediaItemID produit l'identifiant d'un média avant son upload.
//
// Exposé parce que la clé R2 se construit à partir de cet identifiant
// (r2.GenerateCDSMediaKey) : le fichier doit donc être nommé avant que la
// ligne n'existe en base. L'AdminHandler génère l'id, uploade, puis passe les
// deux à CreateMediaItem.
func NewMediaItemID() string {
	return helpers.GeneratePrefixedID(helpers.CDSMediaItemIDPrefix)
}

// CreateMediaItem ajoute un média à la rotation (D6).
//
// mediaID peut être vide (généré ici) ou pré-généré par l'appelant quand un
// fichier a déjà été uploadé sous cet identifiant. url est renseigné par
// l'AdminHandler après upload R2 pour une image ou une vidéo ; un média 'qr'
// ne porte pas de fichier, seulement qrPayload.
func (s *Service) CreateMediaItem(ctx context.Context, merchantID, displayID, mediaID, kind string, url, qrPayload *string, durationSeconds int) (*MediaItemResponse, error) {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return nil, err
	}

	switch kind {
	case "image", "video":
		if url == nil || strings.TrimSpace(*url) == "" {
			return nil, models.ErrCDSMediaInvalid
		}
	case "qr":
		if qrPayload == nil || strings.TrimSpace(*qrPayload) == "" {
			return nil, models.ErrCDSMediaInvalid
		}
	default:
		return nil, models.ErrCDSMediaInvalid
	}

	if durationSeconds == 0 {
		durationSeconds = defaultMediaDurationSeconds
	}
	if durationSeconds < 3 || durationSeconds > 120 {
		return nil, models.ErrCDSMediaInvalid
	}

	sortOrder, err := s.repo.GetNextMediaSortOrder(ctx, displayID)
	if err != nil {
		return nil, err
	}

	if mediaID == "" {
		mediaID = NewMediaItemID()
	}

	item := MediaItemRow{
		ID:              mediaID,
		DisplayID:       displayID,
		Kind:            kind,
		URL:             url,
		QRPayload:       qrPayload,
		DurationSeconds: durationSeconds,
		SortOrder:       sortOrder,
		Enabled:         true,
	}
	if err := s.repo.CreateMediaItem(ctx, item); err != nil {
		return nil, err
	}

	return &MediaItemResponse{
		ID:              item.ID,
		Kind:            item.Kind,
		URL:             item.URL,
		QRPayload:       item.QRPayload,
		DurationSeconds: item.DurationSeconds,
		SortOrder:       item.SortOrder,
	}, nil
}

// defaultMediaDurationSeconds aligné sur le DEFAULT de la colonne
// cds_media_items.duration_seconds (migration 147).
const defaultMediaDurationSeconds = 10

// ReorderMedia réécrit l'ordre de la rotation. Transaction obligatoire : un
// réordonnancement partiel laisserait deux médias sur le même rang.
func (s *Service) ReorderMedia(ctx context.Context, merchantID, displayID string, orderedIDs []string) error {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return err
	}
	return dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		return s.repo.ReorderMediaItems(txCtx, displayID, orderedIDs)
	})
}

// DeleteMediaItem retire un média de la rotation et retourne la ligne
// supprimée, pour que l'AdminHandler puisse effacer le fichier R2 associé.
// Sans cette remontée, chaque suppression laisserait un objet orphelin dans
// le bucket — la ligne en base était le seul endroit portant son URL.
func (s *Service) DeleteMediaItem(ctx context.Context, merchantID, displayID, mediaID string) (*MediaItemRow, error) {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return nil, err
	}

	item, err := s.repo.GetMediaItem(ctx, displayID, mediaID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, models.ErrCDSMediaNotFound
	}

	deleted, err := s.repo.DeleteMediaItem(ctx, displayID, mediaID)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, models.ErrCDSMediaNotFound
	}
	return item, nil
}
