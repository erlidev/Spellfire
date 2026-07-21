package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"spellfire/server/internal/auth"
	"spellfire/server/internal/model"
	"spellfire/server/internal/store"
)

var characterName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{1,18}[A-Za-z0-9]$`)

type API struct {
	auth  *auth.Service
	store store.Store
}

func New(authService *auth.Service, data store.Store) *API {
	return &API{auth: authService, store: data}
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.withAccount(a.logout))
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
	writeJSON(w, http.StatusCreated, map[string]string{"token": token})
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
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

type accountHandler func(http.ResponseWriter, *http.Request, string, string)

func (a *API) withAccount(next accountHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		accountID, err := a.auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Your session has expired. Sign in again.")
			return
		}
		next(w, r, accountID, token)
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, _ string, token string) {
	if err := a.auth.Logout(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not sign out.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) characters(w http.ResponseWriter, r *http.Request, accountID, _ string) {
	characters, err := a.store.Characters(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not load characters.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"characters": characters})
}

func (a *API) createCharacter(w http.ResponseWriter, r *http.Request, accountID, _ string) {
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
	characters, err := a.store.Characters(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not validate the character.")
		return
	}
	if len(characters) >= 4 {
		writeError(w, http.StatusConflict, "This account already has four characters.")
		return
	}
	character := model.Character{ID: auth.NewID(), AccountID: accountID, Name: body.Name, Class: body.Class, Level: 1}
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
