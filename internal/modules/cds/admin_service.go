package cds

// Opérations back-office du module CDS — parc d'écrans, codes d'enrôlement,
// paramètres et rotation marketing. Séparées de service.go, qui porte le
// parcours device (enrôlement, tokens, heartbeat, plateau de commandes) :
// même découpage que le module kiosk entre handler.go et admin_handler.go.

import (
	"context"
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

		DefaultMediaDurationSeconds: settings.DefaultMediaDurationSeconds,
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

	if req.LayoutMode != nil && !validLayoutModes[*req.LayoutMode] {
		return models.ErrCDSSettingsInvalid
	}
	if req.PreparingZoneRatio != nil && (*req.PreparingZoneRatio < 20 || *req.PreparingZoneRatio > 60) {
		return models.ErrCDSSettingsInvalid
	}
	if req.DefaultMediaDurationSeconds != nil && !validMediaDuration(*req.DefaultMediaDurationSeconds) {
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

	defaultDuration, err := s.defaultMediaDuration(ctx, displayID)
	if err != nil {
		return nil, err
	}

	items, err := s.repo.ListMediaItems(ctx, displayID, false)
	if err != nil {
		return nil, err
	}

	resp := make([]MediaItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toMediaItemResponse(item, defaultDuration))
	}
	return resp, nil
}

// defaultMediaDuration lit la durée par défaut de l'écran, avec un repli sur la
// constante si sa ligne de paramètres est absente (ne devrait pas arriver :
// elle est créée à l'enrôlement).
func (s *Service) defaultMediaDuration(ctx context.Context, displayID string) (int, error) {
	settings, err := s.repo.GetSettings(ctx, displayID)
	if err != nil {
		return 0, err
	}
	if settings == nil || settings.DefaultMediaDurationSeconds <= 0 {
		return defaultMediaDurationSeconds, nil
	}
	return settings.DefaultMediaDurationSeconds, nil
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
func (s *Service) CreateMediaItem(ctx context.Context, merchantID, displayID, mediaID, kind string, url, qrPayload *string, durationSeconds *int) (*MediaItemResponse, error) {
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

	// Une durée absente n'est PAS remplacée par une valeur : le média reste sans
	// durée propre et suit celle de l'écran. Figer 10 s ici, comme avant, le
	// déconnecterait du réglage global dès sa création.
	customDuration, err := normalizeMediaDuration(kind, durationSeconds)
	if err != nil {
		return nil, err
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
		DurationSeconds: customDuration,
		SortOrder:       sortOrder,
		Enabled:         true,
	}
	if err := s.repo.CreateMediaItem(ctx, item); err != nil {
		return nil, err
	}

	defaultDuration, err := s.defaultMediaDuration(ctx, displayID)
	if err != nil {
		return nil, err
	}
	resp := toMediaItemResponse(item, defaultDuration)
	return &resp, nil
}

// Bornes des durées d'affichage, alignées sur les contraintes CHECK de
// cds_media_items (147) et cds_settings (152). Validées ici pour renvoyer une
// erreur lisible plutôt qu'un 500 sur violation de contrainte.
const (
	minMediaDurationSeconds = 3
	maxMediaDurationSeconds = 120

	// defaultMediaDurationSeconds est le DEFAULT de
	// cds_settings.default_media_duration_seconds (152). Sert aussi de repli si
	// la ligne de paramètres d'un écran est absente.
	defaultMediaDurationSeconds = 10
)

// validMediaDuration dit si une durée renseignée est dans les bornes.
func validMediaDuration(seconds int) bool {
	return seconds >= minMediaDurationSeconds && seconds <= maxMediaDurationSeconds
}

// normalizeMediaDuration décide de la durée PROPRE à stocker pour un nouveau
// média : nil pour « suit la durée par défaut », une valeur pour une durée
// personnalisée.
//
//   - absente ou 0  -> nil. 0 est traité comme « non renseigné » : c'est ce
//     qu'envoyait un client qui n'avait rien à dire, et le refuser casserait
//     un ancien back-office ;
//   - vidéo         -> nil, quoi qu'on envoie : elle est jouée en entier, une
//     durée n'aurait aucun effet et ferait croire le contraire ;
//   - hors bornes   -> erreur.
func normalizeMediaDuration(kind string, seconds *int) (*int, error) {
	if kind == "video" || seconds == nil || *seconds == 0 {
		return nil, nil
	}
	if !validMediaDuration(*seconds) {
		return nil, models.ErrCDSMediaInvalid
	}
	value := *seconds
	return &value, nil
}

// resolveMediaDuration calcule la durée effective d'un média : sa durée propre
// si elle existe, sinon celle de l'écran. Le booléen dit si elle est propre.
func resolveMediaDuration(custom *int, defaultDuration int) (int, bool) {
	if custom != nil {
		return *custom, true
	}
	return defaultDuration, false
}

// toMediaItemResponse projette une ligne de média en réponse, durée résolue.
func toMediaItemResponse(item MediaItemRow, defaultDuration int) MediaItemResponse {
	seconds, custom := resolveMediaDuration(item.DurationSeconds, defaultDuration)
	return MediaItemResponse{
		ID:              item.ID,
		Kind:            item.Kind,
		URL:             item.URL,
		QRPayload:       item.QRPayload,
		DurationSeconds: seconds,
		CustomDuration:  custom,
		SortOrder:       item.SortOrder,
	}
}

// UpdateMediaItem règle la durée propre d'un média, ou la retire pour qu'il
// suive à nouveau la durée par défaut de l'écran.
func (s *Service) UpdateMediaItem(ctx context.Context, merchantID, displayID, mediaID string, req UpdateMediaItemRequest) (*MediaItemResponse, error) {
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

	// Ici le refus est explicite, contrairement à la création : régler la durée
	// d'une vidéo est une action délibérée sur un contrôle que l'interface ne
	// propose pas. L'ignorer en silence masquerait un bug côté appelant.
	if item.Kind == "video" && req.DurationSeconds != nil && *req.DurationSeconds != 0 {
		return nil, models.ErrCDSMediaInvalid
	}

	customDuration, err := normalizeMediaDuration(item.Kind, req.DurationSeconds)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.UpdateMediaDuration(ctx, displayID, mediaID, customDuration)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, models.ErrCDSMediaNotFound
	}

	item.DurationSeconds = customDuration
	defaultDuration, err := s.defaultMediaDuration(ctx, displayID)
	if err != nil {
		return nil, err
	}
	resp := toMediaItemResponse(*item, defaultDuration)
	return &resp, nil
}

// ResetMediaDurations retire toutes les durées propres de l'écran : chaque
// média suit la durée par défaut. C'est l'action « appliquer à tous » des
// logiciels d'affichage dynamique. Retourne le nombre de médias réinitialisés.
func (s *Service) ResetMediaDurations(ctx context.Context, merchantID, displayID string) (int64, error) {
	if _, err := s.requireDisplay(ctx, merchantID, displayID); err != nil {
		return 0, err
	}
	return s.repo.ClearMediaDurations(ctx, displayID)
}

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
