package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	styleURL         = "https://tiles.openfreemap.org/styles/liberty"
	styleSHA256      = "6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60"
	spriteJSONURL    = "https://tiles.openfreemap.org/sprites/ofm_f384/ofm.json"
	spritePNGURL     = "https://tiles.openfreemap.org/sprites/ofm_f384/ofm.png"
	spriteJSONSHA256 = "73e75e58d8c7bb62cc25d9d150660500552f4d04f6eac5efa4e236076773c356"
	spritePNGSHA256  = "8996a519d218dc5f98015267709dae272a77bb74ef0ecc5a0992dcf276c1be4c"
)

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	fetchPinned(client, styleURL, "liberty_style.json", styleSHA256, 2<<20)
	fetchPinned(client, spriteJSONURL, "liberty_sprite.json", spriteJSONSHA256, 2<<20)
	fetchPinned(client, spritePNGURL, "liberty_sprite.png", spritePNGSHA256, 8<<20)
}

func fetchPinned(client *http.Client, url, output, expected string, maximumBytes int64) {
	response, err := client.Get(url)
	if err != nil {
		fatal("fetch %s: %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fatal("fetch %s: unexpected status %s", url, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if err != nil {
		fatal("read %s: %v", url, err)
	}
	if int64(len(data)) > maximumBytes {
		fatal("read %s: exceeds %d-byte limit", url, maximumBytes)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	if expected == "" {
		fmt.Fprintf(os.Stderr, "%s SHA-256: %s\n", output, actual)
	} else if actual != expected {
		fatal("%s checksum changed: got %s, want %s", output, actual, expected)
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		fatal("write %s: %v", output, err)
	}
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
