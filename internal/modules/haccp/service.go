package haccp

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/audit"
	"welloresto-api/internal/utils/dbutils"
)

type Service struct {
	repo         *Repository
	auditService audit.AuditService
	db           *sql.DB
	r2Client     *r2.Client
}

func NewService(repo *Repository, auditService audit.AuditService, db *sql.DB, r2Client *r2.Client) *Service {
	return &Service{
		repo:         repo,
		auditService: auditService,
		db:           db,
		r2Client:     r2Client,
	}
}

func (s *Service) ListTemperatureZones(ctx context.Context) ([]Zone, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ListTemperatureZones(ctx, user.MerchantID)
}

func (s *Service) CreateTemperatureZone(ctx context.Context, req CreateZoneRequest) (*Zone, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, models.ErrValidationError
	}
	if req.TargetTempMin > req.TargetTempMax {
		return nil, models.ErrValidationError
	}

	return s.repo.CreateTemperatureZone(ctx, user.MerchantID, req)
}

func (s *Service) ReplaceTemperatureZone(ctx context.Context, zoneID string, req ReplaceZoneRequest) (*Zone, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(zoneID) == "" || strings.TrimSpace(req.Name) == "" {
		return nil, models.ErrValidationError
	}
	if req.TargetTempMin > req.TargetTempMax {
		return nil, models.ErrValidationError
	}

	zone, err := s.repo.ReplaceTemperatureZone(ctx, user.MerchantID, zoneID, req)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	return zone, err
}

func (s *Service) DeleteTemperatureZone(ctx context.Context, zoneID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	if strings.TrimSpace(zoneID) == "" {
		return models.ErrValidationError
	}

	err = s.repo.SoftDeleteTemperatureZone(ctx, user.MerchantID, zoneID)
	if err == sql.ErrNoRows {
		return models.ErrNotFound
	}
	return err
}

func (s *Service) ListTemperatureReadings(ctx context.Context, dateValue, zoneID string) ([]Reading, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	normalizedDate, err := normalizeTemperatureReadingsDate(dateValue, time.Now().UTC(), user.TimeZone)
	if err != nil {
		return nil, models.ErrValidationError
	}

	return s.repo.ListTemperatureReadings(ctx, user.MerchantID, normalizedDate, zoneID)
}

func (s *Service) ListActivities(ctx context.Context, params ActivitiesListParams) (*ActivitiesListResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	activityType := strings.ToLower(strings.TrimSpace(params.Type))
	if activityType != "" && activityType != ActivityTypeTemperatures && activityType != ActivityTypeCleanings {
		return nil, models.ErrValidationError
	}

	activityStatus := strings.ToLower(strings.TrimSpace(params.Status))
	if activityStatus != "" {
		switch activityStatus {
		case "ok", "alert", "critical", "done":
		default:
			return nil, models.ErrValidationError
		}
	}

	responseDate := strings.TrimSpace(params.Date)
	if responseDate == "" {
		loc := time.UTC
		if user.TimeZone != "" {
			if l, err := time.LoadLocation(user.TimeZone); err == nil {
				loc = l
			}
		}
		responseDate = time.Now().In(loc).Format("2006-01-02")
	}

	normalizedDate, err := normalizeTemperatureReadingsDate(params.Date, time.Now().UTC(), user.TimeZone)
	if err != nil {
		return nil, models.ErrValidationError
	}

	startAt, err := time.Parse("2006-01-02 15:04:05", normalizedDate)
	if err != nil {
		return nil, err
	}
	endAt := startAt.Add(24 * time.Hour)

	page := params.Page
	if page <= 0 {
		page = 1
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	items, totalItems, err := s.repo.ListActivities(ctx, user.MerchantID, startAt.UTC(), endAt.UTC(), activityType, activityStatus, page, pageSize)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	return &ActivitiesListResponse{
		Activities: items,
		Page:       page,
		PageSize:   pageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
		Date:       responseDate,
		Type:       activityType,
	}, nil
}

// normalizeTemperatureReadingsDate returns the UTC timestamp of midnight for the
// given date interpreted in the merchant's local timezone (tz, e.g. "Europe/Paris").
// When tz is empty or invalid it falls back to UTC, preserving the previous behaviour.
func normalizeTemperatureReadingsDate(raw string, now time.Time, tz string) (string, error) {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	var base time.Time
	v := strings.TrimSpace(raw)
	switch {
	case v == "":
		base = now.In(loc)
	default:
		if t, err := time.Parse("2006-01-02", v); err == nil {
			base = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		} else if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			base = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		} else if t, err := time.Parse(time.RFC3339, v); err == nil {
			base = t.In(loc)
		} else {
			return "", models.ErrValidationError
		}
	}

	// Midnight of that calendar day in the merchant's timezone, expressed in UTC.
	midnight := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, loc)
	return midnight.UTC().Format("2006-01-02 15:04:05"), nil
}

func (s *Service) CreateTemperatureReadingsBatch(ctx context.Context, req BatchCreateReadingsRequest) (*BatchCreateReadingsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if len(req.Readings) == 0 {
		return nil, models.ErrValidationError
	}

	zoneIDs := make([]string, 0, len(req.Readings))
	seen := make(map[string]struct{}, len(req.Readings))
	for _, rd := range req.Readings {
		if strings.TrimSpace(rd.ZoneID) == "" {
			return nil, models.ErrValidationError
		}
		if _, ok := seen[rd.ZoneID]; !ok {
			zoneIDs = append(zoneIDs, rd.ZoneID)
			seen[rd.ZoneID] = struct{}{}
		}
	}

	zones, err := s.repo.FindZonesByIDs(ctx, user.MerchantID, zoneIDs)
	if err != nil {
		return nil, err
	}
	if len(zones) != len(zoneIDs) {
		return nil, models.ErrValidationError
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	toInsert := make([]Reading, 0, len(req.Readings))
	for _, input := range req.Readings {
		zone := zones[input.ZoneID]
		status := computeStatus(input.Value, zone.TargetTempMin, zone.TargetTempMax)

		if settings.TempCorrectiveActions && status != "ok" {
			if input.Comment == nil || strings.TrimSpace(*input.Comment) == "" {
				return nil, models.ErrValidationError
			}
		}

		toInsert = append(toInsert, Reading{
			ZoneID:    input.ZoneID,
			Value:     input.Value,
			Status:    status,
			PhotoURL:  input.PhotoURL,
			Signature: input.Signature,
			Comment:   input.Comment,
		})
	}

	var session *TemperatureSession
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		sess, err := s.repo.CreateTemperatureSession(txCtx, user.MerchantID, user.UserID)
		if err != nil {
			return err
		}
		session = sess

		if err := s.repo.InsertTemperatureReadingsBatch(txCtx, user.MerchantID, user.UserID, session.ID, toInsert); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Audit hors transaction : ne doit jamais bloquer ni rollbacker le métier.
	s.auditService.LogChange(ctx, user.MerchantID, user.UserID, "session_validated", "haccp_temperature_session", session.ID, nil, map[string]interface{}{
		"session_id": session.ID,
		"count":      len(toInsert),
	})

	return &BatchCreateReadingsResponse{
		SessionID: session.ID,
		Readings:  toInsert,
	}, nil
}

func (s *Service) GetSettings(ctx context.Context) (*HACCPSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.GetOrCreateSettings(ctx, user.MerchantID)
}

func (s *Service) ReplaceSettings(ctx context.Context, req HACCPSettings) (*HACCPSettings, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.ReplaceSettings(ctx, user.MerchantID, req)
}

func (s *Service) UploadHACCPFile(ctx context.Context, contentType string, fileReader io.Reader) (string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return "", models.ErrUnauthorized
	}

	ext := r2.GetExtensionFromContentType(contentType)
	if ext == "" {
		return "", fmt.Errorf("invalid content type")
	}

	recordID := helpers.GeneratePrefixedID(helpers.HACCPUploadRecordIDPrefix)
	key := fmt.Sprintf("wello_resto_images_storage/merchants/%s/haccp/%s%s", user.MerchantID, recordID, ext)

	url, err := s.r2Client.UploadFile(ctx, key, fileReader, contentType)
	if err != nil {
		return "", err
	}
	return url, nil
}

func (s *Service) ListCleaningTasks(ctx context.Context) ([]CleaningTaskWithComputed, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	tasks, err := s.repo.ListCleaningTasks(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for i := range tasks {
		dueToday, overdue := computeCleaningComputed(now, tasks[i].Computed.LastExecutionAt, tasks[i].FrequencyUnit, tasks[i].FrequencyCount)
		tasks[i].Computed.DueToday = dueToday
		tasks[i].Computed.Overdue = overdue
	}

	return tasks, nil
}

func (s *Service) CreateCleaningTask(ctx context.Context, req CreateCleaningTaskRequest) (*CleaningTask, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := validateCleaningTaskFields(req.Zone, req.Name, req.FrequencyUnit, req.FrequencyCount); err != nil {
		return nil, err
	}

	return s.repo.CreateCleaningTask(ctx, user.MerchantID, req)
}

func (s *Service) UpdateCleaningTask(ctx context.Context, taskID string, req UpdateCleaningTaskRequest) (*CleaningTask, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(taskID) == "" {
		return nil, models.ErrValidationError
	}

	if err := validateCleaningTaskFields(req.Zone, req.Name, req.FrequencyUnit, req.FrequencyCount); err != nil {
		return nil, err
	}

	if req.Active == nil {
		existingTask, err := s.repo.GetCleaningTaskByID(ctx, user.MerchantID, taskID)
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		req.Active = &existingTask.Active
	}

	task, err := s.repo.UpdateCleaningTask(ctx, user.MerchantID, taskID, req)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}

	return task, err
}

func (s *Service) DeleteCleaningTask(ctx context.Context, taskID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	if strings.TrimSpace(taskID) == "" {
		return models.ErrValidationError
	}

	err = s.repo.SoftDeleteCleaningTask(ctx, user.MerchantID, taskID)
	if err == sql.ErrNoRows {
		return models.ErrNotFound
	}

	return err
}

func (s *Service) ListCleaningExecutions(ctx context.Context, params CleaningExecutionsListParams) (*CleaningExecutionsListResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	taskID := strings.TrimSpace(params.TaskID)
	if taskID != "" {
		if _, err := s.repo.GetCleaningTaskByID(ctx, user.MerchantID, taskID); err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}

	page := params.Page
	if page <= 0 {
		page = 1
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	items, totalItems, err := s.repo.ListCleaningExecutions(ctx, user.MerchantID, taskID, page, pageSize)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	resp := &CleaningExecutionsListResponse{
		CleaningExecutions: items,
		Page:               page,
		PageSize:           pageSize,
		TotalItems:         totalItems,
		TotalPages:         totalPages,
	}

	return resp, nil
}

func (s *Service) GetTemperatureSession(ctx context.Context, sessionID string) (*TemperatureSessionDetail, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(sessionID) == "" {
		return nil, models.ErrValidationError
	}

	session, err := s.repo.GetTemperatureSessionDetail(ctx, user.MerchantID, sessionID)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	session.Status = computeTemperatureSessionStatus(session.Readings)
	return session, nil
}

func (s *Service) GetCleaningExecution(ctx context.Context, executionID string) (*CleaningExecutionDetail, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(executionID) == "" {
		return nil, models.ErrValidationError
	}

	detail, err := s.repo.GetCleaningExecutionDetail(ctx, user.MerchantID, executionID)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return detail, nil
}

func (s *Service) CreateCleaningExecution(ctx context.Context, req CreateCleaningExecutionRequest) (*CleaningExecution, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(req.TaskID) == "" {
		return nil, models.ErrValidationError
	}

	task, err := s.repo.GetCleaningTaskByID(ctx, user.MerchantID, req.TaskID)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !task.Active {
		return nil, models.ErrValidationError
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	if settings.CleaningPhoto {
		if req.PhotoURL == nil || strings.TrimSpace(*req.PhotoURL) == "" {
			return nil, models.ErrCleaningPhotoRequired
		}
	}

	return s.repo.CreateCleaningExecution(ctx, user.MerchantID, user.UserID, req)
}

func (s *Service) CreateGoodsReceipt(ctx context.Context, req CreateGoodsReceiptRequest) (*GoodsReceipt, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(req.Supplier) == "" || strings.TrimSpace(req.ProductType) == "" || strings.TrimSpace(req.BatchNumber) == "" {
		return nil, models.ErrValidationError
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	if settings.ReceptionControlSample {
		if req.ControlSample == nil || strings.TrimSpace(*req.ControlSample) == "" {
			return nil, models.ErrValidationError
		}
	}

	if settings.ReceptionNonConformities && len(req.NonConformities) == 0 {
		return nil, models.ErrValidationError
	}

	if settings.ReceptionPhoto {
		if req.InvoiceURL == nil || strings.TrimSpace(*req.InvoiceURL) == "" {
			return nil, models.ErrValidationError
		}
	}

	return s.repo.CreateGoodsReceipt(ctx, user.MerchantID, user.UserID, req)
}

func computeCleaningComputed(now time.Time, lastExecutionAt *time.Time, frequencyUnit string, frequencyCount int) (bool, bool) {
	if frequencyCount <= 0 {
		frequencyCount = 1
	}

	if lastExecutionAt == nil {
		return true, false
	}

	last := lastExecutionAt.UTC()
	var nextDue time.Time

	switch strings.ToLower(strings.TrimSpace(frequencyUnit)) {
	case "week":
		nextDue = last.AddDate(0, 0, 7*frequencyCount)
	case "month":
		nextDue = last.AddDate(0, frequencyCount, 0)
	default:
		nextDue = last.AddDate(0, 0, frequencyCount)
	}

	nowDay := now.Format("2006-01-02")
	nextDay := nextDue.Format("2006-01-02")

	if nextDay < nowDay {
		return false, true
	}
	if nextDay == nowDay {
		return true, false
	}

	return false, false
}

func validateCleaningTaskFields(zone, name, frequencyUnit string, frequencyCount int) error {
	if strings.TrimSpace(zone) == "" || strings.TrimSpace(name) == "" {
		return models.ErrValidationError
	}

	unit := strings.ToLower(strings.TrimSpace(frequencyUnit))
	if unit != "day" && unit != "week" && unit != "month" {
		return models.ErrValidationError
	}

	if frequencyCount <= 0 {
		return models.ErrValidationError
	}

	return nil
}

func computeTemperatureSessionStatus(readings []Reading) string {
	status := "ok"
	for _, rd := range readings {
		switch rd.Status {
		case "critical":
			return "critical"
		case "alert":
			if status != "critical" {
				status = "alert"
			}
		}
	}
	return status
}
