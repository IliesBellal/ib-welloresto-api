package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Appels de l'API Menu d'Uber Eats (docs/ubereats-menu-sync.md). Fichier à part :
// les appels de commandes restent dans client.go.

// maxErrorBody borne le texte d'erreur d'Uber conservé dans le journal.
const maxErrorBody = 2000

// menuCallError est une réponse d'erreur d'Uber Eats ; HTTPStatus sert au journal.
type menuCallError struct {
	HTTPStatus int
	Body       string
}

func (e *menuCallError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Uber Eats a répondu %d", e.HTTPStatus)
	}
	return fmt.Sprintf("Uber Eats a répondu %d : %s", e.HTTPStatus, e.Body)
}

// doMenuRequest envoie une requête JSON authentifiée. Le corps n'est jamais écrit
// dans les logs applicatifs (le journal des appels sortants le conserve). Renvoie
// le code HTTP même en cas d'erreur d'Uber.
func (c *UberClient) doMenuRequest(ctx context.Context, method, endpoint, token string, payload any) (int, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("sérialisation: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return resp.StatusCode, &menuCallError{HTTPStatus: resp.StatusCode, Body: string(bytes.TrimSpace(raw))}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// UploadMenu remplace le menu du restaurant (PUT /v2/eats/stores/{id}/menus,
// 204 sans corps). L'envoi est immédiat : pas de webhook de résultat.
func (c *UberClient) UploadMenu(ctx context.Context, storeID, token string, menu *UberMenu) (int, error) {
	endpoint := fmt.Sprintf("%s/v2/eats/stores/%s/menus", c.config.BaseURL, storeID)
	return c.doMenuRequest(ctx, http.MethodPut, endpoint, token, menu)
}

// itemSuspensionUpdate : corps d'une mise à jour partielle d'article. suspension
// à null remet l'article en vente ; la clé est toujours envoyée.
type itemSuspensionUpdate struct {
	SuspensionInfo struct {
		Suspension *UberSuspension `json:"suspension"`
	} `json:"suspension_info"`
}

// SetItemSuspension suspend (sold = true, jusqu'à nouvel ordre) ou remet en vente
// un article du menu publié (POST .../menus/items/{item_id}, mise à jour partielle).
func (c *UberClient) SetItemSuspension(ctx context.Context, storeID, itemID, token string, sold bool) (int, error) {
	var upd itemSuspensionUpdate
	if sold {
		upd.SuspensionInfo.Suspension = &UberSuspension{SuspendUntil: suspendForever, Reason: "Épuisé"}
	}
	endpoint := fmt.Sprintf("%s/v2/eats/stores/%s/menus/items/%s", c.config.BaseURL, url.PathEscape(storeID), url.PathEscape(itemID))
	return c.doMenuRequest(ctx, http.MethodPost, endpoint, token, upd)
}
