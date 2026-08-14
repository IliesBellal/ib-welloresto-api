package customers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers/importer"
)

// Erreurs sentinelles du chemin d'import clients, mappées en codes HTTP par
// le handler.
var (
	ErrCustomerImportProviderRequired    = errors.New("customer_import_provider_required")
	ErrCustomerImportFileRequired        = errors.New("customer_import_file_required")
	ErrCustomerImportNoCustomers         = errors.New("customer_import_no_customers")
	ErrCustomerImportPreviewNotStored    = errors.New("customer_import_preview_not_stored")
	ErrCustomerImportTemplateUnavailable = errors.New("customer_import_template_unavailable")
)

// PreviewLookupRepository est la part lecture seule dont la preview a besoin.
// Une interface plutôt que *CustomersRepository directement : elle rend le
// service testable sans base, et documente que la preview ne touche jamais
// aux méthodes d'écriture du même repository.
type PreviewLookupRepository interface {
	// LoadExistingByEmails résout des emails en minuscule vers un
	// customer_id, pour un marchand donné.
	LoadExistingByEmails(ctx context.Context, merchantID string, lowerEmails []string) (map[string]int, error)

	// LoadExistingByPhones résout des téléphones déjà normalisés (FR) vers un
	// customer_id, pour un marchand donné.
	LoadExistingByPhones(ctx context.Context, merchantID string, normalizedPhones []string) (map[string]int, error)

	// LoadImportMappings résout des external_id déjà mappés par un import
	// précédent du même provider, pour un marchand donné.
	LoadImportMappings(ctx context.Context, merchantID, provider string, externalIDs []string) (map[string]importer.MappingEntry, error)
}

// customerImportPreviewStore est ce que le service attend du cache : déposer
// un snapshot (preview), le relire et le consommer (commit). Une interface
// plutôt que *redisclient.Client directement, ce dernier étant une struct
// concrète à champ privé — impossible à simuler en test autrement.
type customerImportPreviewStore interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
	Get(ctx context.Context, key string) (string, bool)
	Delete(ctx context.Context, key string) bool
}

// PreviewInput porte l'une OU l'autre des deux portes d'entrée de la preview :
// un flux fichier (providers zelty, wello-generic) ou une saisie manuelle
// déjà typée (provider "manual"). Exactement l'un des deux est renseigné,
// selon le provider demandé.
type PreviewInput struct {
	File   io.Reader
	Inputs []importer.ManualCustomerInput
}

// CustomerImportService orchestre la preview ET le commit d'import de
// clients. La preview ne fait que lire (lookups batchés) et déposer un
// snapshot Redis ; seul CommitImport (customer_import_commit_service.go)
// écrit en base, dans une unique transaction tout-ou-rien.
//
// Dépendances disjointes de CustomersService à dessein, comme
// menu.ImportService l'est de MenuService : l'import n'a besoin ni de la
// fidélité, ni de la recherche clients, et son périmètre reste facile à
// vérifier isolément.
//
// writer et db ne sont utilisés que par le commit (writer.UpdateOrCreateCustomer
// et dbutils.RunInTx(ctx, db, ...) dans materializeImportTx) : la preview ne
// s'en sert jamais, ce qui reste vérifiable en lisant PreviewImport seul.
type CustomerImportService struct {
	repo     PreviewLookupRepository
	writer   CommitCustomerWriter
	db       *sql.DB
	registry *importer.Registry
	store    customerImportPreviewStore

	previewTTL time.Duration
}

func NewCustomerImportService(
	repo PreviewLookupRepository,
	writer CommitCustomerWriter,
	db *sql.DB,
	registry *importer.Registry,
	store customerImportPreviewStore,
) *CustomerImportService {
	return &CustomerImportService{
		repo:       repo,
		writer:     writer,
		db:         db,
		registry:   registry,
		store:      store,
		previewTTL: models.CustomerImportPreviewTTL,
	}
}

// PreviewImport calcule un dry-run d'import de clients et dépose son
// snapshot sous un token à durée de vie limitée. Aucune écriture SQL à aucun
// moment : lookups en lecture seule, puis un SET Redis.
func (s *CustomerImportService) PreviewImport(ctx context.Context, provider string, body PreviewInput) (*importer.PreviewResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	imp, resolvedProvider, err := s.resolveImport(provider, body)
	if err != nil {
		return nil, err
	}

	emails, phones, externalIDs := collectLookupKeys(imp.Customers)

	byEmail, err := s.repo.LoadExistingByEmails(ctx, user.MerchantID, emails)
	if err != nil {
		return nil, err
	}
	byPhone, err := s.repo.LoadExistingByPhones(ctx, user.MerchantID, phones)
	if err != nil {
		return nil, err
	}
	byMapping, err := s.repo.LoadImportMappings(ctx, user.MerchantID, resolvedProvider, externalIDs)
	if err != nil {
		return nil, err
	}

	result := importer.BuildPreview(imp, importer.PreviewLookups{
		ByEmail:   byEmail,
		ByPhone:   byPhone,
		ByMapping: byMapping,
	})

	token := uuid.New().String()
	createdAt := time.Now().UTC()

	snapshot := &importer.PreviewSnapshot{
		MerchantID: user.MerchantID,
		Provider:   resolvedProvider,
		CreatedAt:  createdAt,
		Customers:  imp.Customers,
		Rows:       result.Rows,
	}

	payload, err := snapshot.Encode()
	if err != nil {
		return nil, err
	}

	// Le token est le seul lien vers le commit (phase 4) : si le dépôt
	// échoue, renvoyer une preview reviendrait à mentir sur ce qui est
	// réellement rejouable.
	if !s.store.Set(ctx, helpers.GetCustomerImportPreviewKey(user.MerchantID, token), payload, s.previewTTL) {
		return nil, ErrCustomerImportPreviewNotStored
	}

	result.Token = token
	return result, nil
}

// resolveImport résout la source du canonique : la saisie manuelle si le
// provider demandé est "manual", sinon un provider du registre appliqué au
// flux fourni.
func (s *CustomerImportService) resolveImport(provider string, body PreviewInput) (*importer.IntermediateCustomerImport, string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, "", ErrCustomerImportProviderRequired
	}

	if provider == importer.ManualSlug {
		if len(body.Inputs) == 0 {
			return nil, "", ErrCustomerImportNoCustomers
		}
		imp, err := importer.BuildManualCustomerImport(body.Inputs)
		if err != nil {
			return nil, "", err
		}
		return imp, provider, nil
	}

	if body.File == nil {
		return nil, "", ErrCustomerImportFileRequired
	}

	p, err := s.registry.Get(provider)
	if err != nil {
		return nil, "", err
	}

	imp, err := p.Parse(body.File)
	if err != nil {
		return nil, "", err
	}

	return imp, provider, nil
}

// ImportTemplate génère le classeur vierge d'un provider.
//
// Aucune lecture ni écriture en base : le modèle ne dépend pas du marchand,
// seulement du format attendu par le parser.
func (s *CustomerImportService) ImportTemplate(providerSlug string) (data []byte, filename string, err error) {
	if providerSlug == "" {
		return nil, "", ErrCustomerImportProviderRequired
	}

	provider, err := s.registry.Get(providerSlug)
	if err != nil {
		return nil, "", err
	}

	template, ok := provider.(importer.TemplateProvider)
	if !ok {
		return nil, "", fmt.Errorf("%w: %q", ErrCustomerImportTemplateUnavailable, providerSlug)
	}

	filename, data, err = template.Template()
	if err != nil {
		return nil, "", err
	}
	return data, filename, nil
}

// collectLookupKeys rassemble, sans doublons, les clés de lookup dont la
// preview ET le commit ont besoin : emails en minuscule, téléphones
// normalisés (le parser les a déjà normalisés — voir
// importer.normalizePhoneFR), et external_id (toujours présents, pour le
// lookup de mapping). Prend directement []CanonicalCustomer plutôt qu'un
// *IntermediateCustomerImport : le commit rejoue les mêmes lookups depuis
// snapshot.Customers, qui n'est pas un IntermediateCustomerImport.
func collectLookupKeys(customers []importer.CanonicalCustomer) (emails, phones, externalIDs []string) {
	seenEmail := make(map[string]struct{}, len(customers))
	seenPhone := make(map[string]struct{}, len(customers))

	for _, c := range customers {
		externalIDs = append(externalIDs, c.ExternalID)

		if c.Email != nil {
			email := strings.ToLower(strings.TrimSpace(*c.Email))
			if email != "" {
				if _, dup := seenEmail[email]; !dup {
					seenEmail[email] = struct{}{}
					emails = append(emails, email)
				}
			}
		}

		if c.Phone != nil {
			phone := strings.TrimSpace(*c.Phone)
			if phone != "" {
				if _, dup := seenPhone[phone]; !dup {
					seenPhone[phone] = struct{}{}
					phones = append(phones, phone)
				}
			}
		}
	}

	return emails, phones, externalIDs
}
