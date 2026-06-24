package websocket

import (
	"encoding/json"
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

func websocketCloseText(code int) string {
	switch code {
	case websocket.CloseNormalClosure:
		return "normal closure"
	case websocket.CloseGoingAway:
		return "going away"
	case websocket.CloseProtocolError:
		return "protocol error"
	case websocket.CloseUnsupportedData:
		return "unsupported data"
	case websocket.CloseNoStatusReceived:
		return "no status received"
	case websocket.CloseAbnormalClosure:
		return "abnormal closure"
	case websocket.CloseInvalidFramePayloadData:
		return "invalid frame payload data"
	case websocket.ClosePolicyViolation:
		return "policy violation"
	case websocket.CloseMessageTooBig:
		return "message too big"
	case websocket.CloseMandatoryExtension:
		return "mandatory extension missing"
	case websocket.CloseInternalServerErr:
		return "internal server error"
	case websocket.CloseServiceRestart:
		return "service restart"
	case websocket.CloseTryAgainLater:
		return "try again later"
	case websocket.CloseTLSHandshake:
		return "tls handshake failure"
	default:
		return "unknown close code"
	}
}

// ServeWS gère la connexion WebSocket pour un client humain authentifié
// (POS, back-office) — auth via middleware.Auth (middleware.GetUser).
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	serveWS(hub, w, r, user.MerchantID, "")
}

// ServeKioskWS gère la connexion WebSocket d'une borne Kiosk — auth via
// middleware.KioskAuth (middleware.GetKiosk), distincte de l'auth humaine.
// Réutilise le même Hub (clé par merchantID, indifférent à l'origine de la
// connexion) : la borne reçoit donc les mêmes events que le POS/back-office
// du même merchant (menu_updated, availability_update, device_command,
// kiosk_status_changed, ...), voir docs/KIOSK_DECISIONS.md incrément 7.
func ServeKioskWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	kiosk := middleware.GetKiosk(r)
	if kiosk == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	serveWS(hub, w, r, kiosk.MerchantID, kiosk.KioskID)
}

func serveWS(hub *Hub, w http.ResponseWriter, r *http.Request, merchantID, kioskID string) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	// Upgrader la connexion HTTP en WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("websocket upgrade failed",
			zap.Error(err),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("origin", r.Header.Get("Origin")),
			zap.String("user_agent", r.UserAgent()),
		)
		http.Error(w, `{"error":"failed to upgrade to websocket"}`, http.StatusInternalServerError)
		return
	}

	connID := uuid.New().String()
	clientLog := log.With(
		zap.String("merchant_id", merchantID),
		zap.String("conn_id", connID),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("origin", r.Header.Get("Origin")),
		zap.String("user_agent", r.UserAgent()),
	)

	// Créer un nouveau client avec un UUID unique
	client := &Client{
		conn:       conn,
		merchantID: merchantID,
		connID:     connID,
		kioskID:    kioskID,
		send:       make(chan []byte, 256),
		startedAt:  time.Now(),
		log:        clientLog,
	}

	// Enregistrer le client dans le hub
	hub.Register(client)

	clientLog.Info("🔗 websocket connected")

	// Lancer les goroutines pour la gestion de la connexion
	go client.writePump(hub)
	client.readPump(hub)
}

// readPump lis les messages du WebSocket (bloquant pour la goroutine courante)
func (c *Client) readPump(hub *Hub) {
	closeReason := "client disconnected"
	closeCode := 0
	logLevel := zap.InfoLevel

	defer func() {
		hub.Unregister(c)
		c.conn.Close()
		c.log.Log(logLevel, "🔌 websocket disconnected",
			zap.Int("close_code", closeCode),
			zap.String("close_reason", closeReason),
			zap.Duration("connection_duration", time.Since(c.startedAt)),
		)
	}()

	c.conn.SetReadLimit(512 * 1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	c.conn.SetCloseHandler(func(code int, text string) error {
		closeCode = code
		if text != "" {
			closeReason = text
		} else {
			closeReason = websocketCloseText(code)
		}
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok {
				closeCode = closeErr.Code
				if closeErr.Text != "" {
					closeReason = closeErr.Text
				} else {
					closeReason = websocketCloseText(closeErr.Code)
				}
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
					logLevel = zap.WarnLevel
				}
			} else {
				closeReason = err.Error()
				logLevel = zap.WarnLevel
			}
			break
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Vérifier si c'est un PING message
		if string(message) == `{"type":"PING"}` {
			select {
			case c.send <- []byte(`{"type":"PONG"}`):
			default:
				c.log.Warn("websocket pong dropped: send buffer full")
			}
			continue
		}

		// Relais des messages envoyés par une borne (kiosk_unavailable) vers
		// le reste du canal merchant (POS/back-office) — seules les
		// connexions device (kioskID non vide) sont autorisées à émettre ce
		// message, kiosk_id est toujours forcé à l'identité authentifiée
		// pour empêcher l'usurpation d'une autre borne.
		if c.kioskID != "" {
			c.handleIncomingMessage(hub, message)
		}
	}
}

func (c *Client) handleIncomingMessage(hub *Hub, raw []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	if msgType, _ := msg["type"].(string); msgType != "kiosk_unavailable" {
		return
	}
	msg["kiosk_id"] = c.kioskID
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	hub.BroadcastToMerchantExcept(c.merchantID, c.connID, payload)
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
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					c.log.Debug("websocket close frame write failed", zap.Error(err))
				}
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.log.Warn("websocket write failed", zap.Error(err))
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				c.log.Warn("websocket ping failed", zap.Error(err))
				return
			}
		}
	}
}
