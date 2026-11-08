package appreleases

import (
	"errors"
	"net/http"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Download handles GET /v1/public/apps/{app}/download — public, seul endpoint
// du module : cible directe du bouton « Télécharger » de la page
// Téléchargements du site vitrine, il redirige vers l'APK de la dernière
// version publique. Le bouton fonctionne ainsi sans JavaScript et sans CORS,
// et le lien reste le même d'une version à l'autre (on peut le partager).
// Redirection temporaire et non mise en cache, pour qu'une nouvelle version
// soit servie dès sa publication. Lecture seule d'une ligne : pas de
// limitation de débit dédiée.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	release, err := h.svc.Latest(r.Context(), chi.URLParam(r, "app"))
	if err != nil {
		// Le visiteur arrive ici par un lien : une page lisible plutôt
		// que l'enveloppe JSON des erreurs de l'API.
		status := http.StatusInternalServerError
		message := "Le téléchargement est momentanément indisponible. Réessayez dans quelques minutes."
		if errors.Is(err, models.ErrNotFound) {
			status = http.StatusNotFound
			message = "Aucune version de cette application n'est disponible au téléchargement pour le moment."
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(message))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, release.DownloadURL, http.StatusFound)
}
