package main

import (
	"bytes"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAPIToken(t *testing.T) {
	token, err := newAPIToken()
	require.NoError(t, err)
	assert.Len(t, token, 64)
	_, err = hex.DecodeString(token)
	assert.NoError(t, err)
}

func TestSecureAPIRequiresBearerToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	handler := secureAPI(token, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong", header: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "same length wrong", header: "Bearer " + token[:63] + "0", wantStatus: http.StatusUnauthorized},
		{name: "valid", header: "Bearer " + token, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/waypoints", nil)
			req.Header.Set("Authorization", tt.header)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, req)
			assert.Equal(t, tt.wantStatus, resp.Code)
		})
	}
}

func TestSecureAPIRequiresJSONAndLimitsBody(t *testing.T) {
	const token = "secret"
	handler := secureAPI(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	t.Run("content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/bookmarks", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusUnsupportedMediaType, resp.Code)
	})

	t.Run("body limit", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), maxAPIRequestBody+1)
		req := httptest.NewRequest(http.MethodPost, "/api/bookmarks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})
}
