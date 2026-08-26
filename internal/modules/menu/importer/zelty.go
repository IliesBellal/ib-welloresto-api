package importer

import (
	"io"

	"welloresto-api/internal/importutil"
)

// ZeltySlug identifie le provider Zelty. Il est stocke tel quel dans la colonne
// provider des tables import_*_mapping.
const ZeltySlug = "zelty"

// Colonnes de l'export Zelty. Le fichier est mono-feuille, au format "long" :
// une seule grille de 12 colonnes accueille successivement les tags, les
// produits, puis les options et leurs valeurs. Chaque section n'utilise que les
// colonnes qui la concernent, les autres restent vides.
const (
	zeltyColID          = 0 // "ID"
	zeltyColType        = 1 // "Type"
	zeltyColName        = 2 // "Nom"
	zeltyColPrice       = 3 // "Prix"
	zeltyColTvaIn       = 4 // "TVA"
	zeltyColTvaTakeAway = 5 // "TVA emporte"
	zeltyColTvaDelivery = 6 // "TVA livraison"
	zeltyColTags        = 7 // "Tags"
)

// Valeurs de la colonne Type. Les lignes d'en-tete portent litteralement
// "Type" dans cette colonne, elles sont ecartees avant le routage.
const (
	zeltyTypeTag         = "Tag"
	zeltyTypeProduct     = "Produit"
	zeltyTypeOption      = "Option"
	zeltyTypeOptionValue = "Option Value"
)

// zeltySyntheticTagPrefix prefixe les tags fabriques pour un libelle qu'un
// produit reference sans qu'il figure dans la section Tags. Minuscule, donc
// sans collision possible avec les identifiants Zelty reels (ZT...).
const zeltySyntheticTagPrefix = "zt-syn"

// ZeltyProvider lit un export de menu Zelty (.xlsx).
//
// Ce que le format ne donne pas, et qui est donc absent du canonique produit
// ici : aucune description produit, aucune image, aucun lien produit -> option
// (les groupes d'options sortent non rattaches), aucun min/max sur les groupes
// d'options (defauts poses par applyDefaults), et un seul prix par produit,
// recopie sur les trois canaux.
type ZeltyProvider struct{}

func NewZeltyProvider() *ZeltyProvider { return &ZeltyProvider{} }

func (p *ZeltyProvider) Slug() string { return ZeltySlug }

// Parse deroule la feuille ligne a ligne. Le routage se fait sur la colonne
// Type et non sur la section courante : c'est plus robuste qu'un automate
// indexe sur les en-tetes, les exports comportant des sections d'en-tete sans
// aucune ligne de donnees.
//
// Le seul etat reellement necessaire est le groupe d'options courant : les
// lignes "Option Value" suivent leur "Option" sans jamais la referencer.
func (p *ZeltyProvider) Parse(r io.Reader) (*IntermediateImport, error) {
	rows, err := importutil.ReadSheetRows(r)
	if err != nil {
		return nil, err
	}

	out := &IntermediateImport{Provider: ZeltySlug}

	// Index libelle normalise -> identifiant du tag. La premiere declaration
	// gagne : deux tags homonymes sont deux entites Zelty distinctes, mais un
	// produit qui cite ce libelle ne peut en designer qu'une.
	tagIDByLabel := make(map[string]string)
	currentAttribute := -1

	for i, row := range rows {
		line := i + 1 // numerotation du tableur
		if importutil.RowIsEmpty(row) {
			continue
		}

		id := importutil.CellAt(row, zeltyColID)
		rowType := importutil.CellAt(row, zeltyColType)

		// Ligne d'en-tete de section : elle ouvre une nouvelle section, donc
		// clot le groupe d'options en cours.
		if id == "ID" && rowType == "Type" {
			currentAttribute = -1
			continue
		}

		switch rowType {
		case zeltyTypeTag:
			name := importutil.CellAt(row, zeltyColName)
			if id == "" || name == "" {
				return nil, rowErrorf(line, "Nom", "tag sans identifiant ou sans nom")
			}
			out.Tags = append(out.Tags, CanonicalTag{ExternalID: id, Name: name})
			if key := importutil.NormalizeLabel(name); tagIDByLabel[key] == "" {
				tagIDByLabel[key] = id
			}

		case zeltyTypeProduct:
			product, err := parseZeltyProduct(row, line)
			if err != nil {
				return nil, err
			}
			product.TagExternalIDs = resolveZeltyTags(out, tagIDByLabel, importutil.CellAt(row, zeltyColTags))
			out.Products = append(out.Products, *product)

		case zeltyTypeOption:
			name := importutil.CellAt(row, zeltyColName)
			if id == "" || name == "" {
				return nil, rowErrorf(line, "Nom", "groupe d'options sans identifiant ou sans nom")
			}
			out.Attributes = append(out.Attributes, CanonicalAttribute{ExternalID: id, Name: name})
			currentAttribute = len(out.Attributes) - 1

		case zeltyTypeOptionValue:
			if currentAttribute < 0 {
				return nil, rowErrorf(line, "Type", "valeur d'option rencontree avant tout groupe d'options")
			}
			title := importutil.CellAt(row, zeltyColName)
			if id == "" || title == "" {
				return nil, rowErrorf(line, "Nom", "valeur d'option sans identifiant ou sans nom")
			}
			extraPrice, err := parsePriceCents(importutil.CellAt(row, zeltyColPrice))
			if err != nil {
				return nil, rowErrorf(line, "Prix", "%s", err)
			}
			attribute := &out.Attributes[currentAttribute]
			attribute.Options = append(attribute.Options, CanonicalOption{
				ExternalID: id,
				Title:      title,
				ExtraPrice: extraPrice,
			})

		default:
			// Separateurs et lignes d'une section non exploitee.
			continue
		}
	}

	for i := range out.Attributes {
		out.Attributes[i].applyDefaults()
	}

	if len(out.Products) == 0 {
		return nil, ErrNoProducts
	}
	return out, nil
}

// parseZeltyProduct lit une ligne produit, hors resolution des tags.
//
// Zelty n'expose qu'un prix, valable pour tous les canaux : il est recopie sur
// les trois. Les taux de TVA sont conserves bruts, 0 compris — un 0 ne signifie
// pas que le canal est indisponible, seulement qu'aucun taux specifique n'est
// defini pour ce canal ; c'est la preview qui choisira le taux de repli (le
// seul taux defini, ou le plus bas s'il y en a plusieurs).
func parseZeltyProduct(row []string, line int) (*CanonicalProduct, error) {
	id := importutil.CellAt(row, zeltyColID)
	name := importutil.CellAt(row, zeltyColName)
	if id == "" || name == "" {
		return nil, rowErrorf(line, "Nom", "produit sans identifiant ou sans nom")
	}

	price, err := parsePriceCents(importutil.CellAt(row, zeltyColPrice))
	if err != nil {
		return nil, rowErrorf(line, "Prix", "%s", err)
	}

	tvaIn, err := parseTvaRate(importutil.CellAt(row, zeltyColTvaIn))
	if err != nil {
		return nil, rowErrorf(line, "TVA", "%s", err)
	}
	tvaTakeAway, err := parseTvaRate(importutil.CellAt(row, zeltyColTvaTakeAway))
	if err != nil {
		return nil, rowErrorf(line, "TVA emporte", "%s", err)
	}
	tvaDelivery, err := parseTvaRate(importutil.CellAt(row, zeltyColTvaDelivery))
	if err != nil {
		return nil, rowErrorf(line, "TVA livraison", "%s", err)
	}

	return &CanonicalProduct{
		ExternalID:      id,
		Name:            name,
		PriceIn:         price,
		PriceTakeAway:   price,
		PriceDelivery:   price,
		TvaRateIn:       tvaIn,
		TvaRateTakeAway: tvaTakeAway,
		TvaRateDelivery: tvaDelivery,
		// Zelty ne distingue pas categorie et tag : CategoryExternalID reste
		// vide et la categorie est tranchee en preview.
		AllPricesZero: price == 0,
	}, nil
}

// resolveZeltyTags traduit la liste de libelles d'un produit en identifiants de
// tags, dans l'ordre du fichier.
//
// Un libelle absent de la section Tags donne lieu a un tag synthetique ajoute a
// l'import : le rejeter perdrait l'information, et l'ignorer silencieusement
// ferait disparaitre une categorie potentielle du produit. Cas jamais observe
// sur les exports connus, c'est un filet.
func resolveZeltyTags(out *IntermediateImport, tagIDByLabel map[string]string, raw string) []string {
	labels := importutil.SplitLabels(raw)
	if len(labels) == 0 {
		return nil
	}

	ids := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))

	for _, label := range labels {
		key := importutil.NormalizeLabel(label)

		id, known := tagIDByLabel[key]
		if !known {
			id = importutil.GeneratedExternalID(zeltySyntheticTagPrefix, label)
			tagIDByLabel[key] = id
			out.Tags = append(out.Tags, CanonicalTag{ExternalID: id, Name: label, Synthetic: true})
		}

		// Un meme tag cite deux fois sur la ligne ne doit pas produire deux
		// rattachements : product_tags a pour PK (product_id, tag_id).
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids
}
