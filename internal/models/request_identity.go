package models

// RequestIdentity porte l'identité authentifiée d'une requête vers les
// middlewares situés avant l'authentification, qui ne peuvent pas la lire dans
// le contexte : context.WithValue crée un contexte dérivé, invisible de
// l'appelant une fois la requête traitée. Le middleware de journal des requêtes
// y dépose un pointeur (ContextRequestIdentity), le middleware d'authentification
// le remplit. Écrit une seule fois, avant le handler, lu après son retour.
type RequestIdentity struct {
	UserID     string
	MerchantID string
}

const ContextRequestIdentity contextKey = "request_identity"
