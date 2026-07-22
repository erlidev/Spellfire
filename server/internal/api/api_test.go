package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/crafting"
	"spellfire/server/internal/game"
	"spellfire/server/internal/loadout"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/store"
	"spellfire/server/internal/tuning"
)

type recordingAdminTools struct {
	spawn      game.AdminSpawn
	attributes map[string]float64
	materials  map[string]int
	level      int
	state      game.AdminEntityState
	entityID   string
}

func (r *recordingAdminTools) AdminSpawn(_ string, spawn game.AdminSpawn) error {
	r.spawn = spawn
	return nil
}

func (r *recordingAdminTools) SetAdminAttributes(_ string, attributes map[string]float64) error {
	r.attributes = attributes
	return nil
}

func (r *recordingAdminTools) AdminInspect(_, entityID string) (game.AdminEntityState, error) {
	r.entityID = entityID
	return game.AdminEntityState{ID: entityID, DefinitionID: "tree", Values: map[string]string{"vitals.health": "500"}}, nil
}

func (r *recordingAdminTools) AdminEdit(_, entityID string, attributes map[string]string) (game.AdminEntityState, error) {
	r.entityID = entityID
	r.state = game.AdminEntityState{ID: entityID, DefinitionID: "tree", Values: attributes}
	return r.state, nil
}

func (r *recordingAdminTools) AdminDelete(_, entityID string) error {
	r.entityID = entityID
	return nil
}

func (r *recordingAdminTools) GrantMaterials(_ string, materials map[string]int) error {
	r.materials = materials
	return nil
}

func (r *recordingAdminTools) GrantProgress(_ string, level int) error {
	r.level = level
	return nil
}

func testAPI(t *testing.T, adminEmails ...string) http.Handler {
	t.Helper()
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	mux := http.NewServeMux()
	application := New(auth.New(data, time.Hour, adminEmails...), data, tuning.MustLoad())
	application.RegisterRoutes(mux)
	mux.HandleFunc("GET /api/admin-test", application.withAdmin(func(w http.ResponseWriter, _ *http.Request, _ auth.Principal, _ string) {
		w.WriteHeader(http.StatusNoContent)
	}))
	return mux
}

func request(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &payload)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func tokenFrom(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}

func TestAccountAndCharacterHTTPFlow(t *testing.T) {
	handler := testAPI(t)
	registered := request(t, handler, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "hero@example.com", "password": "long password"})
	if registered.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", registered.Code, registered.Body.String())
	}
	token := tokenFrom(t, registered)
	unauthorized := request(t, handler, http.MethodGet, "/api/characters", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized = %d", unauthorized.Code)
	}
	invalid := request(t, handler, http.MethodPost, "/api/characters", token, map[string]string{"name": "x", "class": "paladin"})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid character = %d %s", invalid.Code, invalid.Body.String())
	}
	created := request(t, handler, http.MethodPost, "/api/characters", token, map[string]string{"name": "Ember Fox", "class": "mage"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	listed := request(t, handler, http.MethodGet, "/api/characters", token, nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("list = %d", listed.Code)
	}
	var list struct {
		Characters []struct{ Name, Class string } `json:"characters"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Characters) != 1 || list.Characters[0].Name != "Ember Fox" || list.Characters[0].Class != "mage" {
		t.Fatalf("characters = %#v", list.Characters)
	}
	duplicate := request(t, handler, http.MethodPost, "/api/characters", token, map[string]string{"name": "Ember Fox", "class": "gunslinger"})
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate = %d", duplicate.Code)
	}
	logout := request(t, handler, http.MethodPost, "/api/auth/logout", token, nil)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", logout.Code)
	}
	if got := request(t, handler, http.MethodGet, "/api/characters", token, nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("token after logout = %d", got)
	}
}

// A character is created with its starter kit already in the ledger, so it can
// fill a coherent loadout before it has earned or harvested anything.
func TestCharacterCreationRollsTheStarterKit(t *testing.T) {
	handler := testAPI(t)
	registered := request(t, handler, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "kit@example.com", "password": "long password"})
	token := tokenFrom(t, registered)
	created := request(t, handler, http.MethodPost, "/api/characters", token, map[string]string{"name": "Ember Fox", "class": "mage"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var character struct {
		Level   int      `json:"level"`
		XP      int      `json:"xp"`
		Unlocks []string `json:"unlocks"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &character); err != nil {
		t.Fatal(err)
	}
	if character.Level != 1 || character.XP != 0 {
		t.Fatalf("new character = level %d, xp %d", character.Level, character.XP)
	}
	tables := tuning.MustLoad()
	ledger := progression.New(character.Unlocks)
	if len(loadout.Equippable(tables, model.Mage, crafting.Inventory{Ledger: ledger}, loadout.KindWeapon)) == 0 {
		t.Fatalf("a new character owns no weapon of its class: %v", character.Unlocks)
	}
	// The kit is persisted, not recomputed: the listing carries the same ledger.
	listed := request(t, handler, http.MethodGet, "/api/characters", token, nil)
	var list struct {
		Characters []struct {
			Unlocks []string `json:"unlocks"`
		} `json:"characters"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Characters) != 1 || !reflect.DeepEqual(list.Characters[0].Unlocks, ledger.IDs()) {
		t.Fatalf("stored ledger = %#v, want %v", list.Characters, ledger.IDs())
	}
}

func TestAuthenticationErrorsDoNotLeakAccountExistence(t *testing.T) {
	handler := testAPI(t)
	request(t, handler, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "hero@example.com", "password": "long password"})
	wrong := request(t, handler, http.MethodPost, "/api/auth/login", "", map[string]string{"email": "hero@example.com", "password": "wrong password"})
	missing := request(t, handler, http.MethodPost, "/api/auth/login", "", map[string]string{"email": "nobody@example.com", "password": "wrong password"})
	if wrong.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized || wrong.Body.String() != missing.Body.String() {
		t.Fatalf("credential responses differ: %d %q vs %d %q", wrong.Code, wrong.Body.String(), missing.Code, missing.Body.String())
	}
}

func TestJSONDecoderRejectsUnknownAndTrailingData(t *testing.T) {
	handler := testAPI(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"a@example.com","password":"password","admin":true}`))
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", w.Code)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"a@example.com","password":"password"}{}`))
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing object = %d", w.Code)
	}
}

func TestAdminRoleComesFromConfiguredAccountEmail(t *testing.T) {
	handler := testAPI(t, " ADMIN@EXAMPLE.COM ")
	admin := request(t, handler, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "admin@example.com", "password": "long password"})
	if admin.Code != http.StatusCreated {
		t.Fatalf("admin register = %d %s", admin.Code, admin.Body.String())
	}
	adminToken := tokenFrom(t, admin)
	var session struct {
		Account struct {
			Email string `json:"email"`
			Admin bool   `json:"is_admin"`
		} `json:"account"`
	}
	if err := json.Unmarshal(admin.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Account.Email != "admin@example.com" || !session.Account.Admin {
		t.Fatalf("admin session = %#v", session.Account)
	}
	if got := request(t, handler, http.MethodGet, "/api/admin-test", adminToken, nil).Code; got != http.StatusNoContent {
		t.Fatalf("admin route = %d", got)
	}

	player := request(t, handler, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "player@example.com", "password": "long password"})
	playerToken := tokenFrom(t, player)
	if got := request(t, handler, http.MethodGet, "/api/admin-test", playerToken, nil).Code; got != http.StatusForbidden {
		t.Fatalf("player admin route = %d", got)
	}
	account := request(t, handler, http.MethodGet, "/api/account", playerToken, nil)
	if account.Code != http.StatusOK || strings.Contains(account.Body.String(), `"is_admin":true`) {
		t.Fatalf("player account = %d %s", account.Code, account.Body.String())
	}
	if got := request(t, handler, http.MethodGet, "/api/admin-test", "", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous admin route = %d", got)
	}
}

func TestAdminWorldControlsRequireAdminAndCharacterOwnership(t *testing.T) {
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "admin-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	tools := &recordingAdminTools{}
	mux := http.NewServeMux()
	New(auth.New(data, time.Hour, "admin@example.com"), data, tuning.MustLoad(), tools).RegisterRoutes(mux)
	admin := request(t, mux, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "admin@example.com", "password": "long password"})
	adminToken := tokenFrom(t, admin)
	character := request(t, mux, http.MethodPost, "/api/characters", adminToken, map[string]string{"name": "Admin Hero", "class": "mage"})
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(character.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	spawn := request(t, mux, http.MethodPost, "/api/admin/spawn", adminToken, map[string]any{"character_id": created.ID, "spawn_id": "training-mage", "x": 42.0, "y": -15.0, "config": map[string]string{"name": "Target"}})
	if spawn.Code != http.StatusNoContent || tools.spawn.ID != "training-mage" || tools.spawn.Position != (game.Vec{X: 42, Y: -15}) || tools.spawn.Config["name"] != "Target" {
		t.Fatalf("admin spawn = %d %#v", spawn.Code, tools.spawn)
	}
	attributes := request(t, mux, http.MethodPost, "/api/admin/attributes", adminToken, map[string]any{"character_id": created.ID, "attributes": map[string]float64{"speed_multiplier": 2}})
	if attributes.Code != http.StatusNoContent || tools.attributes["speed_multiplier"] != 2 {
		t.Fatalf("admin attributes = %d %#v", attributes.Code, tools.attributes)
	}
	inspect := request(t, mux, http.MethodPost, "/api/admin/entity/inspect", adminToken, map[string]any{"character_id": created.ID, "entity_id": "tree-01"})
	if inspect.Code != http.StatusOK || tools.entityID != "tree-01" || !strings.Contains(inspect.Body.String(), `"definition_id":"tree"`) {
		t.Fatalf("admin inspect = %d %s", inspect.Code, inspect.Body.String())
	}
	edit := request(t, mux, http.MethodPost, "/api/admin/entity/edit", adminToken, map[string]any{"character_id": created.ID, "entity_id": "tree-01", "attributes": map[string]string{"vitals.health": "250"}})
	if edit.Code != http.StatusOK || tools.state.Values["vitals.health"] != "250" {
		t.Fatalf("admin edit = %d %#v", edit.Code, tools.state)
	}
	removed := request(t, mux, http.MethodPost, "/api/admin/entity/delete", adminToken, map[string]any{"character_id": created.ID, "entity_id": "tree-01"})
	if removed.Code != http.StatusNoContent || tools.entityID != "tree-01" {
		t.Fatalf("admin delete = %d %q", removed.Code, tools.entityID)
	}
	player := request(t, mux, http.MethodPost, "/api/auth/register", "", map[string]string{"email": "player@example.com", "password": "long password"})
	if got := request(t, mux, http.MethodPost, "/api/admin/spawn", tokenFrom(t, player), map[string]any{"character_id": created.ID, "spawn_id": "training-mage", "x": 0, "y": 0, "config": map[string]string{}}).Code; got != http.StatusForbidden {
		t.Fatalf("non-admin spawn = %d", got)
	}
}
