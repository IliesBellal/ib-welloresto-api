package websocket

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
)

// upgrader configure le upgrade WebSocket avec CheckOrigin permissif pour l'instant
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Permissif pour l'instant, à restreindre en production
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// ServeWS gère la connexion WebSocket pour un client
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Récupérer le user depuis le contexte
	user := middleware.GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Upgrader la connexion HTTP en WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("[WEBSOCKET] Upgrade error: " + err.Error())
		http.Error(w, `{"error":"failed to upgrade to websocket"}`, http.StatusInternalServerError)
		return
	}

	// Créer un nouveau client avec un UUID unique
	client := &Client{
		conn:       conn,
		merchantID: user.MerchantID,
		connID:     uuid.New().String(),
		send:       make(chan []byte, 256),
	}

	// Enregistrer le client dans le hub
	hub.Register(client)

	log.Info("🔗 WebSocket connected",
		zap.String("merchant_id", client.merchantID),
		zap.String("conn_id", client.connID),
	)

	// Lancer les goroutines pour la gestion de la connexion
	go client.writePump(hub)
	client.readPump(hub)
}

// readPump lis les messages du WebSocket (bloquant pour la goroutine courante)
func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("[WebSocket] Error: %v\n", err)
			}
			break
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Vérifier si c'est un PING message
		if string(message) == `{"type":"PING"}` {
			select {
			case c.send <- []byte(`{"type":"PONG"}`):
			default:
				// Si le channel est plein, ignorer silencieusement
			}
		}
	}
}

// writePump écrit les messages vers le WebSocket (non-bloquant)
func (c *Client) writePump(hub *Hub) {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Le channel a été fermé par hub.Unregister
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}
