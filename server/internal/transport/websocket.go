package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/game"
	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/store"
)

const (
	writeWait      = 5 * time.Second
	pongWait       = 20 * time.Second
	pingPeriod     = 8 * time.Second
	maxMessageSize = 2048
)

type WebSocket struct {
	auth   *auth.Service
	store  store.Store
	engine *game.Engine
}

func NewWebSocket(authService *auth.Service, data store.Store, engine *game.Engine) *WebSocket {
	return &WebSocket{auth: authService, store: data, engine: engine}
}

func (h *WebSocket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 4096, CheckOrigin: allowedOrigin}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	connection.SetReadLimit(maxMessageSize)
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, data, err := connection.ReadMessage()
	if err != nil || messageType != websocket.BinaryMessage {
		writeProtocolError(connection, "The first message must be a binary join request.")
		return
	}
	join, err := protocol.DecodeClient(data)
	if err != nil || join.Kind != protocol.ClientJoin {
		writeProtocolError(connection, "Invalid join request.")
		return
	}
	accountID, err := h.auth.Authenticate(r.Context(), join.SessionToken)
	if err != nil {
		writeProtocolError(connection, "Session expired.")
		return
	}
	character, err := h.store.Character(r.Context(), accountID, join.CharacterID)
	if err != nil {
		writeProtocolError(connection, "Character unavailable.")
		return
	}
	client, err := h.engine.Join(character, time.Now())
	if errors.Is(err, game.ErrAccountInWorld) {
		writeProtocolError(connection, "Another character on this account is still in the world. Wait a moment and try again.")
		return
	}
	if err != nil {
		writeProtocolError(connection, "Could not enter the world.")
		return
	}
	defer h.engine.Leave(client)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go h.writeLoop(ctx, cancel, connection, client)
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(time.Now().Add(pongWait)) })
	_ = connection.SetReadDeadline(time.Now().Add(pongWait))
	for {
		messageType, data, err = connection.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		message, err := protocol.DecodeClient(data)
		if err != nil {
			continue
		}
		switch message.Kind {
		case protocol.ClientInput:
			h.engine.Input(client.PlayerID, message.Input)
		case protocol.ClientRespawn:
			h.engine.Respawn(client.PlayerID, time.Now())
		case protocol.ClientPing:
			h.engine.Pong(client.PlayerID, message.ClientTimeMS, time.Now())
		case protocol.ClientLoadout:
			h.engine.SetLoadout(client.PlayerID, model.Loadout{
				Weapon: message.Loadout.Weapon, Gadgets: message.Loadout.Gadgets, Spells: message.Loadout.Spells,
			}, time.Now())
		}
	}
}

func (h *WebSocket) writeLoop(ctx context.Context, cancel context.CancelFunc, connection *websocket.Conn, client *game.Client) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer cancel()
	for {
		select {
		case message := <-client.Send:
			_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
			if err := connection.WriteMessage(websocket.BinaryMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
			if err := connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-client.Kick:
			_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
			_ = connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "connection replaced"), time.Now().Add(writeWait))
			_ = connection.Close()
			return
		case <-ctx.Done():
			return
		}
	}
}

func writeProtocolError(connection *websocket.Conn, message string) {
	_ = connection.SetWriteDeadline(time.Now().Add(writeWait))
	_ = connection.WriteMessage(websocket.BinaryMessage, protocol.EncodeServer(protocol.ServerEnvelope{Kind: protocol.ServerError, Error: message, ServerTimeMS: uint64(time.Now().UnixMilli())}))
}

func allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := parsed.Hostname()
	requestHost := r.Host
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		requestHost = host
	}
	return strings.EqualFold(originHost, requestHost) || (isLoopback(originHost) && isLoopback(requestHost))
}

func isLoopback(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }
