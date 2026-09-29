// Package tileio shares bounded tile/glyph transport and checksum-pair disk cache
// mechanics with the Qt-bound vecmap adapter. It owns no decoding or scheduling.
package tileio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
)

const MaxBytes = mvt.MaxTileBytes

var ErrLimit = errors.New("tile I/O limit exceeded")

// StatusError preserves HTTP status for caller-owned retry/missing policy.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string { return "fetch tile: unexpected HTTP status " + e.Status }

func NewClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: CheckRedirect}
}

func CheckRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 10 {
		return errors.New("refuse invalid tile redirect chain")
	}
	original := via[0].URL
	if request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Host, original.Host) {
		return fmt.Errorf("refuse tile redirect from %s to %s", original.Host, request.URL.Host)
	}
	return nil
}

// Fetch returns the bounded body of a 200 response. A 204 response returns no
// data and no error; other statuses return a StatusError.
func Fetch(ctx context.Context, client *http.Client, url, accept string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create tile request: %w", err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "vecmap/1")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch tile: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return []byte{}, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, &StatusError{response.StatusCode, response.Status}
	}
	data, err := Read(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read tile response: %w", err)
	}
	return data, nil
}

func ReadFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Read(file)
}

func Read(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%w: tile exceeds %d-byte limit", ErrLimit, MaxBytes)
	}
	return data, nil
}

func Verify(data []byte, expected string) error {
	actual := Checksum(data)
	if actual != expected {
		return fmt.Errorf("tile checksum mismatch: got %s", actual)
	}
	return nil
}

func Checksum(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
