// Package notify stores in-app notifications and delivers push messages (docs/design/09 §5.2, ADR-015).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2/google"
)

// Push is one message to one device token. Data carries only identifiers (never coordinates).
type Push struct {
	Title, Body string
	Data        map[string]string
	High        bool // Android high priority (SOS)
}

// Pusher delivers a push message. ErrUnregistered means the token is dead and must be forgotten.
type Pusher interface {
	Send(ctx context.Context, token string, m Push) error
	Enabled() bool
}

var (
	ErrUnregistered = errors.New("notify: fcm token unregistered")
	ErrTransient    = errors.New("notify: transient push failure")
)

// NoopPusher is used when FCM is not configured (local dev, free tier before Firebase is set up).
type NoopPusher struct{}

func (NoopPusher) Send(context.Context, string, Push) error { return nil }
func (NoopPusher) Enabled() bool                            { return false }

// FCM talks to the HTTP v1 API with a service-account credential.
type FCM struct {
	project string
	hc      *http.Client
	base    string
}

// NewFCM builds a pusher from a service-account JSON file. An empty path returns NoopPusher.
func NewFCM(ctx context.Context, credFile, projectID string) (Pusher, error) {
	if credFile == "" {
		return NoopPusher{}, nil
	}
	raw, err := os.ReadFile(credFile)
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	creds, err := google.CredentialsFromJSON(ctx, raw, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("fcm credentials: %w", err)
	}
	if projectID == "" {
		projectID = creds.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("fcm: project id missing (set FCM_PROJECT_ID)")
	}
	hc := newOAuthClient(ctx, creds)
	hc.Timeout = 10 * time.Second
	return &FCM{project: projectID, hc: hc, base: "https://fcm.googleapis.com"}, nil
}

func (f *FCM) Enabled() bool { return true }

func (f *FCM) Send(ctx context.Context, token string, m Push) error {
	android := map[string]any{"priority": "normal"}
	if m.High {
		android = map[string]any{"priority": "high"}
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token": token, "data": m.Data, "android": android,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.base+"/v1/projects/"+f.project+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode == http.StatusNotFound || bytes.Contains(b, []byte("UNREGISTERED")):
		return ErrUnregistered
	case resp.StatusCode == http.StatusBadRequest && bytes.Contains(b, []byte("INVALID_ARGUMENT")):
		return ErrUnregistered // malformed / foreign token: useless for this user
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("fcm status %d: %s", resp.StatusCode, b)
	}
}
