package identity

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"motolink/internal/platform/apperr"
)

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// A deliberately tiny deny-list; real protection is length + argon2id + rate limit. [GIẢ ĐỊNH]
var commonPasswords = map[string]struct{}{
	"password": {}, "12345678": {}, "123456789": {}, "1234567890": {}, "qwertyui": {}, "qwerty123": {},
	"iloveyou": {}, "motolink": {}, "11111111": {}, "00000000": {}, "abc12345": {}, "password1": {},
	"matkhau123": {}, "12341234": {},
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	if s == "" || len(s) > 254 {
		return false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return false
	}
	at := strings.LastIndex(s, "@")
	return at > 0 && strings.Contains(s[at+1:], ".") && !strings.HasSuffix(s, ".")
}

func validPassword(p string) (code string, ok bool) {
	n := utf8.RuneCountInString(p)
	switch {
	case n < 8:
		return "too_short", false
	case len(p) > 256:
		return "too_long", false
	}
	if _, bad := commonPasswords[strings.ToLower(p)]; bad {
		return "too_common", false
	}
	return "", true
}

func validDisplayName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	return s, n >= 1 && n <= 50
}

func validateRegister(in *RegisterInput) error {
	var f []apperr.FieldError
	in.Email = normalizeEmail(in.Email)
	if !validEmail(in.Email) {
		f = append(f, apperr.FieldError{Field: "email", Code: "invalid"})
	}
	if code, ok := validPassword(in.Password); !ok {
		f = append(f, apperr.FieldError{Field: "password", Code: code})
	}
	if name, ok := validDisplayName(in.DisplayName); ok {
		in.DisplayName = name
	} else {
		f = append(f, apperr.FieldError{Field: "display_name", Code: "invalid_length"})
	}
	have := map[string]bool{}
	for _, c := range in.Consents {
		if strings.TrimSpace(c.Version) == "" {
			f = append(f, apperr.FieldError{Field: "consents", Code: "missing_version"})
			continue
		}
		if c.Kind == "terms" || c.Kind == "privacy_policy" {
			have[c.Kind] = true
		}
	}
	if !have["terms"] || !have["privacy_policy"] {
		f = append(f, apperr.FieldError{Field: "consents", Code: "terms_and_privacy_required"})
	}
	if err := validateDevice(in.Device); err != nil {
		f = append(f, err...)
	}
	if len(f) > 0 {
		return apperr.Validation(f...)
	}
	return nil
}

func validateDevice(d *DeviceInfo) []apperr.FieldError {
	if d == nil {
		return nil
	}
	var f []apperr.FieldError
	if d.DeviceID.String() == "00000000-0000-0000-0000-000000000000" {
		f = append(f, apperr.FieldError{Field: "device.device_id", Code: "required"})
	}
	if len(d.Model) > 100 || len(d.OSVersion) > 50 || len(d.AppVersion) > 50 {
		f = append(f, apperr.FieldError{Field: "device", Code: "too_long"})
	}
	return f
}

func validatePatch(p *ProfilePatch) error {
	var f []apperr.FieldError
	if p.DisplayName != nil {
		if name, ok := validDisplayName(*p.DisplayName); ok {
			p.DisplayName = &name
		} else {
			f = append(f, apperr.FieldError{Field: "display_name", Code: "invalid_length"})
		}
	}
	if p.AvatarColor != nil && !colorRe.MatchString(*p.AvatarColor) {
		f = append(f, apperr.FieldError{Field: "avatar_color", Code: "invalid"})
	}
	if p.Locale != nil && *p.Locale != "vi" && *p.Locale != "en" {
		f = append(f, apperr.FieldError{Field: "locale", Code: "unsupported"})
	}
	if len(f) > 0 {
		return apperr.Validation(f...)
	}
	return nil
}
