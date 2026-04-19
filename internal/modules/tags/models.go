package tags

import (
	"welloresto-api/internal/models"
)

// CreateTagRequest is the DTO for creating a new tag.
type CreateTagRequest struct {
	ID    *string `json:"id,omitempty"` // ID is optional in the request, will be generated if not provided
	Name  string  `json:"name"`
	Color *string `json:"color,omitempty"`
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

// TagDisplayOrderItem represents a tag in the display order request
type TagDisplayOrderItem struct {
	ID string `json:"id"`
}

// UpdateTagsDisplayOrderRequest is the DTO for updating tags display order
type UpdateTagsDisplayOrderRequest struct {
	Tags []TagDisplayOrderItem `json:"tags"`
}

// Validate checks that the request is valid
func (r *UpdateTagsDisplayOrderRequest) Validate() error {
	if len(r.Tags) == 0 {
		return models.ErrInvalidInput
	}
	for _, tag := range r.Tags {
		if len(tag.ID) == 0 {
			return models.ErrInvalidInput
		}
	}
	return nil
}

// UpdateTagRequest is the DTO for updating a tag.
type UpdateTagRequest struct {
	Name         *string `json:"name,omitempty"`
	Color        *string `json:"color,omitempty"`
	DisplayOrder *int    `json:"display_order,omitempty"`
}

// Validate checks that the request is valid
func (r *UpdateTagRequest) Validate() error {
	if r.Name != nil && len(*r.Name) == 0 {
		return models.ErrInvalidInput
	}
	if r.Name != nil && len(*r.Name) > 100 {
		return models.ErrInvalidInput
	}
	if r.DisplayOrder != nil && *r.DisplayOrder < 0 {
		return models.ErrInvalidInput
	}
	return nil
}
