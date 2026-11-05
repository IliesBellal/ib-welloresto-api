package locations

import (
	"context"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Bornes alignées sur les colonnes : floors.name varchar(50),
// locations.location_name varchar(20). Un nom plus long ferait échouer
// l'INSERT en erreur SQL (500) ; on le refuse en amont avec un 422.
const (
	maxFloorNameLength = 50
	maxTableNameLength = 20
	maxIDLength        = 64
)

func isTempID(id string) bool {
	return strings.HasPrefix(id, TempIDPrefix)
}

func totalPublishOperations(req *PublishPlanRequest) int {
	return len(req.Floors.Create) + len(req.Floors.Update) + len(req.Floors.Delete) +
		len(req.Tables.Create) + len(req.Tables.Update) + len(req.Tables.Delete) +
		len(req.Obstacles.Create) + len(req.Obstacles.Update) + len(req.Obstacles.Delete) +
		len(req.Areas.Create) + len(req.Areas.Update) + len(req.Areas.Delete)
}

// validatePublishPlan contrôle, sans toucher à la base, tout ce qui peut être
// vérifié sur le seul payload : identifiants temporaires (préfixe « tmp_ »,
// uniques sur tout le lot), cibles de mise à jour/suppression réelles (jamais
// temporaires, jamais à la fois modifiées et supprimées), noms, et géométrie de
// chaque entité avec les mêmes règles que les routes unitaires. Les références
// qui dépendent de la base (étage existant, entité appartenant au marchand)
// sont résolues dans la transaction.
func validatePublishPlan(req *PublishPlanRequest) error {
	if req.BaseRevision < 0 || totalPublishOperations(req) > maxPublishOperations {
		return models.ErrInvalidPlanPayload
	}

	seenTemp := map[string]bool{}
	newTempID := func(id string) error {
		if !isTempID(id) || len(id) > maxIDLength || seenTemp[id] {
			return models.ErrInvalidPlanPayload
		}
		seenTemp[id] = true
		return nil
	}

	touched := map[string]bool{}
	existingID := func(kind, id string) error {
		key := kind + ":" + id
		if strings.TrimSpace(id) == "" || isTempID(id) || len(id) > maxIDLength || touched[key] {
			return models.ErrInvalidPlanPayload
		}
		touched[key] = true
		return nil
	}

	floorRef := func(id string) error {
		if strings.TrimSpace(id) == "" || len(id) > maxIDLength {
			return models.ErrInvalidPlanPayload
		}
		return nil
	}

	name := func(value string, max int) error {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || len(trimmed) > max {
			return models.ErrInvalidPlanPayload
		}
		return nil
	}

	// --- Étages ---
	for _, f := range req.Floors.Create {
		if err := newTempID(f.TempID); err != nil {
			return err
		}
		if err := name(f.Name, maxFloorNameLength); err != nil {
			return err
		}
	}
	for _, f := range req.Floors.Update {
		if err := existingID("floor", f.ID); err != nil {
			return err
		}
		if err := name(f.Name, maxFloorNameLength); err != nil {
			return err
		}
	}
	for _, id := range req.Floors.Delete {
		if err := existingID("floor", id); err != nil {
			return err
		}
	}

	// --- Tables ---
	for i := range req.Tables.Create {
		t := &req.Tables.Create[i]
		if err := newTempID(t.TempID); err != nil {
			return err
		}
		if err := floorRef(t.FloorID); err != nil {
			return err
		}
		if err := name(t.LocationName, maxTableNameLength); err != nil {
			return err
		}
		if err := validateTableGeometry(&t.X, &t.Y, &t.Width, &t.Height, &t.Angle, &t.Seats, &t.Shape); err != nil {
			return err
		}
	}
	for i := range req.Tables.Update {
		t := &req.Tables.Update[i]
		if err := existingID("table", t.ID); err != nil {
			return err
		}
		if t.FloorID != nil {
			if err := floorRef(*t.FloorID); err != nil {
				return err
			}
		}
		if t.LocationName != nil {
			if err := name(*t.LocationName, maxTableNameLength); err != nil {
				return err
			}
		}
		if err := validateTableGeometry(t.X, t.Y, t.Width, t.Height, t.TableAngle(), t.Seats, t.Shape); err != nil {
			return err
		}
	}
	for _, id := range req.Tables.Delete {
		if err := existingID("table", id); err != nil {
			return err
		}
	}

	// --- Obstacles ---
	for i := range req.Obstacles.Create {
		o := &req.Obstacles.Create[i]
		if err := newTempID(o.TempID); err != nil {
			return err
		}
		if err := floorRef(o.FloorID); err != nil {
			return err
		}
		if err := validateObstacleGeometry(&o.Type, &o.X, &o.Y, &o.Width, &o.Height, &o.Angle, o.Direction); err != nil {
			return err
		}
	}
	for i := range req.Obstacles.Update {
		o := &req.Obstacles.Update[i]
		if err := existingID("obstacle", o.ID); err != nil {
			return err
		}
		if err := validateObstacleGeometry(o.Type, o.X, o.Y, o.Width, o.Height, o.Angle, o.Direction); err != nil {
			return err
		}
	}
	for _, id := range req.Obstacles.Delete {
		if err := existingID("obstacle", id); err != nil {
			return err
		}
	}

	// --- Zones ---
	for i := range req.Areas.Create {
		a := &req.Areas.Create[i]
		if err := newTempID(a.TempID); err != nil {
			return err
		}
		if err := floorRef(a.FloorID); err != nil {
			return err
		}
		if err := validateAreaGeometry(&a.Name, &a.StrokeColor, &a.Color, &a.Points, &a.Angle); err != nil {
			return err
		}
	}
	for i := range req.Areas.Update {
		a := &req.Areas.Update[i]
		if err := existingID("area", a.ID); err != nil {
			return err
		}
		if err := validateAreaGeometry(a.Name, a.StrokeColor, a.Color, a.Points, a.Angle); err != nil {
			return err
		}
	}
	for _, id := range req.Areas.Delete {
		if err := existingID("area", id); err != nil {
			return err
		}
	}

	return nil
}

// PublishPlan applique en une transaction le différentiel d'un brouillon de
// plan de salle. Voir publish_repository.go pour le déroulé.
func (s *LocationsService) PublishPlan(ctx context.Context, token string, req PublishPlanRequest) (*PublishPlanResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := validatePublishPlan(&req); err != nil {
		return nil, err
	}

	return s.locationsRepo.PublishPlan(ctx, user.MerchantID, req)
}
