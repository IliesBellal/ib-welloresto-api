package googlemaps

import (
	"log"
	"time"
)

// GoogleMapsRepository définit l'interface pour le stockage des logs
type GoogleMapsRepository interface {
	SaveLog(userID, origin, destination string) error
}

type logRepository struct {
	// Ici, tu mettrais ta connexion DB (ex: *sql.DB)
}

func NewGoogleMapsRepository() GoogleMapsRepository {
	return &logRepository{}
}

func (r *logRepository) SaveLog(userID, origin, destination string) error {
	// Simulation d'une insertion en base de données
	// Ex: INSERT INTO route_logs (user_id, origin, dest, created_at) VALUES (...)
	log.Printf("[DB LOG] User: %s a demandé un trajet de '%s' vers '%s' à %s",
		userID, origin, destination, time.Now().Format(time.RFC3339))
	return nil
}
