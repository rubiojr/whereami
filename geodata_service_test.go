package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/geodata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceTestResolver struct {
	version admingeo.DatasetVersion
}

func (r *serviceTestResolver) Resolve(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error) {
	return admingeo.AdminPath{}, nil
}

func (r *serviceTestResolver) Version() admingeo.DatasetVersion { return r.version }
func (r *serviceTestResolver) Close() error                     { return nil }

func TestEmbeddedGeodataManifestIsValid(t *testing.T) {
	manifest, err := geodata.ParseManifest(embeddedGeodataManifest, maxGeodataArtifactBytes)
	require.NoError(t, err)
	for _, generation := range manifest.Generations {
		roles := make(map[geodata.ArtifactRole]bool)
		for _, artifact := range generation.Artifacts {
			roles[artifact.Role] = true
		}
		assert.Equal(t, map[geodata.ArtifactRole]bool{geodataIndexRole: true, geodataSlabRole: true}, roles)
	}
}

func TestOpenGeodataServiceFileAcceptsLocalManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, embeddedGeodataManifest, 0o600))
	service, err := openGeodataServiceFile(filepath.Join(t.TempDir(), "geodata"), manifestPath)
	require.NoError(t, err)
	service.Close()
}

func TestOpenGeodataServiceFileRejectsOversizedManifest(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, bytes.Repeat([]byte{'x'}, int(maxGeodataManifestBytes)+1), 0o600))
	service, err := openGeodataServiceFile(filepath.Join(t.TempDir(), "geodata"), manifestPath)
	assert.Nil(t, service)
	assert.ErrorContains(t, err, "exceeds")
}

func TestOpenXiangshanResolverRequiresExactArtifactRoles(t *testing.T) {
	_, err := openXiangshanResolver(context.Background(), geodata.Generation{DatasetVersion: "test"}, map[geodata.ArtifactRole]string{
		geodataIndexRole: "index",
	})
	assert.ErrorContains(t, err, "exactly index and polygons")

	_, err = openXiangshanResolver(context.Background(), geodata.Generation{DatasetVersion: "test"}, map[geodata.ArtifactRole]string{
		geodataIndexRole: "index",
		geodataSlabRole:  "slab",
		"extra":          "extra",
	})
	assert.ErrorContains(t, err, "exactly index and polygons")
}

func TestGeodataAPIStatusAndValidation(t *testing.T) {
	root := t.TempDir()
	cleanupGeodataTestRoot(t, root)
	service, err := newGeodataService(root, geodata.Manifest{FormatVersion: geodata.ManifestFormatVersion}, nil, serviceTestFactory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	mux := http.NewServeMux()
	RegisterGeodataAPI(mux, service)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/geodata", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), "http")
	var status geodataAPIStatus
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Empty(t, status.Available)
	assert.Equal(t, "idle", status.Install.State)

	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/geodata/install", bytes.NewBufferString(`{"generation_id":"unknown"}`))
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/geodata/install", bytes.NewBufferString(`{"generation_id":"unknown"} {}`))
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestGeodataAPIInstallsInBackground(t *testing.T) {
	body := []byte("geodata")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	manifest := serviceTestManifest(server.URL+"/artifact", body)
	root := t.TempDir()
	cleanupGeodataTestRoot(t, root)
	service, err := newGeodataService(root, manifest, server.Client(), serviceTestFactory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	mux := http.NewServeMux()
	RegisterGeodataAPI(mux, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/geodata/install", bytes.NewBufferString(`{"generation_id":"test-v1"}`))
	mux.ServeHTTP(response, request)
	assert.Equal(t, http.StatusAccepted, response.Code)

	require.Eventually(t, func() bool {
		return service.Status().Install.State == "installed"
	}, time.Second, 10*time.Millisecond)
	status := service.Status()
	assert.Equal(t, "test-v1", status.Current.GenerationID)
	assert.True(t, status.Current.Valid)
}

func TestGeodataAPICancelsInstall(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	body := []byte("geodata")
	root := t.TempDir()
	cleanupGeodataTestRoot(t, root)
	service, err := newGeodataService(root, serviceTestManifest(server.URL+"/artifact", body), server.Client(), serviceTestFactory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	mux := http.NewServeMux()
	RegisterGeodataAPI(mux, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/geodata/install", bytes.NewBufferString(`{"generation_id":"test-v1"}`))
	mux.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code)
	<-started

	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/geodata/install", nil))
	assert.Equal(t, http.StatusAccepted, response.Code)
	require.Eventually(t, func() bool {
		return service.Status().Install.State == "cancelled"
	}, time.Second, 10*time.Millisecond)
}

func serviceTestFactory(_ context.Context, generation geodata.Generation, _ map[geodata.ArtifactRole]string) (admingeo.Resolver, error) {
	return &serviceTestResolver{version: generation.DatasetVersion}, nil
}

func cleanupGeodataTestRoot(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, _ error) error {
			if entry != nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
}

func serviceTestManifest(downloadURL string, body []byte) geodata.Manifest {
	hash := sha256.Sum256(body)
	return geodata.Manifest{
		FormatVersion: geodata.ManifestFormatVersion,
		Generations: []geodata.Generation{{
			ID:             "test-v1",
			DatasetVersion: "dataset-v1",
			SourceVersion:  "source-v1",
			Attribution:    "Test contributors",
			License:        "Test license",
			Artifacts: []geodata.Artifact{{
				Role:     geodataIndexRole,
				Filename: "test.index",
				URL:      downloadURL,
				Bytes:    int64(len(body)),
				SHA256:   hex.EncodeToString(hash[:]),
			}},
			Probes: []geodata.Probe{{Latitude: 0, Longitude: 0}},
		}},
	}
}
