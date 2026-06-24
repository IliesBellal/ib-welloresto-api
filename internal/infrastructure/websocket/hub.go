package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Client représente une connexion WebSocket active pour un merchant
type Client struct {
	conn       *websocket.Conn
	merchantID string
	connID     string
	kioskID    string // vide pour un client humain (POS/back-office)
	send       chan []byte
	startedAt  time.Time
	log        *zap.Logger
}

// Hub gère toutes les connexions WebSocket des merchants
// Structure : merchantID -> connID -> *Client
type Hub struct {
	clients map[string]map[string]*Client
	mu      sync.RWMutex
}

// NewHub crée une nouvelle instance du hub
func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]map[string]*Client),
	}
}

// Register ajoute un client au hub de manière thread-safe
func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Créer la map interne si elle n'existe pas
	if _, exists := h.clients[client.merchantID]; !exists {
		h.clients[client.merchantID] = make(map[string]*Client)
	}

	// Ajouter le client
	h.clients[client.merchantID][client.connID] = client
}

// Unregister retire un client du hub de manière thread-safe
// Ferme le channel send et nettoie la map si le merchant n'a plus de clients
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Vérifier que le merchant et le client existent
	if merchant, exists := h.clients[client.merchantID]; exists {
		if _, connExists := merchant[client.connID]; connExists {
			// Fermer le channel send
			close(client.send)
			// Retirer le client
			delete(merchant, client.connID)

			// Si c'était le dernier client du merchant, nettoyer la map
			if len(merchant) == 0 {
				delete(h.clients, client.merchantID)
			}
		}
	}
}

// BroadcastToMerchant envoie un message à tous les clients d'un merchant
// Retourne true si au moins un client a reçu le message
// Retourne false si aucun client connecté ou si tous les envois ont échoué
// De silencieusement les clients en cas d'erreur d'écriture sans bloquer les autres
func (h *Hub) BroadcastToMerchant(merchantID string, message []byte) bool {
	h.mu.RLock()
	merchant, exists := h.clients[merchantID]
	h.mu.RUnlock()

	if !exists || len(merchant) == 0 {
		return false
	}

	sent := false
	failedClients := []*Client{}

	// Copier la liste des clients pour éviter de modifier la map pendant l'itération
	h.mu.RLock()
	clientsToNotify := make([]*Client, 0, len(merchant))
	for _, client := range merchant {
		clientsToNotify = append(clientsToNotify, client)
	}
	h.mu.RUnlock()

	// Envoyer le message à chaque client
	for _, client := range clientsToNotify {
		select {
		case client.send <- message:
			sent = true
		default:
			// Le channel est plein, marquer pour désinscription
			failedClients = append(failedClients, client)
		}
	}

	// Désincrire silencieusement les clients qui ont échoué
	for _, client := range failedClients {
		h.Unregister(client)
	}

	return sent
}

// IsConnected retourne true si au moins une connexion est active pour ce merchant
func (h *Hub) IsConnected(merchantID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	merchant, exists := h.clients[merchantID]
	return exists && len(merchant) > 0
}

// BroadcastToMerchantExcept envoie un message à tous les clients d'un merchant
// sauf la connexion excludeConnID — utilisé pour relayer un message reçu d'un
// client (ex. kiosk_unavailable envoyé par une borne) sans le renvoyer à son
// propre émetteur.
func (h *Hub) BroadcastToMerchantExcept(merchantID, excludeConnID string, message []byte) bool {
	h.mu.RLock()
	merchant, exists := h.clients[merchantID]
	if !exists || len(merchant) == 0 {
		h.mu.RUnlock()
		return false
	}
	clientsToNotify := make([]*Client, 0, len(merchant))
	for connID, client := range merchant {
		if connID == excludeConnID {
			continue
		}
		clientsToNotify = append(clientsToNotify, client)
	}
	h.mu.RUnlock()

	sent := false
	failedClients := []*Client{}
	for _, client := range clientsToNotify {
		select {
		case client.send <- message:
			sent = true
		default:
			failedClients = append(failedClients, client)
		}
	}
	for _, client := range failedClients {
		h.Unregister(client)
	}

	return sent
}

// CloseKioskConnections ferme immédiatement toute connexion WebSocket active
// d'une borne donnée (identifiée par kioskID) dans le canal d'un merchant —
// utilisé lors d'une révocation pour ne pas attendre l'expiration naturelle
// de l'access token. Retourne true si au moins une connexion a été fermée.
func (h *Hub) CloseKioskConnections(merchantID, kioskID string, code int, reason string) bool {
	h.mu.RLock()
	merchant, exists := h.clients[merchantID]
	if !exists {
		h.mu.RUnlock()
		return false
	}
	targets := make([]*Client, 0, 1)
	for _, client := range merchant {
		if client.kioskID != "" && client.kioskID == kioskID {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return false
	}

	closeMsg := websocket.FormatCloseMessage(code, reason)
	for _, client := range targets {
		_ = client.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(5*time.Second))
		_ = client.conn.Close()
	}

	return true
}
