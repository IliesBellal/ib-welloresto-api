// Package indexnow signale aux moteurs de recherche compatibles IndexNow
// (Bing — donc Yahoo, DuckDuckGo et Ecosia qui en reprennent l'index —,
// Yandex, Seznam, Naver…) les pages qui viennent de changer, pour qu'elles
// soient revisitées en minutes plutôt qu'en jours. Google ne participe pas :
// il lit le sitemap. Protocole : https://www.indexnow.org/documentation.
package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultEndpoint : point d'entrée commun, qui relaie aux moteurs participants.
const DefaultEndpoint = "https://api.indexnow.org/indexnow"

// maxURLsPerRequest : limite du protocole.
const maxURLsPerRequest = 10000

// keyPattern : 8 à 128 caractères alphanumériques ou tirets (protocole).
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{8,128}$`)

// ErrInvalidKey : clé absente ou hors format.
var ErrInvalidKey = errors.New("indexnow: clé invalide (8 à 128 caractères a-z, A-Z, 0-9, -)")

type Client struct {
	key         string
	host        string
	keyLocation string
	endpoint    string
	http        *http.Client
}

// New prépare un client pour le site siteBaseURL (ex.
// https://scannorder.welloresto.fr). Le fichier de clé doit être servi à
// {siteBaseURL}/{key}.txt et contenir la clé (serveur du front ScanNOrder,
// variable INDEXNOW_KEY).
func New(key, siteBaseURL string) (*Client, error) {
	if !keyPattern.MatchString(key) {
		return nil, ErrInvalidKey
	}
	base, err := url.Parse(strings.TrimRight(siteBaseURL, "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, fmt.Errorf("indexnow: adresse du site invalide %q (https attendu)", siteBaseURL)
	}
	return &Client{
		key:         key,
		host:        base.Host,
		keyLocation: base.Scheme + "://" + base.Host + "/" + key + ".txt",
		endpoint:    DefaultEndpoint,
		http:        &http.Client{Timeout: 20 * time.Second},
	}, nil
}

// WithEndpoint remplace le point d'entrée (tests).
func (c *Client) WithEndpoint(endpoint string) *Client {
	c.endpoint = endpoint
	return c
}

type submission struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation"`
	URLList     []string `json:"urlList"`
}

// Submit signale les URL données, par lots de 10 000. Les URL d'un autre
// domaine que le site sont refusées par le protocole : l'appelant n'en passe
// pas. 200 et 202 (clé en cours de vérification) sont des succès.
func (c *Client) Submit(ctx context.Context, urls []string) error {
	for start := 0; start < len(urls); start += maxURLsPerRequest {
		batch := urls[start:min(start+maxURLsPerRequest, len(urls))]
		body, err := json.Marshal(submission{Host: c.host, Key: c.key, KeyLocation: c.keyLocation, URLList: batch})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("indexnow: %w", err)
		}
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("indexnow: HTTP %d (%s)", resp.StatusCode, strings.TrimSpace(string(snippet)))
		}
	}
	return nil
}
