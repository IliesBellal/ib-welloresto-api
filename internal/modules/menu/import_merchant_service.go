package menu

import (
	"context"
	"errors"
	"strings"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/menu/importer"
)

// ErrSourceMerchantNotFound couvre trois cas indistinguables à dessein :
// aucun source_merchant_id fourni, un identifiant qui ne désigne aucun
// marchand, et un marchand réel sur lequel l'utilisateur n'a pas de droits
// actifs. Les distinguer renseignerait un appelant sur l'existence d'un
// marchand qu'il ne devrait pas pouvoir sonder — même parti pris que
// ErrImportPreviewNotFound dans import_commit_service.go.
var ErrSourceMerchantNotFound = errors.New("import_source_merchant_not_found")

// merchantRightsChecker est la part du dépôt auth utilisée ici. La déclarer
// permet de tester le service sans dépendre du module auth complet.
type merchantRightsChecker interface {
	HasRightsOnMerchant(ctx context.Context, userID, merchantID string) (bool, error)
}

// merchantCatalogReader construit le modèle canonique du catalogue vivant d'un
// marchand — l'équivalent, pour la porte "autre établissement", de ce qu'un
// importer.ImportProvider fait pour un fichier.
type merchantCatalogReader interface {
	BuildMerchantCanonicalImport(ctx context.Context, sourceMerchantID string) (*importer.IntermediateImport, error)
}

// PreviewImportFromMerchant traite la porte "autre établissement" : le
// canonique est construit en lisant le catalogue vivant d'un autre marchand,
// plutôt qu'en parsant un fichier (PreviewImportFile) ou en traduisant une
// saisie (PreviewImportManual).
//
// Le marchand destination reste, comme pour toute requête de l'API, celui du
// token courant (résolu par buildAndStore via middleware.UserFromContext) —
// jamais un champ du corps de la requête. sourceMerchantID, lui, est un champ
// client non fiable : HasRightsOnMerchant est le seul rempart avant toute
// lecture cross-marchand, et tourne à chaque appel, sans mise en cache — un
// accès peut être révoqué à tout moment entre deux imports.
func (s *ImportService) PreviewImportFromMerchant(ctx context.Context, sourceMerchantID string) (*importer.PreviewResult, error) {
	sourceMerchantID = strings.TrimSpace(sourceMerchantID)
	if sourceMerchantID == "" {
		return nil, ErrSourceMerchantNotFound
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Importer depuis soi-même n'a pas de sens (la preview se limiterait à
	// détecter que tout est "déjà importé") : même refus générique plutôt
	// qu'un message dédié, pour ne pas distinguer ce cas de celui d'un
	// marchand sur lequel l'utilisateur n'a pas de droits.
	if sourceMerchantID == user.MerchantID {
		return nil, ErrSourceMerchantNotFound
	}

	ok, err := s.rights.HasRightsOnMerchant(ctx, user.UserID, sourceMerchantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSourceMerchantNotFound
	}

	imp, err := s.catalogReader.BuildMerchantCanonicalImport(ctx, sourceMerchantID)
	if err != nil {
		return nil, err
	}

	// Slug de provider namespacé par marchand source : deux établissements
	// sources différents peuvent tous deux avoir un product_id "42", le
	// provider doit donc porter l'identité de la source pour que
	// import_*_mapping (clé (merchant_id, provider, external_id)) ne les
	// confonde jamais. Namespacer donne aussi l'idempotence gratuitement :
	// importer deux fois depuis le même marchand B réutilise la même lignée de
	// mapping (mise à jour, jamais de doublon) ; importer ensuite depuis un
	// marchand C démarre une lignée indépendante.
	imp.Provider = "merchant-" + sourceMerchantID

	return s.buildAndStore(ctx, imp)
}
