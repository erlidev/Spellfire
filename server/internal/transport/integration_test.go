package transport

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protowire"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/game"
	"spellfire/server/internal/model"
	"spellfire/server/internal/protocol"
	"spellfire/server/internal/store"
)

func TestWebSocketAuthenticatedJoinReceivesWelcome(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	authService := auth.New(data, time.Hour)
	token, err := authService.Register(context.Background(), "socket@example.com", "socket password")
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := authService.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Socket Hero", Class: model.Gunslinger, Progress: model.Progress{Level: 1}}
	if err := data.CreateCharacter(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	engine := game.NewEngine(game.DefaultTuning(), nil)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var join []byte
	join = protowire.AppendTag(join, 1, protowire.VarintType)
	join = protowire.AppendVarint(join, protocol.ClientJoin)
	join = protowire.AppendTag(join, 2, protowire.BytesType)
	join = protowire.AppendString(join, token)
	join = protowire.AppendTag(join, 3, protowire.BytesType)
	join = protowire.AppendString(join, character.ID)
	if err := connection.WriteMessage(websocket.BinaryMessage, join); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	messageType, welcome, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("message type = %d", messageType)
	}
	number, wire, n := protowire.ConsumeTag(welcome)
	if n < 0 || number != 1 || wire != protowire.VarintType {
		t.Fatalf("welcome tag invalid: %x", welcome)
	}
	kind, n := protowire.ConsumeVarint(welcome[n:])
	if n < 0 || kind != protocol.ServerWelcome {
		t.Fatalf("welcome kind = %d", kind)
	}
}

func TestWebSocketRejectsInvalidSession(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	authService := auth.New(data, time.Hour)
	engine := game.NewEngine(game.DefaultTuning(), nil)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var join []byte
	join = protowire.AppendTag(join, 1, protowire.VarintType)
	join = protowire.AppendVarint(join, protocol.ClientJoin)
	join = protowire.AppendTag(join, 2, protowire.BytesType)
	join = protowire.AppendString(join, "invalid")
	join = protowire.AppendTag(join, 3, protowire.BytesType)
	join = protowire.AppendString(join, "missing")
	if err := connection.WriteMessage(websocket.BinaryMessage, join); err != nil {
		t.Fatal(err)
	}
	_, response, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	number, _, n := protowire.ConsumeTag(response)
	if n < 0 || number != 1 {
		t.Fatalf("invalid response: %x", response)
	}
	kind, _ := protowire.ConsumeVarint(response[n:])
	if kind != protocol.ServerError {
		t.Fatalf("response kind = %d", kind)
	}
}

// The phase's headline behaviour, end to end: a character that disconnects in
// the field comes back where it left off instead of at the hub.
func TestWebSocketDisconnectPersistsWorldPosition(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	ctx := context.Background()
	authService := auth.New(data, time.Hour)
	token, err := authService.Register(ctx, "field@example.com", "socket password")
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := authService.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Field Hand", Class: model.Gunslinger, Progress: model.Progress{Level: 1}}
	if err := data.CreateCharacter(ctx, character); err != nil {
		t.Fatal(err)
	}
	saved := model.CharacterState{Position: model.Point{X: 1500, Y: -200}, Placed: true, LastSeen: time.Now(), Materials: map[string]int{}}
	if err := data.SaveCharacterState(ctx, character.ID, saved); err != nil {
		t.Fatal(err)
	}

	balance := game.DefaultTuning()
	balance.LogoutLinger = 200 * time.Millisecond
	engine := game.NewEngine(balance, data)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go engine.Run(runCtx)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var join []byte
	join = protowire.AppendTag(join, 1, protowire.VarintType)
	join = protowire.AppendVarint(join, protocol.ClientJoin)
	join = protowire.AppendTag(join, 2, protowire.BytesType)
	join = protowire.AppendString(join, token)
	join = protowire.AppendTag(join, 3, protowire.BytesType)
	join = protowire.AppendString(join, character.ID)
	if err := connection.WriteMessage(websocket.BinaryMessage, join); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := connection.ReadMessage(); err != nil {
		t.Fatal(err)
	}

	// Walk east for a moment, so the saved position is one the test never wrote
	// and only the world could have produced.
	var input []byte
	input = protowire.AppendTag(input, 1, protowire.VarintType)
	input = protowire.AppendVarint(input, protocol.ClientInput)
	var fields []byte
	fields = protowire.AppendTag(fields, 1, protowire.VarintType)
	fields = protowire.AppendVarint(fields, 1)
	fields = protowire.AppendTag(fields, 2, protowire.VarintType)
	fields = protowire.AppendVarint(fields, uint64(game.ButtonRight))
	fields = protowire.AppendTag(fields, 3, protowire.Fixed32Type)
	fields = protowire.AppendFixed32(fields, math.Float32bits(1))
	input = protowire.AppendTag(input, 4, protowire.BytesType)
	input = protowire.AppendBytes(input, fields)
	if err := connection.WriteMessage(websocket.BinaryMessage, input); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	connection.Close()

	// The body lingers, then is reaped and saved. Had the join dropped the
	// character at the hub, the save would land near the spawn ring instead of
	// east of where it logged out.
	deadline := time.Now().Add(2 * time.Second)
	for {
		reloaded, err := data.Character(ctx, accountID, character.ID)
		if err != nil {
			t.Fatal(err)
		}
		moved := reloaded.State.Placed && reloaded.State.Position.X > saved.Position.X && reloaded.State.Position.Y == saved.Position.Y
		if moved {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("state after disconnect = %#v, want a position east of %#v", reloaded.State, saved.Position)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Combat logging must not work: after the socket closes the body is still in
// the world, still a target, and only leaves when the logout window closes.
func TestWebSocketDisconnectLeavesTheBodyBehindBriefly(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	ctx := context.Background()
	authService := auth.New(data, time.Hour)
	token, err := authService.Register(ctx, "logger@example.com", "socket password")
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := authService.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Combat Logger", Class: model.Gunslinger, Progress: model.Progress{Level: 1}}
	if err := data.CreateCharacter(ctx, character); err != nil {
		t.Fatal(err)
	}
	balance := game.DefaultTuning()
	balance.LogoutLinger = 400 * time.Millisecond
	engine := game.NewEngine(balance, data)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go engine.Run(runCtx)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var join []byte
	join = protowire.AppendTag(join, 1, protowire.VarintType)
	join = protowire.AppendVarint(join, protocol.ClientJoin)
	join = protowire.AppendTag(join, 2, protowire.BytesType)
	join = protowire.AppendString(join, token)
	join = protowire.AppendTag(join, 3, protowire.BytesType)
	join = protowire.AppendString(join, character.ID)
	if err := connection.WriteMessage(websocket.BinaryMessage, join); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := connection.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	connection.Close()

	// Immediately after the socket drops, the body is still there to be shot at.
	time.Sleep(100 * time.Millisecond)
	if !engine.Present(character.ID) {
		t.Fatal("the body vanished with the connection; combat logging works")
	}
	deadline := time.Now().Add(3 * time.Second)
	for engine.Present(character.ID) {
		if time.Now().After(deadline) {
			t.Fatal("the body never left after its logout window")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An account gets one body in the world. A second character's join is refused
// at the socket rather than quietly added beside the first.
func TestWebSocketRefusesASecondCharacterOnTheSameAccount(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	ctx := context.Background()
	authService := auth.New(data, time.Hour)
	token, err := authService.Register(ctx, "two@example.com", "socket password")
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := authService.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	first := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "First Hero", Class: model.Gunslinger, Progress: model.Progress{Level: 1}}
	second := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Second Hero", Class: model.Mage, Progress: model.Progress{Level: 1}}
	for _, character := range []model.Character{first, second} {
		if err := data.CreateCharacter(ctx, character); err != nil {
			t.Fatal(err)
		}
	}
	engine := game.NewEngine(game.DefaultTuning(), nil)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()

	firstConnection, kind, err := dialAndJoin(t, server.URL, token, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer firstConnection.Close()
	if kind != protocol.ServerWelcome {
		t.Fatalf("first join kind = %d, want welcome", kind)
	}
	secondConnection, kind, err := dialAndJoin(t, server.URL, token, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer secondConnection.Close()
	if kind != protocol.ServerError {
		t.Fatalf("second join kind = %d, want error", kind)
	}
	if engine.Present(second.ID) {
		t.Fatal("the refused character entered the world anyway")
	}
}

// dialAndJoin opens a socket, sends a join, and reports the kind of the single
// message the server answers with.
func dialAndJoin(t *testing.T, serverURL, token, characterID string) (*websocket.Conn, uint64, error) {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http"), nil)
	if err != nil {
		return nil, 0, err
	}
	var join []byte
	join = protowire.AppendTag(join, 1, protowire.VarintType)
	join = protowire.AppendVarint(join, protocol.ClientJoin)
	join = protowire.AppendTag(join, 2, protowire.BytesType)
	join = protowire.AppendString(join, token)
	join = protowire.AppendTag(join, 3, protowire.BytesType)
	join = protowire.AppendString(join, characterID)
	if err := connection.WriteMessage(websocket.BinaryMessage, join); err != nil {
		connection.Close()
		return nil, 0, err
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		connection.Close()
		return nil, 0, err
	}
	_, response, err := connection.ReadMessage()
	if err != nil {
		connection.Close()
		return nil, 0, err
	}
	number, wire, n := protowire.ConsumeTag(response)
	if n < 0 || number != 1 || wire != protowire.VarintType {
		connection.Close()
		return nil, 0, fmt.Errorf("invalid response: %x", response)
	}
	kind, read := protowire.ConsumeVarint(response[n:])
	if read < 0 {
		connection.Close()
		return nil, 0, fmt.Errorf("invalid response kind: %x", response)
	}
	return connection, kind, nil
}

// The loadout is committed over the socket and the safe-zone lock is enforced
// server-side, so a client that ignores its own disabled controls still cannot
// rearrange its kit in the field.
func TestWebSocketLoadoutCommitHonoursTheSafeZoneLock(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	ctx := context.Background()
	authService := auth.New(data, time.Hour)
	balance := game.DefaultTuning()
	engine := game.NewEngine(balance, data)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go engine.Run(runCtx)
	server := httptest.NewServer(NewWebSocket(authService, data, engine))
	defer server.Close()

	// One Mage logged out in the field and one that has never been placed, so
	// it enters on the hub spawn ring. Separate accounts, because an account
	// gets one body in the world at a time.
	field := &model.Point{X: balance.SafeRadius + 400}
	inField, fieldToken, _ := mage(t, ctx, data, authService, "field-kit@example.com", field)
	inHub, hubToken, hubAccount := mage(t, ctx, data, authService, "hub-kit@example.com", nil)

	request := loadoutEnvelope("starter-staff", []string{"", "fire-bolt"})
	fieldConn, kind, err := dialAndJoin(t, server.URL, fieldToken, inField)
	if err != nil || kind != protocol.ServerWelcome {
		t.Fatalf("field join = %d, %v", kind, err)
	}
	defer fieldConn.Close()
	if err := fieldConn.WriteMessage(websocket.BinaryMessage, request); err != nil {
		t.Fatal(err)
	}
	refused := readLoadoutReply(t, fieldConn)
	if refused.rejection == "" {
		t.Fatal("a commit from the field was accepted")
	}
	if refused.editable {
		t.Fatal("the field reported the loadout as editable")
	}
	if refused.spells[1] == "fire-bolt" {
		t.Fatal("the refusal reported the requested set rather than the one still equipped")
	}

	hubConn, kind, err := dialAndJoin(t, server.URL, hubToken, inHub)
	if err != nil || kind != protocol.ServerWelcome {
		t.Fatalf("hub join = %d, %v", kind, err)
	}
	defer hubConn.Close()
	if err := hubConn.WriteMessage(websocket.BinaryMessage, request); err != nil {
		t.Fatal(err)
	}
	accepted := readLoadoutReply(t, hubConn)
	if accepted.rejection != "" {
		t.Fatalf("a commit inside the hub was refused: %s", accepted.rejection)
	}
	if !accepted.editable || len(accepted.spells) < 2 || accepted.spells[1] != "fire-bolt" {
		t.Fatalf("reply = %#v", accepted)
	}

	// A committed set is persisted immediately, not only at the next autosave.
	deadline := time.Now().Add(2 * time.Second)
	for {
		reloaded, err := data.Character(ctx, hubAccount, inHub)
		if err != nil {
			t.Fatal(err)
		}
		if len(reloaded.State.Loadout.Spells) > 1 && reloaded.State.Loadout.Spells[1] == "fire-bolt" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved loadout = %#v", reloaded.State.Loadout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// mage registers an account with one Mage, optionally logged out at a position.
func mage(t *testing.T, ctx context.Context, data store.Store, authService *auth.Service, email string, at *model.Point) (characterID, token, accountID string) {
	t.Helper()
	token, err := authService.Register(ctx, email, "socket password")
	if err != nil {
		t.Fatal(err)
	}
	accountID, err = authService.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Kit " + email[:4], Class: model.Mage, Progress: model.Progress{Level: 1}}
	if err := data.CreateCharacter(ctx, character); err != nil {
		t.Fatal(err)
	}
	if at != nil {
		state := model.CharacterState{Position: *at, Placed: true, LastSeen: time.Now(), Materials: map[string]int{}}
		if err := data.SaveCharacterState(ctx, character.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	return character.ID, token, accountID
}

func loadoutEnvelope(weapon string, spells []string) []byte {
	var set []byte
	set = protowire.AppendTag(set, 1, protowire.BytesType)
	set = protowire.AppendString(set, weapon)
	for _, id := range spells {
		set = protowire.AppendTag(set, 3, protowire.BytesType)
		set = protowire.AppendString(set, id)
	}
	var envelope []byte
	envelope = protowire.AppendTag(envelope, 1, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, protocol.ClientLoadout)
	envelope = protowire.AppendTag(envelope, 6, protowire.BytesType)
	envelope = protowire.AppendBytes(envelope, set)
	return envelope
}

// loadoutReply is the part of a server loadout message this test reads. The
// server never decodes its own output, so the fields are pulled out here.
type loadoutReply struct {
	rejection string
	editable  bool
	spells    []string
}

// readLoadoutReply reads past the snapshots the world keeps broadcasting.
func readLoadoutReply(t *testing.T, connection *websocket.Conn) loadoutReply {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := connection.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if reply, ok := parseLoadoutReply(t, data); ok {
			return reply
		}
	}
	t.Fatal("no loadout reply arrived")
	return loadoutReply{}
}

func parseLoadoutReply(t *testing.T, data []byte) (loadoutReply, bool) {
	t.Helper()
	var reply loadoutReply
	loadout := false
	for len(data) > 0 {
		number, wire, n := protowire.ConsumeTag(data)
		if n < 0 {
			t.Fatalf("invalid reply: %x", data)
		}
		data = data[n:]
		switch number {
		case 1:
			kind, m := protowire.ConsumeVarint(data)
			if m < 0 {
				t.Fatalf("invalid kind: %x", data)
			}
			if kind != protocol.ServerLoadout {
				return reply, false
			}
			loadout = true
			data = data[m:]
			continue
		case 7:
			value, m := protowire.ConsumeString(data)
			if m < 0 {
				t.Fatalf("invalid error: %x", data)
			}
			reply.rejection, data = value, data[m:]
			continue
		case 9:
			value, m := protowire.ConsumeBytes(data)
			if m < 0 {
				t.Fatalf("invalid loadout: %x", data)
			}
			reply.spells, data = parseSpellSlots(t, value), data[m:]
			continue
		case 10:
			value, m := protowire.ConsumeVarint(data)
			if m < 0 {
				t.Fatalf("invalid editable flag: %x", data)
			}
			reply.editable, data = value != 0, data[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(number, wire, data)
		if m < 0 {
			t.Fatalf("invalid field %d: %x", number, data)
		}
		data = data[m:]
	}
	return reply, loadout
}

func parseSpellSlots(t *testing.T, data []byte) []string {
	t.Helper()
	var spells []string
	for len(data) > 0 {
		number, wire, n := protowire.ConsumeTag(data)
		if n < 0 {
			t.Fatalf("invalid loadout field: %x", data)
		}
		data = data[n:]
		if number == 3 {
			value, m := protowire.ConsumeString(data)
			if m < 0 {
				t.Fatalf("invalid spell slot: %x", data)
			}
			spells, data = append(spells, value), data[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(number, wire, data)
		if m < 0 {
			t.Fatalf("invalid loadout field %d: %x", number, data)
		}
		data = data[m:]
	}
	return spells
}
