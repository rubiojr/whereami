package admincache

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
)

const (
	sessionMemoCapacity = 1024
	sessionBatchSize    = 128
	sessionWriteTimeout = 5 * time.Second
)

// Session decorates one resolver lease with persistent cache lookups, a
// bounded duplicate-coordinate memo, and batched write-through updates. Close
// flushes cache writes but never closes the upstream resolver.
type Session struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	store     *Store
	upstream  admingeo.Resolver
	version   admingeo.DatasetVersion
	closed    bool
	closeErr  error
	writeErr  error
	memo      map[memoKey]admingeo.AdminPath
	memoOrder []memoKey
	memoNext  int
	pending   []Entry
}

var _ admingeo.Resolver = (*Session)(nil)

type memoKey struct {
	longitude uint64
	latitude  uint64
}

// NewSession creates a cache-backed view of upstream.
func NewSession(store *Store, upstream admingeo.Resolver) (*Session, error) {
	if store == nil {
		return nil, errors.New("admincache: session store is nil")
	}
	if upstream == nil {
		return nil, errors.New("admincache: session upstream resolver is nil")
	}
	return &Session{
		store:     store,
		upstream:  upstream,
		version:   upstream.Version(),
		memo:      make(map[memoKey]admingeo.AdminPath, sessionMemoCapacity),
		memoOrder: make([]memoKey, 0, sessionMemoCapacity),
		pending:   make([]Entry, 0, sessionBatchSize),
	}, nil
}

// Resolve checks the in-memory and persistent caches before consulting the
// upstream resolver. Cache failures fall through and do not fail resolution.
func (s *Session) Resolve(ctx context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	if ctx == nil {
		return admingeo.AdminPath{}, errors.New("admincache: nil resolution context")
	}
	if err := ctx.Err(); err != nil {
		return admingeo.AdminPath{}, err
	}

	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	if s.closed {
		return admingeo.AdminPath{}, errors.New("admincache: session is closed")
	}

	key := memoKey{longitude: math.Float64bits(coordinate.Longitude), latitude: math.Float64bits(coordinate.Latitude)}
	s.mu.Lock()
	path, found := s.memo[key]
	s.mu.Unlock()
	if found {
		return path, nil
	}

	path, found, cacheErr := s.store.Lookup(ctx, s.version, coordinate)
	if err := ctx.Err(); err != nil {
		return admingeo.AdminPath{}, err
	}
	if cacheErr == nil && found {
		s.mu.Lock()
		s.memoPut(key, path)
		s.mu.Unlock()
		return path, nil
	}

	path, err := s.upstream.Resolve(ctx, coordinate)
	if err != nil {
		return admingeo.AdminPath{}, err
	}
	if err := ctx.Err(); err != nil {
		return admingeo.AdminPath{}, err
	}

	s.mu.Lock()
	s.memoPut(key, path)
	s.pending = append(s.pending, Entry{Coordinate: coordinate, Path: path})
	batch := s.takeBatch()
	s.mu.Unlock()
	if len(batch) != 0 {
		s.write(ctx, batch)
	}
	return path, nil
}

// Version returns the immutable upstream dataset version captured at creation.
func (s *Session) Version() admingeo.DatasetVersion {
	return s.version
}

// Close flushes pending cache updates. It does not close the upstream resolver.
func (s *Session) Close() error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true

	s.mu.Lock()
	batch := append([]Entry(nil), s.pending...)
	s.pending = nil
	s.memo = nil
	s.memoOrder = nil
	s.mu.Unlock()
	if len(batch) != 0 {
		s.write(context.Background(), batch)
	}

	s.mu.Lock()
	s.closeErr = s.writeErr
	s.mu.Unlock()
	return s.closeErr
}

func (s *Session) memoPut(key memoKey, path admingeo.AdminPath) {
	if _, exists := s.memo[key]; exists {
		s.memo[key] = path
		return
	}
	if len(s.memoOrder) < sessionMemoCapacity {
		s.memoOrder = append(s.memoOrder, key)
	} else {
		delete(s.memo, s.memoOrder[s.memoNext])
		s.memoOrder[s.memoNext] = key
		s.memoNext = (s.memoNext + 1) % sessionMemoCapacity
	}
	s.memo[key] = path
}

func (s *Session) takeBatch() []Entry {
	if len(s.pending) < sessionBatchSize {
		return nil
	}
	batch := append([]Entry(nil), s.pending...)
	s.pending = s.pending[:0]
	return batch
}

func (s *Session) write(ctx context.Context, entries []Entry) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionWriteTimeout)
	defer cancel()
	if err := s.store.Put(writeCtx, s.version, entries); err != nil {
		s.mu.Lock()
		if s.writeErr == nil {
			s.writeErr = fmt.Errorf("admincache: flush session cache: %w", err)
		}
		s.mu.Unlock()
	}
}
