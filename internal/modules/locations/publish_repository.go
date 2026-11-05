package locations

import (
	"context"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// currentPlanRevision lit la révision publiée du plan (0 si l'établissement n'a
// jamais publié par lot). Tolérante : toute erreur — notamment l'absence de la
// table avant la migration 200 — vaut 0, pour ne jamais faire échouer
// GET /locations, qui sert aussi le POS.
func (r *LocationsRepository) currentPlanRevision(ctx context.Context, merchantID string) int64 {
	db := dbx.GetDB(ctx, r.db)

	var revision int64
	if err := db.QueryRowContext(ctx,
		`SELECT revision FROM floor_plan_revisions WHERE merchant_id = ?`, merchantID,
	).Scan(&revision); err != nil {
		return 0
	}

	return revision
}

// lockPlanRevision crée au besoin la ligne de révision de l'établissement puis
// la verrouille jusqu'à la fin de la transaction : deux publications
// concurrentes sont sérialisées, la seconde voit la révision incrémentée par la
// première et échoue en plan_revision_conflict.
func (r *LocationsRepository) lockPlanRevision(ctx context.Context, merchantID string) (int64, error) {
	db := dbx.GetDB(ctx, r.db)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO floor_plan_revisions (merchant_id, revision, updated_at)
		VALUES (?, 0, now())
		ON CONFLICT (merchant_id) DO NOTHING`, merchantID,
	); err != nil {
		return 0, err
	}

	var revision int64
	if err := db.QueryRowContext(ctx,
		`SELECT revision FROM floor_plan_revisions WHERE merchant_id = ? FOR UPDATE`, merchantID,
	).Scan(&revision); err != nil {
		return 0, err
	}

	return revision, nil
}

func (r *LocationsRepository) bumpPlanRevision(ctx context.Context, merchantID string) (int64, error) {
	db := dbx.GetDB(ctx, r.db)

	var revision int64
	err := db.QueryRowContext(ctx, `
		UPDATE floor_plan_revisions
		SET revision = revision + 1, updated_at = now()
		WHERE merchant_id = ?
		RETURNING revision`, merchantID,
	).Scan(&revision)

	return revision, err
}

// requireActiveTables vérifie que toutes les tables ciblées existent, sont
// actives et appartiennent au marchand (les routes unitaires ignorent
// silencieusement une table inconnue ; un lot doit échouer entièrement).
func (r *LocationsRepository) requireActiveTables(ctx context.Context, merchantID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	db := dbx.GetDB(ctx, r.db)

	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, merchantID)
	for _, id := range ids {
		args = append(args, id)
	}

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM locations
		 WHERE merchant_id = ? AND enabled IS TRUE
		   AND location_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`,
		args...,
	).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return models.ErrNotFound
	}

	return nil
}

// disableFloorContent désactive les obstacles et zones restants d'un étage
// supprimé. Les tables ne sont volontairement pas touchées : DeleteFloor refuse
// (floor_not_empty) un étage qui en contient encore, c'est un garde-fou.
func (r *LocationsRepository) disableFloorContent(ctx context.Context, merchantID, floorID string) error {
	db := dbx.GetDB(ctx, r.db)

	if _, err := db.ExecContext(ctx,
		`UPDATE floor_obstacles SET enabled = FALSE WHERE floor_id = ? AND merchant_id = ? AND enabled = TRUE`,
		floorID, merchantID,
	); err != nil {
		return err
	}

	_, err := db.ExecContext(ctx,
		`UPDATE floor_areas SET enabled = FALSE WHERE floor_id = ? AND enabled = TRUE`,
		floorID,
	)

	return err
}

// planPublisher porte l'état d'une publication : correspondances temp_id →
// id réel et cache des étages déjà vérifiés.
type planPublisher struct {
	repo       *LocationsRepository
	ctx        context.Context
	merchantID string

	idMap          map[string]string // tous les temp_id créés → id réel
	tempFloors     map[string]string // temp_id d'étage → id réel (références d'étage uniquement)
	deletedFloors  map[string]bool
	verifiedFloors map[string]bool
}

// resolveFloor transforme une référence d'étage du payload en id réel : un
// temp_id d'étage créé dans le même lot, ou un étage existant du marchand qui
// n'est pas supprimé par ce lot.
func (p *planPublisher) resolveFloor(ref string) (string, error) {
	if id, ok := p.tempFloors[ref]; ok {
		return id, nil
	}
	if isTempID(ref) || p.deletedFloors[ref] {
		return "", models.ErrInvalidPlanPayload
	}
	if p.verifiedFloors[ref] {
		return ref, nil
	}

	exists, err := p.repo.FloorExists(p.ctx, p.merchantID, ref)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", models.ErrFloorNotFound
	}
	p.verifiedFloors[ref] = true

	return ref, nil
}

// PublishPlan applique le différentiel d'un brouillon de plan en UNE
// transaction (tout ou rien). Ordre :
//
//  1. verrou + contrôle de révision (409 plan_revision_conflict si périmée) ;
//  2. étages créés puis renommés ;
//  3. tables, obstacles, zones créés puis modifiés (les références d'étage
//     temporaires sont résolues avec les ids que l'étape 2 vient de générer) ;
//  4. suppressions : tables, obstacles, zones, puis étages en dernier — un
//     étage ne peut disparaître qu'une fois vidé de ses tables
//     (floor_not_empty) ;
//  5. incrément de la révision.
func (r *LocationsRepository) PublishPlan(ctx context.Context, merchantID string, req PublishPlanRequest) (*PublishPlanResult, error) {
	var result *PublishPlanResult

	err := dbutils.RunInTx(ctx, r.db, func(txCtx context.Context) error {
		current, err := r.lockPlanRevision(txCtx, merchantID)
		if err != nil {
			return err
		}
		if current != req.BaseRevision {
			return models.ErrPlanRevisionConflict
		}

		p := &planPublisher{
			repo:           r,
			ctx:            txCtx,
			merchantID:     merchantID,
			idMap:          map[string]string{},
			tempFloors:     map[string]string{},
			deletedFloors:  map[string]bool{},
			verifiedFloors: map[string]bool{},
		}
		for _, id := range req.Floors.Delete {
			p.deletedFloors[id] = true
		}

		// --- Étages ---
		for _, f := range req.Floors.Create {
			id, err := r.CreateFloor(txCtx, merchantID, strings.TrimSpace(f.Name))
			if err != nil {
				return err
			}
			p.idMap[f.TempID] = id
			p.tempFloors[f.TempID] = id
		}
		for _, f := range req.Floors.Update {
			if err := r.UpdateFloor(txCtx, merchantID, f.ID, strings.TrimSpace(f.Name)); err != nil {
				return err
			}
		}

		// Tables ciblées par une mise à jour ou une suppression : existence
		// contrôlée d'un bloc.
		touchedTables := make([]string, 0, len(req.Tables.Update)+len(req.Tables.Delete))
		for _, t := range req.Tables.Update {
			touchedTables = append(touchedTables, t.ID)
		}
		touchedTables = append(touchedTables, req.Tables.Delete...)
		if err := r.requireActiveTables(txCtx, merchantID, touchedTables); err != nil {
			return err
		}

		// --- Tables ---
		for _, t := range req.Tables.Create {
			floorID, err := p.resolveFloor(t.FloorID)
			if err != nil {
				return err
			}
			id, err := r.CreateTable(txCtx, merchantID, floorID, CreateTableRequest{
				LocationName: strings.TrimSpace(t.LocationName),
				Seats:        t.Seats,
				Shape:        t.Shape,
				X:            t.X,
				Y:            t.Y,
				Width:        t.Width,
				Height:       t.Height,
				Angle:        t.Angle,
				Attributes:   t.Attributes,
			})
			if err != nil {
				return err
			}
			p.idMap[t.TempID] = id
		}
		for _, t := range req.Tables.Update {
			update := t.UpdateTableRequest
			if update.FloorID != nil {
				floorID, err := p.resolveFloor(*update.FloorID)
				if err != nil {
					return err
				}
				update.FloorID = &floorID
			}
			if err := r.UpdateTable(txCtx, merchantID, t.ID, update); err != nil {
				return err
			}
		}

		// --- Obstacles ---
		for _, o := range req.Obstacles.Create {
			floorID, err := p.resolveFloor(o.FloorID)
			if err != nil {
				return err
			}
			id, err := r.CreateObstacle(txCtx, merchantID, CreateObstacleRequest{
				FloorID:   floorID,
				Type:      o.Type,
				X:         o.X,
				Y:         o.Y,
				Width:     o.Width,
				Height:    o.Height,
				Angle:     o.Angle,
				Direction: o.Direction,
			})
			if err != nil {
				return err
			}
			p.idMap[o.TempID] = id
		}
		for _, o := range req.Obstacles.Update {
			if err := r.UpdateObstacle(txCtx, merchantID, o.ID, o.UpdateObstacleRequest); err != nil {
				return err
			}
		}

		// --- Zones ---
		for _, a := range req.Areas.Create {
			floorID, err := p.resolveFloor(a.FloorID)
			if err != nil {
				return err
			}
			id, err := r.CreateArea(txCtx, CreateAreaRequest{
				FloorID:     floorID,
				Name:        a.Name,
				StrokeColor: a.StrokeColor,
				Color:       a.Color,
				X:           a.X,
				Y:           a.Y,
				Points:      a.Points,
				Angle:       a.Angle,
			})
			if err != nil {
				return err
			}
			p.idMap[a.TempID] = id
		}
		for _, a := range req.Areas.Update {
			if err := r.UpdateArea(txCtx, merchantID, a.ID, a.UpdateAreaRequest); err != nil {
				return err
			}
		}

		// --- Suppressions ---
		for _, id := range req.Tables.Delete {
			if err := r.DeleteTable(txCtx, merchantID, id); err != nil {
				return err
			}
		}
		for _, id := range req.Obstacles.Delete {
			if err := r.DeleteObstacle(txCtx, merchantID, id); err != nil {
				return err
			}
		}
		for _, id := range req.Areas.Delete {
			if err := r.DeleteArea(txCtx, merchantID, id); err != nil {
				return err
			}
		}
		for _, id := range req.Floors.Delete {
			if err := r.disableFloorContent(txCtx, merchantID, id); err != nil {
				return err
			}
			if err := r.DeleteFloor(txCtx, merchantID, id); err != nil {
				return err
			}
		}

		revision, err := r.bumpPlanRevision(txCtx, merchantID)
		if err != nil {
			return err
		}

		result = &PublishPlanResult{Status: "1", Revision: revision, IDMap: p.idMap}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
