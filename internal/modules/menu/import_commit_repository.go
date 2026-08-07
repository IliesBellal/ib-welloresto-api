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

// Tables de correspondance alimentées par l'import (migration 080).
const (
	importProductsMappingTable         = "import_products_mapping"
	importCategoriesMappingTable       = "import_categories_mapping"
	importTagsMappingTable             = "import_tags_mapping"
	importAttributesMappingTable       = "import_attributes_mapping"
	importAttributeOptionsMappingTable = "import_attribute_options_mapping"
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
	Categories []ImportCommitEntity `json:"categories"`
	Tags       []ImportCommitEntity `json:"tags"`
	Attributes []ImportCommitEntity `json:"attributes"`
	Products   []ImportCommitEntity `json:"products"`

	OptionsCreated int `json:"-"`
}

// importCommitState porte les correspondances construites au fil de la
// transaction : un produit référence sa catégorie et ses tags par identifiant
// externe, et ceux-ci n'existent parfois qu'après leur propre insertion.
type importCommitState struct {
	merchantCategIDByExternal map[string]string
	tagIDByExternal           map[string]string
}

// MaterializeImportTx écrit un lot d'import dans une transaction unique.
//
// Tout ou rien : la moindre erreur annule l'ensemble, y compris les
// correspondances import_*_mapping. Un lot à moitié écrit laisserait des
// mappings qui feraient sauter les entités manquantes au ré-import.
//
// L'ordre est imposé par les dépendances : catégories et tags d'abord (les
// produits les référencent), attributs ensuite (indépendants), produits enfin.
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
		merchantCategIDByExternal: make(map[string]string, len(plan.Categories)),
		tagIDByExternal:           make(map[string]string, len(plan.Tags)),
	}

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		if err := r.materializeCategoriesTx(txCtx, merchantID, plan, state, outcome); err != nil {
			return err
		}
		if err := r.materializeTagsTx(txCtx, merchantID, plan, state, outcome, tagCreator); err != nil {
			return err
		}
		if err := r.materializeAttributesTx(txCtx, merchantID, plan, outcome); err != nil {
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
			if err := r.insertImportMappingTx(ctx, importCategoriesMappingTable,
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
		if err := r.insertImportMappingTx(ctx, importCategoriesMappingTable,
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
			if err := r.insertImportMappingTx(ctx, importTagsMappingTable,
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
		if err := r.insertImportMappingTx(ctx, importTagsMappingTable,
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

// materializeAttributesTx crée les groupes d'options non rattachés.
//
// V1a : aucun produit ne les référence. Les exports ne portent pas de lien
// produit -> option, le rattachement se fait ensuite via la matrice du
// back-office. insertProductTx ne touche donc jamais
// product_configurable_attribute, sa garde len(Configuration) > 0 n'étant
// jamais franchie.
//
// L'attribut est inséré sans ses options, puis les options le sont
// séparément : c'est ce qui permet de récupérer leurs identifiants pour
// import_attribute_options_mapping, insertAttributeTx ne remontant que
// l'identifiant du groupe.
func (r *MenuRepository) materializeAttributesTx(
	ctx context.Context,
	merchantID string,
	plan *importer.CommitPlan,
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
			options = append(options, UpdateAttributeOptionPayload{
				Title: option.Title,
				Price: option.ExtraPrice,
			})
		}

		optionIDs, err := r.insertAttributeOptionsTx(ctx, attributeID, options)
		if err != nil {
			return fmt.Errorf("import: création des options de %q: %w", attribute.Name, err)
		}
		if len(optionIDs) != len(attribute.Options) {
			return fmt.Errorf("import: %d options créées pour %q, %d attendues",
				len(optionIDs), attribute.Name, len(attribute.Options))
		}

		if err := r.insertImportMappingTx(ctx, importAttributesMappingTable,
			merchantID, plan.Provider, attribute.ExternalID, attributeID); err != nil {
			return err
		}

		for i, option := range attribute.Options {
			if err := r.insertImportMappingTx(ctx, importAttributeOptionsMappingTable,
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
			// Configuration volontairement absent : V1a crée les groupes
			// d'options non rattachés.
		}

		productID, err := r.insertProductTx(ctx, payload)
		if err != nil {
			return fmt.Errorf("import: création du produit %q: %w", product.Name, err)
		}

		welloID, err := strconv.Atoi(productID)
		if err != nil {
			return fmt.Errorf("import: identifiant de produit inattendu %q: %w", productID, err)
		}
		if err := r.insertImportMappingTx(ctx, importProductsMappingTable,
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

// insertImportMappingTx pose une correspondance identifiant externe ->
// identifiant Wello. Le nom de table vient d'une constante du paquet, jamais
// d'une entrée utilisateur.
func (r *MenuRepository) insertImportMappingTx(
	ctx context.Context,
	table, merchantID, provider, externalID string,
	welloID interface{},
) error {
	db := dbx.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`INSERT INTO `+table+` (merchant_id, provider, external_id, wello_id) VALUES (?, ?, ?, ?)`,
		merchantID, provider, externalID, welloID,
	)
	if err != nil {
		return fmt.Errorf("import: écriture de %s pour %q: %w", table, externalID, err)
	}
	return nil
}
