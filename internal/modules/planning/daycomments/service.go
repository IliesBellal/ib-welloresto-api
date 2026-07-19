package daycomments

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	auditpkg "welloresto-api/internal/modules/audit"
	sharedpkg "welloresto-api/internal/modules/planning/shared"
)

// maxDayCommentRangeDays mirrors schedule.maxShiftRangeDays: a two-month
// window is enough to paint a back-office grid (week/month views) in one
// call without allowing unbounded range scans.
const maxDayCommentRangeDays = 62

type Service struct {
	repo         *Repository
	auditService auditpkg.AuditService
}

func NewService(repo *Repository, auditService auditpkg.AuditService) *Service {
	return &Service{repo: repo, auditService: auditService}
}

func (s *Service) ListByDateRange(ctx context.Context, startDateRaw, endDateRaw string) ([]PlanningDayComment, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	startDate, endDate, err := sharedpkg.ParsePlanningDateRange(startDateRaw, endDateRaw)
	if err != nil {
		return nil, err
	}

	rangeDays := int(endDate.Sub(startDate).Hours()/24) + 1
	if rangeDays > maxDayCommentRangeDays {
		return nil, models.ErrPlanningInvalidDate
	}

	return s.repo.ListByDateRange(ctx, user.MerchantID, startDate, endDate)
}

func (s *Service) Upsert(ctx context.Context, dateRaw string, req PlanningDayCommentUpsertRequest) (*PlanningDayComment, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	commentDate, err := sharedpkg.ParsePlanningDate(dateRaw)
	if err != nil {
		return nil, err
	}

	comment := strings.TrimSpace(req.Comment)
	if comment == "" {
		return nil, models.ErrValidationError
	}
	if len([]rune(comment)) > MaxCommentLength {
		return nil, models.ErrPlanningDayCommentTooLong
	}

	before, err := s.repo.GetByDate(ctx, user.MerchantID, commentDate)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	after, err := s.repo.Upsert(ctx, user.MerchantID, commentDate, comment, user.UserID)
	if err != nil {
		return nil, err
	}

	s.logChange(ctx, user.MerchantID, user.UserID, before, after)
	return after, nil
}

func (s *Service) Delete(ctx context.Context, dateRaw string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	commentDate, err := sharedpkg.ParsePlanningDate(dateRaw)
	if err != nil {
		return err
	}

	before, err := s.repo.GetByDate(ctx, user.MerchantID, commentDate)
	if err == sql.ErrNoRows {
		return models.ErrPlanningDayCommentNotFound
	}
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, user.MerchantID, commentDate); err != nil {
		if err == sql.ErrNoRows {
			return models.ErrPlanningDayCommentNotFound
		}
		return err
	}

	s.logChange(ctx, user.MerchantID, user.UserID, before, nil)
	return nil
}

func (s *Service) logChange(ctx context.Context, merchantID, userID string, before, after *PlanningDayComment) {
	if s.auditService == nil {
		return
	}

	action := "update"
	resourceID := ""
	switch {
	case before == nil && after != nil:
		action = "create"
		resourceID = after.ID
	case after == nil && before != nil:
		action = "delete"
		resourceID = before.ID
	case after != nil:
		resourceID = after.ID
	case before != nil:
		resourceID = before.ID
	}

	_ = s.auditService.LogChange(ctx, merchantID, userID, action, "planning_day_comment", resourceID,
		snapshotForAudit(before), snapshotForAudit(after),
	)
}

func snapshotForAudit(item *PlanningDayComment) map[string]any {
	if item == nil {
		return nil
	}
	return map[string]any{"comment_date": item.CommentDate.String(), "comment": item.Comment}
}
