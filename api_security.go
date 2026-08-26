package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

const maxAPIRequestBody = 1 << 20
const maxTileZoom = 20

func newAPIToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func secureAPI(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		if isPublicTileRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		if !validBearerToken(r.Header.Get("Authorization"), token) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized\n"))
			return
		}

		if requestHasBody(r) {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxAPIRequestBody)
		}

		next.ServeHTTP(w, r)
	})
}

func isPublicTileRequest(r *http.Request) bool {
	if r.Method != http.MethodGet || r.Header.Get("Origin") != "" {
		return false
	}
	if fetchSite := r.Header.Get("Sec-Fetch-Site"); fetchSite != "" && fetchSite != "same-origin" {
		return false
	}
	_, _, _, ok := parseTilePath(r.URL.Path)
	return ok
}

func parseTilePath(path string) (int, int, int, bool) {
	if !strings.HasPrefix(path, "/api/tiles/") {
		return 0, 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/tiles/"), "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[2], ".png") {
		return 0, 0, 0, false
	}
	parts[2] = strings.TrimSuffix(parts[2], ".png")
	values := [3]int{}
	for index, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return !unicode.IsDigit(r) || r > unicode.MaxASCII }) >= 0 {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, false
		}
		values[index] = value
	}
	z, x, y := values[0], values[1], values[2]
	if z > maxTileZoom || x >= 1<<z || y >= 1<<z {
		return 0, 0, 0, false
	}
	return z, x, y, true
}

func validBearerToken(header, token string) bool {
	const prefix = "Bearer "
	if token == "" || !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	if len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func requestHasBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	return r.ContentLength != 0
}
