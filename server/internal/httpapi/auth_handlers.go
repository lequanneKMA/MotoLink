package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"motolink/internal/identity"
	"motolink/internal/platform/apperr"
)

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, a.Client)
	return nil
}

func (a *API) register(w http.ResponseWriter, r *http.Request) error {
	if err := a.limit(r.Context(), "register:"+a.clientIP(r), int64(a.registerLimit()), time.Hour); err != nil {
		return err
	}
	var in identity.RegisterInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.UserAgent = r.UserAgent()
	pair, err := a.Identity.Register(r.Context(), in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, pair)
	return nil
}

func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	var in identity.LoginInput
	if err := decode(r, &in); err != nil {
		return err
	}
	ip := a.clientIP(r)
	// 5 attempts / 15 min per (ip, email); the email is hashed so it never lands in Redis keys.
	if err := a.limit(r.Context(), "login:"+ip+":"+shortHash(strings.ToLower(strings.TrimSpace(in.Email))), 5, 15*time.Minute); err != nil {
		return err
	}
	if err := a.limit(r.Context(), "login-ip:"+ip, 50, 15*time.Minute); err != nil {
		return err
	}
	in.UserAgent = r.UserAgent()
	pair, err := a.Identity.Login(r.Context(), in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pair)
	return nil
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) error {
	if err := a.limit(r.Context(), "refresh:"+a.clientIP(r), 120, time.Minute); err != nil {
		return err
	}
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	pair, err := a.Identity.Refresh(r.Context(), in.RefreshToken, r.UserAgent())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pair)
	return nil
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := a.Identity.Logout(r.Context(), p.SID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) logoutAll(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := a.Identity.LogoutAll(r.Context(), p.UserID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request, p Principal) error {
	u, err := a.Identity.GetUser(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	cs, err := a.Identity.Consents(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "consents": cs, "settings": map[string]any{}})
	return nil
}

func (a *API) patchMe(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in identity.ProfilePatch
	if err := decode(r, &in); err != nil {
		return err
	}
	u, err := a.Identity.UpdateProfile(r.Context(), p.UserID, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
	return nil
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

var _ = apperr.Unauthorized // keep import used if handlers change

func (a *API) registerLimit() int {
	if a.RegisterPerHour > 0 {
		return a.RegisterPerHour
	}
	return 10
}
