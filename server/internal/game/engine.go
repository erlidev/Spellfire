package game

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
)

const (
	// autosaveEvery bounds what an unclean shutdown can discard: a disconnected
	// body is saved when its logout window closes, so this covers a crash
	// mid-session and the window itself.
	autosaveEvery = 15 * time.Second
	saveTimeout   = 5 * time.Second
	// saveQueue is deep enough that a write burst is absorbed rather than run
	// on a goroutine of its own.
	saveQueue = 256
)

// Persister receives the world state that must survive a disconnect. The engine
// decides when to write; the store decides how.
type Persister interface {
	SaveCharacterState(ctx context.Context, characterID string, state model.CharacterState) error
}

type Client struct {
	PlayerID string
	Send     chan []byte
	Kick     chan struct{}
}

type characterSave struct {
	id    string
	state model.CharacterState
}

type Engine struct {
	mu                   sync.Mutex
	world                *World
	clients              map[string]*Client
	tickEvery, sendEvery time.Duration
	persist              Persister
	saveEvery            time.Duration
}

// NewEngine builds the world and its client registry. A nil persister runs the
// world without saving, which is what the simulation tests want.
func NewEngine(t Tuning, persist Persister) *Engine {
	return &Engine{
		world: NewWorld(t), clients: make(map[string]*Client), persist: persist, saveEvery: autosaveEvery,
		tickEvery: time.Second / time.Duration(t.TickRate), sendEvery: time.Second / time.Duration(t.SendRate),
	}
}

func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(e.tickEvery)
	defer ticker.Stop()
	// Saves go to a writer goroutine: a database write must never sit inside
	// the tick loop, and a slow write must never delay the simulation.
	saves := make(chan characterSave, saveQueue)
	writerDone := make(chan struct{})
	go e.writeLoop(saves, writerDone)
	lastSend, lastSave := time.Time{}, time.Time{}
	for {
		select {
		case now := <-ticker.C:
			e.mu.Lock()
			e.world.Step(now)
			if lastSend.IsZero() || now.Sub(lastSend) >= e.sendEvery-time.Millisecond {
				e.broadcastLocked(now)
				lastSend = now
			}
			pending := e.reapLingeringLocked(now)
			if e.persist != nil && now.Sub(lastSave) >= e.saveEvery {
				pending, lastSave = append(pending, e.statesLocked(now)...), now
			}
			e.mu.Unlock()
			e.enqueue(saves, pending)
		case <-ctx.Done():
			// A shutdown must not discard the session, including bodies still
			// inside their logout window.
			e.mu.Lock()
			pending := e.statesLocked(time.Now())
			e.mu.Unlock()
			e.enqueue(saves, pending)
			close(saves)
			<-writerDone
			return
		}
	}
}

// reapLingeringLocked removes the bodies whose logout window has closed and
// returns their final state to be saved.
func (e *Engine) reapLingeringLocked(now time.Time) []characterSave {
	var pending []characterSave
	for _, id := range e.world.ExpiredLingering(now) {
		if state, ok := e.world.StateOf(id, now); ok {
			pending = append(pending, characterSave{id: id, state: state})
		}
		e.world.RemovePlayer(id)
	}
	return pending
}

func (e *Engine) statesLocked(now time.Time) []characterSave {
	if e.persist == nil {
		return nil
	}
	states := e.world.States(now)
	pending := make([]characterSave, 0, len(states))
	for id, state := range states {
		pending = append(pending, characterSave{id: id, state: state})
	}
	return pending
}

// enqueue hands saves to the writer without ever blocking the tick loop. A full
// queue means the database is far behind; the save still happens, just on a
// goroutine of its own rather than in queue order.
func (e *Engine) enqueue(saves chan<- characterSave, pending []characterSave) {
	if e.persist == nil {
		return
	}
	for _, save := range pending {
		select {
		case saves <- save:
		default:
			slog.Warn("character save queue is full", "character", save.id)
			go e.write(save)
		}
	}
}

func (e *Engine) writeLoop(saves <-chan characterSave, done chan<- struct{}) {
	defer close(done)
	for save := range saves {
		e.write(save)
	}
}

// write persists one character. It uses its own context so a shutdown or a
// closed connection cannot abandon the save that shutdown or disconnect caused.
func (e *Engine) write(save characterSave) {
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	if err := e.persist.SaveCharacterState(ctx, save.id, save.state); err != nil {
		slog.Warn("persist character state", "character", save.id, "error", err)
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

// Leave drops a client and leaves its body in the world for the logout window,
// so disconnecting mid-fight does not remove a target. The body is saved and
// removed when that window closes, or resumed if the player reconnects first.
// A client that has already been replaced by a newer connection owns neither
// the player nor its fate.
func (e *Engine) Leave(client *Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.clients[client.PlayerID] != client {
		return
	}
	delete(e.clients, client.PlayerID)
	e.world.BeginLinger(client.PlayerID, time.Now())
}

// Present reports whether a character has a body in the world, connected or
// lingering after a disconnect.
func (e *Engine) Present(characterID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.world.players[characterID] != nil
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
