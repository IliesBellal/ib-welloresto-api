// Package importer porte le modele canonique de l'import de produits en masse
// et les adaptateurs qui traduisent un fichier source vers ce modele.
//
// Trois portes d'entree convergent vers un seul pipeline : un provider tiers
// (Zelty en premier), un template .xlsx defini par Wello, et un formulaire de
// saisie en masse cote back-office. Toutes produisent un *IntermediateImport,
// consomme ensuite par la preview (dry-run) puis par le commit atomique.
//
// Invariant de ce package : le parser ne decide rien. Il ne resout pas la TVA
// (les taux bruts sont conserves tels quels, 0 compris), il ne classe pas les
// libelles source en categorie ou en tag, il ne dedoublonne pas contre
// l'existant en base et il ne touche pas la base du tout. Ces arbitrages sont
// pris dans la preview et redescendent via ImportDecisions.
package importer

import (
	"fmt"
	"strconv"
	"strings"
)

// AttributeTypeCheck est la seule valeur d'attribute_type reellement emise par
// le chemin menu (le back-office l'envoie en dur, la distinction radio/checkbox
// etant derivee de min == max == 1). L'import s'aligne dessus.
const AttributeTypeCheck = "CHECK"

// IntermediateImport est le resultat d'un parse : la representation neutre du
// fichier source, avant toute decision.
type IntermediateImport struct {
	// Provider est le slug du provider qui a produit cet import.
	Provider string

	// Categories ne contient que les categories que la source designe
	// explicitement comme telles (colonne dediee du template). Pour Zelty
	// elle reste vide : l'export n'a qu'une notion de tag, et la promotion
	// d'un libelle en categorie est une decision de la preview.
	Categories []CanonicalCategory

	// Tags contient tous les libelles source, categories potentielles
	// comprises. Chaque entree doit finir creee cote Wello (comme categorie
	// ou comme tag) selon ImportDecisions.TagClassification.
	Tags []CanonicalTag

	Products []CanonicalProduct

	// Attributes sont les groupes d'options. En V1a (fichier/saisie) ils sont
	// crees non rattaches (product_id = 0) : les exports ne portent aucun lien
	// produit -> option, le rattachement se fait ensuite via la matrice du
	// back-office. La porte "autre etablissement" (V1b) est la premiere source
	// a fournir CanonicalProduct.AttributeExternalIDs et referme ce manque.
	Attributes []CanonicalAttribute

	// ComponentCategories et Components n'existent que pour la porte "autre
	// etablissement" (V1b, cf. import_merchant_repository.go) : aucun format
	// fichier connu ne porte de composition. Vides pour toute autre source,
	// ce qui laisse la preview et le commit inchanges sur ce chemin.
	ComponentCategories []CanonicalComponentCategory
	Components          []CanonicalComponent
}

// CanonicalComponentCategory est une categorie d'ingredient (component_category).
type CanonicalComponentCategory struct {
	ExternalID string
	Name       string
}

// CanonicalComponent est un ingredient (components). UnitOfMeasureID et
// PurchaseUnitOfMeasureID referencent unit_of_measure, une table globale (pas
// de merchant_id) : ils passent donc tels quels d'un marchand a l'autre, sans
// resolution — a la difference de CategoryExternalID, qui doit etre
// redeclaree cote destination comme toute categorie de cet import.
type CanonicalComponent struct {
	ExternalID string
	Name       string

	// CategoryExternalID reference ComponentCategories.ExternalID. Vide =
	// aucune categorie (component_category.merchant_categ_id est cependant
	// requis en base : un composant sans categorie source est un cas que la
	// porte "autre etablissement" ne devrait pas produire, la source ayant
	// elle-meme une categorie pour chaque composant actif).
	CategoryExternalID string

	UnitOfMeasureID         string
	PurchaseUnitOfMeasureID string

	Price            int // centimes, prix de vente si le composant est facture
	PurchaseCost     int // centimes
	PurchaseCostQty  float64
	ConservationDays *int
	ConservationType string
	StorageTempMin   *float64
	StorageTempMax   *float64
}

// CanonicalCategory est une categorie caisse (productcateg) designee
// explicitement par la source.
type CanonicalCategory struct {
	ExternalID string
	Name       string
}

// CanonicalTag est un libelle source. Il deviendra une categorie caisse ou un
// tag Wello selon la classification decidee en preview.
type CanonicalTag struct {
	ExternalID string
	Name       string

	// Synthetic marque un libelle que la source n'a pas declare : un produit
	// le cite, mais aucune ligne ne le definit. Le parser le fabrique pour ne
	// pas perdre l'information ; la preview le signale, car c'est souvent le
	// symptome d'un export tronque.
	Synthetic bool
}

// CanonicalProduct est un produit du fichier source.
type CanonicalProduct struct {
	ExternalID  string
	Name        string
	Description string

	// Prix en centimes entiers, par canal. Une source qui n'expose qu'un
	// prix unique (Zelty) le recopie sur les trois.
	PriceIn       int
	PriceTakeAway int
	PriceDelivery int

	// Taux de TVA bruts en pourcentage, tels que lus dans le fichier.
	// nil = colonne absente ou cellule vide ; 0 = taux explicitement nul,
	// ce qui vaut desactivation du canal (decide en preview, pas ici).
	// La resolution taux -> tva_categories.tva_id appartient a la preview.
	TvaRateIn       *float64
	TvaRateTakeAway *float64
	TvaRateDelivery *float64

	// CategoryExternalID est la categorie explicite de la source, quand elle
	// en donne une (colonne Categorie du template). Vide pour Zelty : la
	// categorie est alors resolue en preview a partir de la classification
	// des libelles, le defaut propose etant le premier de TagExternalIDs.
	CategoryExternalID string

	// TagExternalIDs sont les libelles du produit, dans l'ordre du fichier.
	// Ils referencent IntermediateImport.Tags.
	TagExternalIDs []string

	// AllPricesZero marque les lignes sans prix (frais de livraison, frais de
	// service...). Elles sont importees quand meme, en statut
	// removed_from_menu.
	AllPricesZero bool

	// Components et AttributeExternalIDs n'existent que pour la porte "autre
	// etablissement" (V1b) : composition (recettes) et rattachement d'options,
	// que ni le fichier ni la saisie manuelle ne portent. Vides ailleurs.
	Components           []CanonicalProductComponent
	AttributeExternalIDs []string

	// AvailableIn/TakeAway/Delivery : nil = comportement historique inchangé
	// (le commit pose TRUE sur les trois canaux, seule valeur qu'un fichier ou
	// une saisie manuelle puisse vouloir — aucun des deux n'expose ces trois
	// cases). La porte "autre etablissement" les renseigne en recopiant ce que
	// la source a reellement configure.
	AvailableIn       *bool
	AvailableTakeAway *bool
	AvailableDelivery *bool
}

// CanonicalProductComponent est une ligne de composition (requires) : quelle
// quantite d'un ingredient entre dans un produit.
type CanonicalProductComponent struct {
	ComponentExternalID string // reference IntermediateImport.Components.ExternalID
	Quantity            float64
	UnitOfMeasureID     string // unit_of_measure, table globale — passe tel quel

	// Disponibilite de cette ligne de composition par canal. Le chemin
	// unitaire (SyncProductComponents) ne les ecrit pas aujourd'hui — la
	// colonne garde son defaut TRUE — mais la porte "autre etablissement"
	// copie ce que la source a reellement configure plutot que d'ecraser
	// silencieusement un canal desactive.
	InOrders, TakeAwayOrders, DeliveryOrders bool
}

// CanonicalAttribute est un groupe d'options.
type CanonicalAttribute struct {
	ExternalID string
	Name       string

	// Type, MinOptions, MaxOptions et IsRequired ne figurent dans aucun
	// export connu : ils sont poses par applyDefaults.
	Type       string
	MinOptions int
	MaxOptions int
	IsRequired bool

	Options []CanonicalOption
}

// CanonicalOption est une valeur d'option ; ExtraPrice est le supplement.
type CanonicalOption struct {
	ExternalID string
	Title      string
	ExtraPrice int // centimes

	// ComponentExternalID est le lien ingredient (projection de cout) porte
	// par configurable_attribute_options.component_id. "" = aucun lien —
	// c'est ce que toute source autre que "autre etablissement" produit
	// aujourd'hui, aucun format fichier ne l'exprimant.
	ComponentExternalID string
	Quantity            float64
	UnitOfMeasureID     string
}

// applyDefaults pose la configuration d'un groupe d'options que les exports ne
// fournissent pas. max_options est NOT NULL sans defaut cote base : le laisser
// a zero rendrait le groupe inselectionnable, on l'ouvre donc a toutes ses
// options. min_options a 0 et is_required a false gardent le groupe facultatif,
// seule hypothese sure en l'absence d'information source.
func (a *CanonicalAttribute) applyDefaults() {
	a.Type = AttributeTypeCheck
	a.MinOptions = 0
	a.MaxOptions = len(a.Options)
	a.IsRequired = false
}

// TagClass est le sort reserve a un libelle source : devenir une categorie
// caisse ou un tag Wello.
type TagClass string

const (
	TagClassCategory TagClass = "category"
	TagClassTag      TagClass = "tag"
)

// NameCollisionResolution est l'arbitrage retenu pour un produit dont le nom
// existe deja chez le marchand. Le mecanisme de confirmation Redis du chemin
// unitaire ne s'applique pas a l'import : la collision est tranchee en preview.
type NameCollisionResolution string

const (
	CollisionSkip         NameCollisionResolution = "skip"
	CollisionImportAnyway NameCollisionResolution = "import_anyway"
)

// ReimportResolution est l'arbitrage retenu pour une entite qu'un import
// precedent a deja creee.
//
// Par defaut on ignore : c'est ce qui rend l'import rejouable sans dupliquer.
// Mais le mapping survit a l'entite qu'il designe — supprimer un produit dans
// Wello ne le retire pas de import_products_mapping — et sans arbitrage
// explicite, un marchand qui a supprime son menu pour le reimporter se
// retrouvait devant un commit sans effet : « 0 cree, 141 ignores ».
type ReimportResolution string

const (
	ReimportSkip     ReimportResolution = "skip"
	ReimportRecreate ReimportResolution = "recreate"
)

// TvaChannel reprend les valeurs de tva_categories.delivery_type telles
// qu'elles sont stockees, pour que le mapping soit directement utilisable en
// requete.
//
// Attention : le commentaire SQL de la colonne annonce « 0 => in, 1 =>
// delivery, 3 => take away ». Il est faux — les donnees portent 'IN',
// 'TAKE_AWAY' et 'DELIVERY', ce que confirment la jointure
// labels.label_value = tva_categories.delivery_type de GetTVARates et le
// back-office, qui compare a ces memes chaines. La colonne fait foi, pas son
// commentaire.
type TvaChannel string

const (
	TvaChannelIn       TvaChannel = "IN"
	TvaChannelTakeAway TvaChannel = "TAKE_AWAY"
	TvaChannelDelivery TvaChannel = "DELIVERY"
)

// AllTvaChannels liste les canaux dans l'ordre d'affichage du back-office.
var AllTvaChannels = []TvaChannel{TvaChannelIn, TvaChannelTakeAway, TvaChannelDelivery}

// IsKnown ecarte une valeur de delivery_type qui ne correspond a aucun canal
// de vente connu.
func (c TvaChannel) IsKnown() bool {
	switch c {
	case TvaChannelIn, TvaChannelTakeAway, TvaChannelDelivery:
		return true
	default:
		return false
	}
}

// Label rend le nom du canal en francais. Il apparait dans les messages de
// blocage lus par le restaurateur, ou « TAKE_AWAY » n'aurait rien dit.
func (c TvaChannel) Label() string {
	switch c {
	case TvaChannelIn:
		return "sur place"
	case TvaChannelTakeAway:
		return "a emporter"
	case TvaChannelDelivery:
		return "en livraison"
	default:
		return string(c)
	}
}

func (c TvaChannel) String() string { return string(c) }

// TvaRateKey est la cle composite du mapping de TVA : un meme taux se resout
// en trois tva_id differents selon le canal.
type TvaRateKey struct {
	Rate    float64
	Channel TvaChannel
}

// MarshalText / UnmarshalText rendent TvaRateKey utilisable comme cle de map
// en JSON. encoding/json refuse les cles de type struct : sans ces methodes,
// ImportDecisions.TvaMapping ne peut pas etre serialise dans le snapshot de
// preview. Le format est "<taux>:<canal>", ex. "5.5:DELIVERY".
func (k TvaRateKey) MarshalText() ([]byte, error) {
	return []byte(strconv.FormatFloat(k.Rate, 'f', -1, 64) + ":" + string(k.Channel)), nil
}

func (k *TvaRateKey) UnmarshalText(text []byte) error {
	rawRate, rawChannel, ok := strings.Cut(string(text), ":")
	if !ok {
		return fmt.Errorf("cle de TVA %q: format attendu \"<taux>:<canal>\"", text)
	}

	rate, err := strconv.ParseFloat(rawRate, 64)
	if err != nil {
		return fmt.Errorf("cle de TVA %q: taux illisible", text)
	}
	channel := TvaChannel(rawChannel)
	if !channel.IsKnown() {
		return fmt.Errorf("cle de TVA %q: canal inconnu", text)
	}

	k.Rate = rate
	k.Channel = channel
	return nil
}

// ImportDecisions porte les arbitrages pris dans la preview et rejoues au
// commit. Un IntermediateImport seul ne suffit pas a ecrire en base.
// Les tags JSON sont explicites : ImportDecisions transite par HTTP dans les
// deux sens (la preview la propose, le commit la recoit amendee) et par Redis
// dans le snapshot. Sans eux, encoding/json emettrait les noms de champs Go en
// PascalCase, seule entorse au snake_case de tout le reste du contrat d'import.
type ImportDecisions struct {
	// TagClassification indique, pour chaque libelle source, s'il devient une
	// categorie caisse ou un tag Wello.
	TagClassification map[string]TagClass `json:"tag_classification"`

	// CategoryPerProduct force la categorie d'un produit, par identifiant
	// externe de produit -> identifiant externe du libelle. Elle couvre les
	// produits sans categorie explicite et les corrections manuelles.
	CategoryPerProduct map[string]string `json:"category_per_product"`

	// TvaMapping resout un couple (taux, canal) en tva_categories.tva_id. Les
	// cles sont serialisees "<taux>:<canal>" par TvaRateKey.MarshalText.
	TvaMapping map[TvaRateKey]int `json:"tva_mapping"`

	// NameCollisions tranche les produits homonymes d'un produit existant.
	NameCollisions map[string]NameCollisionResolution `json:"name_collisions"`

	// AlreadyImported tranche les produits qu'un import precedent a deja
	// crees : les ignorer (defaut) ou les recreer. Ne concerne que les
	// produits — categories, tags et groupes d'options dont le mapping est
	// perime sont recrees d'office, personne ne voulant arbitrer un contenant.
	AlreadyImported map[string]ReimportResolution `json:"already_imported"`

	// ExcludedProducts ecarte un produit du catalogue source avant tout autre
	// arbitrage (categorie, TVA, collision) — c'est la case a decocher de la
	// porte "autre etablissement" (V1b), seule facon proposee de choisir un
	// sous-ensemble du catalogue plutot que la sequence complete. Distinct
	// d'AlreadyImported, qui tranche le sort d'un produit qu'un import
	// precedent a deja cree : un produit exclu ici ne l'a jamais ete par
	// aucun import, il ne l'est simplement pas par celui-ci.
	ExcludedProducts map[string]bool `json:"excluded_products"`
}
