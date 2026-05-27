package refs

type SystemRef struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
	Active    bool   `json:"active"`
}
