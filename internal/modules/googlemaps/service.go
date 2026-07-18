package googlemaps

import (
	"context"

	"welloresto-api/internal/logger"
)

type RouteService interface {
	GetAndLogRoute(ctx context.Context, userID, merchantID, origin, destination string) ([]byte, error)
}

type routeService struct {
	repo   GoogleMapsRepository
	client GoogleMapsClient
}

func NewRouteService(repo GoogleMapsRepository, client GoogleMapsClient) RouteService {
	return &routeService{
		repo:   repo,
		client: client,
	}
}

func (s *routeService) GetAndLogRoute(ctx context.Context, userID, merchantID, origin, destination string) ([]byte, error) {
	// 1. Logger la demande (on le fait de manière asynchrone ou non selon le besoin de critique)
	// Ici on loggue avant l'appel. Si l'appel échoue, on saura quand même que l'user a essayé.
	if err := s.repo.SaveLog(userID, origin, destination); err != nil {
		// On loggue l'erreur interne mais on ne bloque pas l'utilisateur pour autant
		// log.Println("Error logging to DB:", err)
	}

	// 2. Appeler Google
	result, err := s.client.FetchRoute(origin, destination)
	if err != nil {
		return nil, err
	}

	// 3. Comptabiliser l'appel abouti, par merchant/mois (fire-and-forget, ne bloque jamais la réponse).
	s.recordCallAsync(merchantID)

	return result, nil
}

func (s *routeService) recordCallAsync(merchantID string) {
	go func() {
		ctx := context.Background()
		log := logger.FromContext(ctx)

		if err := s.repo.RecordGoogleMapsCall(ctx, merchantID, 1); err != nil {
			log.Error("GetAndLogRoute: failed to record google maps call: " + err.Error())
		}
	}()
}
