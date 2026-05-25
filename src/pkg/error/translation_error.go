package error

import "net/http"

// Translation errors carry their own HTTP status code and stable ErrCode so
// the existing error middleware can render them consistently — same shape as
// the existing pkg/error types (LoginError, AuthError, ...).
//
// Using typed string aliases keeps each error free-form for the message
// while the type carries the wire contract. New error variants get added by
// declaring another type and following the same trio of methods.

// TranslationDisabledError is returned when TRANSLATION_ENABLED=false.
type TranslationDisabledError string

func (e TranslationDisabledError) Error() string  { return string(e) }
func (e TranslationDisabledError) ErrCode() string { return "TRANSLATION_DISABLED" }
func (e TranslationDisabledError) StatusCode() int { return http.StatusServiceUnavailable }

// TranslationDeviceMissingError is returned when a translation call could
// not resolve a device id from the context. The middleware normally
// guarantees a device, so this only fires for direct usecase callers.
type TranslationDeviceMissingError string

func (e TranslationDeviceMissingError) Error() string  { return string(e) }
func (e TranslationDeviceMissingError) ErrCode() string { return "TRANSLATION_DEVICE_REQUIRED" }
func (e TranslationDeviceMissingError) StatusCode() int { return http.StatusBadRequest }

// TranslationMessageNotFoundError covers the specific case of a missing
// message lookup or a cross-device leak attempt. We keep the wire code
// stable so clients can distinguish "couldn't translate" from "input was bad".
type TranslationMessageNotFoundError string

func (e TranslationMessageNotFoundError) Error() string  { return string(e) }
func (e TranslationMessageNotFoundError) ErrCode() string { return "TRANSLATION_MESSAGE_NOT_FOUND" }
func (e TranslationMessageNotFoundError) StatusCode() int { return http.StatusNotFound }

// TranslationEmptyMessageError covers the message-has-no-text case. It's a
// 422 because the request itself is well-formed; the resource just has no
// translatable content.
type TranslationEmptyMessageError string

func (e TranslationEmptyMessageError) Error() string  { return string(e) }
func (e TranslationEmptyMessageError) ErrCode() string { return "TRANSLATION_NO_TEXT_CONTENT" }
func (e TranslationEmptyMessageError) StatusCode() int { return http.StatusUnprocessableEntity }

// TranslationProviderError wraps a provider failure (network, quota, parse).
// The 502 mirrors how upstream-dependent failures are usually surfaced.
type TranslationProviderError string

func (e TranslationProviderError) Error() string  { return string(e) }
func (e TranslationProviderError) ErrCode() string { return "TRANSLATION_PROVIDER_ERROR" }
func (e TranslationProviderError) StatusCode() int { return http.StatusBadGateway }
