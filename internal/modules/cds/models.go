package cds

import (
	"time"

	"welloresto-api/internal/middleware"
)

// AuthenticatedCDS est un alias du type porté par middleware (voir
// internal/middleware/cds_auth.go) — défini là-bas pour que ce middleware
// n'ait jamais besoin d'importer ce module. Sens unique : cds -> middleware.
type AuthenticatedCDS = middleware.AuthenticatedCDS

// ---- Lignes de base ----

// DisplayRow mappe la table cds_displays. Pas de champ Enabled : un écran est
// 'pending', 'active' ou 'revoked', il n'existe pas d'état désactivé
// (CDS_DECISIONS.md D15).
type DisplayRow struct {
	ID                string
	MerchantID        string
	Name              string
	LocationID        *string
	Status            string
	AppVersion        *string
	HardwareModel     *string
	OSVersion         *string
	DeviceID          *string
	AdminPinEncrypted []byte
	LastHeartbeatAt   *time.Time
	LastIP            *string
	LastError         *string
	LastErrorAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         *time.Time
}

// EnrollmentCodeRow mappe la table cds_enrollment_codes.
type EnrollmentCodeRow struct {
	ID         string
	MerchantID string
	CodeHash   string
	// DisplayName est le nom choisi au back-office pour l'écran à enrôler.
	// Nil pour un code généré sans nom : l'écran garde alors celui qu'il
	// envoie lui-même.
	DisplayName     *string
	DisplayID       *string
	ExpiresAt       time.Time
	UsedAt          *time.Time
	CreatedByUserID *string
	CreatedAt       time.Time
}

// DeviceTokenRow mappe la table cds_device_tokens.
type DeviceTokenRow struct {
	ID         string
	DisplayID  string
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// SettingsRow mappe la table cds_settings. Scopée par DisplayID et non par
// merchant — écart volontaire avec kiosk_settings : deux écrans du même
// restaurant ont des filtres et une zone marketing différents
// (CDS_DECISIONS.md D2).
type SettingsRow struct {
	DisplayID          string
	LayoutMode         string
	PreparingZoneRatio int
	OrderTypes         []string
	Channels           []string
	ShowWaitTime       bool
	MarketingEnabled   bool
	CreatedAt          time.Time
	UpdatedAt          *time.Time
}

// MediaItemRow mappe la table cds_media_items (rotation marketing, D6).
type MediaItemRow struct {
	ID              string
	DisplayID       string
	Kind            string
	URL             *string
	QRPayload       *string
	DurationSeconds int
	SortOrder       int
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       *time.Time
}

// ---- Device : requêtes / réponses ----

type EnrollRequest struct {
	EnrollmentCode string `json:"enrollment_code"`
	Name           string `json:"name"`
	HardwareModel  string `json:"hardware_model"`
	OSVersion      string `json:"os_version"`
	AppVersion     string `json:"app_version"`
	// DeviceID — identifiant dérivé de l'OS (Android ID), pas un secret.
	// Capturé pour permettre la ré-identification si le stockage local est
	// perdu (POST /cds/auth/reclaim). Optionnel.
	DeviceID string `json:"device_id"`
}

// EnrollResponse — admin_pin n'est retourné en clair qu'à l'enrôlement.
// Entre-temps, seule sa forme chiffrée est stockée.
type EnrollResponse struct {
	DisplayID    string `json:"display_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	AdminPin     string `json:"admin_pin"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// NOTE — POST /cds/auth/reclaim n'est volontairement pas implémenté à ce
// stade, et ses DTO ne sont donc pas déclarés ici (pas de contrat mort).
//
// Le besoin est réel : un écran qui perd son stockage local après une coupure
// de courant doit pouvoir se ré-identifier par device_id sans qu'un humain se
// déplace — c'est tout l'objet de kiosk.Service.ReclaimDevice, que
// cds_displays.device_id prépare déjà côté schéma.
//
// Mais le garde-fou du reclaim kiosk est un PIN admin, et celui du CDS fait
// 6 chiffres. Ouvrir cette route ajouterait une deuxième surface publique
// brute-forçable, en plus de l'enrôlement, alors que le middleware de rate
// limiting n'existe toujours pas dans cette API — voir
// docs/audits/2026-09-19-enrollment-rate-limiting.md. À implémenter une fois
// ce middleware en place, en reprenant le lockout Redis de kiosk
// (AdminPinLockoutError).

type HeartbeatRequest struct {
	AppVersion string `json:"app_version"`
}

// HeartbeatResponse — réduite à `status` par rapport au kiosk, qui expose en
// plus kiosk_status/enabled pour rattraper un kiosk_status_changed manqué.
// Le CDS n'a rien à rattraper : aucun état ne bascule à distance, seule la
// révocation agit et elle ferme la connexion WebSocket (CDS_DECISIONS.md D15).
type HeartbeatResponse struct {
	Status        string `json:"status"`
	DisplayStatus string `json:"display_status"`
}

// ---- Device : projection du plateau de commandes ----

// BoardOrder est la projection légère d'une commande pour l'affichage.
// Volontairement pauvre : un écran n'a besoin que d'un libellé, d'un statut,
// d'une source et de deux horodatages. Construire un models.Order complet
// (FetchAndBuildOrders : produits, extras, options, paiements, client, table,
// session de livraison) pour afficher « Paul — prêt » serait du gaspillage
// sur un écran qui se resynchronise à chaque reconnexion réseau.
type BoardOrder struct {
	OrderID string `json:"order_id"`
	// Label est résolu côté serveur selon la cascade prénom -> pager ->
	// order_num -> brand_order_num -> repli (CDS_DECISIONS.md D3). Le client
	// Flutter n'implémente aucune règle métier.
	Label string `json:"label"`
	// LabelKind ne sert qu'à la présentation : un prénom ne se préfixe pas
	// d'un « # ».
	LabelKind string `json:"label_kind"`
	// Status est un enum propre au CDS : "preparing" | "ready". Le client ne
	// voit jamais brand_status ni isDistributed.
	Status    string `json:"status"`
	OrderType string `json:"order_type"`
	Channel   string `json:"channel"`
	// Since — entrée dans le statut courant (approximée par orders.last_update),
	// utilisée pour trier la zone « Prêt » par ancienneté décroissante.
	Since string `json:"since"`
	// EstimatedReady alimente le compte à rebours optionnel (D11). Nullable :
	// ComputeEstimatedReady avale toute erreur SQL et laisse la colonne vide
	// si le merchant n'a pas de ligne average_distribution_time. Une commande
	// sans estimation n'affiche simplement pas de minuteur.
	EstimatedReady *string `json:"estimated_ready"`
	// Scheduled distingue une commande planifiée d'une commande immédiate.
	// Le filtrage H-1 (D12) est déjà fait en SQL ; ce champ ne sert qu'à la
	// présentation.
	Scheduled bool `json:"scheduled"`
}

// BoardResponse — réponse de GET /cds/orders.
type BoardResponse struct {
	Orders []BoardOrder `json:"orders"`
	// ServerTime permet au client de mesurer la dérive de son horloge et de
	// corriger l'ancienneté comme le compte à rebours. Une Android Box bon
	// marché dérive ; sans cette référence, le minuteur mentirait.
	ServerTime string `json:"server_time"`
}

// DeviceSettingsResponse — GET /cds/settings, la vue que l'écran a de sa
// propre configuration. Les filtres (order_types/channels) n'y figurent pas :
// ils sont appliqués côté serveur dans la projection, l'écran n'a pas à les
// connaître ni à pouvoir les contourner.
type DeviceSettingsResponse struct {
	DisplayName        string              `json:"display_name"`
	LayoutMode         string              `json:"layout_mode"`
	PreparingZoneRatio int                 `json:"preparing_zone_ratio"`
	ShowWaitTime       bool                `json:"show_wait_time"`
	MarketingEnabled   bool                `json:"marketing_enabled"`
	Media              []MediaItemResponse `json:"media"`
}

type MediaItemResponse struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	URL             *string `json:"url"`
	QRPayload       *string `json:"qr_payload"`
	DurationSeconds int     `json:"duration_seconds"`
	SortOrder       int     `json:"sort_order"`
}

// ---- Admin (back-office) ----

// GenerateEnrollmentCodeRequest — body optionnel de
// POST /pos/settings/cds/enrollment-codes.
//
// Name est facultatif : un corps vide ou absent reste valide, ce qui garde
// compatible tout client qui ne l'envoie pas encore.
type GenerateEnrollmentCodeRequest struct {
	Name string `json:"name"`
}

type GenerateEnrollmentCodeResponse struct {
	Code      string `json:"code"`
	Name      string `json:"name,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

type DisplayResponse struct {
	DisplayID       string  `json:"display_id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	AppVersion      *string `json:"app_version"`
	HardwareModel   *string `json:"hardware_model"`
	OSVersion       *string `json:"os_version"`
	LastHeartbeatAt *string `json:"last_heartbeat_at"`
	LastIP          *string `json:"last_ip"`
	CreatedAt       string  `json:"created_at"`
}

// ListDisplaysResponse expose aussi le plafond et l'usage courant : le
// back-office doit pouvoir afficher « 3 / 4 écrans » et désactiver le bouton
// d'ajout avant que le restaurateur ne génère un code pour rien (D4).
type ListDisplaysResponse struct {
	Displays   []DisplayResponse `json:"displays"`
	ActiveUsed int               `json:"active_used"`
	ActiveMax  int               `json:"active_max"`
}

type UpdateDisplayRequest struct {
	Name string `json:"name"`
}

type EnrollmentCodeListItem struct {
	ID string `json:"id"`
	// Name permet de distinguer plusieurs codes en attente : sans lui, la
	// liste ne montre que des dates identiques d'un code à l'autre.
	Name      *string `json:"name"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	UsedAt    *string `json:"used_at"`
}

type ListEnrollmentCodesResponse struct {
	Codes []EnrollmentCodeListItem `json:"codes"`
}

// SettingsResponse — vue back-office, filtres compris (contrairement à
// DeviceSettingsResponse).
type SettingsResponse struct {
	LayoutMode         string   `json:"layout_mode"`
	PreparingZoneRatio int      `json:"preparing_zone_ratio"`
	OrderTypes         []string `json:"order_types"`
	Channels           []string `json:"channels"`
	ShowWaitTime       bool     `json:"show_wait_time"`
	MarketingEnabled   bool     `json:"marketing_enabled"`
}

// UpdateSettingsRequest — mise à jour partielle : seuls les champs non nil
// sont écrits.
type UpdateSettingsRequest struct {
	LayoutMode         *string   `json:"layout_mode"`
	PreparingZoneRatio *int      `json:"preparing_zone_ratio"`
	OrderTypes         *[]string `json:"order_types"`
	Channels           *[]string `json:"channels"`
	ShowWaitTime       *bool     `json:"show_wait_time"`
	MarketingEnabled   *bool     `json:"marketing_enabled"`
}

type AdminPinResponse struct {
	AdminPin string `json:"admin_pin"`
}
