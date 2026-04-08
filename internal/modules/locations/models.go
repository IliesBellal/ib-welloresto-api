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
	X             *float64 `json:"x"`
	Y             *float64 `json:"y"`
	Angle         *float64 `json:"angle"`
}

type FloorCreateRequest struct {
	Name string `json:"name"`
}
