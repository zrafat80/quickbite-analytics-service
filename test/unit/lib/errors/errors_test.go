package errors

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/errors"
	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
)

func TestAppErrorLifecycle(t *testing.T) {
	base := New("CODE", http.StatusTeapot, "message")
	assert.Equal(t, "CODE: message", base.Error())
	assert.Nil(t, base.Unwrap())

	cause := errors.New("cause")
	withCause := base.WithCause(cause)
	assert.NotSame(t, base, withCause)
	assert.Equal(t, "CODE: message: cause", withCause.Error())
	assert.ErrorIs(t, withCause, cause)

	found, ok := As(withCause)
	require.True(t, ok)
	assert.Equal(t, "CODE", found.Code)
	_, ok = As(errors.New("plain"))
	assert.False(t, ok)

	var nilError *AppError
	assert.Nil(t, nilError.WithCause(cause))
}

func TestWrapRendersKnownAndUnknownErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"known", New("BAD", http.StatusBadRequest, "bad"), http.StatusBadRequest, `"code":"BAD"`},
		{"known server", New("UPSTREAM", http.StatusBadGateway, "failed"), http.StatusBadGateway, `"code":"UPSTREAM"`},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, `"code":"INTERNAL_ERROR"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(logger.WithContext(request.Context(), log))
			recorder := httptest.NewRecorder()
			Wrap(func(http.ResponseWriter, *http.Request) error { return test.err }).ServeHTTP(recorder, request)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.wantBody)
		})
	}
}

func TestWrapLeavesSuccessfulHandlerResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
		return nil
	}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "ok", recorder.Body.String())
}
