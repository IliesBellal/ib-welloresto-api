package locations

// Publication du plan de salle par lot : POST /locations/plan/publish.
//
// L'éditeur du back-office travaille en brouillon (créations, modifications et
// suppressions locales, avec des ids temporaires « tmp_… ») puis envoie le
// différentiel complet en une requête. L'API l'applique dans UNE transaction :
// tout est enregistré ou rien. Voir publish.go pour l'ordre d'exécution.
//
// Les mises à jour portent l'entité complète (pas un patch partiel), en
// particulier `type` pour un obstacle, dont la validation de `direction`
// dépend.

// TempIDPrefix préfixe tout identifiant fourni par le client pour une entité
// qui n'existe pas encore. Les identifiants réels (entiers pour les étages,
// tables et zones ; « obs-… » pour les obstacles) ne peuvent pas le porter.
const TempIDPrefix = "tmp_"

// maxPublishOperations borne la taille d'un lot (créations + mises à jour +
// suppressions, toutes entités confondues).
const maxPublishOperations = 2000

type PublishPlanRequest struct {
	// BaseRevision est la révision du plan au moment où le brouillon a été
	// chargé (champ `revision` de GET /locations).
	BaseRevision int64            `json:"base_revision"`
	Floors       PublishFloors    `json:"floors"`
	Tables       PublishTables    `json:"tables"`
	Obstacles    PublishObstacles `json:"obstacles"`
	Areas        PublishAreas     `json:"areas"`
}

type PublishFloors struct {
	Create []PublishFloorCreate `json:"create"`
	Update []PublishFloorUpdate `json:"update"`
	Delete []string             `json:"delete"`
}

type PublishFloorCreate struct {
	TempID string `json:"temp_id"`
	Name   string `json:"name"`
}

type PublishFloorUpdate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PublishTables struct {
	Create []PublishTableCreate `json:"create"`
	Update []PublishTableUpdate `json:"update"`
	Delete []string             `json:"delete"`
}

// PublishTableCreate : FloorID est un identifiant réel d'étage ou le temp_id
// d'un étage créé dans le même lot.
type PublishTableCreate struct {
	TempID       string           `json:"temp_id"`
	FloorID      string           `json:"floor_id"`
	LocationName string           `json:"location_name"`
	Seats        int              `json:"seats"`
	Shape        string           `json:"shape"`
	X            float64          `json:"x"`
	Y            float64          `json:"y"`
	Width        float64          `json:"width"`
	Height       float64          `json:"height"`
	Angle        float64          `json:"angle"`
	Attributes   *TableAttributes `json:"attributes"`
}

type PublishTableUpdate struct {
	ID string `json:"id"`
	UpdateTableRequest
}

type PublishObstacles struct {
	Create []PublishObstacleCreate `json:"create"`
	Update []PublishObstacleUpdate `json:"update"`
	Delete []string                `json:"delete"`
}

type PublishObstacleCreate struct {
	TempID    string       `json:"temp_id"`
	FloorID   string       `json:"floor_id"`
	Type      ObstacleType `json:"type"`
	X         float64      `json:"x"`
	Y         float64      `json:"y"`
	Width     float64      `json:"width"`
	Height    float64      `json:"height"`
	Angle     float64      `json:"angle"`
	Direction *float64     `json:"direction"`
}

type PublishObstacleUpdate struct {
	ID string `json:"id"`
	UpdateObstacleRequest
}

type PublishAreas struct {
	Create []PublishAreaCreate `json:"create"`
	Update []PublishAreaUpdate `json:"update"`
	Delete []string            `json:"delete"`
}

type PublishAreaCreate struct {
	TempID      string      `json:"temp_id"`
	FloorID     string      `json:"floor_id"`
	Name        string      `json:"name"`
	StrokeColor string      `json:"stroke_color"`
	Color       string      `json:"color"`
	X           float64     `json:"x"`
	Y           float64     `json:"y"`
	Points      []AreaPoint `json:"points"`
	Angle       float64     `json:"angle"`
}

type PublishAreaUpdate struct {
	ID string `json:"id"`
	UpdateAreaRequest
}

// PublishPlanResult : `id_map` associe chaque temp_id reçu à l'identifiant
// réel généré ; `revision` est la nouvelle révision du plan.
type PublishPlanResult struct {
	Status   string            `json:"status"`
	Revision int64             `json:"revision"`
	IDMap    map[string]string `json:"id_map"`
}
