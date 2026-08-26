package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu/importer"
)

// Erreurs sentinelles du chemin d'import, mappées en codes HTTP par le handler.
var (
	ErrImportProviderRequired = errors.New("import_provider_required")
	ErrImportFileRequired     = errors.New("import_file_required")
	ErrImportNoProducts       = errors.New("import_no_products")
	ErrImportPreviewNotStored = errors.New("import_preview_not_stored")
)

// importPreviewStore est ce que le service attend du cache : déposer, relire
// et consommer un snapshot. Une interface plutôt que *redisclient.Client
// directement, parce que ce dernier est une struct concrète à champ privé —
// impossible à simuler en test autrement.
//
// L'invalidation des caches de menu après un commit n'en fait plus partie :
// elle est portée par MenuChangeNotifier, avec la diffusion `menu_updated`
// qui doit l'accompagner (incrément B).
type importPreviewStore interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
	Get(ctx context.Context, key string) (string, bool)
	Delete(ctx context.Context, key string) bool
}

// importPreviewReader est la part de MenuRepository utilisée ici. La déclarer
// permet de tester le service sans base du tout.
type importPreviewReader interface {
	LoadImportPreviewLookups(ctx context.Context, merchantID, provider string) (importer.PreviewLookups, error)
}

// ImportService orchestre la preview d'import : obtenir le canonique, charger
// l'existant, calculer le dry-run, déposer le snapshot.
//
// Il ne partage pas les dépendances de MenuService à dessein — il n'a besoin
// ni de Deliveroo, ni d'Uber Eats, ni de la synchronisation de statuts — et il
// n'écrit rien, ce qui rend son périmètre facile à vérifier.
type ImportService struct {
	reader     importPreviewReader
	writer     importCommitWriter
	registry   *importer.Registry
	store      importPreviewStore
	tagCreator importTagCreator
	changes    *MenuChangeNotifier

	previewTTL time.Duration
}

// NewImportService câble la preview et le commit. reader et writer sont la même
// instance de MenuRepository en production ; les séparer permet de vérifier en
// test qu'un lot refusé n'atteint jamais la part qui écrit.
//
// changes est partagé avec MenuService : la fenêtre d'amortissement de
// `menu_updated` doit être commune aux deux chemins d'écriture, sinon un
// import lancé pendant des éditions unitaires diffuserait en double.
func NewImportService(
	reader importPreviewReader,
	writer importCommitWriter,
	registry *importer.Registry,
	store importPreviewStore,
	tagCreator importTagCreator,
	changes *MenuChangeNotifier,
) *ImportService {
	return &ImportService{
		reader:     reader,
		writer:     writer,
		registry:   registry,
		store:      store,
		tagCreator: tagCreator,
		changes:    changes,
		previewTTL: models.MenuImportPreviewTTL,
	}
}

// PreviewImportFile traite la porte « fichier » : un provider et un flux.
func (s *ImportService) PreviewImportFile(ctx context.Context, providerSlug string, file io.Reader) (*importer.PreviewResult, error) {
	if providerSlug == "" {
		return nil, ErrImportProviderRequired
	}
	if file == nil {
		return nil, ErrImportFileRequired
	}

	provider, err := s.registry.Get(providerSlug)
	if err != nil {
		return nil, err
	}

	imp, err := provider.Parse(file)
	if err != nil {
		return nil, err
	}

	return s.buildAndStore(ctx, imp)
}

// PreviewImportManual traite la porte « saisie en masse » : le canonique est
// construit directement, sans parsing.
func (s *ImportService) PreviewImportManual(ctx context.Context, req *ImportPreviewJSONRequest) (*importer.PreviewResult, error) {
	if req == nil || len(req.Products) == 0 {
		return nil, ErrImportNoProducts
	}

	imp, err := importer.BuildManualImport(req.toManualProducts())
	if err != nil {
		return nil, err
	}

	// Le provider n'est pas libre : il devient une clé d'idempotence dans
	// import_*_mapping. On n'accepte qu'un slug connu, faute de quoi on
	// retombe sur "manual".
	if req.Provider != "" {
		if _, err := s.registry.Get(req.Provider); err == nil {
			imp.Provider = req.Provider
		}
	}

	return s.buildAndStore(ctx, imp)
}

// buildAndStore est le tronc commun des deux portes : charger l'existant,
// calculer, déposer. Aucune écriture en base à aucun moment.
func (s *ImportService) buildAndStore(ctx context.Context, imp *importer.IntermediateImport) (*importer.PreviewResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	lookups, err := s.reader.LoadImportPreviewLookups(ctx, user.MerchantID, imp.Provider)
	if err != nil {
		return nil, err
	}

	result, err := importer.BuildPreview(imp, lookups)
	if err != nil {
		return nil, err
	}

	token := uuid.New().String()
	createdAt := time.Now().UTC()

	snapshot := &importer.PreviewSnapshot{
		Token:      token,
		MerchantID: user.MerchantID,
		Provider:   imp.Provider,
		CreatedAt:  createdAt,
		Import:     imp,
		Decisions:  result.Decisions,
	}

	payload, err := snapshot.Encode()
	if err != nil {
		return nil, err
	}

	// Le token est le seul lien vers le commit : si le dépôt échoue, renvoyer
	// une preview serait mentir sur ce qui est réellement rejouable.
	if !s.store.Set(ctx, helpers.GetMenuImportPreviewKey(user.MerchantID, token), payload, s.previewTTL) {
		return nil, ErrImportPreviewNotStored
	}

	result.Token = token
	result.ExpiresAt = createdAt.Add(s.previewTTL).Format(time.RFC3339)

	return result, nil
}

// LoadPreviewSnapshot relit un snapshot déposé. Utilisé par le commit, et par
// les tests pour vérifier ce qui a réellement été stocké.
func (s *ImportService) LoadPreviewSnapshot(ctx context.Context, merchantID, token string) (*importer.PreviewSnapshot, error) {
	payload, ok := s.store.Get(ctx, helpers.GetMenuImportPreviewKey(merchantID, token))
	if !ok {
		return nil, fmt.Errorf("preview d'import %q introuvable ou expirée", token)
	}
	return importer.DecodePreviewSnapshot(payload)
}

// ErrImportTemplateUnavailable : le provider existe mais ne fournit pas de
// modèle. C'est le cas d'un export produit par un logiciel tiers — il n'y a
// rien que Wello puisse proposer à remplir.
var ErrImportTemplateUnavailable = errors.New("import_template_unavailable")

// ImportTemplate génère le classeur vierge d'un provider.
//
// Aucune lecture ni écriture en base : le modèle ne dépend pas du marchand,
// seulement du format attendu par le parser.
func (s *ImportService) ImportTemplate(providerSlug string) (data []byte, filename string, err error) {
	if providerSlug == "" {
		return nil, "", ErrImportProviderRequired
	}

	provider, err := s.registry.Get(providerSlug)
	if err != nil {
		return nil, "", err
	}

	template, ok := provider.(importer.TemplateProvider)
	if !ok {
		return nil, "", fmt.Errorf("%w: %q", ErrImportTemplateUnavailable, providerSlug)
	}

	var buf bytes.Buffer
	if err := template.BuildTemplate(&buf); err != nil {
		return nil, "", err
	}

	return buf.Bytes(), template.TemplateFilename(), nil
}
