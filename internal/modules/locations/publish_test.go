package locations

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"welloresto-api/internal/models"
)

func f64(v float64) *float64 { return &v }
func str(v string) *string   { return &v }
func i(v int) *int           { return &v }

// validPlan : un lot cohérent qui touche chaque famille d'opérations.
func validPlan() PublishPlanRequest {
	return PublishPlanRequest{
		BaseRevision: 3,
		Floors: PublishFloors{
			Create: []PublishFloorCreate{{TempID: "tmp_f1", Name: "Terrasse"}},
			Update: []PublishFloorUpdate{{ID: "12", Name: "Salle"}},
			Delete: []string{"13"},
		},
		Tables: PublishTables{
			Create: []PublishTableCreate{{
				TempID: "tmp_t1", FloorID: "tmp_f1", LocationName: "Table 1", Seats: 2,
				Shape: "circle", X: 100, Y: 100, Width: 80, Height: 80,
			}},
			Update: []PublishTableUpdate{{ID: "40", UpdateTableRequest: UpdateTableRequest{
				LocationName: str("Table 2"), Seats: i(4), X: f64(200),
			}}},
			Delete: []string{"41"},
		},
		Obstacles: PublishObstacles{
			Create: []PublishObstacleCreate{{
				TempID: "tmp_o1", FloorID: "12", Type: ObstacleTypePlant,
				X: 50, Y: 50, Width: 40, Height: 40,
			}},
			Update: []PublishObstacleUpdate{{ID: "obs-1", UpdateObstacleRequest: UpdateObstacleRequest{
				Type: obstacleType(ObstacleTypeWall), Width: f64(900),
			}}},
			Delete: []string{"obs-2"},
		},
		Areas: PublishAreas{
			Create: []PublishAreaCreate{{
				TempID: "tmp_a1", FloorID: "12", Name: "Coin enfants",
				StrokeColor: "#64748B", Color: "#E2E8F0",
				Points: []AreaPoint{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}},
			}},
			Update: []PublishAreaUpdate{{ID: "5", UpdateAreaRequest: UpdateAreaRequest{Name: str("Bar")}}},
			Delete: []string{"6"},
		},
	}
}

func obstacleType(t ObstacleType) *ObstacleType { return &t }

func TestValidatePublishPlan_AcceptsAConsistentBatch(t *testing.T) {
	plan := validPlan()
	if err := validatePublishPlan(&plan); err != nil {
		t.Fatalf("expected a valid plan, got %v", err)
	}
}

func TestValidatePublishPlan_AcceptsAnEmptyBatch(t *testing.T) {
	plan := PublishPlanRequest{}
	if err := validatePublishPlan(&plan); err != nil {
		t.Fatalf("expected an empty plan to be valid, got %v", err)
	}
}

func TestValidatePublishPlan_Rejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(p *PublishPlanRequest)
		want   error
	}{
		{"negative base revision", func(p *PublishPlanRequest) { p.BaseRevision = -1 }, models.ErrInvalidPlanPayload},
		{"too many operations", func(p *PublishPlanRequest) {
			p.Tables.Delete = make([]string, maxPublishOperations+1)
			for n := range p.Tables.Delete {
				p.Tables.Delete[n] = "1"
			}
		}, models.ErrInvalidPlanPayload},

		{"temp id without prefix", func(p *PublishPlanRequest) { p.Floors.Create[0].TempID = "f1" }, models.ErrInvalidPlanPayload},
		{"empty temp id", func(p *PublishPlanRequest) { p.Tables.Create[0].TempID = "" }, models.ErrInvalidPlanPayload},
		{"temp id reused across entities", func(p *PublishPlanRequest) { p.Tables.Create[0].TempID = "tmp_f1" }, models.ErrInvalidPlanPayload},
		{"temp id too long", func(p *PublishPlanRequest) {
			p.Obstacles.Create[0].TempID = TempIDPrefix + strings.Repeat("x", 80)
		}, models.ErrInvalidPlanPayload},

		{"update targets a temp id", func(p *PublishPlanRequest) { p.Floors.Update[0].ID = "tmp_f1" }, models.ErrInvalidPlanPayload},
		{"delete targets a temp id", func(p *PublishPlanRequest) { p.Tables.Delete[0] = "tmp_t1" }, models.ErrInvalidPlanPayload},
		{"entity both updated and deleted", func(p *PublishPlanRequest) { p.Tables.Delete[0] = "40" }, models.ErrInvalidPlanPayload},
		{"entity deleted twice", func(p *PublishPlanRequest) { p.Areas.Delete = []string{"6", "6"} }, models.ErrInvalidPlanPayload},
		{"empty target id", func(p *PublishPlanRequest) { p.Obstacles.Delete[0] = " " }, models.ErrInvalidPlanPayload},

		{"floor name empty", func(p *PublishPlanRequest) { p.Floors.Create[0].Name = "   " }, models.ErrInvalidPlanPayload},
		{"floor name longer than the varchar(50) column", func(p *PublishPlanRequest) {
			p.Floors.Update[0].Name = strings.Repeat("a", 51)
		}, models.ErrInvalidPlanPayload},
		{"table name longer than the varchar(20) column", func(p *PublishPlanRequest) {
			p.Tables.Create[0].LocationName = strings.Repeat("a", 21)
		}, models.ErrInvalidPlanPayload},
		{"table update with an empty name", func(p *PublishPlanRequest) { p.Tables.Update[0].LocationName = str(" ") }, models.ErrInvalidPlanPayload},
		{"table without floor", func(p *PublishPlanRequest) { p.Tables.Create[0].FloorID = "" }, models.ErrInvalidPlanPayload},

		{"table out of the canvas", func(p *PublishPlanRequest) { p.Tables.Create[0].X = 1200 }, models.ErrInvalidTableGeometry},
		{"table too small", func(p *PublishPlanRequest) { p.Tables.Create[0].Width = 10 }, models.ErrInvalidTableGeometry},
		{"table without seats", func(p *PublishPlanRequest) { p.Tables.Create[0].Seats = 0 }, models.ErrInvalidTableGeometry},
		{"table with unknown shape", func(p *PublishPlanRequest) { p.Tables.Create[0].Shape = "hexagon" }, models.ErrInvalidTableGeometry},
		{"table update out of bounds", func(p *PublishPlanRequest) { p.Tables.Update[0].Y = f64(-5) }, models.ErrInvalidTableGeometry},

		{"obstacle with unknown type", func(p *PublishPlanRequest) { p.Obstacles.Create[0].Type = "banana" }, models.ErrInvalidObstacleGeometry},
		{"obstacle larger than the canvas", func(p *PublishPlanRequest) { p.Obstacles.Create[0].Width = 1001 }, models.ErrInvalidObstacleGeometry},
		{"direction on a non-door obstacle", func(p *PublishPlanRequest) { p.Obstacles.Create[0].Direction = f64(90) }, models.ErrInvalidObstacleGeometry},

		{"area with fewer than 3 points", func(p *PublishPlanRequest) {
			p.Areas.Create[0].Points = p.Areas.Create[0].Points[:2]
		}, models.ErrInvalidAreaGeometry},
		{"area with a bad color", func(p *PublishPlanRequest) { p.Areas.Create[0].Color = "red" }, models.ErrInvalidAreaGeometry},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlan()
			tc.mutate(&plan)

			err := validatePublishPlan(&plan)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidatePublishPlan_AcceptsTheNewObstacleTypesAndLongWalls(t *testing.T) {
	for _, typ := range []ObstacleType{
		ObstacleTypeWall, ObstacleTypeDoor, ObstacleTypeWindow, ObstacleTypeStairs,
		ObstacleTypePlant, ObstacleTypeBar, ObstacleTypePillar,
	} {
		plan := PublishPlanRequest{Obstacles: PublishObstacles{Create: []PublishObstacleCreate{{
			TempID: "tmp_o", FloorID: "1", Type: typ, X: 10, Y: 10, Width: 1000, Height: 12,
		}}}}
		if err := validatePublishPlan(&plan); err != nil {
			t.Errorf("type %q: unexpected error %v", typ, err)
		}
	}
}

// Le front distingue un conflit de révision (recharger le plan) d'un payload
// invalide (bug) à leur code HTTP et à leur champ `status` : contrat figé ici.
func TestPublishPlanErrorsMapToHTTPContract(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{models.ErrPlanRevisionConflict, http.StatusConflict, "plan_revision_conflict"},
		{models.ErrInvalidPlanPayload, http.StatusUnprocessableEntity, "invalid_plan_payload"},
		{models.ErrFloorNotEmpty, http.StatusConflict, "floor_not_empty"},
	}

	for _, tc := range cases {
		t.Run(tc.wantCode, func(t *testing.T) {
			rec := httptest.NewRecorder()
			models.SendErrorJSON(rec, "locations", "publish_plan", tc.err)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			var body struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
			}
			if body.Data.Status != tc.wantCode {
				t.Fatalf("data.status = %q, want %q (body: %s)", body.Data.Status, tc.wantCode, rec.Body.String())
			}
		})
	}
}
