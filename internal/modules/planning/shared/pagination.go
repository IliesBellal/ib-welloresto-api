package shared

import (
	"strconv"
	"strings"

	"welloresto-api/internal/models"
)

const (
	DefaultPlanningPage     = 1
	DefaultPlanningPageSize = 20
	MaxPlanningPageSize     = 100
)

type PaginationParams struct {
	Page     int
	PageSize int
}

func ParsePlanningPagination(rawPage, rawPageSize string) (PaginationParams, error) {
	params := PaginationParams{}
	if strings.TrimSpace(rawPage) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(rawPage))
		if err != nil {
			return PaginationParams{}, models.ErrInvalidPage
		}
		params.Page = parsed
	}
	if strings.TrimSpace(rawPageSize) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(rawPageSize))
		if err != nil {
			return PaginationParams{}, models.ErrInvalidPageSize
		}
		params.PageSize = parsed
	}
	return NormalizePlanningPagination(params.Page, params.PageSize), nil
}

func NormalizePlanningPagination(page, pageSize int) PaginationParams {
	if page <= 0 {
		page = DefaultPlanningPage
	}
	if pageSize <= 0 {
		pageSize = DefaultPlanningPageSize
	}
	if pageSize > MaxPlanningPageSize {
		pageSize = MaxPlanningPageSize
	}
	return PaginationParams{Page: page, PageSize: pageSize}
}

func PaginationOffset(params PaginationParams) int {
	return (params.Page - 1) * params.PageSize
}

func BuildPaginationMetadata(totalItems int, params PaginationParams) models.PaginationMetadata {
	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + params.PageSize - 1) / params.PageSize
	}
	return models.PaginationMetadata{
		TotalItems:  totalItems,
		TotalPages:  totalPages,
		CurrentPage: params.Page,
		Limit:       params.PageSize,
	}
}
