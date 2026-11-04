package deliveroo_menu

import (
	"context"

	"welloresto-api/internal/modules/deliveroo"
)

// menuUploadCompleter clôt une publication de menu (satisfait par
// *deliveroo.DeliverooService).
type menuUploadCompleter interface {
	CompleteMenuUpload(ctx context.Context, res deliveroo.MenuUploadResult) error
}

type MenuWebhookService struct {
	publisher menuUploadCompleter
}

func NewMenuWebhookService(publisher *deliveroo.DeliverooService) *MenuWebhookService {
	return &MenuWebhookService{publisher: publisher}
}

// ProcessMenuUploadResult transmet le résultat au moteur de publication : il
// met à jour le journal (marketplace_menu_syncs) puis, si le menu est en
// ligne, aligne les disponibilités de tous les articles publiés (scénario 13).
func (s *MenuWebhookService) ProcessMenuUploadResult(ctx context.Context, payload MenuWebhookPayload) error {
	return s.publisher.CompleteMenuUpload(ctx, payload.Body.MenuUploadResult)
}
