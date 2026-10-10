package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"motolink/internal/notify"
	"motolink/internal/platform/apperr"
)

// NotificationStore is the inbox behind GET /notifications.
type NotificationStore interface {
	List(ctx context.Context, user uuid.UUID, cursor string, limit int) ([]notify.Item, string, error)
	MarkRead(ctx context.Context, user uuid.UUID, ids []uuid.UUID, all bool) error
}

func (a *API) notificationRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/notifications", a.authed(a.listNotifications))
	mux.Handle("POST /v1/notifications/read", a.authed(a.readNotifications))
}

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request, p Principal) error {
	items, next, err := a.Notifications.List(r.Context(), p.UserID, r.URL.Query().Get("cursor"), queryInt(r, "limit", 30))
	if err != nil {
		return apperr.BadRequest("cursor không hợp lệ")
	}
	var nc any
	if next != "" {
		nc = next
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nc})
	return nil
}

func (a *API) readNotifications(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		IDs []uuid.UUID `json:"ids"`
		All bool        `json:"all"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if !in.All && len(in.IDs) == 0 {
		return apperr.BadRequest("cần ids hoặc all")
	}
	if len(in.IDs) > 200 {
		return apperr.BadRequest("tối đa 200 ids")
	}
	if err := a.Notifications.MarkRead(r.Context(), p.UserID, in.IDs, in.All); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
