package menu

// ImportPreviewMerchantRequest est le corps de POST /menu/import/preview-from-merchant
// — la porte "autre établissement" : copier le catalogue d'un marchand auquel
// l'utilisateur courant a également accès vers le marchand de sa session.
//
// SourceMerchantID est un champ client, donc non fiable par défaut : la
// destination réelle reste toujours le marchand du token (voir
// ImportService.PreviewImportFromMerchant), et la source est vérifiée via
// merchantRightsChecker avant toute lecture.
type ImportPreviewMerchantRequest struct {
	SourceMerchantID string `json:"source_merchant_id"`
}
