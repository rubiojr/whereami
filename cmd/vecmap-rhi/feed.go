//go:build vecmap_rhi

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rubiojr/whereami/pkg/vecmap/retained"
	"github.com/rubiojr/whereami/pkg/vecmap/scene"
)

const maxDocumentBytes = 128 << 20

// sceneFeed owns one Store and one latest-document slot. File decoding, validation
// and resource identity remapping happen before QApplication or on its producer
// goroutine. A reload replaces a single retained fragment conservatively.
type sceneFeed struct {
	latest atomic.Pointer[scene.Document]
	errors chan error
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	store  *retained.Store
}

func newSceneFeed(path string, interval time.Duration) (*sceneFeed, error) {
	if interval < 0 {
		return nil, fmt.Errorf("reload interval must not be negative")
	}
	data, err := readDocumentBytes(path)
	if err != nil {
		return nil, err
	}
	store, err := retained.New(retained.Limits{})
	if err != nil {
		return nil, err
	}
	f := &sceneFeed{errors: make(chan error, 1), stop: make(chan struct{}), done: make(chan struct{}), store: store}
	if err := f.replace(data); err != nil {
		return nil, err
	}
	if interval == 0 {
		close(f.done)
	} else {
		go f.run(path, interval, sha256.Sum256(data))
	}
	return f, nil
}

func readDocumentBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDocumentBytes {
		return nil, fmt.Errorf("scene document exceeds %d bytes", maxDocumentBytes)
	}
	return data, nil
}

func (f *sceneFeed) replace(data []byte) error {
	var document scene.Document
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Transforms) > 65536 || len(document.TileSpaces) > 65536 || len(document.Scene.Draws) > 65536 {
		return fmt.Errorf("scene mapping exceeds 65536 slots/draws")
	}
	if err := document.Validate(); err != nil {
		return err
	}
	if previous := f.latest.Load(); previous != nil {
		if document.Width != previous.Width || document.Height != previous.Height || (len(document.TileSpaces) > 0) != (len(previous.TileSpaces) > 0) {
			return fmt.Errorf("reload must retain viewport size and geographic/affine coordinate mode")
		}
	}
	if err := f.store.Apply([]retained.Change{{Key: "document", Scene: &document.Scene}}); err != nil {
		return err
	}
	ranges := make([]retained.Range, len(document.Scene.Draws))
	for i, draw := range document.Scene.Draws {
		ranges[i] = retained.Range{Key: "document", First: i, Count: 1, Transform: draw.Transform}
	}
	snapshot, err := f.store.Snapshot(ranges)
	if err != nil {
		return err
	}
	document.Scene = *snapshot
	f.latest.Store(&document)
	return nil
}

func (f *sceneFeed) run(path string, interval time.Duration, digest [32]byte) {
	defer close(f.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
		}
		data, err := readDocumentBytes(path)
		if err == nil {
			next := sha256.Sum256(data)
			if next == digest {
				continue
			}
			digest = next // report an invalid file once, retry after content changes
			err = f.replace(data)
		}
		if err != nil {
			select {
			case f.errors <- err:
			default:
			}
		}
	}
}

func (f *sceneFeed) close() {
	f.once.Do(func() { close(f.stop) })
	<-f.done
}
