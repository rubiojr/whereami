package main

import (
	"os"
	"path/filepath"
	"strings"
)

// fileExists reports whether the given path exists and is a file (not a directory).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// xdgConfigDir returns $XDG_CONFIG_HOME or falls back to $HOME/.config.
func xdgConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home := os.Getenv("HOME")
	if home == "" {
		// Last resort: current working directory (should not normally happen in Flatpak)
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, ".config")
	}
	return filepath.Join(home, ".config")
}

// xdgCacheDir returns $XDG_CACHE_HOME or falls back to $HOME/.cache.
func xdgCacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return d
	}
	home := os.Getenv("HOME")
	if home == "" {
		// Last resort: current working directory (should not normally happen in Flatpak)
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, ".cache")
	}
	return filepath.Join(home, ".cache")
}

// xdgDataDir returns $XDG_DATA_HOME or falls back to $HOME/.local/share.
func xdgDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home := os.Getenv("HOME")
	if home == "" {
		// Last resort: current working directory (should not normally happen in Flatpak)
		cwd, _ := os.Getwd()
		return filepath.Join(cwd, ".local", "share")
	}
	return filepath.Join(home, ".local", "share")
}

// effectiveDataDir resolves the writable data directory used for persistent
// application state (tags, history, imports). It prefers the explicit global
// `dataDir` if set; otherwise it derives a XDG / HOME based fallback.
func effectiveDataDir() string {
	if dataDir != "" {
		return dataDir
	}
	// Try XDG_DATA_HOME
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		dir := filepath.Join(xdg, "whereami")
		_ = os.MkdirAll(dir, 0o755)
		return dir
	}
	// Fallback to $HOME/.local/share/whereami
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		dir := filepath.Join(home, ".local", "share", "whereami")
		_ = os.MkdirAll(dir, 0o755)
		return dir
	}
	return ""
}

// effectiveConfigDir resolves the configuration directory used for app settings.
// It prefers the explicit global `configDir` if set; otherwise it derives a XDG fallback.
func effectiveConfigDir() string {
	if configDir != "" {
		return configDir
	}
	return filepath.Join(xdgConfigDir(), "whereami")
}

// effectiveCacheDir resolves the cache directory used for temporary files.
// It prefers the explicit global `cacheDir` if set; otherwise it derives a XDG fallback.
func effectiveCacheDir() string {
	if cacheDir != "" {
		return cacheDir
	}
	return filepath.Join(xdgCacheDir(), "whereami")
}

// ensureDir creates the directory and any necessary parents if it doesn't exist.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
