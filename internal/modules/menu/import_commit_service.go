package menu

import (
	"context"
	"errors"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/menu/importer"
)

var (
	// ErrImportPreviewNotFound couvre un token expiré, déjà consommé, ou qui
	// n'a jamais existé. Les trois sont indistinguables — la clé Redis a
	// simplement disparu — et les distinguer renseignerait un appelant sur
	// l'existence d'un token qui n'est pas le sien.
	ErrImportPreviewNotFound = errors.New("import_preview_not_found")

	// ErrImportTokenRequired : pas de token dans le corps.
	ErrImportTokenRequired = errors.New("import_token_required")
)

// ImportNotCommittableError porte les raisons de refuser un lot. Aucune
// écriture n'a eu lieu quand elle remonte.
type ImportNotCommittableError struct {
	Blockers []importer.CommitBlocker
}

func (e *ImportNotCommittableError) Error() string {
	return "import_not_committable: " + importer.BlockersMessage(e.Blockers)
}

// importCommitWriter est la seule part du dépôt qui écrit. La déclarer permet
// de vérifier en test qu'un lot refusé ne l'atteint jamais.
type importCommitWriter interface {
	MaterializeImportTx(ctx context.Context, merchantID string, plan *importer.CommitPlan, tagCreator importTagCreator) (*ImportCommitOutcome, error)
	TouchMenuUpdated(ctx context.Context, merchantID string) error
}

// CommitImport matérialise un lot précédemment prévisualisé.
//
// Seule méthode du chemin d'import qui écrit. Elle refuse plutôt que d'écrire
// à moitié : tant qu'un produit est sans catégorie, un taux non résolu ou une
// collision non tranchée, rien ne part en base.
func (s *ImportService) CommitImport(ctx context.Context, req *ImportCommitRequest) (*ImportCommitResponse, error) {
	log := logger.FromContext(ctx)

	if req == nil || req.Token == "" {
		return nil, ErrImportTokenRequired
	}

	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	key := helpers.GetMenuImportPreviewKey(user.MerchantID, req.Token)
	payload, ok := s.store.Get(ctx, key)
	if !ok {
		return nil, ErrImportPreviewNotFound
	}

	snapshot, err := importer.DecodePreviewSnapshot(payload)
	if err != nil {
		return nil, err
	}

	// Le marchand fait déjà partie de la clé : une discordance ici signale un
	// snapshot forgé ou une clé mal construite, pas un usage normal. On refuse
	// sans en dire plus.
	if snapshot.MerchantID != user.MerchantID {
		log.Warn("[WARN] CommitImport: snapshot de preview d'un autre marchand (clé " + key + ")")
		return nil, ErrImportPreviewNotFound
	}

	decisions := snapshot.Decisions
	if req.Decisions != nil {
		decisions = *req.Decisions
	}

	// Lookups rechargés : la preview a pu être calculée il y a une demi-heure,
	// le plan doit refléter la base au moment où il sera écrit.
	lookups, err := s.reader.LoadImportPreviewLookups(ctx, user.MerchantID, snapshot.Provider)
	if err != nil {
		return nil, err
	}

	plan, blockers := importer.BuildCommitPlan(snapshot.Import, decisions, lookups)
	if len(blockers) > 0 {
		return nil, &ImportNotCommittableError{Blockers: blockers}
	}

	outcome, err := s.writer.MaterializeImportTx(ctx, user.MerchantID, plan, s.tagCreator)
	if err != nil {
		return nil, err
	}

	// Hors transaction et une seule fois pour tout le lot : le chemin unitaire
	// les appelle par entité, ce qui donnerait ici plusieurs centaines
	// d'invalidations pour un seul import.
	if err := s.writer.TouchMenuUpdated(ctx, user.MerchantID); err != nil {
		log.Warn("[WARN] CommitImport: setMenuUpdated: " + err.Error())
	}
	s.store.InvalidateMerchantMenuCaches(ctx, user.MerchantID)

	// Le token est consommé : un double envoi du formulaire ne doit pas
	// rejouer le lot. L'idempotence par import_*_mapping le rendrait inoffensif,
	// mais autant ne pas y arriver.
	s.store.Delete(ctx, key)

	return newImportCommitResponse(snapshot.Provider, outcome), nil
}
