package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/store"
)

func testAPI(t *testing.T) http.Handler {
	t.Helper()
	data, err := store.OpenSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	mux := http.NewServeMux()
	New(auth.New(data, time.Hour), data).RegisterRoutes(mux)
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
