package customers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers/importer"
	"welloresto-api/internal/utils/dbutils"
)

// Erreurs sentinelles du commit, mappées en codes HTTP par le handler.
var (
	// ErrCustomerImportTokenRequired : pas de token de preview dans le corps.
	ErrCustomerImportTokenRequired = errors.New("customer_import_token_required")

	// ErrCustomerImportPreviewNotFound couvre un token expiré, déjà consommé,
	// ou qui n'a jamais existé — les trois sont indistinguables une fois la
	// clé Redis disparue, et les distinguer renseignerait un appelant sur
	// l'existence d'un token qui n'est pas le sien.
	ErrCustomerImportPreviewNotFound = errors.New("customer_import_preview_not_found")
)

// CommitCustomerWriter est la part écriture du repository utilisée par le
// commit : transaction-agnostique (dbx.GetDB(ctx,...) en interne), donc
// exécutable dans la transaction ouverte par materializeImportTx. Une
// interface, comme PreviewLookupRepository, pour rester testable sans base.
type CommitCustomerWriter interface {
	UpdateOrCreateCustomer(ctx context.Context, c *models.Customer) (*string, error)
}

// CommitRequest est le corps de POST /customers/import/commit.
type CommitRequest struct {
	Token     string                       `json:"token"`
	Decisions []importer.CommitRowDecision `json:"decisions,omitempty"`
}

// mappingInsertChunkSize borne le nombre de lignes par INSERT groupé du
// mapping. 500 lignes * 4 paramètres = 2000 paramètres par requête, très en
// dessous des limites de Postgres — la valeur est choisie pour garder des
// requêtes lisibles en log, pas parce qu'une limite serait proche.
const mappingInsertChunkSize = 500

// CommitImport matérialise un lot précédemment prévisualisé.
//
// Seule méthode du chemin d'import clients qui écrit. Revalide chaque ligne
// contre l'état frais de la base (BuildCommitPlan) avant d'écrire quoi que ce
// soit : un lot qui ne peut pas être entièrement résolu repart avec la liste
// des blocages, sans qu'une seule ligne ait été touchée.
func (s *CustomerImportService) CommitImport(ctx context.Context, req CommitRequest) (*importer.CommitSummary, []importer.CommitBlocker, error) {
	log := logger.FromContext(ctx)

	if strings.TrimSpace(req.Token) == "" {
		return nil, nil, ErrCustomerImportTokenRequired
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, nil, err
	}

	key := helpers.GetCustomerImportPreviewKey(user.MerchantID, req.Token)
	payload, ok := s.store.Get(ctx, key)
	if !ok {
		return nil, nil, ErrCustomerImportPreviewNotFound
	}

	snapshot, err := importer.DecodePreviewSnapshot(payload)
	if err != nil {
		return nil, nil, err
	}

	// Le marchand fait déjà partie de la clé : une discordance ici signale un
	// snapshot forgé ou une clé mal construite, pas un usage normal. On
	// refuse sans en dire plus (même code que "introuvable").
	if snapshot.MerchantID != user.MerchantID {
		log.Warn("[WARN] CustomerImportService.CommitImport: snapshot d'un autre marchand (clé " + key + ")")
		return nil, nil, ErrCustomerImportPreviewNotFound
	}

	// Lookups rechargés : la preview a pu être calculée il y a une demi-heure,
	// le plan doit refléter la base au moment où il sera écrit (audit produit
	// §5.3 — le commit ne fait jamais confiance à la preview).
	emails, phones, externalIDs := collectLookupKeys(snapshot.Customers)

	byEmail, err := s.repo.LoadExistingByEmails(ctx, user.MerchantID, emails)
	if err != nil {
		return nil, nil, err
	}
	byPhone, err := s.repo.LoadExistingByPhones(ctx, user.MerchantID, phones)
	if err != nil {
		return nil, nil, err
	}
	byMapping, err := s.repo.LoadImportMappings(ctx, user.MerchantID, snapshot.Provider, externalIDs)
	if err != nil {
		return nil, nil, err
	}

	fresh := importer.PreviewLookups{ByEmail: byEmail, ByPhone: byPhone, ByMapping: byMapping}

	plan, blockers := importer.BuildCommitPlan(snapshot, importer.CommitDecisions{Decisions: req.Decisions}, fresh)
	if len(blockers) > 0 {
		return nil, blockers, nil
	}

	summary, err := s.materializeImportTx(ctx, user.MerchantID, snapshot.Provider, plan)
	if err != nil {
		return nil, nil, err
	}

	// Le token est consommé : un double envoi du formulaire ne doit pas
	// rejouer le lot. L'idempotence par import_customers_mapping le rendrait
	// inoffensif de toute façon, mais autant ne pas y arriver. Hors
	// transaction et après son succès : un échec de suppression Redis ne doit
	// pas faire annuler un commit déjà écrit.
	s.store.Delete(ctx, key)

	return &summary, nil, nil
}

// mappingRow est une ligne à écrire dans import_customers_mapping, accumulée
// pendant la transaction et écrite en une fois par paquets à la fin.
type mappingRow struct {
	externalID string
	welloID    int
}

// materializeImportTx écrit un lot d'import dans UNE SEULE transaction,
// tout-ou-rien : la moindre erreur SQL annule l'ensemble, mapping compris.
// Adapté à l'échelle réelle (jusqu'à ~18 500 clients) par les inserts groupés
// du mapping (voir upsertImportMappingsBatchTx) — le reste (create/update
// client) reste une requête par ligne, UpdateOrCreateCustomer ne se prêtant
// pas à un batch sans le réécrire.
func (s *CustomerImportService) materializeImportTx(ctx context.Context, merchantID, provider string, plan *importer.CommitPlan) (importer.CommitSummary, error) {
	summary := importer.CommitSummary{Skipped: plan.SkippedCount}

	err := dbutils.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		mappings := make([]mappingRow, 0, len(plan.Creates)+len(plan.Updates)+len(plan.Recreates))

		for _, action := range plan.Creates {
			id, err := s.createCustomerTx(txCtx, merchantID, action)
			if err != nil {
				return fmt.Errorf("creation du client %q: %w", action.ExternalID, err)
			}
			mappings = append(mappings, mappingRow{action.ExternalID, id})
			summary.Created++
		}

		for _, action := range plan.Recreates {
			id, err := s.createCustomerTx(txCtx, merchantID, action)
			if err != nil {
				return fmt.Errorf("recreation du client %q: %w", action.ExternalID, err)
			}
			mappings = append(mappings, mappingRow{action.ExternalID, id})
			summary.Recreated++
		}

		for _, action := range plan.Updates {
			if err := s.updateCustomerTx(txCtx, merchantID, action); err != nil {
				return fmt.Errorf("mise a jour du client %q: %w", action.ExternalID, err)
			}
			mappings = append(mappings, mappingRow{action.ExternalID, *action.TargetCustomerID})
			summary.Updated++
		}

		return s.upsertImportMappingsBatchTx(txCtx, merchantID, provider, mappings)
	})
	if err != nil {
		return importer.CommitSummary{}, err
	}

	return summary, nil
}

// createCustomerTx insère un nouveau client (Create ou Recreate partagent la
// même écriture) et applique, dans la même transaction, les colonnes hors du
// périmètre d'UpdateOrCreateCustomer (creation_date, delivery_notes — voir la
// documentation C2 sur buildCommitCustomer dans commit_plan.go).
func (s *CustomerImportService) createCustomerTx(ctx context.Context, merchantID string, action importer.CommitAction) (int, error) {
	customer := action.Customer
	customer.CustomerID = nil // force la branche INSERT
	customer.MerchantID = merchantID

	idStr, err := s.writer.UpdateOrCreateCustomer(ctx, &customer)
	if err != nil {
		return 0, err
	}
	id, err := parseCustomerID(idStr)
	if err != nil {
		return 0, err
	}

	// allowCreationDate=true : une fiche fraîchement créée peut porter la
	// date d'inscription source (Zelty). delivery_notes suit la même
	// mécanique, seulement si le fichier en fournit une.
	if err := s.patchUnsupportedFieldsTx(ctx, merchantID, id, customer.CreationDate, customer.CustomerDeliveryNotes, true); err != nil {
		return 0, err
	}

	return id, nil
}

// updateCustomerTx met à jour un client existant. La branche UPDATE
// d'UpdateOrCreateCustomer omet du SET tout champ nil/vide du
// models.Customer (voir C3 dans commit_plan.go) : elle ne vide donc jamais un
// champ que le fichier ne fournit pas.
func (s *CustomerImportService) updateCustomerTx(ctx context.Context, merchantID string, action importer.CommitAction) error {
	if action.TargetCustomerID == nil {
		return fmt.Errorf("mise a jour sans cible pour %q", action.ExternalID)
	}

	customer := action.Customer
	idStr := strconv.Itoa(*action.TargetCustomerID)
	customer.CustomerID = &idStr
	customer.MerchantID = merchantID

	if _, err := s.writer.UpdateOrCreateCustomer(ctx, &customer); err != nil {
		return err
	}

	// allowCreationDate=false : une mise à jour ne doit jamais réécrire la
	// date de création d'une fiche existante (C2 ne concerne que la
	// création). delivery_notes reste patchée si le fichier en fournit une.
	return s.patchUnsupportedFieldsTx(ctx, merchantID, *action.TargetCustomerID, nil, customer.CustomerDeliveryNotes, false)
}

// patchUnsupportedFieldsTx applique les colonnes que UpdateOrCreateCustomer
// ne sait pas écrire (voir C2 dans commit_plan.go). N'exécute une requête que
// s'il y a réellement quelque chose à écrire.
func (s *CustomerImportService) patchUnsupportedFieldsTx(ctx context.Context, merchantID string, customerID int, creationDate, deliveryNotes *string, allowCreationDate bool) error {
	var setParts []string
	var args []interface{}

	if allowCreationDate && creationDate != nil {
		setParts = append(setParts, "creation_date = ?")
		args = append(args, *creationDate)
	}
	if deliveryNotes != nil {
		setParts = append(setParts, "delivery_notes = ?")
		args = append(args, *deliveryNotes)
	}
	if len(setParts) == 0 {
		return nil
	}

	args = append(args, customerID, merchantID)
	db := dbx.GetDB(ctx, s.db)
	_, err := db.ExecContext(ctx,
		`UPDATE customer SET `+strings.Join(setParts, ", ")+` WHERE customer_id = ? AND merchant_id = ?`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("ecriture des champs creation_date/delivery_notes pour le client %d: %w", customerID, err)
	}
	return nil
}

// upsertImportMappingsBatchTx écrit le mapping en INSERTS GROUPÉS, par
// paquets de mappingInsertChunkSize lignes.
//
// Syntaxe Postgres (ON CONFLICT) volontairement non portable MySQL : le
// dépôt tourne désormais sur Postgres pour ce chemin (contrainte mise à jour
// de la phase 4), contrairement à menu.upsertImportMappingTx qui reste
// dialecte-agnostique (UPDATE puis INSERT, une ligne à la fois) pour des
// raisons historiques. À l'échelle réelle (~18 500 lignes), une ligne à la
// fois y serait de toute façon inadaptée.
//
// ON CONFLICT (merchant_id, provider, external_id) — l'unique index posé par
// la migration 084 — DO UPDATE remet enabled=true et deletion_date=NULL :
// gère à la fois le rejeu idempotent (même external_id, même wello_id) et le
// remap d'un recreate (même external_id, nouveau wello_id).
func (s *CustomerImportService) upsertImportMappingsBatchTx(ctx context.Context, merchantID, provider string, rows []mappingRow) error {
	if len(rows) == 0 {
		return nil
	}

	db := dbx.GetDB(ctx, s.db)

	for _, chunk := range chunkMappingRows(rows, mappingInsertChunkSize) {
		placeholders := make([]string, 0, len(chunk))
		args := make([]interface{}, 0, len(chunk)*4)
		for _, row := range chunk {
			placeholders = append(placeholders, "(?, ?, ?, ?)")
			args = append(args, merchantID, provider, row.externalID, row.welloID)
		}

		query := `
			INSERT INTO import_customers_mapping (merchant_id, provider, external_id, wello_id)
			VALUES ` + strings.Join(placeholders, ", ") + `
			ON CONFLICT (merchant_id, provider, external_id) DO UPDATE SET
				wello_id = EXCLUDED.wello_id,
				enabled = true,
				deletion_date = NULL
		`
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("ecriture groupee du mapping import clients: %w", err)
		}
	}

	return nil
}

func chunkMappingRows(rows []mappingRow, size int) [][]mappingRow {
	if len(rows) == 0 {
		return nil
	}
	chunks := make([][]mappingRow, 0, (len(rows)+size-1)/size)
	for i := 0; i < len(rows); i += size {
		end := i + size
		if end > len(rows) {
			end = len(rows)
		}
		chunks = append(chunks, rows[i:end])
	}
	return chunks
}

func parseCustomerID(idStr *string) (int, error) {
	if idStr == nil || *idStr == "" {
		return 0, errors.New("identifiant client absent apres creation")
	}
	id, err := strconv.Atoi(*idStr)
	if err != nil {
		return 0, fmt.Errorf("identifiant client inattendu %q: %w", *idStr, err)
	}
	return id, nil
}
