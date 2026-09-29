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
	"strconv"
	"strings"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/mvt"
)

const MaxBytes = mvt.MaxTileBytes

var ErrLimit = errors.New("tile I/O limit exceeded")

// MaxRetryAfter bounds how long a server can ask a client to wait.
const MaxRetryAfter = 24 * time.Hour

// StatusError preserves HTTP status for caller-owned retry/missing policy.
// RetryAfter is the response's Retry-After delay, at most MaxRetryAfter, and
// zero when the header is absent, invalid or already past.
type StatusError struct {
	Code       int
	Status     string
	RetryAfter time.Duration
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
		return nil, &StatusError{response.StatusCode, response.Status, RetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	data, err := Read(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read tile response: %w", err)
	}
	return data, nil
}

// RetryAfter parses a Retry-After value, delay seconds or an HTTP date, into a
// delay from now of at most MaxRetryAfter. It is zero for an absent, invalid or
// past value.
func RetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	var delay time.Duration
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		delay = MaxRetryAfter
		if seconds < uint64(MaxRetryAfter/time.Second) {
			delay = time.Duration(seconds) * time.Second
		}
	} else if at, err := http.ParseTime(value); err == nil {
		delay = at.Sub(now)
	}
	return min(max(delay, 0), MaxRetryAfter)
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
