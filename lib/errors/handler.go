package errors

import (
	"errors"
	"log/slog"
	"net/http"

	libhttp "github.com/zrafat80/quickbite/analytics-service/lib/http"
	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
)

// HandlerFunc is the Go-analogue of a NestJS controller method: a function
// taking (w, r) and returning an error. The Wrap middleware below converts
// it into a standard http.Handler that renders the envelope on error.
//
// Why a custom signature instead of plain http.Handler? Because returning an
// error from each handler matches how services/controllers already return
// `error` — no need to write `responseHelper.Error(w, err); return` at every
// failure point. http.Handler is preserved as the OUTER type via Wrap, so
// routers (chi) still see http.Handler everywhere.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// Wrap converts a HandlerFunc into http.HandlerFunc, logging + rendering on
// error. Any *AppError surfaces with its (code, status, message); anything
// else becomes a generic 500 INTERNAL_ERROR.
func Wrap(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			renderError(w, r, err)
		}
	}
}

func renderError(w http.ResponseWriter, r *http.Request, err error) {
	log := logger.FromContext(r.Context())

	var ae *AppError
	if errors.As(err, &ae) {
		if ae.Status >= 500 {
			log.Error("request failed", "code", ae.Code, "err", err.Error())
		} else {
			log.Warn("request rejected", "code", ae.Code, "status", ae.Status, "err", err.Error())
		}
		libhttp.SendError(w, ae.Status, ae.Code, ae.Message)
		return
	}

	log.Error("unhandled error", "err", err.Error(), slog.Any("type", "internal"))
	libhttp.SendError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}
