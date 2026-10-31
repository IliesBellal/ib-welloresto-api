// Package idempotency exécute le traitement d'un événement de webhook une seule
// fois, sans jamais perdre un événement dont le traitement a échoué.
//
// Avant ce paquet, Uber Eats et Deliveroo marquaient l'événement « traité » dans
// Redis avant de le traiter, et ne l'effaçaient pas en cas d'échec : le webhook
// répondait 500, la plateforme réessayait avec le même identifiant, la nouvelle
// tentative était prise pour un doublon et recevait 200. La commande était perdue.
//
// Ici l'événement est réservé pour la durée du traitement, la marque longue n'est
// posée qu'en cas de succès, et la réservation est levée sur toute erreur ou
// panique : la tentative suivante de la plateforme est vraiment traitée.
package idempotency

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
)

// Store est la partie de *redis.Client dont on a besoin. Il ne distingue pas
// « la clé existe » de « Redis est indisponible » (SetNX renvoie false dans les
// deux cas) : Run relit la clé pour trancher.
type Store interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) bool
	Delete(ctx context.Context, key string) bool
}

const (
	// ClaimTTL borne la réservation d'un traitement : si le processus meurt en
	// cours de route, la plateforme peut réessayer passé ce délai.
	ClaimTTL = 5 * time.Minute

	processing = "processing"
	processed  = "processed"
)

// Run exécute fn si l'événement n'est ni en cours ni déjà traité. skipped vaut
// true quand fn n'a pas été appelée parce que l'événement est un doublon.
//
// store nil, ou Redis indisponible : fn est exécutée sans réservation (le
// contrôle de doublon en base reste le filet de sécurité).
func Run(ctx context.Context, store Store, log *zap.Logger, key string, doneTTL time.Duration, fn func() error) (skipped bool, err error) {
	if store == nil {
		return false, call(log, key, fn)
	}

	if _, found := store.Get(ctx, key); found {
		return true, nil
	}
	if !store.SetNX(ctx, key, processing, ClaimTTL) {
		if _, found := store.Get(ctx, key); found {
			return true, nil // un autre traitement vient de réserver l'événement
		}
		log.Warn("[webhook] Redis indisponible, événement traité sans réservation", zap.String("key", key))
		return false, call(log, key, fn)
	}

	if err := call(log, key, fn); err != nil {
		store.Delete(ctx, key) // la plateforme doit pouvoir réessayer
		return false, err
	}
	store.Set(ctx, key, processed, doneTTL)
	return false, nil
}

// call exécute fn en transformant une panique en erreur : le webhook répond 500
// (la plateforme réessaie) au lieu de couper la connexion.
func call(log *zap.Logger, key string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("[webhook] panique pendant le traitement", zap.String("key", key),
				zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}
