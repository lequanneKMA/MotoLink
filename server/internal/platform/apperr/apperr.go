// Package apperr defines API errors with stable machine codes (docs/design/06 §1.4).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// FieldError describes one invalid field.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Error is an error that maps to an HTTP response.
type Error struct {
	Status     int
	Code       string
	Title      string
	Detail     string
	Fields     []FieldError
	RetryAfter int            // seconds, for 429
	Extra      map[string]any // machine-readable context, e.g. the open session for session_already_open
	Cause      error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Title, e.Cause)
	}
	return e.Code + ": " + e.Title
}
func (e *Error) Unwrap() error { return e.Cause }

func new(status int, code, title string) *Error {
	return &Error{Status: status, Code: code, Title: title}
}

func BadRequest(detail string) *Error {
	e := new(http.StatusBadRequest, "bad_request", "Yêu cầu không hợp lệ")
	e.Detail = detail
	return e
}
func Validation(fields ...FieldError) *Error {
	e := new(http.StatusUnprocessableEntity, "validation_failed", "Dữ liệu không hợp lệ")
	e.Fields = fields
	return e
}
func Unauthorized() *Error {
	return new(http.StatusUnauthorized, "unauthorized", "Cần đăng nhập")
}
func TokenExpired() *Error {
	return new(http.StatusUnauthorized, "token_expired", "Phiên đã hết hạn")
}
func SessionRevoked() *Error {
	return new(http.StatusUnauthorized, "session_revoked", "Phiên đã bị thu hồi")
}
func InvalidCredentials() *Error {
	return new(http.StatusUnauthorized, "invalid_credentials", "Email hoặc mật khẩu không đúng")
}
func InvalidRefresh() *Error {
	return new(http.StatusUnauthorized, "invalid_refresh", "Refresh token không hợp lệ")
}
func RefreshReused() *Error {
	return new(http.StatusUnauthorized, "refresh_reused", "Refresh token đã bị dùng lại, mọi phiên liên quan bị thu hồi")
}
func Forbidden() *Error { return new(http.StatusForbidden, "forbidden", "Không đủ quyền") }
func NotFound() *Error  { return new(http.StatusNotFound, "not_found", "Không tìm thấy") }
func EmailTaken() *Error {
	return new(http.StatusConflict, "email_taken", "Email đã được đăng ký")
}
func Conflict(code, title string) *Error { return new(http.StatusConflict, code, title) }
func Gone(code, title string) *Error     { return new(http.StatusGone, code, title) }
func ForbiddenCode(code, title string) *Error {
	return new(http.StatusForbidden, code, title)
}
func Unavailable() *Error {
	return new(http.StatusServiceUnavailable, "unavailable", "Dịch vụ tạm thời không khả dụng")
}
func RequestInProgress() *Error {
	return new(http.StatusConflict, "request_in_progress", "Yêu cầu đang được xử lý")
}
func IdempotencyKeyReuse() *Error {
	return new(http.StatusUnprocessableEntity, "idempotency_key_reuse", "Idempotency-Key đã dùng cho yêu cầu khác")
}
func PayloadTooLarge() *Error {
	return new(http.StatusRequestEntityTooLarge, "payload_too_large", "Nội dung quá lớn")
}
func RateLimited(retryAfterSeconds int) *Error {
	e := new(http.StatusTooManyRequests, "rate_limited", "Quá nhiều yêu cầu")
	e.RetryAfter = retryAfterSeconds
	return e
}
func Internal(cause error) *Error {
	e := new(http.StatusInternalServerError, "internal", "Lỗi hệ thống")
	e.Cause = cause
	return e
}

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
