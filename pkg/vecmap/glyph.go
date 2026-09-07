package vecmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	openFreeMapGlyphURL      = "https://tiles.openfreemap.org/fonts/{fontstack}/{range}.pbf"
	glyphRangeSize           = uint32(256)
	maximumGlyphCodePoint    = uint32(0xffff)
	maximumGlyphRangeStacks  = 8
	maximumGlyphsPerRange    = 256
	maximumGlyphDimension    = 255
	maximumGlyphMetric       = 127
	minimumGlyphMetric       = -128
	glyphRangeRetryInterval  = 30 * time.Second
	maximumConcurrentGlyphs  = 4
	glyphPBFBorder           = 3
	glyphAtlasGuard          = 1
	glyphAtlasPadding        = glyphPBFBorder + glyphAtlasGuard
	openFreeMapGlyphCacheDir = "openfreemap-fonts"
)

type sdfGlyph struct {
	id      uint32
	bitmap  []byte
	width   uint32
	height  uint32
	left    int32
	top     int32
	advance uint32
}

type sdfGlyphRange struct {
	fontStack string
	rangeName string
	glyphs    map[uint32]sdfGlyph
}

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
		fontGlyphs := make(map[uint32]sdfGlyph, len(m.glyphs[key.fontStack])+len(rangeData.glyphs))
		for id, glyph := range m.glyphs[key.fontStack] {
			fontGlyphs[id] = glyph
		}
		for id, glyph := range rangeData.glyphs {
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
	return fmt.Sprintf("%d-%d", start, start+glyphRangeSize-1)
}

func decodeSDFGlyphRange(data []byte, key glyphRangeKey) (*sdfGlyphRange, error) {
	reader := protobufReader{data: data}
	var matched *sdfGlyphRange
	stackCount := 0
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return nil, fmt.Errorf("decode glyph range: %w", err)
		}
		if field != 1 {
			if err := reader.skip(wire); err != nil {
				return nil, fmt.Errorf("decode glyph range: %w", err)
			}
			continue
		}
		payload, err := reader.bytes(wire)
		if err != nil {
			return nil, fmt.Errorf("decode glyph range stack: %w", err)
		}
		stackCount++
		if stackCount > maximumGlyphRangeStacks {
			return nil, errors.New("glyph range exceeds font stack limit")
		}
		stack, err := decodeSDFGlyphStack(payload, key.start)
		if err != nil {
			return nil, err
		}
		if stack.rangeName == glyphRangeName(key.start) {
			if matched != nil {
				return nil, errors.New("glyph range contains duplicate matching stacks")
			}
			matched = stack
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("glyph range %s for %q is missing", glyphRangeName(key.start), key.fontStack)
	}
	matched.fontStack = key.fontStack
	return matched, nil
}

func decodeSDFGlyphStack(data []byte, rangeStart uint32) (*sdfGlyphRange, error) {
	reader := protobufReader{data: data}
	stack := &sdfGlyphRange{glyphs: make(map[uint32]sdfGlyph)}
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return nil, fmt.Errorf("decode glyph stack: %w", err)
		}
		switch field {
		case 1:
			value, err := reader.bytes(wire)
			if err != nil {
				return nil, fmt.Errorf("decode glyph stack name: %w", err)
			}
			stack.fontStack = string(value)
		case 2:
			value, err := reader.bytes(wire)
			if err != nil {
				return nil, fmt.Errorf("decode glyph stack range: %w", err)
			}
			stack.rangeName = string(value)
		case 3:
			if len(stack.glyphs) >= maximumGlyphsPerRange {
				return nil, errors.New("glyph range exceeds glyph count limit")
			}
			payload, err := reader.bytes(wire)
			if err != nil {
				return nil, fmt.Errorf("decode glyph: %w", err)
			}
			glyph, err := decodeSDFGlyph(payload, rangeStart)
			if err != nil {
				return nil, err
			}
			if _, exists := stack.glyphs[glyph.id]; exists {
				return nil, fmt.Errorf("glyph range contains duplicate glyph %d", glyph.id)
			}
			stack.glyphs[glyph.id] = glyph
		default:
			if err := reader.skip(wire); err != nil {
				return nil, fmt.Errorf("decode glyph stack: %w", err)
			}
		}
	}
	if stack.fontStack == "" || stack.rangeName == "" {
		return nil, errors.New("glyph stack is missing name or range")
	}
	return stack, nil
}

func decodeSDFGlyph(data []byte, rangeStart uint32) (sdfGlyph, error) {
	reader := protobufReader{data: data}
	glyph := sdfGlyph{}
	var required uint8
	for reader.more() {
		field, wire, err := reader.field()
		if err != nil {
			return sdfGlyph{}, fmt.Errorf("decode glyph: %w", err)
		}
		switch field {
		case 1:
			value, err := reader.varint(wire)
			if err != nil || value > math.MaxUint32 {
				return sdfGlyph{}, errors.New("glyph id is invalid")
			}
			glyph.id = uint32(value)
			required |= 1 << 0
		case 2:
			value, err := reader.bytes(wire)
			if err != nil {
				return sdfGlyph{}, fmt.Errorf("decode glyph bitmap: %w", err)
			}
			glyph.bitmap = append([]byte(nil), value...)
		case 3, 4, 7:
			value, err := reader.varint(wire)
			if err != nil || value > math.MaxUint32 {
				return sdfGlyph{}, errors.New("glyph unsigned metric is invalid")
			}
			switch field {
			case 3:
				glyph.width = uint32(value)
				required |= 1 << 1
			case 4:
				glyph.height = uint32(value)
				required |= 1 << 2
			case 7:
				glyph.advance = uint32(value)
				required |= 1 << 5
			}
		case 5, 6:
			value, err := reader.varint(wire)
			if err != nil || value > math.MaxUint32 {
				return sdfGlyph{}, errors.New("glyph signed metric is invalid")
			}
			metric := int32(decodeZigZag(uint32(value)))
			if field == 5 {
				glyph.left = metric
				required |= 1 << 3
			} else {
				glyph.top = metric
				required |= 1 << 4
			}
		default:
			if err := reader.skip(wire); err != nil {
				return sdfGlyph{}, fmt.Errorf("decode glyph: %w", err)
			}
		}
	}
	if required != 0b11_1111 {
		return sdfGlyph{}, errors.New("glyph is missing required metrics")
	}
	if glyph.id < rangeStart || glyph.id >= rangeStart+glyphRangeSize {
		return sdfGlyph{}, fmt.Errorf("glyph %d is outside requested range", glyph.id)
	}
	if glyph.width > maximumGlyphDimension || glyph.height > maximumGlyphDimension || glyph.advance > maximumGlyphDimension {
		return sdfGlyph{}, errors.New("glyph dimensions exceed metric limit")
	}
	if glyph.left < minimumGlyphMetric || glyph.left > maximumGlyphMetric ||
		glyph.top < minimumGlyphMetric || glyph.top > maximumGlyphMetric {
		return sdfGlyph{}, errors.New("glyph bearing exceeds metric limit")
	}
	expectedBitmapBytes := 0
	if glyph.width > 0 && glyph.height > 0 {
		expectedBitmapBytes = int(glyph.width+2*glyphPBFBorder) * int(glyph.height+2*glyphPBFBorder)
	}
	if len(glyph.bitmap) != expectedBitmapBytes {
		return sdfGlyph{}, fmt.Errorf("glyph %d bitmap has %d bytes, want %d", glyph.id, len(glyph.bitmap), expectedBitmapBytes)
	}
	return glyph, nil
}
