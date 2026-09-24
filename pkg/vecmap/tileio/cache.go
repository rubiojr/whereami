package tileio

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultCacheBytes int64 = 256 << 20
const MaxCacheFiles = 16384

// Preserve the parent's process-wide serialization of pair reads/writes/eviction.
// Directory is an application-owned private cache, not an untrusted filesystem.
var cacheMu sync.Mutex

func ReadCached(path, expected string) ([]byte, string, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	data, err := ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if expected == "" {
		checksum, err := ReadFile(path + ".sha256")
		if err != nil {
			return nil, "", err
		}
		expected = strings.TrimSpace(string(checksum))
		decoded, err := hex.DecodeString(expected)
		if err != nil || len(decoded) != sha256.Size {
			return nil, "", errors.New("cached tile checksum is invalid")
		}
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	if expected != "" {
		_ = os.Chtimes(path+".sha256", now, now)
	}
	return data, expected, nil
}

func WritePair(directory, name string, data []byte, checksum, pinned string) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if len(data) > MaxBytes {
		return ErrLimit
	}
	if len(checksum) != sha256.Size*2 {
		return errors.New("invalid cache checksum")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return err
	}
	if err := WriteAtomic(directory, name, data); err != nil {
		return err
	}
	if err := WriteAtomic(directory, name+".sha256", []byte(checksum+"\n")); err != nil {
		_ = os.Remove(filepath.Join(directory, name))
		_ = os.Remove(filepath.Join(directory, name+".sha256"))
		return err
	}
	if err := enforce(directory, pinned, DefaultCacheBytes); err != nil {
		// Do not let tiny responses grow an unbounded metadata cache. Cache
		// admission is optional for loading; callers can still use the response.
		_ = os.Remove(filepath.Join(directory, name))
		_ = os.Remove(filepath.Join(directory, name+".sha256"))
		return err
	}
	return nil
}

type cacheEntry struct {
	base     string
	paths    []string
	size     int64
	modified time.Time
}

func Enforce(directory, pinned string, maximumBytes int64) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	return enforce(directory, pinned, maximumBytes)
}

func enforce(directory, pinned string, maximumBytes int64) error {
	if directory == "" || maximumBytes <= 0 {
		return nil
	}
	entries := make(map[string]*cacheEntry)
	var cleanupErrors []error
	visited := 0
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		visited++
		if visited > MaxCacheFiles {
			return ErrLimit
		}
		if walkErr != nil {
			cleanupErrors = append(cleanupErrors, walkErr)
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".tile-") {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, err)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			cleanupErrors = append(cleanupErrors, err)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		base := strings.TrimSuffix(path, ".sha256")
		item := entries[base]
		if item == nil {
			item = &cacheEntry{base: base}
			entries[base] = item
		}
		item.paths = append(item.paths, path)
		// Controlled cache files are <=MaxBytes. Saturating their accounting
		// also prevents unrelated huge files from overflowing an eviction sum.
		item.size += min(info.Size(), 1<<40)
		if info.ModTime().After(item.modified) {
			item.modified = info.ModTime()
		}
		return nil
	})
	if errors.Is(err, ErrLimit) {
		return err
	}
	if err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	var total int64
	candidates := make([]*cacheEntry, 0, len(entries))
	pinnedPath := filepath.Join(directory, pinned)
	for _, entry := range entries {
		total += entry.size
		if entry.base != pinnedPath {
			candidates = append(candidates, entry)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.Before(candidates[j].modified) })
	for _, entry := range candidates {
		if total <= maximumBytes {
			break
		}
		removed := true
		for _, path := range entry.paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, err)
				removed = false
			}
		}
		if removed {
			total -= entry.size
		}
	}
	return errors.Join(cleanupErrors...)
}

func WriteAtomic(directory, name string, data []byte) error {
	clean := filepath.Clean(name)
	if filepath.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("tile cache path escapes cache directory")
	}
	target := filepath.Join(directory, clean)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".tile-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), target)
}
