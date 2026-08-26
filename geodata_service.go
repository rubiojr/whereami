package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/admingeo/xiangshan"
	"github.com/rubiojr/whereami/internal/geodata"
)

const maxGeodataArtifactBytes int64 = 1 << 30
const maxGeodataManifestBytes int64 = 1 << 20

const (
	geodataIndexRole geodata.ArtifactRole = "index"
	geodataSlabRole  geodata.ArtifactRole = "polygons"
)

//go:embed geodata_manifest.json
var embeddedGeodataManifest []byte

type geodataAvailableGeneration struct {
	ID             string                  `json:"id"`
	DatasetVersion admingeo.DatasetVersion `json:"dataset_version"`
	SourceVersion  string                  `json:"source_version"`
	Attribution    string                  `json:"attribution"`
	License        string                  `json:"license"`
	Bytes          int64                   `json:"bytes"`
}

type geodataInstallProgress struct {
	Role          geodata.ArtifactRole `json:"role,omitempty"`
	Filename      string               `json:"filename,omitempty"`
	ArtifactIndex int                  `json:"artifact_index,omitempty"`
	ArtifactCount int                  `json:"artifact_count,omitempty"`
	Bytes         int64                `json:"bytes,omitempty"`
	TotalBytes    int64                `json:"total_bytes,omitempty"`
}

type geodataInstallStatus struct {
	State        string                 `json:"state"`
	GenerationID string                 `json:"generation_id,omitempty"`
	Progress     geodataInstallProgress `json:"progress"`
	Error        string                 `json:"error,omitempty"`
}

type geodataAPIStatus struct {
	Available []geodataAvailableGeneration `json:"available"`
	Current   geodata.GenerationStatus     `json:"current"`
	Previous  geodata.GenerationStatus     `json:"previous"`
	Install   geodataInstallStatus         `json:"install"`
	Error     string                       `json:"error,omitempty"`
}

type geodataService struct {
	mu          sync.Mutex
	manager     *geodata.Manager
	available   []geodataAvailableGeneration
	known       map[string]struct{}
	status      geodata.Status
	install     geodataInstallStatus
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	closed      bool
	onActivated func()
}

// SetActivationCallback registers work to run after a generation is installed
// and activated. The callback runs asynchronously from the requesting API call.
func (s *geodataService) SetActivationCallback(callback func()) {
	s.mu.Lock()
	s.onActivated = callback
	s.mu.Unlock()
}

func openGeodataService(root string) (*geodataService, error) {
	return openGeodataServiceManifest(root, embeddedGeodataManifest)
}

func openGeodataServiceFile(root, path string) (*geodataService, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open geodata manifest override: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGeodataManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read geodata manifest override: %w", err)
	}
	if len(data) > int(maxGeodataManifestBytes) {
		return nil, fmt.Errorf("geodata manifest override exceeds %d bytes", maxGeodataManifestBytes)
	}
	return openGeodataServiceManifest(root, data)
}

func openGeodataServiceManifest(root string, data []byte) (*geodataService, error) {
	manifest, err := geodata.ParseManifest(data, maxGeodataArtifactBytes)
	if err != nil {
		return nil, err
	}
	return newGeodataService(root, manifest, nil, openXiangshanResolver)
}

func newGeodataService(root string, manifest geodata.Manifest, client *http.Client, factory geodata.ResolverFactory) (*geodataService, error) {
	manager, err := geodata.Open(context.Background(), root, manifest, factory, geodata.Options{
		HTTPClient:       client,
		MaxArtifactBytes: maxGeodataArtifactBytes,
	})
	if err != nil {
		return nil, err
	}
	service := &geodataService{
		manager:   manager,
		available: make([]geodataAvailableGeneration, 0, len(manifest.Generations)),
		known:     make(map[string]struct{}, len(manifest.Generations)),
		status:    manager.Status(),
		install:   geodataInstallStatus{State: "idle"},
	}
	for _, generation := range manifest.Generations {
		var bytes int64
		for _, artifact := range generation.Artifacts {
			bytes += artifact.Bytes
		}
		service.known[generation.ID] = struct{}{}
		service.available = append(service.available, geodataAvailableGeneration{
			ID:             generation.ID,
			DatasetVersion: generation.DatasetVersion,
			SourceVersion:  generation.SourceVersion,
			Attribution:    generation.Attribution,
			License:        generation.License,
			Bytes:          bytes,
		})
	}
	return service, nil
}

func openXiangshanResolver(_ context.Context, generation geodata.Generation, paths map[geodata.ArtifactRole]string) (admingeo.Resolver, error) {
	indexPath, hasIndex := paths[geodataIndexRole]
	slabPath, hasSlab := paths[geodataSlabRole]
	if !hasIndex || !hasSlab || len(paths) != 2 {
		return nil, errors.New("xiangshan generation must contain exactly index and polygons artifacts")
	}
	return xiangshan.New(indexPath, slabPath, generation.DatasetVersion)
}

func (s *geodataService) Status() geodataAPIStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return geodataAPIStatus{
		Available: append([]geodataAvailableGeneration(nil), s.available...),
		Current:   s.status.Current,
		Previous:  s.status.Previous,
		Install:   s.install,
		Error:     s.status.Error,
	}
}

func (s *geodataService) StartInstall(generationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return geodata.ErrClosed
	}
	if _, ok := s.known[generationID]; !ok {
		return fmt.Errorf("unknown geodata generation %q", generationID)
	}
	if s.cancel != nil {
		return geodata.ErrInstallInProgress
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.install = geodataInstallStatus{State: "installing", GenerationID: generationID}
	s.wg.Add(1)
	go s.runInstall(ctx, generationID)
	return nil
}

func (s *geodataService) runInstall(ctx context.Context, generationID string) {
	defer s.wg.Done()
	err := s.manager.Install(ctx, generationID, func(progress geodata.Progress) {
		s.mu.Lock()
		s.install.Progress = geodataInstallProgress{
			Role:          progress.Role,
			Filename:      progress.Filename,
			ArtifactIndex: progress.ArtifactIndex,
			ArtifactCount: progress.ArtifactCount,
			Bytes:         progress.Bytes,
			TotalBytes:    progress.TotalBytes,
		}
		s.mu.Unlock()
	})
	status := s.manager.Status()

	s.mu.Lock()
	cancel := s.cancel
	s.status = status
	s.cancel = nil
	s.install.Error = ""
	activated := err == nil && !s.closed
	onActivated := s.onActivated
	switch {
	case err == nil:
		s.install.State = "installed"
	case errors.Is(err, context.Canceled):
		s.install.State = "cancelled"
	default:
		s.install.State = "failed"
		s.install.Error = err.Error()
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if activated && onActivated != nil {
		onActivated()
	}
}

func (s *geodataService) CancelInstall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}

func (s *geodataService) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.onActivated = nil
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return s.manager.Close()
}

func RegisterGeodataAPI(mux *http.ServeMux, service *geodataService) {
	if mux == nil {
		mux = http.DefaultServeMux
	}
	mux.HandleFunc("GET /api/geodata", func(w http.ResponseWriter, _ *http.Request) {
		if service == nil {
			http.Error(w, "administrative geodata unavailable", http.StatusServiceUnavailable)
			return
		}
		writeAPIJSON(w, http.StatusOK, service.Status())
	})
	mux.HandleFunc("POST /api/geodata/install", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			http.Error(w, "administrative geodata unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			GenerationID string `json:"generation_id"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
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
		if err := service.StartInstall(request.GenerationID); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, geodata.ErrInstallInProgress) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	})
	mux.HandleFunc("DELETE /api/geodata/install", func(w http.ResponseWriter, _ *http.Request) {
		if service == nil {
			http.Error(w, "administrative geodata unavailable", http.StatusServiceUnavailable)
			return
		}
		if !service.CancelInstall() {
			http.Error(w, "no geodata install in progress", http.StatusConflict)
			return
		}
		writeAPIJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
	})
}

func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
