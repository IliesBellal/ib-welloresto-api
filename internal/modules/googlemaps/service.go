package googlemaps

type RouteService interface {
	GetAndLogRoute(userID, origin, destination string) ([]byte, error)
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

func (s *routeService) GetAndLogRoute(userID, origin, destination string) ([]byte, error) {
	// 1. Logger la demande (on le fait de manière asynchrone ou non selon le besoin de critique)
	// Ici on loggue avant l'appel. Si l'appel échoue, on saura quand même que l'user a essayé.
	if err := s.repo.SaveLog(userID, origin, destination); err != nil {
		// On loggue l'erreur interne mais on ne bloque pas l'utilisateur pour autant
		// log.Println("Error logging to DB:", err)
	}

	// 2. Appeler Google
	return s.client.FetchRoute(origin, destination)
}
