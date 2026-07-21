package transport

import (
	"context"
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
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: "Socket Hero", Class: model.Gunslinger, Level: 1}
	if err := data.CreateCharacter(context.Background(), character); err != nil {
		t.Fatal(err)
	}
	engine := game.NewEngine(game.DefaultTuning())
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
	engine := game.NewEngine(game.DefaultTuning())
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
