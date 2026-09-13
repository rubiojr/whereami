package vecmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
)

const (
	openFreeMapGlyphURL      = "https://tiles.openfreemap.org/fonts/{fontstack}/{range}.pbf"
	glyphRangeSize           = glyph.RangeSize
	maximumGlyphCodePoint    = glyph.MaxCodePoint
	glyphRangeRetryInterval  = 30 * time.Second
	maximumConcurrentGlyphs  = 4
	glyphPBFBorder           = glyph.PBFBorder
	glyphAtlasPadding        = glyph.AtlasPadding
	openFreeMapGlyphCacheDir = "openfreemap-fonts"
)

type sdfGlyph = glyph.Glyph
type sdfGlyphRange = glyph.Range

type glyphRangeKey struct {
	fontStack string
	start     uint32
}

type glyphRangeEntry struct {
	rangeData *sdfGlyphRange
	loading   bool
	failedAt  time.Time
}

type glyphRangeLoader func(context.Context, glyphRangeKey) (*sdfGlyphRange, error)

type glyphManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	load      glyphRangeLoader
	update    func()
	semaphore chan struct{}

	mu       sync.Mutex
	entries  map[glyphRangeKey]*glyphRangeEntry
	glyphs   map[string]map[uint32]sdfGlyph
	revision uint64
	closed   bool
	wg       sync.WaitGroup
}

func newGlyphManager(load glyphRangeLoader, update func()) *glyphManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &glyphManager{
		ctx:       ctx,
		cancel:    cancel,
		load:      load,
		update:    update,
		semaphore: make(chan struct{}, maximumConcurrentGlyphs),
		entries:   make(map[glyphRangeKey]*glyphRangeEntry),
		glyphs:    make(map[string]map[uint32]sdfGlyph),
	}
}

func (m *glyphManager) close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *glyphManager) currentRevision() uint64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.revision
}

func (m *glyphManager) resolve(fontStack, text string) (map[uint32]sdfGlyph, bool) {
	if m == nil || m.load == nil || fontStack == "" || text == "" || !utf8.ValidString(text) {
		return nil, false
	}
	keys := glyphRangeKeys(fontStack, text)
	if len(keys) == 0 {
		return nil, false
	}

	now := time.Now()
	launch := make([]glyphRangeKey, 0, len(keys))
	var resolved map[uint32]sdfGlyph
	ready := true
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, false
	}
	for _, key := range keys {
		entry := m.entries[key]
		if entry == nil {
			entry = &glyphRangeEntry{}
			m.entries[key] = entry
		}
		if entry.rangeData != nil {
			continue
		}
		ready = false
		if !entry.loading && (entry.failedAt.IsZero() || now.Sub(entry.failedAt) >= glyphRangeRetryInterval) {
			entry.loading = true
			m.wg.Add(1)
			launch = append(launch, key)
		}
	}
	if ready {
		resolved = m.glyphs[fontStack]
	}
	m.mu.Unlock()
	for _, key := range launch {
		go m.loadRange(key)
	}
	if !ready {
		return nil, false
	}
	for _, codePoint := range text {
		if codePoint == '\n' || codePoint == '\r' {
			continue
		}
		if _, exists := resolved[uint32(codePoint)]; !exists {
			return nil, false
		}
	}
	return resolved, true
}

func (m *glyphManager) loadRange(key glyphRangeKey) {
	defer m.wg.Done()
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-m.ctx.Done():
		m.finishRange(key, nil, m.ctx.Err())
		return
	}
	rangeData, err := m.load(m.ctx, key)
	m.finishRange(key, rangeData, err)
}

func (m *glyphManager) finishRange(key glyphRangeKey, rangeData *sdfGlyphRange, err error) {
	if err == nil && rangeData == nil {
		err = errors.New("glyph loader returned no range data")
	}
	m.mu.Lock()
	entry := m.entries[key]
	if entry == nil {
		entry = &glyphRangeEntry{}
		m.entries[key] = entry
	}
	entry.loading = false
	if err == nil && rangeData != nil {
		entry.rangeData = rangeData
		entry.failedAt = time.Time{}
		fontGlyphs := make(map[uint32]sdfGlyph, len(m.glyphs[key.fontStack])+len(rangeData.Glyphs))
		for id, glyph := range m.glyphs[key.fontStack] {
			fontGlyphs[id] = glyph
		}
		for id, glyph := range rangeData.Glyphs {
			fontGlyphs[id] = glyph
		}
		m.glyphs[key.fontStack] = fontGlyphs
		m.revision++
	} else if !errors.Is(err, context.Canceled) {
		entry.failedAt = time.Now()
	}
	closed := m.closed
	update := m.update
	m.mu.Unlock()
	if err != nil && !errors.Is(err, context.Canceled) {
		reportVectorError(
			"vecmap glyph range %s for font %q failed: %v",
			glyphRangeName(key.start),
			key.fontStack,
			err,
		)
	}
	if !closed && err == nil && rangeData != nil && update != nil {
		update()
	}
}

func glyphRangeKeys(fontStack, text string) []glyphRangeKey {
	seen := make(map[uint32]struct{})
	keys := make([]glyphRangeKey, 0, 2)
	for _, codePoint := range text {
		if codePoint == '\n' || codePoint == '\r' {
			continue
		}
		if codePoint < 0 || uint32(codePoint) > maximumGlyphCodePoint {
			return nil
		}
		start := uint32(codePoint) / glyphRangeSize * glyphRangeSize
		if _, exists := seen[start]; exists {
			continue
		}
		seen[start] = struct{}{}
		keys = append(keys, glyphRangeKey{fontStack: fontStack, start: start})
	}
	return keys
}

func openFreeMapGlyphRangeLoader(cacheDir string) glyphRangeLoader {
	return func(ctx context.Context, key glyphRangeKey) (*sdfGlyphRange, error) {
		return loadOpenFreeMapGlyphRange(ctx, tileHTTPClient, cacheDir, key)
	}
}

func loadOpenFreeMapGlyphRange(
	ctx context.Context,
	client *http.Client,
	cacheDir string,
	key glyphRangeKey,
) (*sdfGlyphRange, error) {
	rangeName := glyphRangeName(key.start)
	stackHash := sha256.Sum256([]byte(key.fontStack))
	cacheName := filepath.Join(
		openFreeMapGlyphCacheDir,
		hex.EncodeToString(stackHash[:]),
		rangeName+".pbf",
	)
	cachePath := filepath.Join(cacheDir, cacheName)
	if cacheDir != "" {
		data, expected, err := readCachedTile(cachePath, "")
		if err == nil && verifyTileChecksum(data, expected) == nil {
			decoded, decodeErr := decodeSDFGlyphRange(data, key)
			if decodeErr == nil {
				return decoded, nil
			}
		}
	}

	glyphURL := strings.ReplaceAll(openFreeMapGlyphURL, "{fontstack}", url.PathEscape(key.fontStack))
	glyphURL = strings.ReplaceAll(glyphURL, "{range}", rangeName)
	data, err := fetchBounded(ctx, client, glyphURL, "application/x-protobuf")
	if err != nil {
		return nil, fmt.Errorf("load glyph range %s: %w", rangeName, err)
	}
	decoded, err := decodeSDFGlyphRange(data, key)
	if err != nil {
		return nil, err
	}
	if cacheDir != "" {
		_ = writeCachedTilePair(cacheDir, cacheName, data, tileChecksum(data))
	}
	return decoded, nil
}

func glyphRangeName(start uint32) string {
	return glyph.RangeName(start)
}

func decodeSDFGlyphRange(data []byte, key glyphRangeKey) (*sdfGlyphRange, error) {
	return glyph.DecodeRange(data, key.fontStack, key.start)
}
