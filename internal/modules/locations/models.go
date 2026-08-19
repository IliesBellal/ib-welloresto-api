package locations

import "time"

// BookingWindow porte le créneau optionnel passé à GET /locations
// (booking_date_from/booking_date_to) pour calculer, par table, un conflit
// de réservation sur ce créneau plutôt que l'occupation "maintenant" par
// défaut. ExcludeBookingID exclut la résa elle-même du calcul (cas de la
// réaffectation de tables sur une résa existante) ; vide en création.
type BookingWindow struct {
	DateFrom         time.Time
	DateTo           time.Time
	ExcludeBookingID string
}

type TableAttributes struct {
	PMR     bool `json:"pmr"`
	Terrace bool `json:"terrace"`
	VIP     bool `json:"vip"`
	Window  bool `json:"window"`
}

type CreateTableRequest struct {
	LocationName string           `json:"location_name"`
	Seats        int              `json:"seats"`
	Shape        string           `json:"shape"`
	X            float64          `json:"x"`
	Y            float64          `json:"y"`
	Width        float64          `json:"width"`
	Height       float64          `json:"height"`
	Angle        float64          `json:"angle"`
	Attributes   *TableAttributes `json:"attributes"`
}

type UpdateTableRequest struct {
	LocationName  *string          `json:"location_name"`
	LocationOrder *int             `json:"location_order"`
	FloorID       *string          `json:"floor_id"`
	Seats         *int             `json:"seats"`
	Shape         *string          `json:"shape"`
	X             *float64         `json:"x"`
	Y             *float64         `json:"y"`
	Width         *float64         `json:"width"`
	Height        *float64         `json:"height"`
	Angle         *float64         `json:"angle"`
	Rotation      *float64         `json:"rotation"`
	Enabled       *bool            `json:"enabled"`
	Attributes    *TableAttributes `json:"attributes"`
}

func (r UpdateTableRequest) TableAngle() *float64 {
	if r.Rotation != nil {
		return r.Rotation
	}

	return r.Angle
}

type FloorCreateRequest struct {
	Name string `json:"name"`
}

type FloorUpdateRequest struct {
	Name string `json:"name"`
}

type ObstacleType string

const (
	ObstacleTypeWall   ObstacleType = "wall"
	ObstacleTypeBar    ObstacleType = "bar"
	ObstacleTypeStairs ObstacleType = "stairs"
	ObstacleTypeDoor   ObstacleType = "door"
)

// Obstacle est le modèle de lecture d'un obstacle du plan de salle (mur, bar,
// escalier, porte), tel que retourné par le repository.
type Obstacle struct {
	ID        string       `json:"id"`
	FloorID   string       `json:"floor_id"`
	Type      ObstacleType `json:"type"`
	X         float64      `json:"x"`
	Y         float64      `json:"y"`
	Width     float64      `json:"width"`
	Height    float64      `json:"height"`
	Angle     float64      `json:"angle"`
	Direction *float64     `json:"direction"`
	Enabled   bool         `json:"enabled"`
}

// CreateObstacleRequest est le payload de création d'un obstacle. FloorID est
// injecté depuis le path param par le handler, pas depuis le body.
type CreateObstacleRequest struct {
	FloorID   string       `json:"-"`
	Type      ObstacleType `json:"type"`
	X         float64      `json:"x"`
	Y         float64      `json:"y"`
	Width     float64      `json:"width"`
	Height    float64      `json:"height"`
	Angle     float64      `json:"angle"`
	Direction *float64     `json:"direction"`
}

// UpdateObstacleRequest reflète CreateObstacleRequest avec des champs
// pointeurs pour supporter le PATCH partiel (même pattern que UpdateTableRequest).
type UpdateObstacleRequest struct {
	Type      *ObstacleType `json:"type"`
	X         *float64      `json:"x"`
	Y         *float64      `json:"y"`
	Width     *float64      `json:"width"`
	Height    *float64      `json:"height"`
	Angle     *float64      `json:"angle"`
	Direction *float64      `json:"direction"`
}

type AreaPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CreateAreaRequest est le payload de création d'une zone-conteneur
// (floor_area). FloorID est injecté depuis le path param par le handler, pas
// depuis le body (même pattern que CreateObstacleRequest).
type CreateAreaRequest struct {
	FloorID     string      `json:"-"`
	Name        string      `json:"name"`
	StrokeColor string      `json:"stroke_color"`
	Color       string      `json:"color"`
	X           float64     `json:"x"`
	Y           float64     `json:"y"`
	Points      []AreaPoint `json:"points"`
	Angle       float64     `json:"angle"`
}

// UpdateAreaRequest reflète CreateAreaRequest avec des champs pointeurs pour
// supporter le PATCH partiel (même pattern que UpdateObstacleRequest).
type UpdateAreaRequest struct {
	Name        *string      `json:"name"`
	StrokeColor *string      `json:"stroke_color"`
	Color       *string      `json:"color"`
	X           *float64     `json:"x"`
	Y           *float64     `json:"y"`
	Points      *[]AreaPoint `json:"points"`
	Angle       *float64     `json:"angle"`
}
