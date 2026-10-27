package scannorder

// Hub public d'une franchise : GET /scannorder/franchises/{slug}, page
// /hub/{slug} du front ScanNOrder. Gestion au back-office : module
// franchises. Décisions : docs/franchises.md.

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

// hubStatusConcurrency : établissements dont le statut est calculé en même
// temps. Le calcul d'ouverture (jours fériés, vacances, créneaux) vit en Go et
// coûte quelques requêtes par établissement ; le pool Postgres (15
// connexions) est partagé avec toute l'API, d'où la limite. Le cache de la
// réponse (ScannorderFranchiseHubTTL) fait que ce calcul tourne au plus une
// fois par minute et par franchise, quel que soit le trafic.
const hubStatusConcurrency = 4

func (s *Service) GetFranchiseHub(ctx context.Context, slug string) (*FranchiseHubResponse, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	key := models.ScannorderFranchiseHub + slug

	if s.redis != nil {
		if cached, found := s.redis.Get(ctx, key); found {
			var resp FranchiseHubResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	resp, err := s.computeFranchiseHub(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Un slug inconnu n'est pas mis en cache : une franchise créée ou renommée
	// juste après doit apparaître tout de suite.
	if s.redis != nil && resp.Franchise != nil {
		if serialized, err := json.Marshal(resp); err == nil {
			s.redis.Set(ctx, key, string(serialized), models.ScannorderFranchiseHubTTL)
		}
	}
	return resp, nil
}

func (s *Service) computeFranchiseHub(ctx context.Context, slug string) (*FranchiseHubResponse, error) {
	franchise, rows, err := s.repo.GetFranchiseHub(ctx, slug)
	if err != nil {
		return nil, err
	}
	if franchise == nil {
		return &FranchiseHubResponse{Status: "not_found", Merchants: []FranchiseHubMerchant{}}, nil
	}

	merchants := make([]FranchiseHubMerchant, len(rows))
	var g errgroup.Group
	g.SetLimit(hubStatusConcurrency)
	for i := range rows {
		g.Go(func() error {
			merchants[i] = s.hubMerchant(ctx, rows[i])
			return nil
		})
	}
	_ = g.Wait()

	return &FranchiseHubResponse{Status: "success", Franchise: franchise, Merchants: merchants}, nil
}

// hubMerchant : un établissement du hub. Un échec du calcul de statut
// l'affiche fermé plutôt que de faire échouer tout le hub.
func (s *Service) hubMerchant(ctx context.Context, row FranchiseHubRow) FranchiseHubMerchant {
	m := FranchiseHubMerchant{
		MerchantID:   row.MerchantID,
		BusinessName: row.FullName,
		Slug:         row.Slug,
		URL:          s.cfg.SNORedirectBaseURL + "/restaurant/" + row.Slug,
		LogoURL:      row.LogoURL,
		Address: Address{
			Address: row.Address,
			City:    row.City,
			ZipCode: row.ZipCode,
			Lat:     row.Lat,
			Lng:     row.Lng,
		},
		OrderTypes: OrderTypes{
			TakeawayEnabled:   row.TakeawayEnabled,
			TakeawayAvailable: row.TakeawayAvailable,
			DeliveryEnabled:   row.DeliveryEnabled,
			DeliveryAvailable: row.DeliveryAvailable,
			InEnabled:         row.InEnabled,
			InAvailable:       row.InAvailable,
		},
	}

	now := time.Now().In(helpers.MerchantLocation(ctx, row.Timezone))
	status, err := s.repo.GetMerchantOpenState(ctx, row.MerchantID, helpers.ISOWeekday(now), now.Format("15:04:05"))
	if err != nil {
		logger.FromContext(ctx).Warn("franchise hub: merchant status failed",
			zap.String("merchant_id", row.MerchantID), zap.Error(err))
	} else {
		m.IsOpen = status.IsOpen
		if !status.IsOpen && status.NextStart != "" {
			next := status.NextStart
			m.NextOpening = &next
		}
	}

	m.PreparationTime = s.GetEffectivePrepMinutes(ctx, &models.MerchantRow{
		MerchantID:       row.MerchantID,
		Timezone:         row.Timezone,
		PrepTimeMode:     row.PrepTimeMode,
		PrepTime:         row.PrepTime,
		ExtraPrepMinutes: row.ExtraPrepMinutes,
	})

	if row.ReservationCode != nil && s.cfg.ReservationBaseURL != "" {
		reservationURL := strings.TrimRight(s.cfg.ReservationBaseURL, "/") + "/restaurant/" + url.PathEscape(*row.ReservationCode)
		m.ReservationURL = &reservationURL
	}

	m.AdvanceOrdersEnabled = row.AdvanceOrdersEnabled
	m.Discounts = []Discount{}
	discounts, err := s.repo.GetDiscounts(ctx, row.MerchantID, "", now)
	if err != nil {
		logger.FromContext(ctx).Warn("franchise hub: discounts failed",
			zap.String("merchant_id", row.MerchantID), zap.Error(err))
	} else {
		m.Discounts = onlineDiscounts(discounts)
	}

	return m
}

// onlineDiscounts garde les remises valables en commande en ligne (à emporter
// ou livraison) : le hub ne mène pas au sur place.
func onlineDiscounts(discounts []Discount) []Discount {
	out := make([]Discount, 0, len(discounts))
	for _, d := range discounts {
		types := strings.ToUpper(d.DiscountOrderType)
		if strings.TrimSpace(types) == "" || strings.Contains(types, "TAKE_AWAY") || strings.Contains(types, "DELIVERY") {
			out = append(out, d)
		}
	}
	return out
}
