package locations

type CreateTableRequest struct {
	LocationName string  `json:"location_name"`
	Seats        int     `json:"seats"`
	Shape        string  `json:"shape"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	Angle        float64 `json:"angle"`
}

type UpdateTableRequest struct {
	LocationName  *string  `json:"location_name"`
	LocationOrder *int     `json:"location_order"`
	FloorID       *string  `json:"floor_id"`
	Seats         *int     `json:"seats"`
	Shape         *string  `json:"shape"`
	X             *float64 `json:"x"`
	Y             *float64 `json:"y"`
	Width         *float64 `json:"width"`
	Height        *float64 `json:"height"`
	Angle         *float64 `json:"angle"`
	Rotation      *float64 `json:"rotation"`
	Enabled       *bool    `json:"enabled"`
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
