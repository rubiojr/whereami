package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rubiojr/whereami/internal/admincache"
	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/rubiojr/whereami/internal/timeline"
	"github.com/rubiojr/whereami/pkg/logger"
)

const (
	maxQueuedTimelines   = 8
	maxRetainedTimelines = 16
	maxTimelineDays      = 7320
	maxTimelineDuration  = 10 * time.Minute
	timelineRetention    = 15 * time.Minute
	timelinePruneEvery   = time.Minute
)

var (
	errInvalidTimelineDates = errors.New("invalid timeline dates")
	errTimelineQueueFull    = errors.New("timeline queue is full")
)

type timelineRunFunc func(context.Context, time.Time, time.Time, func(int64)) (timeline.Result, error)

type timelineJob struct {
	id        string
	state     string
	startDate string
	endDate   string
	start     time.Time
	end       time.Time
	processed int64
	result    *timeline.Result
	err       string
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool
	finished  time.Time
}

type timelineJobStatus struct {
	ID        string           `json:"id"`
	State     string           `json:"state"`
	StartDate string           `json:"start_date"`
	EndDate   string           `json:"end_date"`
	Processed int64            `json:"processed_observations"`
	Result    *timeline.Result `json:"result,omitempty"`
	Error     string           `json:"error,omitempty"`
}

type timelineService struct {
	mu      sync.Mutex
	ready   func() error
	run     timelineRunFunc
	jobs    map[string]*timelineJob
	order   []string
	pending []string
	wake    chan struct{}
	stop    chan struct{}
	wg      sync.WaitGroup
	closed  bool
}

func newTimelineService(repository *observations.Repository, manager *geodata.Manager, cache *admincache.Store, warmer *admincache.Warmer) *timelineService {
	service := newTimelineJobService(func() error {
		if repository == nil || manager == nil {
			return errors.New("timeline data is unavailable")
		}
		lease, err := manager.Acquire()
		if err != nil {
			return err
		}
		return lease.Close()
	}, func(ctx context.Context, start, end time.Time, progress func(int64)) (timeline.Result, error) {
		if warmer != nil {
			release := warmer.BeginForeground()
			defer release()
		}
		snapshot, err := repository.Snapshot()
		if err != nil {
			return timeline.Result{}, err
		}
		defer snapshot.Close()
		lease, err := manager.Acquire()
		if err != nil {
			return timeline.Result{}, err
		}
		defer lease.Close()
		var resolver admingeo.Resolver = lease
		var session *admincache.Session
		if cache != nil {
			session, err = admincache.NewSession(cache, lease)
			if err != nil {
				return timeline.Result{}, err
			}
			resolver = session
			defer func() {
				if err := session.Close(); err != nil {
					logger.Error("Administrative resolution cache flush failed: %v", err)
				}
			}()
		}
		generation := lease.Generation()
		return timeline.Generate(ctx, snapshot, resolver, timeline.DatasetMetadata{
			DatasetVersion: generation.DatasetVersion,
			SourceVersion:  generation.SourceVersion,
			Attribution:    generation.Attribution,
			License:        generation.License,
		}, start, end, progress)
	})
	return service
}

func newTimelineJobService(ready func() error, run timelineRunFunc) *timelineService {
	if run == nil {
		panic("timeline run function is required")
	}
	service := &timelineService{
		ready: ready,
		run:   run,
		jobs:  make(map[string]*timelineJob),
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
	}
	service.wg.Add(1)
	go service.worker()
	return service
}

func (s *timelineService) Submit(startDate, endDate string) (string, error) {
	start, end, err := parseTimelineDates(startDate, endDate)
	if err != nil {
		return "", err
	}
	if s.ready != nil {
		if err := s.ready(); err != nil {
			return "", err
		}
	}
	id, err := newTimelineID()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &timelineJob{
		id: id, state: "queued", startDate: startDate, endDate: endDate,
		start: start, end: end, ctx: ctx, cancel: cancel,
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return "", errors.New("timeline service is closed")
	}
	s.pruneLocked()
	if len(s.jobs) >= maxRetainedTimelines {
		s.mu.Unlock()
		cancel()
		return "", errTimelineQueueFull
	}
	if len(s.pending) >= maxQueuedTimelines {
		s.mu.Unlock()
		cancel()
		return "", errTimelineQueueFull
	}
	s.jobs[id] = job
	s.order = append(s.order, id)
	s.pending = append(s.pending, id)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.mu.Unlock()
	return id, nil
}

func (s *timelineService) Status(id string) (timelineJobStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return timelineJobStatus{}, false
	}
	return timelineJobStatus{
		ID: job.id, State: job.state, StartDate: job.startDate, EndDate: job.endDate,
		Processed: job.processed, Result: job.result, Error: job.err,
	}, true
}

func (s *timelineService) Cancel(id string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return false, false
	}
	switch job.state {
	case "queued":
		job.cancel()
		job.cancelled = true
		job.state = "cancelled"
		job.finished = time.Now()
		s.removePendingLocked(id)
		return true, true
	case "running":
		job.cancel()
		job.cancelled = true
		job.state = "cancelling"
		return true, true
	default:
		return false, true
	}
}

func (s *timelineService) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.stop)
	for _, job := range s.jobs {
		job.cancel()
		job.cancelled = true
		if job.state == "queued" {
			job.state = "cancelled"
			job.finished = time.Now()
		}
	}
	s.pending = nil
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *timelineService) worker() {
	defer s.wg.Done()
	ticker := time.NewTicker(timelinePruneEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			s.pruneExpiredLocked(now)
			s.mu.Unlock()
		case <-s.wake:
			for {
				id, ok := s.nextPending()
				if !ok {
					break
				}
				s.runJob(id)
				s.mu.Lock()
				s.pruneExpiredLocked(time.Now())
				s.mu.Unlock()
			}
		}
	}
}

func (s *timelineService) runJob(id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil || job.state != "queued" {
		s.mu.Unlock()
		return
	}
	job.state = "running"
	run := s.run
	queuedCtx, start, end := job.ctx, job.start, job.end
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(queuedCtx, maxTimelineDuration)
	result, err := run(ctx, start, end, func(processed int64) {
		s.mu.Lock()
		if current := s.jobs[id]; current != nil {
			current.processed = processed
		}
		s.mu.Unlock()
	})
	ctxErr := ctx.Err()
	cancel()
	job.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	job = s.jobs[id]
	if job == nil {
		return
	}
	job.err = ""
	job.finished = time.Now()
	switch {
	case job.cancelled || errors.Is(err, context.Canceled):
		job.state = "cancelled"
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctxErr, context.DeadlineExceeded):
		job.state = "failed"
		job.err = "timeline exceeded the ten-minute limit"
	case err != nil:
		job.state = "failed"
		job.err = "timeline generation failed"
		logger.Error("Timeline %s failed: %v", id, err)
	default:
		job.state = "completed"
		job.result = &result
	}
}

func (s *timelineService) nextPending() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return "", false
	}
	id := s.pending[0]
	s.pending = s.pending[1:]
	return id, true
}

func (s *timelineService) removePendingLocked(id string) {
	for index, pendingID := range s.pending {
		if pendingID == id {
			s.pending = append(s.pending[:index], s.pending[index+1:]...)
			return
		}
	}
}

func (s *timelineService) pruneLocked() {
	for len(s.jobs) >= maxRetainedTimelines {
		removed := false
		for index, id := range s.order {
			job := s.jobs[id]
			if job == nil || job.state == "completed" || job.state == "failed" || job.state == "cancelled" {
				delete(s.jobs, id)
				s.order = append(s.order[:index], s.order[index+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (s *timelineService) pruneExpiredLocked(now time.Time) {
	retained := s.order[:0]
	for _, id := range s.order {
		job := s.jobs[id]
		if job != nil && !job.finished.IsZero() && now.Sub(job.finished) >= timelineRetention {
			delete(s.jobs, id)
			continue
		}
		retained = append(retained, id)
	}
	s.order = retained
}

func parseTimelineDates(startDate, endDate string) (time.Time, time.Time, error) {
	parse := func(value string) (time.Time, error) {
		if len(value) != len(time.DateOnly) {
			return time.Time{}, fmt.Errorf("%w: dates must use YYYY-MM-DD", errInvalidTimelineDates)
		}
		parsed, err := time.ParseInLocation(time.DateOnly, value, time.UTC)
		if err != nil || parsed.Format(time.DateOnly) != value {
			return time.Time{}, fmt.Errorf("%w: dates must use YYYY-MM-DD", errInvalidTimelineDates)
		}
		return parsed, nil
	}
	start, err := parse(startDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	inclusiveEnd, err := parse(endDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if inclusiveEnd.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end date precedes start date", errInvalidTimelineDates)
	}
	end := inclusiveEnd.AddDate(0, 0, 1)
	if int(end.Sub(start)/(24*time.Hour)) > maxTimelineDays {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: date range exceeds %d days", errInvalidTimelineDates, maxTimelineDays)
	}
	return start, end, nil
}

func newTimelineID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate timeline ID: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func RegisterTimelineAPI(mux *http.ServeMux, service *timelineService) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	mux.HandleFunc("POST /api/timelines", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "timelines unavailable", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var request struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
			return
		}
		id, err := service.Submit(request.StartDate, request.EndDate)
		if err != nil {
			status := http.StatusServiceUnavailable
			message := "timelines unavailable"
			switch {
			case errors.Is(err, errInvalidTimelineDates):
				status = http.StatusBadRequest
				message = err.Error()
			case errors.Is(err, geodata.ErrNoActiveGeneration):
				status = http.StatusConflict
				message = "administrative geodata is not installed"
			case errors.Is(err, errTimelineQueueFull):
				status = http.StatusTooManyRequests
				w.Header().Set("Retry-After", "1")
			}
			http.Error(w, message, status)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"job_id": id})
	})
	mux.HandleFunc("GET /api/timelines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "timelines unavailable", http.StatusServiceUnavailable)
			return
		}
		status, ok := service.Status(r.PathValue("id"))
		if !ok {
			http.Error(w, "timeline not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("result") != "true" {
			status.Result = nil
		}
		writeAPIJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("DELETE /api/timelines/{id}", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "timelines unavailable", http.StatusServiceUnavailable)
			return
		}
		cancelled, found := service.Cancel(r.PathValue("id"))
		if !found {
			http.Error(w, "timeline not found", http.StatusNotFound)
			return
		}
		if !cancelled {
			http.Error(w, "timeline is already finished", http.StatusConflict)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
	})
}
