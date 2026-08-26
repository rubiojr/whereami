package admincache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
)

const (
	warmerBatchSize  = 128
	warmerLeaseScans = 256
	warmerLeaseTime  = 5 * time.Second
	warmerYieldEvery = 32
	warmerYieldDelay = 2 * time.Millisecond
)

var errWarmGenerationChanged = errors.New("admincache: active generation changed during warming")

// AcquireFunc acquires a resolver lease. Its Close method must release the
// lease. A nil resolver and nil error means no generation is currently active.
type AcquireFunc func() (admingeo.Resolver, error)

// RetainedVersionsFunc returns dataset versions that remain eligible for use,
// including the active version and any rollback generation.
type RetainedVersionsFunc func() []admingeo.DatasetVersion

// Warmer fills a Store from distinct reportable observation coordinates. It
// owns one worker goroutine and coalesces concurrent notifications.
type Warmer struct {
	mu         sync.Mutex
	repository *observations.Repository
	store      *Store
	acquire    AcquireFunc
	retained   RetainedVersionsFunc
	onError    func(error)
	ctx        context.Context
	cancel     context.CancelFunc
	trigger    chan struct{}
	done       chan struct{}
	closed     bool
	activePass *warmRun
	restart    bool
	priority   foregroundPriority
}

type warmRun struct {
	cancel context.CancelFunc
}

// NewWarmer starts an idle warmer. Call Trigger after observation or dataset
// changes. onError may be nil.
func NewWarmer(repository *observations.Repository, store *Store, acquire AcquireFunc, retained RetainedVersionsFunc, onError func(error)) (*Warmer, error) {
	if repository == nil {
		return nil, errors.New("admincache: warmer repository is nil")
	}
	if store == nil {
		return nil, errors.New("admincache: warmer store is nil")
	}
	if acquire == nil {
		return nil, errors.New("admincache: warmer acquire function is nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Warmer{
		repository: repository,
		store:      store,
		acquire:    acquire,
		retained:   retained,
		onError:    onError,
		ctx:        ctx,
		cancel:     cancel,
		trigger:    make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Trigger requests a warm pass. Multiple requests pending at once coalesce.
// Calls after Close are safe no-ops.
func (w *Warmer) Trigger() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

// BeginForeground preempts an active warm pass so it releases its snapshot and
// resolver lease. Warming restarts after the foreground work finishes. The
// returned release function is idempotent and should be deferred by reports.
func (w *Warmer) BeginForeground() func() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return func() {}
	}
	releasePriority := w.priority.begin()
	if w.activePass != nil {
		w.activePass.cancel()
		w.restart = true
	}
	w.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			releasePriority()
			if !w.priority.idle() {
				return
			}
			w.mu.Lock()
			restart := w.restart
			w.restart = false
			w.mu.Unlock()
			if restart {
				w.Trigger()
			}
		})
	}
}

// Close cancels the active pass and joins the worker. It is idempotent.
func (w *Warmer) Close() error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		w.cancel()
	}
	done := w.done
	w.mu.Unlock()
	<-done
	return nil
}

func (w *Warmer) run() {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.trigger:
			passCtx, cancel := context.WithCancel(w.ctx)
			pass := &warmRun{cancel: cancel}
			w.mu.Lock()
			if w.closed {
				w.mu.Unlock()
				cancel()
				return
			}
			w.activePass = pass
			w.restart = false
			w.mu.Unlock()

			err := w.warm(passCtx)
			cancel()
			w.mu.Lock()
			if w.activePass == pass {
				w.activePass = nil
			}
			w.mu.Unlock()
			if err != nil && !errors.Is(err, context.Canceled) && w.ctx.Err() == nil && w.onError != nil {
				w.onError(err)
			}
		}
	}
}

func (w *Warmer) warm(ctx context.Context) (resultErr error) {
	if err := w.priority.wait(ctx); err != nil {
		return err
	}
	resolver, err := w.acquire()
	if err != nil {
		return fmt.Errorf("admincache: acquire warmer resolver: %w", err)
	}
	if resolver == nil {
		return nil
	}
	pass := warmPass{
		warmer:       w,
		ctx:          ctx,
		resolver:     resolver,
		version:      resolver.Version(),
		pending:      make([]Entry, 0, warmerBatchSize),
		leaseStarted: time.Now(),
	}
	defer func() {
		if err := pass.closeResolver(); err != nil {
			resultErr = joinWarmCloseError(resultErr, fmt.Errorf("admincache: release warmer resolver: %w", err))
		}
	}()

	snapshot, err := w.repository.Snapshot()
	if err != nil {
		return fmt.Errorf("admincache: open warmer observation snapshot: %w", err)
	}
	defer func() {
		if err := snapshot.Close(); err != nil {
			resultErr = joinWarmCloseError(resultErr, fmt.Errorf("admincache: close warmer observation snapshot: %w", err))
		}
	}()

	complete, err := w.store.WarmComplete(ctx, pass.version, snapshot.Revision())
	if err != nil {
		return err
	}
	if complete {
		return nil
	}

	err = snapshot.ScanResolvableCoordinates(ctx, pass.visit)
	if err != nil {
		if errors.Is(err, errWarmGenerationChanged) {
			return nil
		}
		return fmt.Errorf("admincache: warm observation coordinates: %w", err)
	}
	if err := pass.flush(); err != nil {
		return fmt.Errorf("admincache: flush warmer cache: %w", err)
	}
	if err := w.priority.wait(ctx); err != nil {
		return err
	}
	if err := w.store.MarkWarmComplete(ctx, pass.version, snapshot.Revision()); err != nil {
		return err
	}
	retained := []admingeo.DatasetVersion{pass.version}
	if w.retained != nil {
		retained = append(retained, w.retained()...)
	}
	if err := w.store.PruneVersions(ctx, retained...); err != nil {
		return fmt.Errorf("admincache: prune retired dataset versions: %w", err)
	}
	return nil
}

func joinWarmCloseError(resultErr, closeErr error) error {
	if errors.Is(resultErr, context.Canceled) {
		return closeErr
	}
	return errors.Join(resultErr, closeErr)
}

type warmPass struct {
	warmer             *Warmer
	ctx                context.Context
	resolver           admingeo.Resolver
	version            admingeo.DatasetVersion
	pending            []Entry
	resolvedSinceYield int
	scannedSinceLease  int
	leaseStarted       time.Time
}

func (p *warmPass) visit(longitude, latitude float64) error {
	if err := p.warmer.priority.wait(p.ctx); err != nil {
		return err
	}
	p.scannedSinceLease++
	if p.scannedSinceLease == warmerLeaseScans || time.Since(p.leaseStarted) >= warmerLeaseTime {
		if err := p.flush(); err != nil {
			return err
		}
		if err := p.refreshResolver(); err != nil {
			return err
		}
		p.scannedSinceLease = 0
	}

	coordinate := admingeo.Coordinate{Longitude: longitude, Latitude: latitude}
	_, found, err := p.warmer.store.Lookup(p.ctx, p.version, coordinate)
	if err != nil || found {
		return err
	}
	if err := p.warmer.priority.wait(p.ctx); err != nil {
		return err
	}
	path, err := p.resolver.Resolve(p.ctx, coordinate)
	if err != nil {
		return err
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.pending = append(p.pending, Entry{Coordinate: coordinate, Path: path})
	p.resolvedSinceYield++
	if p.resolvedSinceYield == warmerYieldEvery {
		p.resolvedSinceYield = 0
		if err := backgroundYield(p.ctx); err != nil {
			return err
		}
	}
	if len(p.pending) == warmerBatchSize {
		return p.flush()
	}
	return nil
}

func (p *warmPass) flush() error {
	if len(p.pending) == 0 {
		return nil
	}
	if err := p.warmer.priority.wait(p.ctx); err != nil {
		return err
	}
	if err := p.warmer.store.Put(p.ctx, p.version, p.pending); err != nil {
		return err
	}
	p.pending = p.pending[:0]
	return nil
}

func (p *warmPass) refreshResolver() error {
	next, err := p.warmer.acquire()
	if err != nil {
		return fmt.Errorf("admincache: refresh warmer resolver: %w", err)
	}
	if next == nil {
		return errWarmGenerationChanged
	}
	if next.Version() != p.version {
		if err := next.Close(); err != nil {
			return fmt.Errorf("admincache: release changed warmer resolver: %w", err)
		}
		return errWarmGenerationChanged
	}
	previous := p.resolver
	p.resolver = next
	p.leaseStarted = time.Now()
	if err := previous.Close(); err != nil {
		return fmt.Errorf("admincache: release warmer resolver batch: %w", err)
	}
	return nil
}

func (p *warmPass) closeResolver() error {
	if p.resolver == nil {
		return nil
	}
	resolver := p.resolver
	p.resolver = nil
	return resolver.Close()
}

func backgroundYield(ctx context.Context) error {
	timer := time.NewTimer(warmerYieldDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type foregroundPriority struct {
	mu      sync.Mutex
	active  int
	resumed chan struct{}
}

func (p *foregroundPriority) begin() func() {
	p.mu.Lock()
	if p.active == 0 {
		p.resumed = make(chan struct{})
	}
	p.active++
	p.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.active--
			if p.active == 0 {
				close(p.resumed)
				p.resumed = nil
			}
			p.mu.Unlock()
		})
	}
}

func (p *foregroundPriority) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		resumed := p.resumed
		p.mu.Unlock()
		if resumed == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resumed:
		}
	}
}

func (p *foregroundPriority) idle() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active == 0
}
