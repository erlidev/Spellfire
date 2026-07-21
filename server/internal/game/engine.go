package game

import (
	"context"
	"sync"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

type Client struct {
	PlayerID string
	Send     chan []byte
	Kick     chan struct{}
}

type Engine struct {
	mu                   sync.Mutex
	world                *World
	clients              map[string]*Client
	tickEvery, sendEvery time.Duration
}

func NewEngine(t Tuning) *Engine {
	return &Engine{world: NewWorld(t), clients: make(map[string]*Client), tickEvery: time.Second / time.Duration(t.TickRate), sendEvery: time.Second / time.Duration(t.SendRate)}
}

func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(e.tickEvery)
	defer ticker.Stop()
	lastSend := time.Time{}
	for {
		select {
		case now := <-ticker.C:
			e.mu.Lock()
			e.world.Step(now)
			if lastSend.IsZero() || now.Sub(lastSend) >= e.sendEvery-time.Millisecond {
				e.broadcastLocked(now)
				lastSend = now
			}
			e.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) Join(character model.Character, now time.Time) *Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.world.AddPlayer(character, now)
	if previous := e.clients[character.ID]; previous != nil {
		close(previous.Kick)
	}
	client := &Client{PlayerID: character.ID, Send: make(chan []byte, 2), Kick: make(chan struct{})}
	e.clients[character.ID] = client
	client.Send <- protocol.EncodeServer(e.world.SnapshotFor(character.ID, now, protocol.ServerWelcome))
	return client
}

func (e *Engine) Leave(client *Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.clients[client.PlayerID] == client {
		delete(e.clients, client.PlayerID)
		e.world.RemovePlayer(client.PlayerID)
	}
}

func (e *Engine) Input(playerID string, input protocol.Input) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.world.ApplyInput(playerID, input)
}

func (e *Engine) Respawn(playerID string, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.world.Respawn(playerID, now)
}

func (e *Engine) Pong(playerID string, clientTime uint64, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	client := e.clients[playerID]
	if client == nil {
		return
	}
	e.queue(client, protocol.EncodeServer(protocol.ServerEnvelope{Kind: protocol.ServerPong, ServerTick: e.world.tick, ServerTimeMS: uint64(now.UnixMilli()), PlayerID: playerID, EchoedClientTimeMS: clientTime}))
}

func (e *Engine) broadcastLocked(now time.Time) {
	for id, client := range e.clients {
		e.queue(client, protocol.EncodeServer(e.world.SnapshotFor(id, now, protocol.ServerSnapshot)))
	}
}

func (e *Engine) queue(client *Client, message []byte) {
	select {
	case client.Send <- message:
		return
	default:
	}
	select {
	case <-client.Send:
	default:
	}
	select {
	case client.Send <- message:
	default:
	}
}
