package vecmap

import (
	"context"
	"sync"
)

type libertySceneRequest struct {
	tileRevision uint64
	styleZoom    float64
	tiles        []loadedRoadTile
}

type libertySceneSnapshot struct {
	tileRevision uint64
	styleZoom    float64
	tiles        []loadedRoadTile
}

type libertySceneCompiler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	publish  func(*libertySceneSnapshot)
	requests chan struct{}

	mu      sync.Mutex
	latest  libertySceneRequest
	pending bool
	wg      sync.WaitGroup
}

func newLibertySceneCompiler(publish func(*libertySceneSnapshot)) *libertySceneCompiler {
	ctx, cancel := context.WithCancel(context.Background())
	compiler := &libertySceneCompiler{
		ctx:      ctx,
		cancel:   cancel,
		publish:  publish,
		requests: make(chan struct{}, 1),
	}
	compiler.wg.Add(1)
	go compiler.run()
	return compiler
}

func (c *libertySceneCompiler) request(tileRevision uint64, styleZoom float64, tiles []loadedRoadTile) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.latest = libertySceneRequest{
		tileRevision: tileRevision,
		styleZoom:    styleZoom,
		tiles:        append([]loadedRoadTile(nil), tiles...),
	}
	c.pending = true
	c.mu.Unlock()
	select {
	case c.requests <- struct{}{}:
	default:
	}
}

func (c *libertySceneCompiler) run() {
	defer c.wg.Done()
	for {
		select {
		case <-c.requests:
			request, exists := c.takeRequest()
			if !exists {
				continue
			}
			snapshot := &libertySceneSnapshot{
				tileRevision: request.tileRevision,
				styleZoom:    request.styleZoom,
				tiles:        compileLibertySceneTiles(request.tiles, request.styleZoom),
			}
			if c.isLatest(request) && c.publish != nil {
				c.publish(snapshot)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *libertySceneCompiler) takeRequest() (libertySceneRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pending {
		return libertySceneRequest{}, false
	}
	request := c.latest
	c.pending = false
	return request, true
}

func (c *libertySceneCompiler) isLatest(request libertySceneRequest) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.pending && request.tileRevision == c.latest.tileRevision && request.styleZoom == c.latest.styleZoom
}

func (c *libertySceneCompiler) stop() {
	if c == nil {
		return
	}
	c.cancel()
	c.wg.Wait()
}

func compileLibertySceneTiles(tiles []loadedRoadTile, zoom float64) []loadedRoadTile {
	styled := make([]loadedRoadTile, 0, len(tiles))
	for _, tile := range tiles {
		if tile.roads == nil {
			continue
		}
		compiled := *tile.roads
		if err := compileLibertyTile(&compiled, zoom); err != nil {
			reportVectorError(
				"vecmap style compile failed for tile z=%d x=%d y=%d at zoom %.2f: %v",
				tile.id.Z,
				tile.id.X,
				tile.id.Y,
				zoom,
				err,
			)
			compiled.liberty = tile.roads.liberty
			compiled.symbols = tile.roads.symbols
		}
		styled = append(styled, loadedRoadTile{id: tile.id, roads: &compiled})
	}
	return styled
}
