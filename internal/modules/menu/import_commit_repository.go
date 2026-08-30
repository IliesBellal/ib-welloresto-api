package menu

import (
	"context"
	"fmt"
	"strconv"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu/importer"
	tagsModule "welloresto-api/internal/modules/tags"
	"welloresto-api/internal/utils/dbutils"
)

// Tables de correspondance alimentées par l'import (migration 080, puis 107
// pour les deux dernières — porte "autre établissement").
const (
	importProductsMappingTable            = "import_products_mapping"
	importCategoriesMappingTable          = "import_categories_mapping"
	importTagsMappingTable                = "import_tags_mapping"
	importAttributesMappingTable          = "import_attributes_mapping"
	importAttributeOptionsMappingTable    = "import_attribute_options_mapping"
	importComponentCategoriesMappingTable = "import_component_categories_mapping"
	importComponentsMappingTable          = "import_components_mapping"
)

// Actions rapportées pour chaque entité du lot.
const (
	CommitActionCreated = "created"
	CommitActionReused  = "reused"
	CommitActionSkipped = "skipped"
)

// importTagCreator est la part du module tags utilisée ici.
//
// tags.Repository.CreateTag est transaction-agnostique (dbx.GetDB(ctx)), il
// s'exécute donc dans notre transaction et respecte le tout-ou-rien. On le
// réutilise plutôt que de réécrire l'INSERT : un seul endroit crée un tag.
type importTagCreator interface {
	CreateTag(ctx context.Context, merchantID string, req *tagsModule.CreateTagRequest) (*models.TagEntry, error)
}

// ImportCommitEntity est le sort d'une entité du lot, rapporté à l'appelant.
type ImportCommitEntity struct {
	ExternalID string `json:"external_id"`
	WelloID    string `json:"wello_id,omitempty"`
	Action     string `json:"action"`
}

// ImportCommitOutcome est le résultat de la matérialisation.
type ImportCommitOutcome struct {
	Categories          []ImportCommitEntity `json:"categories"`
	Tags                []ImportCommitEntity `json:"tags"`
	Attributes          []ImportCommitEntity `json:"attributes"`
	Products            []ImportCommitEntity `json:"products"`
	ComponentCategories []ImportCommitEntity `json:"component_categories,omitempty"`
	Components          []ImportCommitEntity `json:"components,omitempty"`

	OptionsCreated int `json:"-"`
}

// importCommitState porte les correspondances construites au fil de la
// transaction : un produit référence sa catégorie et ses tags par identifiant
// externe, et ceux-ci n'existent parfois qu'après leur propre insertion.
type importCommitState struct {
	merchantCategIDByExternal             map[string]string
	tagIDByExternal                       map[string]string
	componentCategoryMerchantIDByExternal map[string]string
	componentIDByExternal                 map[string]string
	attributeIDByExternal                 map[string]string
}

// MaterializeImportTx écrit un lot d'import dans une transaction unique.
//
// Tout ou rien : la moindre erreur annule l'ensemble, y compris les
// correspondances import_*_mapping. Un lot à moitié écrit laisserait des
// mappings qui feraient sauter les entités manquantes au ré-import.
//
// L'ordre est imposé par les dépendances : catégories produit et tags
// d'abord (les produits les référencent), puis catégories d'ingrédient et
// ingrédients (une option d'attribut peut porter un lien vers un ingrédient),
// puis attributs, produits enfin — seuls à référencer tout le reste.
//
// Ne pose ni setMenuUpdated ni invalidation de cache : ils appartiennent à
// l'appelant, qui les fait une seule fois en fin de lot.
func (r *MenuRepository) MaterializeImportTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	tagCreator importTagCreator,
) (*ImportCommitOutcome, error) {
	outcome := &ImportCommitOutcome{}
	state := &importCommitState{
		merchantCategIDByExternal:             make(map[string]string, len(plan.Categories)),
		tagIDByExternal:                       make(map[string]string, len(plan.Tags)),
		componentCategoryMerchantIDByExternal: make(map[string]string, len(plan.ComponentCategories)),
		componentIDByExternal:                 make(map[string]string, len(plan.Components)),
		attributeIDByExternal:                 make(map[string]string, len(plan.Attributes)),
	}

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		if err := r.materializeCategoriesTx(txCtx, merchantID, plan, state, outcome); err != nil {
			return err
		}
		if err := r.materializeTagsTx(txCtx, merchantID, plan, state, outcome, tagCreator); err != nil {
			return err
		}
		if err := r.materializeComponentCategoriesTx(txCtx, merchantID, plan, state, outcome); err != nil {
			return err
		}
		if err := r.materializeComponentsTx(txCtx, merchantID, plan, state, outcome); err != nil {
			return err
		}
		if err := r.materializeAttributesTx(txCtx, merchantID, plan, state, outcome); err != nil {
			return err
		}
		return r.materializeProductsTx(txCtx, merchantID, plan, state, outcome)
	})
	if err != nil {
		return nil, err
	}

	return outcome, nil
}

func (r *MenuRepository) materializeCategoriesTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
) error {
	for _, category := range plan.Categories {
		switch {
		case category.AlreadyImported:
			state.merchantCategIDByExternal[category.ExternalID] = category.ReuseMerchantCategID
			outcome.Categories = append(outcome.Categories, ImportCommitEntity{
				ExternalID: category.ExternalID,
				WelloID:    strconv.Itoa(category.ReuseCategID),
				Action:     CommitActionSkipped,
			})
			continue

		case category.ReuseMerchantCategID != "":
			state.merchantCategIDByExternal[category.ExternalID] = category.ReuseMerchantCategID
			if err := r.upsertImportMappingTx(ctx, importCategoriesMappingTable,
				merchantID, plan.Provider, category.ExternalID, category.ReuseCategID); err != nil {
				return err
			}
			outcome.Categories = append(outcome.Categories, ImportCommitEntity{
				ExternalID: category.ExternalID,
				WelloID:    strconv.Itoa(category.ReuseCategID),
				Action:     CommitActionReused,
			})
			continue
		}

		// CreateProductCategory renseigne merchant_categ_id en deux temps
		// (INSERT à '' puis UPDATE) et n'en remonte pas l'erreur. En unitaire
		// cela passe inaperçu ; en lot, un merchant_categ_id resté vide ferait
		// pointer tous les produits de la catégorie sur ''. On relit donc pour
		// vérifier, ce qui transforme un bug silencieux en rollback.
		categIDStr, err := r.CreateProductCategory(ctx, &CreateProductCategoryPayload{
			Name:       category.Name,
			MerchantID: merchantID,
		})
		if err != nil {
			return fmt.Errorf("import: création de la catégorie %q: %w", category.Name, err)
		}

		categID, err := strconv.Atoi(categIDStr)
		if err != nil {
			return fmt.Errorf("import: identifiant de catégorie inattendu %q: %w", categIDStr, err)
		}

		merchantCategID, err := r.readMerchantCategIDTx(ctx, merchantID, categID)
		if err != nil {
			return err
		}
		if merchantCategID == "" {
			return fmt.Errorf("import: la catégorie %q a été créée sans merchant_categ_id", category.Name)
		}

		state.merchantCategIDByExternal[category.ExternalID] = merchantCategID
		if err := r.upsertImportMappingTx(ctx, importCategoriesMappingTable,
			merchantID, plan.Provider, category.ExternalID, categID); err != nil {
			return err
		}
		outcome.Categories = append(outcome.Categories, ImportCommitEntity{
			ExternalID: category.ExternalID,
			WelloID:    categIDStr,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

func (r *MenuRepository) readMerchantCategIDTx(ctx context.Context, merchantID string, categID int) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var merchantCategID string
	err := db.QueryRowContext(ctx,
		`SELECT merchant_categ_id FROM productcateg WHERE merchant_id = ? AND categ_id = ?`,
		merchantID, categID,
	).Scan(&merchantCategID)
	if err != nil {
		return "", fmt.Errorf("import: relecture de merchant_categ_id (categ_id %d): %w", categID, err)
	}
	return merchantCategID, nil
}

func (r *MenuRepository) materializeTagsTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
	tagCreator importTagCreator,
) error {
	for _, tag := range plan.Tags {
		switch {
		case tag.AlreadyImported:
			if tag.ReuseTagID != "" {
				state.tagIDByExternal[tag.ExternalID] = tag.ReuseTagID
			}
			outcome.Tags = append(outcome.Tags, ImportCommitEntity{
				ExternalID: tag.ExternalID,
				WelloID:    tag.ReuseTagID,
				Action:     CommitActionSkipped,
			})
			continue

		case tag.ReuseTagID != "":
			state.tagIDByExternal[tag.ExternalID] = tag.ReuseTagID
			if err := r.upsertImportMappingTx(ctx, importTagsMappingTable,
				merchantID, plan.Provider, tag.ExternalID, tag.ReuseTagID); err != nil {
				return err
			}
			outcome.Tags = append(outcome.Tags, ImportCommitEntity{
				ExternalID: tag.ExternalID,
				WelloID:    tag.ReuseTagID,
				Action:     CommitActionReused,
			})
			continue
		}

		// Mêmes valeurs que le chemin unitaire : l'identifiant et la couleur
		// par défaut sont posés par le service tags, que l'on court-circuite.
		tagID := helpers.GeneratePrefixedID(helpers.TagIDPrefix)
		defaultColor := importTagDefaultColor
		created, err := tagCreator.CreateTag(ctx, merchantID, &tagsModule.CreateTagRequest{
			ID:    &tagID,
			Name:  tag.Name,
			Color: &defaultColor,
		})
		if err != nil {
			return fmt.Errorf("import: création du tag %q: %w", tag.Name, err)
		}

		state.tagIDByExternal[tag.ExternalID] = created.ID
		if err := r.upsertImportMappingTx(ctx, importTagsMappingTable,
			merchantID, plan.Provider, tag.ExternalID, created.ID); err != nil {
			return err
		}
		outcome.Tags = append(outcome.Tags, ImportCommitEntity{
			ExternalID: tag.ExternalID,
			WelloID:    created.ID,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

// importTagDefaultColor reprend le défaut du service tags.
const importTagDefaultColor = "#FFFFFF"

// materializeComponentCategoriesTx crée les catégories d'ingrédient — porte
// "autre établissement" uniquement, plan.ComponentCategories est vide pour
// tout autre provider et la boucle ne produit alors rien.
//
// Même structure que materializeCategoriesTx : CreateComponentCategory suit
// le même patron en deux temps (INSERT à ” puis UPDATE merchant_categ_id)
// que CreateProductCategory, on relit donc de la même façon plutôt que de
// faire confiance à une valeur qui pourrait être restée vide.
func (r *MenuRepository) materializeComponentCategoriesTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
) error {
	for _, category := range plan.ComponentCategories {
		switch {
		case category.AlreadyImported:
			state.componentCategoryMerchantIDByExternal[category.ExternalID] = category.ReuseMerchantCategID
			outcome.ComponentCategories = append(outcome.ComponentCategories, ImportCommitEntity{
				ExternalID: category.ExternalID,
				WelloID:    strconv.Itoa(category.ReuseCategID),
				Action:     CommitActionSkipped,
			})
			continue

		case category.ReuseMerchantCategID != "":
			state.componentCategoryMerchantIDByExternal[category.ExternalID] = category.ReuseMerchantCategID
			if err := r.upsertImportMappingTx(ctx, importComponentCategoriesMappingTable,
				merchantID, plan.Provider, category.ExternalID, category.ReuseCategID); err != nil {
				return err
			}
			outcome.ComponentCategories = append(outcome.ComponentCategories, ImportCommitEntity{
				ExternalID: category.ExternalID,
				WelloID:    strconv.Itoa(category.ReuseCategID),
				Action:     CommitActionReused,
			})
			continue
		}

		categIDStr, err := r.CreateComponentCategory(ctx, &UpsertComponentCategoryPayload{
			Name:       category.Name,
			MerchantID: merchantID,
		})
		if err != nil {
			return fmt.Errorf("import: création de la catégorie d'ingrédient %q: %w", category.Name, err)
		}

		categID, err := strconv.Atoi(categIDStr)
		if err != nil {
			return fmt.Errorf("import: identifiant de catégorie d'ingrédient inattendu %q: %w", categIDStr, err)
		}

		merchantCategID, err := r.readComponentCategoryMerchantIDTx(ctx, merchantID, categID)
		if err != nil {
			return err
		}
		if merchantCategID == "" {
			return fmt.Errorf("import: la catégorie d'ingrédient %q a été créée sans merchant_categ_id", category.Name)
		}

		state.componentCategoryMerchantIDByExternal[category.ExternalID] = merchantCategID
		if err := r.upsertImportMappingTx(ctx, importComponentCategoriesMappingTable,
			merchantID, plan.Provider, category.ExternalID, categID); err != nil {
			return err
		}
		outcome.ComponentCategories = append(outcome.ComponentCategories, ImportCommitEntity{
			ExternalID: category.ExternalID,
			WelloID:    categIDStr,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

func (r *MenuRepository) readComponentCategoryMerchantIDTx(ctx context.Context, merchantID string, categID int) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var merchantCategID string
	err := db.QueryRowContext(ctx,
		`SELECT merchant_categ_id FROM component_category WHERE merchant_id = ? AND id = ?`,
		merchantID, categID,
	).Scan(&merchantCategID)
	if err != nil {
		return "", fmt.Errorf("import: relecture de merchant_categ_id d'ingrédient (id %d): %w", categID, err)
	}
	return merchantCategID, nil
}

// materializeComponentsTx crée les ingrédients — porte "autre établissement"
// uniquement.
//
// Appelle CreateComponent directement plutôt que de passer par un chemin
// dédié : sa vérification d'unicité de nom (avec confirmation Redis) ne
// devrait jamais se déclencher ici puisque buildComponents a déjà écarté tout
// nom qui collisionnait avec l'existant — si elle se déclenche malgré tout
// (course avec une création concurrente entre la preview et le commit), c'est
// un cas assez rare pour que faire échouer tout le lot plutôt que d'y répondre
// silencieusement soit le bon compromis, comme pour toute autre incohérence
// détectée pendant la transaction.
func (r *MenuRepository) materializeComponentsTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
) error {
	for _, component := range plan.Components {
		switch {
		case component.AlreadyImported:
			state.componentIDByExternal[component.ExternalID] = strconv.Itoa(component.ReuseComponentID)
			outcome.Components = append(outcome.Components, ImportCommitEntity{
				ExternalID: component.ExternalID,
				WelloID:    strconv.Itoa(component.ReuseComponentID),
				Action:     CommitActionSkipped,
			})
			continue

		case component.ReuseComponentID != 0:
			state.componentIDByExternal[component.ExternalID] = strconv.Itoa(component.ReuseComponentID)
			if err := r.upsertImportMappingTx(ctx, importComponentsMappingTable,
				merchantID, plan.Provider, component.ExternalID, component.ReuseComponentID); err != nil {
				return err
			}
			outcome.Components = append(outcome.Components, ImportCommitEntity{
				ExternalID: component.ExternalID,
				WelloID:    strconv.Itoa(component.ReuseComponentID),
				Action:     CommitActionReused,
			})
			continue
		}

		categoryMerchantID, ok := state.componentCategoryMerchantIDByExternal[component.CategoryExternalID]
		if !ok || categoryMerchantID == "" {
			return fmt.Errorf("import: catégorie d'ingrédient %q non résolue pour le composant %q",
				component.CategoryExternalID, component.Name)
		}

		name := component.Name
		unitID := component.UnitOfMeasureID
		price := component.Price
		payload := &UpdateComponentPayload{
			Name:             &name,
			Price:            &price,
			MerchantID:       merchantID,
			CategoryID:       &categoryMerchantID,
			UnitID:           &unitID,
			ConservationDays: component.ConservationDays,
			StorageTempMin:   component.StorageTempMin,
			StorageTempMax:   component.StorageTempMax,
		}
		if component.PurchaseUnitOfMeasureID != "" {
			purchaseUnitID := component.PurchaseUnitOfMeasureID
			payload.PurchaseUnitID = &purchaseUnitID
		}
		if component.PurchaseCost != 0 {
			cost := component.PurchaseCost
			payload.PurchaseCost = &cost
		}
		if component.PurchaseCostQty != 0 {
			qty := component.PurchaseCostQty
			payload.PurchaseCostQty = &qty
		}
		if component.ConservationType != "" {
			conservationType := component.ConservationType
			payload.ConservationType = &conservationType
		}

		componentIDStr, err := r.CreateComponent(ctx, payload)
		if err != nil {
			return fmt.Errorf("import: création du composant %q: %w", component.Name, err)
		}

		componentID, err := strconv.Atoi(componentIDStr)
		if err != nil {
			return fmt.Errorf("import: identifiant de composant inattendu %q: %w", componentIDStr, err)
		}

		state.componentIDByExternal[component.ExternalID] = componentIDStr
		if err := r.upsertImportMappingTx(ctx, importComponentsMappingTable,
			merchantID, plan.Provider, component.ExternalID, componentID); err != nil {
			return err
		}
		outcome.Components = append(outcome.Components, ImportCommitEntity{
			ExternalID: component.ExternalID,
			WelloID:    componentIDStr,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

// materializeAttributesTx crée les groupes d'options.
//
// V1a (fichier/saisie) : aucun produit ne les référence, les exports ne
// portant pas de lien produit -> option — le rattachement se fait alors via la
// matrice du back-office. La porte "autre établissement" (V1b) est la première
// source à fournir ce rattachement, réalisé par materializeProductsTx via
// state.attributeIDByExternal, alimenté ici.
//
// L'attribut est inséré sans ses options, puis les options le sont
// séparément : c'est ce qui permet de récupérer leurs identifiants pour
// import_attribute_options_mapping, insertAttributeTx ne remontant que
// l'identifiant du groupe.
func (r *MenuRepository) materializeAttributesTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
) error {
	for _, attribute := range plan.Attributes {
		if attribute.AlreadyImported {
			outcome.Attributes = append(outcome.Attributes, ImportCommitEntity{
				ExternalID: attribute.ExternalID,
				Action:     CommitActionSkipped,
			})
			continue
		}

		attributeID, err := r.insertAttributeTx(ctx, merchantID, &UpdateAttributePayload{
			Type:  attribute.Type,
			Name:  attribute.Name,
			Title: attribute.Name,
			Min:   attribute.MinOptions,
			Max:   attribute.MaxOptions,
		})
		if err != nil {
			return fmt.Errorf("import: création du groupe d'options %q: %w", attribute.Name, err)
		}

		options := make([]UpdateAttributeOptionPayload, 0, len(attribute.Options))
		for _, option := range attribute.Options {
			optionPayload := UpdateAttributeOptionPayload{
				Title: option.Title,
				Price: option.ExtraPrice,
			}
			// Lien ingrédient : ComponentExternalID n'est déjà renseigné par
			// commitPlanner.buildAttributes que pour un composant réellement
			// matérialisable, resolveComponentID ne peut donc échouer que sur
			// une incohérence interne au plan.
			if option.ComponentExternalID != "" {
				if componentID, ok := state.componentIDByExternal[option.ComponentExternalID]; ok {
					optionPayload.ComponentID = componentID
					optionPayload.Quantity = option.Quantity
					optionPayload.UnitOfMeasureID = option.UnitOfMeasureID
				}
			}
			options = append(options, optionPayload)
		}

		optionIDs, err := r.insertAttributeOptionsTx(ctx, attributeID, options)
		if err != nil {
			return fmt.Errorf("import: création des options de %q: %w", attribute.Name, err)
		}
		if len(optionIDs) != len(attribute.Options) {
			return fmt.Errorf("import: %d options créées pour %q, %d attendues",
				len(optionIDs), attribute.Name, len(attribute.Options))
		}

		state.attributeIDByExternal[attribute.ExternalID] = attributeID
		if err := r.upsertImportMappingTx(ctx, importAttributesMappingTable,
			merchantID, plan.Provider, attribute.ExternalID, attributeID); err != nil {
			return err
		}

		for i, option := range attribute.Options {
			if err := r.upsertImportMappingTx(ctx, importAttributeOptionsMappingTable,
				merchantID, plan.Provider, option.ExternalID, optionIDs[i]); err != nil {
				return err
			}
		}
		outcome.OptionsCreated += len(optionIDs)

		outcome.Attributes = append(outcome.Attributes, ImportCommitEntity{
			ExternalID: attribute.ExternalID,
			WelloID:    attributeID,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

func (r *MenuRepository) materializeProductsTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
	state *importCommitState,
	outcome *ImportCommitOutcome,
) error {
	for _, product := range plan.Products {
		if !product.Materializable() {
			outcome.Products = append(outcome.Products, ImportCommitEntity{
				ExternalID: product.ExternalID,
				Action:     CommitActionSkipped,
			})
			continue
		}

		categoryID, ok := state.merchantCategIDByExternal[product.CategoryExternalID]
		if !ok || categoryID == "" {
			return fmt.Errorf("import: catégorie %q non résolue pour le produit %q",
				product.CategoryExternalID, product.Name)
		}

		tagIDs := make([]string, 0, len(product.TagExternalIDs))
		for _, externalID := range product.TagExternalIDs {
			tagID, ok := state.tagIDByExternal[externalID]
			if !ok || tagID == "" {
				return fmt.Errorf("import: tag %q non résolu pour le produit %q", externalID, product.Name)
			}
			tagIDs = append(tagIDs, tagID)
		}

		// Rattachement d'attributs et composition — porte "autre établissement"
		// uniquement, product.AttributeExternalIDs/Components sont vides pour
		// toute autre source et les deux boucles ne produisent alors rien :
		// Configuration/Components restent nil, insertProductTx ne les
		// synchronise donc pas, comme aujourd'hui pour le fichier et la saisie
		// manuelle (V1a).
		attributeIDs := make([]string, 0, len(product.AttributeExternalIDs))
		for _, externalID := range product.AttributeExternalIDs {
			if attributeID, ok := state.attributeIDByExternal[externalID]; ok && attributeID != "" {
				attributeIDs = append(attributeIDs, attributeID)
			}
		}

		components := make([]ProductComponentUpdate, 0, len(product.Components))
		for _, comp := range product.Components {
			componentID, ok := state.componentIDByExternal[comp.ComponentExternalID]
			if !ok || componentID == "" {
				return fmt.Errorf("import: ingrédient %q non résolu pour le produit %q",
					comp.ComponentExternalID, product.Name)
			}
			inOrders := comp.InOrders
			takeAwayOrders := comp.TakeAwayOrders
			deliveryOrders := comp.DeliveryOrders
			components = append(components, ProductComponentUpdate{
				ComponentID:    componentID,
				Quantity:       comp.Quantity,
				UnitID:         comp.UnitOfMeasureID,
				InOrders:       &inOrders,
				TakeAwayOrders: &takeAwayOrders,
				DeliveryOrders: &deliveryOrders,
			})
		}

		status := product.Status
		availableIn := product.AvailableIn
		availableTakeAway := product.AvailableTakeAway
		availableDelivery := product.AvailableDelivery

		payload := &CreateProductPayload{
			MerchantID:        merchantID,
			Name:              product.Name,
			ProductDesc:       product.Description,
			Price:             float64(product.PriceIn),
			PriceTakeAway:     float64(product.PriceTakeAway),
			PriceDelivery:     float64(product.PriceDelivery),
			TvaInID:           strconv.Itoa(product.TvaInID),
			TvaTakeAwayID:     strconv.Itoa(product.TvaTakeAwayID),
			TvaDeliveryID:     strconv.Itoa(product.TvaDeliveryID),
			CategoryID:        categoryID,
			Status:            &status,
			AvailableIn:       &availableIn,
			AvailableTakeAway: &availableTakeAway,
			AvailableDelivery: &availableDelivery,
			Tags:              tagIDs,
			Configuration:     attributeIDs,
			Components:        components,
		}

		productID, err := r.insertProductTx(ctx, payload)
		if err != nil {
			return fmt.Errorf("import: création du produit %q: %w", product.Name, err)
		}

		welloID, err := strconv.Atoi(productID)
		if err != nil {
			return fmt.Errorf("import: identifiant de produit inattendu %q: %w", productID, err)
		}
		if err := r.upsertImportMappingTx(ctx, importProductsMappingTable,
			merchantID, plan.Provider, product.ExternalID, welloID); err != nil {
			return err
		}

		outcome.Products = append(outcome.Products, ImportCommitEntity{
			ExternalID: product.ExternalID,
			WelloID:    productID,
			Action:     CommitActionCreated,
		})
	}

	return nil
}

// TouchMenuUpdated expose setMenuUpdated au chemin d'import, qui l'appelle une
// seule fois en fin de lot au lieu d'une fois par entité.
func (r *MenuRepository) TouchMenuUpdated(ctx context.Context, merchantID string) error {
	return r.setMenuUpdated(ctx, merchantID)
}

// upsertImportMappingTx pose ou réaffecte une correspondance identifiant
// externe -> identifiant Wello. Le nom de table vient d'une constante du
// paquet, jamais d'une entrée utilisateur.
//
// La mise à jour d'abord, l'insertion ensuite : quand une entité est recréée —
// parce que celle d'un import précédent a été supprimée, ou parce que
// l'utilisateur a demandé un réimport — la ligne existe déjà et l'index unique
// (merchant_id, provider, external_id) refuserait un second INSERT. Deux
// requêtes plutôt qu'un upsert, pour rester indépendant du dialecte.
//
// deletion_date et enabled sont remis à neuf : la correspondance redevient
// active, elle désigne une entité qui existe.
func (r *MenuRepository) upsertImportMappingTx(
	ctx context.Context,
	table, merchantID, provider, externalID string,
	welloID interface{},
) error {
	db := dbx.GetDB(ctx, r.database)

	result, err := db.ExecContext(ctx,
		`UPDATE `+table+`
		 SET wello_id = ?, deletion_date = NULL, enabled = TRUE
		 WHERE merchant_id = ? AND provider = ? AND external_id = ?`,
		welloID, merchantID, provider, externalID,
	)
	if err != nil {
		return fmt.Errorf("import: réaffectation de %s pour %q: %w", table, externalID, err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		return nil
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO `+table+` (merchant_id, provider, external_id, wello_id) VALUES (?, ?, ?, ?)`,
		merchantID, provider, externalID, welloID,
	); err != nil {
		return fmt.Errorf("import: écriture de %s pour %q: %w", table, externalID, err)
	}
	return nil
}
