package redis

import (
	"context"
	"fmt"
	"os"
	"time"

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

// Get récupère une valeur par sa clé. Retourne ("", false, nil) si la clé n'existe pas.
func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		// Clé absente — ce n'est pas une erreur, juste un cache miss
		return "", false, nil
	}
	if err != nil {
		// Vraie erreur réseau ou Redis
		return "", false, err
	}
	return val, true, nil
}

// Set stocke une valeur avec une durée de vie (TTL)
func (c *Client) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

// Delete supprime une clé (utile pour invalider le cache)
func (c *Client) Delete(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

// DeleteAllMerchantKeys supprime toutes les clés liées à un merchant spécifique (ex: après une mise à jour des infos du merchant)
func (c *Client) DeleteAllMerchantKeys(ctx context.Context, merchantID string) error {
	pattern := fmt.Sprintf("%s%s*", models.ScannorderMerchant, merchantID)
	iter := c.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		err := c.rdb.Del(ctx, iter.Val()).Err()
		if err != nil {
			return err
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	return nil
}
