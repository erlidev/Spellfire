package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/build"
	"spellfire/server/internal/game"
	"spellfire/server/internal/model"
	"spellfire/server/internal/progression"
	"spellfire/server/internal/store"
	"spellfire/server/internal/tuning"
)

var characterName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{1,18}[A-Za-z0-9]$`)

type API struct {
	auth  *auth.Service
	store store.Store
	// tables are the versioned tuning tables character creation draws the
	// starter kit from. The HTTP layer reads them; it never authors a value.
	tables     *tuning.Tables
	adminTools AdminController
}

// AdminController is the narrow, server-authoritative seam the HTTP layer
// needs for developer mode. It deliberately exposes neither World nor any
// account/session internals to an admin request.
type AdminController interface {
	AdminSpawn(string, game.AdminSpawn) error
	SetAdminAttributes(string, map[string]float64) error
}

func New(authService *auth.Service, data store.Store, tables *tuning.Tables, adminTools ...AdminController) *API {
	api := &API{auth: authService, store: data, tables: tables}
	if len(adminTools) > 0 {
		api.adminTools = adminTools[0]
	}
	return api
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, build.Get())
	})
	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.withAccount(a.logout))
	mux.HandleFunc("GET /api/account", a.withAccount(a.account))
	mux.HandleFunc("POST /api/admin/spawn", a.withAdmin(a.adminSpawn))
	mux.HandleFunc("POST /api/admin/attributes", a.withAdmin(a.adminAttributes))
	mux.HandleFunc("GET /api/characters", a.withAccount(a.characters))
	mux.HandleFunc("POST /api/characters", a.withAccount(a.createCharacter))
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !decodeJSON(w, r, &body) {
		return
	}
	token, err := a.auth.Register(r.Context(), body.Email, body.Password)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "An account already uses that email.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "Use a valid email and a password of 8 to 72 characters.")
		return
	}
	a.writeSession(w, r, http.StatusCreated, token)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !decodeJSON(w, r, &body) {
		return
	}
	token, err := a.auth.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Email or password is incorrect.")
		return
	}
	a.writeSession(w, r, http.StatusOK, token)
}

type accountHandler func(http.ResponseWriter, *http.Request, auth.Principal, string)

type sessionResponse struct {
	Token   string         `json:"token"`
	Account auth.Principal `json:"account"`
}

func (a *API) writeSession(w http.ResponseWriter, r *http.Request, status int, token string) {
	principal, err := a.auth.AuthenticatePrincipal(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not start the session.")
		return
	}
	writeJSON(w, status, sessionResponse{Token: token, Account: principal})
}

func (a *API) withAccount(next accountHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		principal, err := a.auth.AuthenticatePrincipal(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Your session has expired. Sign in again.")
			return
		}
		next(w, r, principal, token)
	}
}

// withAdmin is the opt-in authorization boundary for privileged features. A
// future endpoint becomes admin-only by registering it through this wrapper;
// handlers never trust a role supplied in request JSON or by the browser.
func (a *API) withAdmin(next accountHandler) http.HandlerFunc {
	return a.withAccount(func(w http.ResponseWriter, r *http.Request, principal auth.Principal, token string) {
		if !principal.Admin {
			writeError(w, http.StatusForbidden, "Administrator access is required.")
			return
		}
		next(w, r, principal, token)
	})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, _ auth.Principal, token string) {
	if err := a.auth.Logout(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign out.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) account(w http.ResponseWriter, _ *http.Request, principal auth.Principal, _ string) {
	writeJSON(w, http.StatusOK, principal)
}

type adminSpawnRequest struct {
	CharacterID string            `json:"character_id"`
	SpawnID     string            `json:"spawn_id"`
	X           float64           `json:"x"`
	Y           float64           `json:"y"`
	Config      map[string]string `json:"config"`
}

func (a *API) adminSpawn(w http.ResponseWriter, r *http.Request, principal auth.Principal, _ string) {
	var body adminSpawnRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !a.adminCharacter(w, r, principal, body.CharacterID) {
		return
	}
	if a.adminTools == nil {
		writeError(w, http.StatusServiceUnavailable, "Developer tools are not available.")
		return
	}
	if err := a.adminTools.AdminSpawn(body.CharacterID, game.AdminSpawn{ID: body.SpawnID, Position: game.Vec{X: body.X, Y: body.Y}, Config: body.Config}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type adminAttributesRequest struct {
	CharacterID string             `json:"character_id"`
	Attributes  map[string]float64 `json:"attributes"`
}

func (a *API) adminAttributes(w http.ResponseWriter, r *http.Request, principal auth.Principal, _ string) {
	var body adminAttributesRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if !a.adminCharacter(w, r, principal, body.CharacterID) {
		return
	}
	if a.adminTools == nil {
		writeError(w, http.StatusServiceUnavailable, "Developer tools are not available.")
		return
	}
	if err := a.adminTools.SetAdminAttributes(body.CharacterID, body.Attributes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adminCharacter(w http.ResponseWriter, r *http.Request, principal auth.Principal, characterID string) bool {
	if characterID == "" {
		writeError(w, http.StatusBadRequest, "Choose a character in the world.")
		return false
	}
	if _, err := a.store.Character(r.Context(), principal.AccountID, characterID); err != nil {
		writeError(w, http.StatusNotFound, "Character unavailable.")
		return false
	}
	return true
}

func (a *API) characters(w http.ResponseWriter, r *http.Request, principal auth.Principal, _ string) {
	characters, err := a.store.Characters(r.Context(), principal.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load characters.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"characters": characters})
}

func (a *API) createCharacter(w http.ResponseWriter, r *http.Request, principal auth.Principal, _ string) {
	var body struct {
		Name  string      `json:"name"`
		Class model.Class `json:"class"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if !characterName.MatchString(body.Name) || !body.Class.Valid() {
		writeError(w, http.StatusBadRequest, "Choose a 3–20 character name and a valid class.")
		return
	}
	characters, err := a.store.Characters(r.Context(), principal.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not validate the character.")
		return
	}
	if len(characters) >= 4 {
		writeError(w, http.StatusConflict, "This account already has four characters.")
		return
	}
	// The starter kit is rolled here, at creation, so the character owns a
	// coherent set of tools before it ever enters the world: one weapon drawn
	// from its class's basic set plus a random low-tier draw of its slot kind.
	// The draw is seeded from the character's own ID, so it is stable.
	id := auth.NewID()
	character := model.Character{
		ID: id, AccountID: principal.AccountID, Name: body.Name, Class: body.Class,
		Progress: model.Progress{Level: 1, Unlocks: progression.StarterKit(a.tables, body.Class, id)},
	}
	if err = a.store.CreateCharacter(r.Context(), character); errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "That character name is already in use on this account.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the character.")
		return
	}
	writeJSON(w, http.StatusCreated, character)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not valid.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "The request must contain one JSON object.")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
