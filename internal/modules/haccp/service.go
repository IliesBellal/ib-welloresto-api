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
		if settings.TempFailurePhotoRequired && status != "ok" {
			if input.PhotoURL == nil || strings.TrimSpace(*input.PhotoURL) == "" {
				return nil, models.ErrTemperatureFailurePhotoRequired
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

func (s *Service) ListCleaningZones(ctx context.Context) ([]CleaningZoneWithSurfaces, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	zones, err := s.repo.ListCleaningZones(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	return s.enrichZonesWithSurfaces(ctx, user.MerchantID, zones)
}

func (s *Service) CreateCleaningZone(ctx context.Context, req CreateCleaningZoneRequest) (*CleaningZoneWithSurfaces, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := validateCleaningZoneName(req.Name); err != nil {
		return nil, err
	}

	zone, err := s.repo.CreateCleaningZone(ctx, user.MerchantID, req)
	if err != nil {
		return nil, err
	}

	return s.enrichSingleZoneWithSurfaces(ctx, user.MerchantID, *zone)
}

func (s *Service) UpdateCleaningZone(ctx context.Context, zoneID string, req UpdateCleaningZoneRequest) (*CleaningZoneWithSurfaces, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(zoneID) == "" {
		return nil, models.ErrValidationError
	}
	if err := validateCleaningZoneName(req.Name); err != nil {
		return nil, err
	}

	zone, err := s.repo.UpdateCleaningZone(ctx, user.MerchantID, zoneID, req)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return s.enrichSingleZoneWithSurfaces(ctx, user.MerchantID, *zone)
}

func (s *Service) DeleteCleaningZone(ctx context.Context, zoneID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	if strings.TrimSpace(zoneID) == "" {
		return models.ErrValidationError
	}

	err = s.repo.SoftDeleteCleaningZone(ctx, user.MerchantID, zoneID)
	if err == sql.ErrNoRows {
		return models.ErrNotFound
	}
	return err
}

func (s *Service) enrichSingleZoneWithSurfaces(ctx context.Context, merchantID string, zone CleaningZone) (*CleaningZoneWithSurfaces, error) {
	surfaces, err := s.repo.ListCleaningSurfaces(ctx, merchantID, zone.ID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for i := range surfaces {
		dueToday, overdue := computeCleaningComputed(now, surfaces[i].Computed.LastExecutionAt, surfaces[i].FrequencyUnit, surfaces[i].FrequencyCount)
		surfaces[i].Computed.DueToday = dueToday
		surfaces[i].Computed.Overdue = overdue
	}

	return &CleaningZoneWithSurfaces{
		ID:         zone.ID,
		MerchantID: zone.MerchantID,
		Name:       zone.Name,
		Enabled:    zone.Enabled,
		CreatedAt:  zone.CreatedAt,
		UpdatedAt:  zone.UpdatedAt,
		DeletedAt:  zone.DeletedAt,
		Surfaces:   surfaces,
	}, nil
}

func (s *Service) enrichZonesWithSurfaces(ctx context.Context, merchantID string, zones []CleaningZone) ([]CleaningZoneWithSurfaces, error) {
	surfaces, err := s.repo.ListCleaningSurfaces(ctx, merchantID, "")
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	byZone := make(map[string][]CleaningSurfaceWithComputed, len(zones))
	for i := range surfaces {
		dueToday, overdue := computeCleaningComputed(now, surfaces[i].Computed.LastExecutionAt, surfaces[i].FrequencyUnit, surfaces[i].FrequencyCount)
		surfaces[i].Computed.DueToday = dueToday
		surfaces[i].Computed.Overdue = overdue
		byZone[surfaces[i].ZoneID] = append(byZone[surfaces[i].ZoneID], surfaces[i])
	}

	out := make([]CleaningZoneWithSurfaces, 0, len(zones))
	for _, zone := range zones {
		zoneSurfaces := byZone[zone.ID]
		if zoneSurfaces == nil {
			zoneSurfaces = make([]CleaningSurfaceWithComputed, 0)
		}
		out = append(out, CleaningZoneWithSurfaces{
			ID:         zone.ID,
			MerchantID: zone.MerchantID,
			Name:       zone.Name,
			Enabled:    zone.Enabled,
			CreatedAt:  zone.CreatedAt,
			UpdatedAt:  zone.UpdatedAt,
			DeletedAt:  zone.DeletedAt,
			Surfaces:   zoneSurfaces,
		})
	}

	return out, nil
}

func (s *Service) ListCleaningSurfaces(ctx context.Context, zoneID string) ([]CleaningSurfaceWithComputed, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	zoneID = strings.TrimSpace(zoneID)
	if zoneID != "" {
		if _, err := s.repo.GetCleaningZoneByID(ctx, user.MerchantID, zoneID); err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}

	surfaces, err := s.repo.ListCleaningSurfaces(ctx, user.MerchantID, zoneID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for i := range surfaces {
		dueToday, overdue := computeCleaningComputed(now, surfaces[i].Computed.LastExecutionAt, surfaces[i].FrequencyUnit, surfaces[i].FrequencyCount)
		surfaces[i].Computed.DueToday = dueToday
		surfaces[i].Computed.Overdue = overdue
	}

	return surfaces, nil
}

func (s *Service) CreateCleaningSurface(ctx context.Context, req CreateCleaningSurfaceRequest) (*CleaningSurface, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if err := validateCleaningSurfaceFields(req.ZoneID, req.Name, req.FrequencyUnit, req.FrequencyCount); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetCleaningZoneByID(ctx, user.MerchantID, req.ZoneID); err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	} else if err != nil {
		return nil, err
	}

	return s.repo.CreateCleaningSurface(ctx, user.MerchantID, req)
}

func (s *Service) UpdateCleaningSurface(ctx context.Context, surfaceID string, req UpdateCleaningSurfaceRequest) (*CleaningSurface, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(surfaceID) == "" {
		return nil, models.ErrValidationError
	}
	if err := validateCleaningSurfaceFields(req.ZoneID, req.Name, req.FrequencyUnit, req.FrequencyCount); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetCleaningZoneByID(ctx, user.MerchantID, req.ZoneID); err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	} else if err != nil {
		return nil, err
	}

	if req.Active == nil {
		existingSurface, err := s.repo.GetCleaningSurfaceByID(ctx, user.MerchantID, surfaceID)
		if err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		req.Active = &existingSurface.Active
	}

	surface, err := s.repo.UpdateCleaningSurface(ctx, user.MerchantID, surfaceID, req)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	return surface, err
}

func (s *Service) DeleteCleaningSurface(ctx context.Context, surfaceID string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return models.ErrUnauthorized
	}

	if strings.TrimSpace(surfaceID) == "" {
		return models.ErrValidationError
	}

	err = s.repo.SoftDeleteCleaningSurface(ctx, user.MerchantID, surfaceID)
	if err == sql.ErrNoRows {
		return models.ErrNotFound
	}
	return err
}

func (s *Service) ListCleaningSessions(ctx context.Context, params CleaningSessionsListParams) ([]CleaningSessionListItem, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	zoneID := strings.TrimSpace(params.ZoneID)
	if zoneID != "" {
		if _, err := s.repo.GetCleaningZoneByID(ctx, user.MerchantID, zoneID); err == sql.ErrNoRows {
			return nil, models.ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}

	normalizedDate, err := normalizeTemperatureReadingsDate(params.Date, time.Now().UTC(), user.TimeZone)
	if err != nil {
		return nil, models.ErrValidationError
	}

	return s.repo.ListCleaningSessions(ctx, user.MerchantID, normalizedDate, zoneID)
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

func (s *Service) GetCleaningSession(ctx context.Context, sessionID string) (*CleaningSessionDetail, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if strings.TrimSpace(sessionID) == "" {
		return nil, models.ErrValidationError
	}

	detail, err := s.repo.GetCleaningSessionDetail(ctx, user.MerchantID, sessionID)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return detail, nil
}

func (s *Service) CreateCleaningSession(ctx context.Context, req CreateCleaningSessionRequest) (*BatchCreateCleaningSessionResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	if len(req.Executions) == 0 {
		return nil, models.ErrValidationError
	}

	surfaceIDs := make([]string, 0, len(req.Executions))
	seen := make(map[string]struct{}, len(req.Executions))
	for _, execution := range req.Executions {
		surfaceID := strings.TrimSpace(execution.SurfaceID)
		if surfaceID == "" {
			return nil, models.ErrValidationError
		}
		if _, exists := seen[surfaceID]; exists {
			return nil, models.ErrValidationError
		}
		seen[surfaceID] = struct{}{}
		surfaceIDs = append(surfaceIDs, surfaceID)
	}

	surfaces, err := s.repo.FindCleaningSurfacesByIDs(ctx, user.MerchantID, surfaceIDs)
	if err != nil {
		return nil, err
	}
	if len(surfaces) != len(surfaceIDs) {
		return nil, models.ErrValidationError
	}

	for _, surface := range surfaces {
		if !surface.Active {
			return nil, models.ErrValidationError
		}
	}

	settings, err := s.repo.GetOrCreateSettings(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	toInsert := make([]CleaningExecution, 0, len(req.Executions))
	for _, input := range req.Executions {
		if settings.CleaningPhoto {
			if input.PhotoURL == nil || strings.TrimSpace(*input.PhotoURL) == "" {
				return nil, models.ErrCleaningPhotoRequired
			}
		}

		surface := surfaces[input.SurfaceID]
		toInsert = append(toInsert, CleaningExecution{
			SurfaceID:   surface.ID,
			SurfaceName: surface.Name,
			ZoneID:      surface.ZoneID,
			ZoneName:    surface.ZoneName,
			Comment:     input.Comment,
			PhotoURL:    input.PhotoURL,
		})
	}

	var session *CleaningSession
	err = dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		sess, err := s.repo.CreateCleaningSession(txCtx, user.MerchantID, user.UserID)
		if err != nil {
			return err
		}
		session = sess

		if err := s.repo.InsertCleaningExecutionsBatch(txCtx, user.MerchantID, user.UserID, session.ID, toInsert); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	s.auditService.LogChange(ctx, user.MerchantID, user.UserID, "session_validated", "haccp_cleaning_session", session.ID, nil, map[string]interface{}{
		"session_id": session.ID,
		"count":      len(toInsert),
	})

	for i := range toInsert {
		toInsert[i].SessionID = session.ID
		toInsert[i].MerchantID = user.MerchantID
		toInsert[i].Status = "done"
		toInsert[i].CreatedBy = user.UserID
		toInsert[i].PerformedBy = ActivityPerformedBy{ID: user.UserID, Name: user.UserID}
	}

	return &BatchCreateCleaningSessionResponse{
		SessionID:  session.ID,
		Executions: toInsert,
	}, nil
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

func validateCleaningZoneName(name string) error {
	if strings.TrimSpace(name) == "" {
		return models.ErrValidationError
	}

	return nil
}

func validateCleaningSurfaceFields(zoneID, name, frequencyUnit string, frequencyCount int) error {
	if strings.TrimSpace(zoneID) == "" || strings.TrimSpace(name) == "" {
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
