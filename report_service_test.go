package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/rubiojr/whereami/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaceReportAPIRequiresAuthAndRunsAsynchronously(t *testing.T) {
	type period struct{ start, end time.Time }
	periods := make(chan period, 1)
	service := newPlaceReportJobService(nil, func(_ context.Context, start, end time.Time, progress func(int64)) (reports.Report, error) {
		periods <- period{start: start, end: end}
		progress(3)
		return reports.Report{
			Period:  reports.Period{StartUTC: start.Format(time.RFC3339), EndUTC: end.Format(time.RFC3339), TimeZone: "UTC"},
			Summary: reports.Summary{RecordedObservations: 3},
			Places:  []reports.Place{{Country: "Spain", Locality: "Barcelona", RecordedObservations: 3}},
		}, nil
	})
	t.Cleanup(service.Close)
	mux := http.NewServeMux()
	RegisterPlaceReportAPI(mux, service)
	handler := secureAPI("secret", mux)
	body := []byte(`{"start_date":"2024-02-28","end_date":"2024-02-29"}`)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/place-reports", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(unauthorized, request)
	assert.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	response := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/place-reports", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	var submitted map[string]string
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &submitted))
	jobID := submitted["job_id"]
	require.NotEmpty(t, jobID)

	runPeriod := <-periods
	assert.Equal(t, "2024-02-28T00:00:00Z", runPeriod.start.Format(time.RFC3339))
	assert.Equal(t, "2024-03-01T00:00:00Z", runPeriod.end.Format(time.RFC3339))
	require.Eventually(t, func() bool {
		status, ok := service.Status(jobID)
		return ok && status.State == "completed"
	}, time.Second, 10*time.Millisecond)

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/place-reports/"+jobID, nil)
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	responseText := strings.ToLower(response.Body.String())
	assert.NotContains(t, responseText, "latitude")
	assert.NotContains(t, responseText, "longitude")
	assert.NotContains(t, responseText, `"places"`)
	assert.Contains(t, responseText, `"state":"completed"`)
	assert.Contains(t, responseText, `"processed_observations":3`)

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/place-reports/"+jobID+"?result=true", nil)
	request.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, strings.ToLower(response.Body.String()), `"locality":"barcelona"`)
}

func TestPlaceReportCancellation(t *testing.T) {
	started := make(chan struct{})
	service := newPlaceReportJobService(nil, func(ctx context.Context, _, _ time.Time, _ func(int64)) (reports.Report, error) {
		close(started)
		<-ctx.Done()
		return reports.Report{}, ctx.Err()
	})
	t.Cleanup(service.Close)
	id, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	<-started

	cancelled, found := service.Cancel(id)
	assert.True(t, found)
	assert.True(t, cancelled)
	require.Eventually(t, func() bool {
		status, ok := service.Status(id)
		return ok && status.State == "cancelled"
	}, time.Second, 10*time.Millisecond)
}

func TestPlaceReportDeadlineStartsWhenJobRuns(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondDeadline := make(chan bool, 1)
	runCount := 0
	service := newPlaceReportJobService(nil, func(ctx context.Context, _, _ time.Time, _ func(int64)) (reports.Report, error) {
		runCount++
		if runCount == 1 {
			close(firstStarted)
			<-releaseFirst
			return reports.Report{}, nil
		}
		_, hasDeadline := ctx.Deadline()
		secondDeadline <- hasDeadline
		return reports.Report{}, nil
	})
	t.Cleanup(service.Close)
	t.Cleanup(func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	})

	_, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	<-firstStarted
	secondID, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	service.mu.Lock()
	_, queuedHasDeadline := service.jobs[secondID].ctx.Deadline()
	service.mu.Unlock()
	assert.False(t, queuedHasDeadline)

	close(releaseFirst)
	select {
	case hasDeadline := <-secondDeadline:
		assert.True(t, hasDeadline)
	case <-time.After(time.Second):
		t.Fatal("queued report did not start")
	}
}

func TestPlaceReportPrunesExpiredResults(t *testing.T) {
	now := time.Now()
	service := &placeReportService{
		jobs: map[string]*placeReportJob{
			"expired": {id: "expired", state: "completed", result: &reports.Report{}, finished: now.Add(-placeReportRetention)},
			"recent":  {id: "recent", state: "completed", result: &reports.Report{}, finished: now},
			"running": {id: "running", state: "running"},
		},
		order: []string{"expired", "recent", "running"},
	}

	service.pruneExpiredLocked(now)
	assert.NotContains(t, service.jobs, "expired")
	assert.Contains(t, service.jobs, "recent")
	assert.Contains(t, service.jobs, "running")
	assert.Equal(t, []string{"recent", "running"}, service.order)
}

func TestPlaceReportQueueIsBounded(t *testing.T) {
	started := make(chan struct{})
	service := newPlaceReportJobService(nil, func(ctx context.Context, _, _ time.Time, _ func(int64)) (reports.Report, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return reports.Report{}, ctx.Err()
	})
	t.Cleanup(service.Close)
	_, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	<-started
	for range maxQueuedPlaceReports {
		_, err = service.Submit("2024-01-01", "2024-01-01")
		require.NoError(t, err)
	}
	_, err = service.Submit("2024-01-01", "2024-01-01")
	assert.ErrorIs(t, err, errPlaceReportQueueFull)
}

func TestPlaceReportCancellingQueuedJobReclaimsCapacity(t *testing.T) {
	started := make(chan struct{})
	service := newPlaceReportJobService(nil, func(ctx context.Context, _, _ time.Time, _ func(int64)) (reports.Report, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return reports.Report{}, ctx.Err()
	})
	t.Cleanup(service.Close)
	_, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	<-started
	queued := make([]string, 0, maxQueuedPlaceReports)
	for range maxQueuedPlaceReports {
		id, submitErr := service.Submit("2024-01-01", "2024-01-01")
		require.NoError(t, submitErr)
		queued = append(queued, id)
	}
	cancelled, found := service.Cancel(queued[0])
	require.True(t, found)
	require.True(t, cancelled)
	_, err = service.Submit("2024-01-01", "2024-01-01")
	assert.NoError(t, err)
}

func TestPlaceReportCancellationWinsOverSuccessfulRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	service := newPlaceReportJobService(nil, func(context.Context, time.Time, time.Time, func(int64)) (reports.Report, error) {
		close(started)
		<-release
		return reports.Report{}, nil
	})
	t.Cleanup(service.Close)
	id, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	<-started
	cancelled, found := service.Cancel(id)
	require.True(t, found)
	require.True(t, cancelled)
	close(release)
	require.Eventually(t, func() bool {
		status, ok := service.Status(id)
		return ok && status.State == "cancelled"
	}, time.Second, 10*time.Millisecond)
}

func TestPlaceReportFailureDoesNotExposeBackendDetails(t *testing.T) {
	service := newPlaceReportJobService(nil, func(context.Context, time.Time, time.Time, func(int64)) (reports.Report, error) {
		return reports.Report{}, errors.New(`open /home/alice/private/observations.sqlite: permission denied`)
	})
	t.Cleanup(service.Close)
	id, err := service.Submit("2024-01-01", "2024-01-01")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		status, ok := service.Status(id)
		return ok && status.State == "failed"
	}, time.Second, 10*time.Millisecond)
	status, _ := service.Status(id)
	assert.Equal(t, "place report generation failed", status.Error)
	assert.NotContains(t, status.Error, "/home/alice")
}

func TestPlaceReportAPIReportsMissingGeodata(t *testing.T) {
	service := newPlaceReportJobService(func() error { return geodata.ErrNoActiveGeneration }, func(context.Context, time.Time, time.Time, func(int64)) (reports.Report, error) {
		return reports.Report{}, nil
	})
	t.Cleanup(service.Close)
	mux := http.NewServeMux()
	RegisterPlaceReportAPI(mux, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/place-reports", bytes.NewBufferString(`{"start_date":"2024-01-01","end_date":"2024-01-01"}`))
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusConflict, response.Code)
}

func TestPlaceReportAPIDoesNotExposeReadinessDetails(t *testing.T) {
	service := newPlaceReportJobService(func() error {
		return errors.New(`open /home/alice/private/geodata/state.json: permission denied`)
	}, func(context.Context, time.Time, time.Time, func(int64)) (reports.Report, error) {
		return reports.Report{}, nil
	})
	t.Cleanup(service.Close)
	mux := http.NewServeMux()
	RegisterPlaceReportAPI(mux, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/place-reports", bytes.NewBufferString(`{"start_date":"2024-01-01","end_date":"2024-01-01"}`))
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Equal(t, "place reports unavailable\n", response.Body.String())
	assert.NotContains(t, response.Body.String(), "/home/alice")
}

func TestParsePlaceReportDates(t *testing.T) {
	for _, test := range []struct {
		name      string
		startDate string
		endDate   string
	}{
		{name: "invalid format", startDate: "2024-1-01", endDate: "2024-01-01"},
		{name: "invalid day", startDate: "2024-02-30", endDate: "2024-03-01"},
		{name: "reverse", startDate: "2024-03-01", endDate: "2024-02-29"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parsePlaceReportDates(test.startDate, test.endDate)
			assert.Error(t, err)
		})
	}
}
