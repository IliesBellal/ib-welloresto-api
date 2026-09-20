package cds

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/utils/security"
)

// Notifier est le sous-ensemble de notification.NotificationService dont ce
// module a besoin — interface plutôt qu'import du type concret, pour que le
// service reste testable et que cds ne dépende pas du module notification.
type Notifier interface {
	CloseCDSConnection(merchantID, displayID string) bool
}

type Service struct {
	cfg      Config
	repo     *Repository
	db       *sql.DB
	redis    *redis.Client
	notifier Notifier
}

func NewService(cfg Config, repo *Repository, db *sql.DB, redisClient *redis.Client, notifier Notifier) *Service {
	return &Service{cfg: cfg, repo: repo, db: db, redis: redisClient, notifier: notifier}
}

// Cache de la projection du plateau.
//
// TTL d'UNE seconde, et pas davantage. Ce cache ne protege que d'une chose :
// la rafale multi-ecrans, quand les 4 ecrans d'un merchant (plafond D4)
// reagissent au meme UPDATE_ORDER a quelques millisecondes d'intervalle. Une
// seconde suffit largement a la collapser.
//
// Le porter a 5 ou 10 secondes serait un contresens produit : le debounce
// cote Flutter est de 400 ms precisement pour que le passage d'une commande
// en "Pret" saute aux yeux du client sans delai. Un cache plus long
// reintroduirait exactement la latence que tout le reste de l'architecture
// cherche a supprimer, pour economiser au maximum 3 requetes sur un endpoint
// deja leger. Le cout et le benefice sont inverses.
const boardCacheTTL = 1 * time.Second

// boardCacheKey indexe par merchant ET par jeu de filtres.
//
// Indexer par merchant seul serait un bug de cloisonnement entre ecrans : un
// ecran filtre sur les livraisons Uber Eats et un autre sur les commandes
// internes ne doivent jamais se servir dans la meme entree (CDS_DECISIONS.md
// D2). Les filtres sont tries avant hachage pour que deux ecrans configures
// a l'identique, mais dans un ordre different, partagent bien la meme entree.
func boardCacheKey(merchantID string, orderTypes, channels []string) string {
	sortedTypes := append([]string(nil), orderTypes...)
	sortedChannels := append([]string(nil), channels...)
	sort.Strings(sortedTypes)
	sort.Strings(sortedChannels)

	sum := sha256.Sum256([]byte(strings.Join(sortedTypes, ",") + "|" + strings.Join(sortedChannels, ",")))
	return fmt.Sprintf("cds:board:%s:%x", merchantID, sum[:8])
}

// ---- Enrôlement ----

// GenerateEnrollmentCode produit un code à usage unique pour le back-office.
//
// Contrairement au module kiosk, aucun quota d'abonnement n'est consulté : le
// CDS est gratuit (CDS_DECISIONS.md D4). Seul le plafond technique de
// maxActiveDisplays s'applique, et il se vérifie ici — refuser au moment de
// la génération plutôt qu'à l'enrôlement évite au restaurateur de saisir un
// code sur l'écran pour découvrir ensuite qu'il est refusé.
//
// name est le nom que le restaurateur donne à l'écran. Il est facultatif :
// vide, le code se comporte comme avant et l'écran garde le nom qu'il envoie.
// Validé ici, au moment de la saisie, plutôt qu'à l'enrôlement : un nom
// refusé devant un écran non tactile ne serait plus corrigeable sur place.
func (s *Service) GenerateEnrollmentCode(ctx context.Context, merchantID, createdByUserID, name string) (*GenerateEnrollmentCodeResponse, error) {
	name = strings.TrimSpace(name)
	var displayName *string
	if name != "" {
		if err := validateDisplayName(name); err != nil {
			return nil, err
		}
		displayName = &name
	}

	activeCount, err := s.repo.GetActiveDisplayCount(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	if activeCount >= maxActiveDisplays {
		return nil, models.ErrCDSMaxDisplaysReached
	}

	code, err := generateEnrollmentCode()
	if err != nil {
		return nil, fmt.Errorf("cds: generate enrollment code: %w", err)
	}

	codeHash := security.HashPIN(code, s.cfg.Pepper)
	expiresAt := time.Now().UTC().Add(time.Duration(s.cfg.EnrollmentCodeTTLMinutes) * time.Minute)
	codeID := helpers.GeneratePrefixedID(helpers.CDSEnrollmentCodeIDPrefix)

	if err := s.repo.CreateEnrollmentCode(ctx, codeID, merchantID, codeHash, displayName, expiresAt, createdByUserID); err != nil {
		return nil, err
	}

	return &GenerateEnrollmentCodeResponse{
		Code:      code,
		Name:      name,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

// resolveEnrollmentName détermine le nom que portera l'écran créé.
//
// Le nom du code, choisi par le restaurateur, prime sur celui de l'appareil :
// ce dernier est auto-généré (« Écran Android Box ») et n'a aucune valeur
// pour repérer un écran dans le parc. L'appareil ne sert que de repli, pour
// un code généré sans nom.
func resolveEnrollmentName(codeName *string, deviceName string) (string, error) {
	if codeName != nil {
		if trimmed := strings.TrimSpace(*codeName); trimmed != "" {
			return trimmed, nil
		}
	}
	if err := validateDisplayName(deviceName); err != nil {
		return "", err
	}
	return strings.TrimSpace(deviceName), nil
}

// generateEnrollmentCode produit un code numérique à 6 chiffres.
//
// Format imposé par le matériel : la saisie se fait sur un pavé numérique
// affiché à l'écran et navigué à la télécommande, sur un moniteur non tactile
// (CDS_DECISIONS.md D16). Le code alphanumérique à 8 caractères du kiosk y
// serait ingérable.
//
// ⚠ L'espace de codes tombe de 32^8 (~10^12) à 10^6. Combiné à l'index UNIQUE
// global sur code_hash — une tentative aveugle est testée contre tous les
// codes en attente de la plateforme — et à l'absence de rate limiting dans
// cette API, c'est un risque réel et tracé :
// docs/audits/2026-09-19-enrollment-rate-limiting.md. Le TTL court réduit la
// cible sans limiter le débit.
//
// Tirage uniforme via rand.Int sur [0, 1000000) et non un modulo sur des
// octets, qui biaiserait la distribution vers les valeurs basses.
func generateEnrollmentCode() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// generateAdminPin produit le PIN administrateur à 6 chiffres, même tirage
// uniforme que le code d'enrôlement.
//
// ⚠ À ce jour ce PIN ne protège RIEN : il est généré, stocké chiffré, affiché
// une fois sur l'écran à l'enrôlement et consultable au back-office, mais ni
// l'application ni l'API n'ont d'écran d'administration ni de route de
// vérification (contrairement au kiosk, qui a POST /kiosk/auth/verify-admin-pin).
// Il a été porté du module kiosk en prévision de cet écran, qui reste à
// concevoir.
func generateAdminPin() (string, error) {
	return generateEnrollmentCode()
}

// EnrollDevice consomme un code d'enrôlement et crée l'écran.
func (s *Service) EnrollDevice(ctx context.Context, req EnrollRequest, ip string) (*EnrollResponse, error) {
	codeHash := security.HashPIN(strings.TrimSpace(req.EnrollmentCode), s.cfg.Pepper)

	code, err := s.repo.GetEnrollmentCodeByHash(ctx, codeHash)
	if err != nil {
		return nil, err
	}
	if code == nil {
		return nil, models.ErrCDSEnrollmentCodeInvalid
	}
	if code.UsedAt != nil {
		return nil, models.ErrCDSEnrollmentCodeUsed
	}
	if time.Now().UTC().After(code.ExpiresAt) {
		return nil, models.ErrCDSEnrollmentCodeExpired
	}

	// Le nom se résout APRÈS la lecture du code : il peut venir du code lui-même
	// (choisi au back-office), auquel cas le nom envoyé par l'appareil n'a pas à
	// être valide. Valider le nom de l'appareil avant tout refuserait un
	// enrôlement légitime sur une erreur qui ne le concerne plus.
	req.Name, err = resolveEnrollmentName(code.DisplayName, req.Name)
	if err != nil {
		return nil, err
	}

	// Re-vérifié ici et pas seulement à la génération : deux codes peuvent
	// avoir été émis avant que le premier ne soit consommé.
	activeCount, err := s.repo.GetActiveDisplayCount(ctx, code.MerchantID)
	if err != nil {
		return nil, err
	}
	if activeCount >= maxActiveDisplays {
		return nil, models.ErrCDSMaxDisplaysReached
	}

	displayID := helpers.GeneratePrefixedID(helpers.CDSDisplayIDPrefix)
	deviceTokenID := helpers.GeneratePrefixedID(helpers.CDSDeviceTokenIDPrefix)

	refreshToken, err := helpers.GenerateToken(32)
	if err != nil {
		return nil, fmt.Errorf("cds enroll: generate refresh token: %w", err)
	}
	refreshTokenHash := security.HashPIN(refreshToken, s.cfg.Pepper)
	refreshExpiresAt := time.Now().UTC().AddDate(0, 0, s.cfg.DeviceRefreshTokenTTLDays)

	adminPin, err := generateAdminPin()
	if err != nil {
		return nil, fmt.Errorf("cds enroll: generate admin pin: %w", err)
	}
	adminPinEncrypted, err := helpers.Encrypt(adminPin)
	if err != nil {
		return nil, fmt.Errorf("cds enroll: encrypt admin pin: %w", err)
	}

	// Chaîne vide -> NULL plutôt qu'une valeur vide stockée : deux écrans ne
	// doivent jamais coïncider sur un device_id "vide" lors d'un reclaim
	// (NULL ne matche jamais rien).
	var deviceID *string
	if trimmed := truncateRunes(strings.TrimSpace(req.DeviceID), maxDeviceIDLen); trimmed != "" {
		deviceID = &trimmed
	}

	// Ces champs sont déclarés par l'appareil et n'ont aucune raison d'être
	// dignes de confiance : ils sont tronqués à la largeur de leur colonne
	// plutôt que rejetés. Une chaîne trop longue est un problème d'affichage
	// dans le back-office, jamais un motif de refuser l'enrôlement — et
	// surtout pas de renvoyer un 500 « value too long for type character
	// varying » à l'installateur, qui n'a aucun moyen d'y remédier.
	hardwareModel := truncateRunes(strings.TrimSpace(req.HardwareModel), maxHardwareModelLen)
	osVersion := truncateRunes(strings.TrimSpace(req.OSVersion), maxOSVersionLen)

	var display *DisplayRow
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		display, err = s.repo.CreateDisplay(txCtx, displayID, code.MerchantID, req.Name, hardwareModel, osVersion, adminPinEncrypted, deviceID)
		if err != nil {
			return err
		}
		if err := s.repo.MarkEnrollmentCodeUsed(txCtx, code.ID, display.ID); err != nil {
			return err
		}
		if err := s.repo.CreateDefaultSettings(txCtx, display.ID); err != nil {
			return err
		}
		return s.repo.CreateDeviceToken(txCtx, deviceTokenID, display.ID, refreshTokenHash, refreshExpiresAt)
	})
	if err != nil {
		return nil, err
	}

	accessToken, expiresAt, err := s.generateAccessToken(display.ID, display.MerchantID)
	if err != nil {
		return nil, err
	}

	return &EnrollResponse{
		DisplayID:    display.ID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		AdminPin:     adminPin,
	}, nil
}

// Largeurs des colonnes de cds_displays alimentées par l'appareil (migration
// 147). À garder synchronisées avec le schéma : un écart ne se voit qu'au
// premier appareil dont la chaîne dépasse — c'est exactement ainsi que
// os_version a produit un 500 sur le premier enrôlement réel, l'Android
// `Platform.operatingSystemVersion` renvoyant toute la chaîne noyau.
const (
	maxHardwareModelLen = 100
	maxOSVersionLen     = 50
	maxAppVersionLen    = 20
	maxDeviceIDLen      = 128
	maxIPLen            = 45
)

// truncateRunes coupe s à max caractères. Comptage en runes et non en octets :
// la colonne Postgres varchar(n) compte des caractères, et couper au milieu
// d'un caractère accentué produirait une chaîne UTF-8 invalide.
func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// validateDisplayName impose un nom non vide et <= 100 caractères (colonne
// cds_displays.name). Comptage en runes, pas en octets : « Écran d'accueil »
// ne doit pas être rejeté pour ses accents.
func validateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > 100 {
		return models.ErrCDSNameInvalid
	}
	return nil
}

// ---- Tokens ----

// RefreshDeviceToken échange un refresh token contre un nouveau couple
// access/refresh. Le refresh token est tournant : l'ancien est révoqué dans
// la même transaction que l'insertion du nouveau.
func (s *Service) RefreshDeviceToken(ctx context.Context, refreshToken string) (*RefreshTokenResponse, error) {
	tokenHash := security.HashPIN(refreshToken, s.cfg.Pepper)

	deviceToken, err := s.repo.GetDeviceTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	if deviceToken == nil || deviceToken.RevokedAt != nil {
		return nil, models.ErrCDSDeviceTokenInvalid
	}
	if time.Now().UTC().After(deviceToken.ExpiresAt) {
		return nil, models.ErrCDSDeviceTokenInvalid
	}

	display, err := s.repo.GetDisplayByID(ctx, deviceToken.DisplayID)
	if err != nil {
		return nil, err
	}
	if display == nil {
		return nil, models.ErrCDSNotFound
	}
	if display.Status == "revoked" {
		return nil, models.ErrCDSRevoked
	}

	newRefreshToken, err := helpers.GenerateToken(32)
	if err != nil {
		return nil, fmt.Errorf("cds refresh: generate refresh token: %w", err)
	}
	newRefreshTokenHash := security.HashPIN(newRefreshToken, s.cfg.Pepper)
	newRefreshExpiresAt := time.Now().UTC().AddDate(0, 0, s.cfg.DeviceRefreshTokenTTLDays)
	newTokenID := helpers.GeneratePrefixedID(helpers.CDSDeviceTokenIDPrefix)

	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		return s.repo.RotateDeviceToken(txCtx, deviceToken.ID, newTokenID, display.ID, newRefreshTokenHash, newRefreshExpiresAt)
	})
	if err != nil {
		return nil, err
	}

	accessToken, expiresAt, err := s.generateAccessToken(display.ID, display.MerchantID)
	if err != nil {
		return nil, err
	}

	return &RefreshTokenResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt.Format(time.RFC3339),
	}, nil
}

// ValidateAccessToken vérifie signature et expiration, sans accès base :
// l'access token est auto-porteur et signé HMAC-SHA256, jamais persisté (même
// choix que kiosk, voir KIOSK_DECISIONS.md G.1). Appelée par
// middleware.CDSAuth sur chaque requête protégée.
func (s *Service) ValidateAccessToken(ctx context.Context, accessToken string) (*AuthenticatedCDS, error) {
	displayID, merchantID, expiresAt, err := s.parseAccessToken(accessToken)
	if err != nil {
		return nil, models.ErrCDSDeviceTokenInvalid
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, models.ErrCDSDeviceTokenInvalid
	}
	return &AuthenticatedCDS{DisplayID: displayID, MerchantID: merchantID}, nil
}

func (s *Service) generateAccessToken(displayID, merchantID string) (string, time.Time, error) {
	expiresAt := time.Now().UTC().Add(time.Duration(s.cfg.AccessTokenTTLMinutes) * time.Minute)
	payload := fmt.Sprintf("%s|%s|%d", displayID, merchantID, expiresAt.Unix())
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encodedPayload + "." + s.signPayload(encodedPayload), expiresAt, nil
}

func (s *Service) parseAccessToken(token string) (displayID, merchantID string, expiresAt time.Time, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", time.Time{}, errors.New("malformed access token")
	}
	encodedPayload, signature := parts[0], parts[1]

	expectedSignature := s.signPayload(encodedPayload)
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return "", "", time.Time{}, errors.New("invalid access token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return "", "", time.Time{}, err
	}

	fields := strings.SplitN(string(payloadBytes), "|", 3)
	if len(fields) != 3 {
		return "", "", time.Time{}, errors.New("malformed access token payload")
	}

	expiresUnix, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return "", "", time.Time{}, err
	}

	return fields[0], fields[1], time.Unix(expiresUnix, 0).UTC(), nil
}

func (s *Service) signPayload(encodedPayload string) string {
	h := hmac.New(sha256.New, []byte(s.cfg.Pepper))
	h.Write([]byte(encodedPayload))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ---- Heartbeat ----

// RecordHeartbeat met à jour le dernier contact connu de l'écran.
//
// La réponse ne porte que le statut, là où le kiosk expose en plus
// kiosk_status/enabled pour rattraper un kiosk_status_changed manqué pendant
// une coupure. Le CDS n'a rien à rattraper : aucun état ne bascule à distance
// (CDS_DECISIONS.md D15). Le heartbeat ne sert donc qu'à deux choses —
// alimenter « dernière activité » côté back-office, et faire découvrir une
// révocation à un écran qui aurait manqué la fermeture de son WebSocket.
func (s *Service) RecordHeartbeat(ctx context.Context, cds *AuthenticatedCDS, req HeartbeatRequest, ip string) (*HeartbeatResponse, error) {
	display, err := s.repo.GetDisplayForMerchant(ctx, cds.MerchantID, cds.DisplayID)
	if err != nil {
		return nil, err
	}
	if display == nil {
		return nil, models.ErrCDSNotFound
	}
	if display.Status == "revoked" {
		return nil, models.ErrCDSRevoked
	}

	// Même précaution qu'à l'enrôlement : cds_displays.app_version est un
	// varchar(20), et un heartbeat qui échoue en 500 toutes les 5 minutes
	// finirait par faire passer un écran sain pour un écran en panne.
	appVersion := truncateRunes(strings.TrimSpace(req.AppVersion), maxAppVersionLen)
	if err := s.repo.UpdateDisplayHeartbeat(ctx, display.ID, appVersion, truncateRunes(ip, maxIPLen)); err != nil {
		return nil, err
	}

	return &HeartbeatResponse{Status: "ok", DisplayStatus: display.Status}, nil
}

// ---- Plateau de commandes ----

// GetBoard construit la projection affichée par un écran.
func (s *Service) GetBoard(ctx context.Context, cds *AuthenticatedCDS) (*BoardResponse, error) {
	settings, err := s.repo.GetSettings(ctx, cds.DisplayID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, models.ErrCDSNotFound
	}

	cacheKey := boardCacheKey(cds.MerchantID, settings.OrderTypes, settings.Channels)

	var orders []BoardOrder
	cached := false
	if s.redis != nil {
		if val, found := s.redis.Get(ctx, cacheKey); found {
			if err := json.Unmarshal([]byte(val), &orders); err == nil {
				cached = true
			}
		}
	}

	if !cached {
		rows, err := s.repo.GetBoardOrders(ctx, cds.MerchantID, settings.OrderTypes, settings.Channels)
		if err != nil {
			return nil, err
		}

		orders = make([]BoardOrder, 0, len(rows))
		for _, row := range rows {
			orders = append(orders, toBoardOrder(row))
		}

		if s.redis != nil {
			if payload, err := json.Marshal(orders); err == nil {
				s.redis.Set(ctx, cacheKey, string(payload), boardCacheTTL)
			}
		}
	}

	// ServerTime est TOUJOURS recalcule, jamais servi depuis le cache : c'est
	// la reference d'horloge que l'ecran utilise pour corriger sa derive et
	// calculer le compte a rebours (D11). Un ServerTime vieux de 5 secondes
	// fausserait tous les minuteurs de la boite Android.
	return &BoardResponse{
		Orders:     orders,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// toBoardOrder applique les deux règles métier du plateau — statut et
// libellé — côté serveur. Le client Flutter n'interprète jamais brand_status,
// isDistributed ni la cascade de libellé : il affiche ce qu'on lui donne.
func toBoardOrder(row boardOrderRow) BoardOrder {
	label, labelKind := resolveLabel(row)

	order := BoardOrder{
		OrderID:   row.OrderID,
		Label:     label,
		LabelKind: labelKind,
		Status:    resolveStatus(row),
		OrderType: row.OrderType.String,
		Channel:   row.Brand.String,
		Scheduled: row.Scheduled,
	}

	if row.LastUpdate.Valid {
		order.Since = row.LastUpdate.Time.UTC().Format(time.RFC3339)
	}
	if row.EstimatedReady.Valid {
		estimated := row.EstimatedReady.Time.UTC().Format(time.RFC3339)
		order.EstimatedReady = &estimated
	}

	return order
}

// resolveStatus traduit l'état interne en enum CDS (CDS_DECISIONS.md D1).
//
// Les deux conditions sont nécessaires, et c'est le piège de ce module :
//   - isDistributed seul ne suffit pas, car SetReadyForDistribution
//     (PATCH /orders/{id}/distributed) pose READY_FOR_TAKE_AWAY /
//     READY_FOR_HANDOFF sans y toucher ;
//   - brand_status seul ne suffit pas non plus, car pour une commande sur
//     place (IN) il vaut 'DONE' une fois distribuée, jamais READY_*.
//
// isDistributed est le seul signal uniforme sur les trois types de commande,
// et il correspond au geste explicite du staff : le bouton de distribution de
// l'écran des commandes en cours du POS.
func resolveStatus(row boardOrderRow) string {
	if row.IsDistributed {
		return "ready"
	}
	switch row.BrandStatus.String {
	case "READY_FOR_TAKE_AWAY", "READY_FOR_HANDOFF":
		return "ready"
	default:
		return "preparing"
	}
}

// resolveLabel applique la cascade de CDS_DECISIONS.md D3 : prénom client,
// puis numéro de pager, puis numéro de commande. Le repli final existe parce
// qu'un bloc sans libellé n'a aucune valeur pour le client — il faut toujours
// afficher quelque chose.
func resolveLabel(row boardOrderRow) (label, kind string) {
	if v := strings.TrimSpace(row.CustomerFirstName.String); v != "" {
		return v, "first_name"
	}
	if v := strings.TrimSpace(row.PagerNumber.String); v != "" {
		return v, "pager"
	}
	if v := strings.TrimSpace(row.OrderNum.String); v != "" {
		return v, "order_num"
	}
	if v := strings.TrimSpace(row.BrandOrderNum.String); v != "" {
		return v, "brand_order_num"
	}
	// Quatre derniers caractères de l'order_id : illisible mais unique, et
	// préférable à une case vide sur un écran public.
	id := row.OrderID
	if len(id) > 4 {
		id = id[len(id)-4:]
	}
	return id, "fallback"
}

// ---- Paramètres vus par l'écran ----

// GetDeviceSettings renvoie à l'écran sa propre configuration.
//
// Les filtres (order_types/channels) n'y figurent pas volontairement : ils
// sont appliqués côté serveur dans la projection. Un écran n'a pas à les
// connaître, et ne doit pas pouvoir les contourner.
func (s *Service) GetDeviceSettings(ctx context.Context, cds *AuthenticatedCDS) (*DeviceSettingsResponse, error) {
	display, err := s.repo.GetDisplayForMerchant(ctx, cds.MerchantID, cds.DisplayID)
	if err != nil {
		return nil, err
	}
	if display == nil {
		return nil, models.ErrCDSNotFound
	}

	settings, err := s.repo.GetSettings(ctx, cds.DisplayID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, models.ErrCDSNotFound
	}

	resp := &DeviceSettingsResponse{
		DisplayName:        display.Name,
		LayoutMode:         settings.LayoutMode,
		PreparingZoneRatio: settings.PreparingZoneRatio,
		ShowWaitTime:       settings.ShowWaitTime,
		MarketingEnabled:   settings.MarketingEnabled,
		Media:              []MediaItemResponse{},
	}

	if settings.MarketingEnabled {
		items, err := s.repo.ListMediaItems(ctx, cds.DisplayID, true)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			resp.Media = append(resp.Media, MediaItemResponse{
				ID:              item.ID,
				Kind:            item.Kind,
				URL:             item.URL,
				QRPayload:       item.QRPayload,
				DurationSeconds: item.DurationSeconds,
				SortOrder:       item.SortOrder,
			})
		}
	}

	return resp, nil
}
