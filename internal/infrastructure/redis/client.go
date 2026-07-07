package redis

import (
	"context"
	"fmt"
	"os"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/redis/go-redis/v9"
)

// Client est le wrapper autour du client Redis officiel
type Client struct {
	rdb *redis.Client
}

// New crée et vérifie la connexion au serveur Redis
func New() (*Client, error) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL n'est pas défini dans les variables d'environnement")
	}

	// ParseURL lit automatiquement host, port, password depuis l'URL
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL invalide : %w", err)
	}

	rdb := redis.NewClient(opts)

	// On vérifie que Redis répond bien au démarrage
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("impossible de joindre Redis : %w", err)
	}

	return &Client{rdb: rdb}, nil
}

// Get récupère une valeur. Si Redis est down, on logue et on fait comme si la clé n'existait pas.
func (c *Client) Get(ctx context.Context, key string) (string, bool) {
	if c == nil || c.rdb == nil {
		return "", false
	}

	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false
	}
	if err != nil {
		// ⚠️ Ici est le secret : on logue l'erreur mais on ne la retourne pas
		logger.FromContext(ctx).Warn("⚠️ Redis Error (Get): " + err.Error())
		return "", false
	}
	return val, true
}

// Set stocke une valeur. Si ça échoue, tant pis, on continue.
func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) bool {
	if c == nil || c.rdb == nil {
		return false
	}

	err := c.rdb.Set(ctx, key, value, ttl).Err()
	if err != nil {
		logger.FromContext(ctx).Warn("⚠️ Redis Error (Set): " + err.Error())
		return false
	}
	return true
}

// Delete pareil : on ne veut pas bloquer un flow métier pour un problème de cache
func (c *Client) Delete(ctx context.Context, key string) bool {
	if c == nil || c.rdb == nil {
		return false
	}
	_ = c.rdb.Del(ctx, key).Err()
	return true
}

// InvalidateMerchantMenuCaches supprime les caches Redis dérivés du catalogue
// produits d'un merchant : menus scannorder et kiosk (toutes variantes
// deliveryType/orderType) et upsell scannorder. À appeler après toute mutation
// du menu (produit, catégorie, attribut, tag, prix…). Best-effort : les
// erreurs sont loguées mais jamais propagées, pour ne pas bloquer la mutation
// métier qui vient de réussir — au pire le cache expire par TTL.
func (c *Client) InvalidateMerchantMenuCaches(ctx context.Context, merchantID string) {
	if c == nil || c.rdb == nil || merchantID == "" {
		return
	}

	patterns := []string{
		models.ScannorderMerchantMenu + merchantID + ":*",
		models.ScannorderMerchantUpsell + merchantID,
		models.KioskMerchantMenu + merchantID + ":*",
	}
	for _, pattern := range patterns {
		if _, err := c.ScanDeleteByPattern(ctx, pattern); err != nil {
			logger.FromContext(ctx).Warn("⚠️ Redis Error (InvalidateMerchantMenuCaches): " + err.Error())
		}
	}
}


// ScanDeleteByPattern supprime toutes les clés correspondant au pattern via SCAN + DEL par batch.
// Retourne le nombre total de clés supprimées.
// Utilise SCAN avec COUNT 100 par itération pour ne pas bloquer Redis.
func (c *Client) ScanDeleteByPattern(ctx context.Context, pattern string) (int, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}

	const batchSize = 100
	var cursor uint64
	deleted := 0

	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, pattern, batchSize).Result()
		if err != nil {
			return deleted, fmt.Errorf("redis scan error for pattern %q: %w", pattern, err)
		}

		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return deleted, fmt.Errorf("redis del error for pattern %q: %w", pattern, err)
			}
			deleted += len(keys)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return deleted, nil
}
