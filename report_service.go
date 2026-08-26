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

	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/rubiojr/whereami/internal/observations"
	"github.com/rubiojr/whereami/internal/reports"
	"github.com/rubiojr/whereami/pkg/logger"
)

const (
	maxQueuedPlaceReports   = 8
	maxRetainedPlaceReports = 16
	maxPlaceReportDays      = 7320
	maxPlaceReportDuration  = 2 * time.Minute
)

var (
	errInvalidPlaceReportDates = errors.New("invalid place report dates")
	errPlaceReportQueueFull    = errors.New("place report queue is full")
)

type placeReportRunFunc func(context.Context, time.Time, time.Time, func(int64)) (reports.Report, error)

type placeReportJob struct {
	id        string
	state     string
	startDate string
	endDate   string
	start     time.Time
	end       time.Time
	processed int64
	result    *reports.Report
	err       string
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool
}

type placeReportJobStatus struct {
	ID        string          `json:"id"`
	State     string          `json:"state"`
	StartDate string          `json:"start_date"`
	EndDate   string          `json:"end_date"`
	Processed int64           `json:"processed_observations"`
	Result    *reports.Report `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type placeReportService struct {
	mu      sync.Mutex
	ready   func() error
	run     placeReportRunFunc
	jobs    map[string]*placeReportJob
	order   []string
	pending []string
	wake    chan struct{}
	stop    chan struct{}
	wg      sync.WaitGroup
	closed  bool
}

func newPlaceReportService(repository *observations.Repository, manager *geodata.Manager) *placeReportService {
	service := newPlaceReportJobService(func() error {
		if repository == nil || manager == nil {
			return errors.New("place report data is unavailable")
		}
		lease, err := manager.Acquire()
		if err != nil {
			return err
		}
		return lease.Close()
	}, func(ctx context.Context, start, end time.Time, progress func(int64)) (reports.Report, error) {
		snapshot, err := repository.Snapshot()
		if err != nil {
			return reports.Report{}, err
		}
		defer snapshot.Close()
		lease, err := manager.Acquire()
		if err != nil {
			return reports.Report{}, err
		}
		defer lease.Close()
		generation := lease.Generation()
		return reports.Generate(ctx, snapshot, lease, reports.DatasetMetadata{
			DatasetVersion: generation.DatasetVersion,
			SourceVersion:  generation.SourceVersion,
			Attribution:    generation.Attribution,
			License:        generation.License,
		}, start, end, progress)
	})
	return service
}

func newPlaceReportJobService(ready func() error, run placeReportRunFunc) *placeReportService {
	if run == nil {
		panic("place report run function is required")
	}
	service := &placeReportService{
		ready: ready,
		run:   run,
		jobs:  make(map[string]*placeReportJob),
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
	}
	service.wg.Add(1)
	go service.worker()
	return service
}

func (s *placeReportService) Submit(startDate, endDate string) (string, error) {
	start, end, err := parsePlaceReportDates(startDate, endDate)
	if err != nil {
		return "", err
	}
	if s.ready != nil {
		if err := s.ready(); err != nil {
			return "", err
		}
	}
	id, err := newPlaceReportID()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxPlaceReportDuration)
	job := &placeReportJob{
		id: id, state: "queued", startDate: startDate, endDate: endDate,
		start: start, end: end, ctx: ctx, cancel: cancel,
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return "", errors.New("place report service is closed")
	}
	s.pruneLocked()
	if len(s.jobs) >= maxRetainedPlaceReports {
		s.mu.Unlock()
		cancel()
		return "", errPlaceReportQueueFull
	}
	if len(s.pending) >= maxQueuedPlaceReports {
		s.mu.Unlock()
		cancel()
		return "", errPlaceReportQueueFull
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

func (s *placeReportService) Status(id string) (placeReportJobStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return placeReportJobStatus{}, false
	}
	return placeReportJobStatus{
		ID: job.id, State: job.state, StartDate: job.startDate, EndDate: job.endDate,
		Processed: job.processed, Result: job.result, Error: job.err,
	}, true
}

func (s *placeReportService) Cancel(id string) (bool, bool) {
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

func (s *placeReportService) Close() {
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
		}
	}
	s.pending = nil
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *placeReportService) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
			for {
				id, ok := s.nextPending()
				if !ok {
					break
				}
				s.runJob(id)
			}
		}
	}
}

func (s *placeReportService) runJob(id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil || job.state != "queued" {
		s.mu.Unlock()
		return
	}
	job.state = "running"
	run := s.run
	ctx, start, end := job.ctx, job.start, job.end
	s.mu.Unlock()

	result, err := run(ctx, start, end, func(processed int64) {
		s.mu.Lock()
		if current := s.jobs[id]; current != nil {
			current.processed = processed
		}
		s.mu.Unlock()
	})
	ctxErr := ctx.Err()
	job.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	job = s.jobs[id]
	if job == nil {
		return
	}
	job.err = ""
	switch {
	case job.cancelled || errors.Is(err, context.Canceled):
		job.state = "cancelled"
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctxErr, context.DeadlineExceeded):
		job.state = "failed"
		job.err = "place report exceeded the two-minute limit"
	case err != nil:
		job.state = "failed"
		job.err = "place report generation failed"
		logger.Error("Place report %s failed: %v", id, err)
	default:
		job.state = "completed"
		job.result = &result
	}
}

func (s *placeReportService) nextPending() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return "", false
	}
	id := s.pending[0]
	s.pending = s.pending[1:]
	return id, true
}

func (s *placeReportService) removePendingLocked(id string) {
	for index, pendingID := range s.pending {
		if pendingID == id {
			s.pending = append(s.pending[:index], s.pending[index+1:]...)
			return
		}
	}
}

func (s *placeReportService) pruneLocked() {
	for len(s.jobs) >= maxRetainedPlaceReports {
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

func parsePlaceReportDates(startDate, endDate string) (time.Time, time.Time, error) {
	parse := func(value string) (time.Time, error) {
		if len(value) != len(time.DateOnly) {
			return time.Time{}, fmt.Errorf("%w: dates must use YYYY-MM-DD", errInvalidPlaceReportDates)
		}
		parsed, err := time.ParseInLocation(time.DateOnly, value, time.UTC)
		if err != nil || parsed.Format(time.DateOnly) != value {
			return time.Time{}, fmt.Errorf("%w: dates must use YYYY-MM-DD", errInvalidPlaceReportDates)
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
		return time.Time{}, time.Time{}, fmt.Errorf("%w: end date precedes start date", errInvalidPlaceReportDates)
	}
	end := inclusiveEnd.AddDate(0, 0, 1)
	if int(end.Sub(start)/(24*time.Hour)) > maxPlaceReportDays {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: date range exceeds %d days", errInvalidPlaceReportDates, maxPlaceReportDays)
	}
	return start, end, nil
}

func newPlaceReportID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate place report ID: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func RegisterPlaceReportAPI(mux *http.ServeMux, service *placeReportService) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	mux.HandleFunc("POST /api/place-reports", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "place reports unavailable", http.StatusServiceUnavailable)
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
			message := "place reports unavailable"
			switch {
			case errors.Is(err, errInvalidPlaceReportDates):
				status = http.StatusBadRequest
				message = err.Error()
			case errors.Is(err, geodata.ErrNoActiveGeneration):
				status = http.StatusConflict
				message = "administrative geodata is not installed"
			case errors.Is(err, errPlaceReportQueueFull):
				status = http.StatusTooManyRequests
				w.Header().Set("Retry-After", "1")
			}
			http.Error(w, message, status)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"job_id": id})
	})
	mux.HandleFunc("GET /api/place-reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "place reports unavailable", http.StatusServiceUnavailable)
			return
		}
		status, ok := service.Status(r.PathValue("id"))
		if !ok {
			http.Error(w, "place report not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("result") != "true" {
			status.Result = nil
		}
		writeAPIJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("DELETE /api/place-reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "place reports unavailable", http.StatusServiceUnavailable)
			return
		}
		cancelled, found := service.Cancel(r.PathValue("id"))
		if !found {
			http.Error(w, "place report not found", http.StatusNotFound)
			return
		}
		if !cancelled {
			http.Error(w, "place report is already finished", http.StatusConflict)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
	})
}
