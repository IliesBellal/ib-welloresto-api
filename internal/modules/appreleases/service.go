package appreleases

import (
	"context"
	"strings"
	"time"

	"welloresto-api/internal/models"
)

// releaseReader est la partie du dépôt dont le service a besoin (substituable
// en test).
type releaseReader interface {
	LatestPublic(ctx context.Context, appID string) (int, time.Time, string, error)
}

type Service struct {
	repo releaseReader
}

func NewService(repo releaseReader) *Service {
	return &Service{repo: repo}
}

// Latest renvoie la dernière version publique de l'application nommée app
// (« caisse », « borne », « cds »). models.ErrNotFound pour un nom inconnu ou
// une application sans version publiée.
func (s *Service) Latest(ctx context.Context, app string) (*LatestRelease, error) {
	app = strings.ToLower(strings.TrimSpace(app))
	appID, ok := publicApps[app]
	if !ok {
		return nil, models.ErrNotFound
	}
	versionCode, releaseDate, downloadURL, err := s.repo.LatestPublic(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &LatestRelease{
		App:         app,
		VersionCode: versionCode,
		ReleaseDate: releaseDate.UTC(),
		DownloadURL: downloadURL,
	}, nil
}
