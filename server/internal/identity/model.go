package identity

import (
	"time"

	"github.com/google/uuid"
)

// User is the public profile shape (docs/design/06).
type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarColor string    `json:"avatar_color"`
	Locale      string    `json:"locale"`
	CreatedAt   time.Time `json:"created_at"`
}

// DeviceInfo is sent at register/login (docs/design/06 §2.1).
type DeviceInfo struct {
	DeviceID   uuid.UUID `json:"device_id"`
	Model      string    `json:"model"`
	OSVersion  string    `json:"os_version"`
	AppVersion string    `json:"app_version"`
	HasGMS     bool      `json:"has_gms"`
}

// ConsentInput records acceptance of a document version.
type ConsentInput struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
}

type RegisterInput struct {
	Email       string         `json:"email"`
	Password    string         `json:"password"`
	DisplayName string         `json:"display_name"`
	Device      *DeviceInfo    `json:"device"`
	Consents    []ConsentInput `json:"consents"`
	UserAgent   string         `json:"-"`
}

type LoginInput struct {
	Email     string      `json:"email"`
	Password  string      `json:"password"`
	Device    *DeviceInfo `json:"device"`
	UserAgent string      `json:"-"`
}

// TokenPair is returned by register/login/refresh.
type TokenPair struct {
	User         *User  `json:"user,omitempty"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// ProfilePatch updates mutable profile fields; nil means unchanged.
type ProfilePatch struct {
	DisplayName *string `json:"display_name"`
	AvatarColor *string `json:"avatar_color"`
	Locale      *string `json:"locale"`
}
