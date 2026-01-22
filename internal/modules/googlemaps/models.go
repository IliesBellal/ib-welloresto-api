package googlemaps

import "encoding/json"

// GoogleResponse agit comme un proxy transparent pour le JSON de Google
type GoogleResponse json.RawMessage

type RouteRequest struct {
	Origin      string
	Destination string
	UserID      string // Pour le logging
}
