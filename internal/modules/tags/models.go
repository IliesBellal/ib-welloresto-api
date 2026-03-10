package tags

import (
	"welloresto-api/internal/models"
)

// CreateTagRequest is the DTO for creating a new tag.
type CreateTagRequest struct {
	Name string `json:"name"`
}

// Validate checks that the tag name is valid.
func (r *CreateTagRequest) Validate() error {
	if len(r.Name) == 0 {
		return models.ErrInvalidInput
	}
	if len(r.Name) > 100 {
		return models.ErrInvalidInput
	}
	return nil
}
