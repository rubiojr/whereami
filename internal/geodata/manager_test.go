package geodata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedArea = admingeo.AdminPath{
	CountryID: "ES",
	Country:   "Spain",
	Region:    "Catalonia",
	County:    "Barcelona",
	Locality:  "Barcelona",
}

type fakeResolver struct {
	result  admingeo.AdminPath
	version admingeo.DatasetVersion
	closed  atomic.Int32
}

func (r *fakeResolver) Resolve(context.Context, admingeo.Coordinate) (admingeo.AdminPath, error) {
	if r.closed.Load() != 0 {
		return admingeo.AdminPath{}, errors.New("resolver is closed")
	}
	return r.result, nil
}

func (r *fakeResolver) Version() admingeo.DatasetVersion { return r.version }

func (r *fakeResolver) Close() error {
	r.closed.Add(1)
	return nil
}

type fakeFactory struct {
	mu       sync.Mutex
	results  map[string]admingeo.AdminPath
	opened   map[string][]*fakeResolver
	openPath map[string]map[ArtifactRole]string
}

func newFakeFactory() *fakeFactory {
	return &fakeFactory{
		results:  make(map[string]admingeo.AdminPath),
		opened:   make(map[string][]*fakeResolver),
		openPath: make(map[string]map[ArtifactRole]string),
	}
}

func (f *fakeFactory) open(_ context.Context, generation Generation, paths map[ArtifactRole]string) (admingeo.Resolver, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result, ok := f.results[generation.ID]
	if !ok {
		result = expectedArea
	}
	resolver := &fakeResolver{result: result, version: generation.DatasetVersion}
	f.opened[generation.ID] = append(f.opened[generation.ID], resolver)
	f.openPath[generation.ID] = paths
	return resolver, nil
}

func (f *fakeFactory) last(t *testing.T, generationID string) *fakeResolver {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	resolvers := f.opened[generationID]
	require.NotEmpty(t, resolvers)
	return resolvers[len(resolvers)-1]
}

func TestInstallSuccess(t *testing.T) {
	body := []byte("verified geodata")
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/artifact", request.URL.Path)
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)

	generation := testGeneration("generation-1", server.URL+"/artifact", body)
	factory := newFakeFactory()
	root := t.TempDir()
	manager := openTestManager(t, root, testManifest(generation), factory, server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	var updates []Progress
	require.NoError(t, manager.Install(context.Background(), generation.ID, func(progress Progress) {
		updates = append(updates, progress)
	}))
	server.Close() // Status and leases must remain entirely local.
	require.NotEmpty(t, updates)
	assert.Zero(t, updates[0].Bytes)
	assert.Equal(t, int64(len(body)), updates[len(updates)-1].Bytes)

	status := manager.Status()
	assert.True(t, status.Current.Installed)
	assert.True(t, status.Current.Valid)
	assert.Equal(t, generation.ID, status.Current.GenerationID)
	assert.Equal(t, generation.DatasetVersion, status.Current.DatasetVersion)
	assert.Equal(t, generation.SourceVersion, status.Current.SourceVersion)
	assert.Empty(t, status.Previous.GenerationID)
	assert.False(t, status.Installing)

	artifactPath := filepath.Join(root, "generations", generation.ID, generation.Artifacts[0].Filename)
	artifactInfo, err := os.Stat(artifactPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), artifactInfo.Mode().Perm())
	directoryInfo, err := os.Stat(filepath.Dir(artifactPath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), directoryInfo.Mode().Perm())
	activationInfo, err := os.Stat(filepath.Join(root, "current.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), activationInfo.Mode().Perm())

	lease, err := manager.Acquire()
	require.NoError(t, err)
	area, err := lease.Resolve(context.Background(), admingeo.Coordinate{Latitude: 41.38, Longitude: 2.17})
	require.NoError(t, err)
	assert.Equal(t, expectedArea, area)
	require.NoError(t, lease.Close())
}

func TestParseManifestUsesStableProbeFieldNames(t *testing.T) {
	generation := testGeneration("generation-1", "https://data.example.test/artifact", []byte("data"))
	encoded, err := json.Marshal(testManifest(generation))
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"country_id":"ES"`)

	parsed, err := ParseManifest(encoded, 1024)
	require.NoError(t, err)
	assert.Equal(t, testManifest(generation), parsed)
}

func TestWrongHashSizeAndTruncationPreserveActiveGeneration(t *testing.T) {
	goodBody := []byte("current generation")
	badBody := []byte("replacement generation")

	tests := []struct {
		name       string
		path       string
		generation func(string) Generation
		handler    http.HandlerFunc
		errorText  string
	}{
		{
			name: "wrong hash",
			path: "/wrong-hash",
			generation: func(downloadURL string) Generation {
				generation := testGeneration("bad-hash", downloadURL, badBody)
				generation.Artifacts[0].SHA256 = digest([]byte("some other body"))
				return generation
			},
			handler:   func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(badBody) },
			errorText: "SHA-256 mismatch",
		},
		{
			name: "wrong size",
			path: "/wrong-size",
			generation: func(downloadURL string) Generation {
				generation := testGeneration("bad-size", downloadURL, badBody)
				generation.Artifacts[0].Bytes++
				return generation
			},
			handler:   func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(badBody) },
			errorText: "content length",
		},
		{
			name: "truncated",
			path: "/truncated",
			generation: func(downloadURL string) Generation {
				return testGeneration("truncated", downloadURL, badBody)
			},
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Length", fmt.Sprint(len(badBody)))
				_, _ = response.Write(badBody[:4])
			},
			errorText: "body",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			t.Cleanup(server.Close)
			mux.HandleFunc("/good", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(goodBody) })
			mux.HandleFunc(test.path, test.handler)

			good := testGeneration("good", server.URL+"/good", goodBody)
			bad := test.generation(server.URL + test.path)
			factory := newFakeFactory()
			manager := openTestManager(t, t.TempDir(), testManifest(good, bad), factory, server.Client())
			t.Cleanup(func() { require.NoError(t, manager.Close()) })
			require.NoError(t, manager.Install(context.Background(), good.ID, nil))
			activeResolver := factory.last(t, good.ID)

			err := manager.Install(context.Background(), bad.ID, nil)
			require.ErrorContains(t, err, test.errorText)
			assertActiveGeneration(t, manager, good.ID)
			assert.Zero(t, activeResolver.closed.Load())
		})
	}
}

func TestCancelledInstallPreservesActiveGeneration(t *testing.T) {
	goodBody := []byte("current generation")
	largeBody := make([]byte, 512*1024)
	for index := range largeBody {
		largeBody[index] = byte(index)
	}
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/good", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(goodBody) })
	mux.HandleFunc("/large", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(largeBody) })

	good := testGeneration("good", server.URL+"/good", goodBody)
	replacement := testGeneration("cancelled", server.URL+"/large", largeBody)
	factory := newFakeFactory()
	manager := openTestManager(t, t.TempDir(), testManifest(good, replacement), factory, server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NoError(t, manager.Install(context.Background(), good.ID, nil))

	ctx, cancel := context.WithCancel(context.Background())
	err := manager.Install(ctx, replacement.ID, func(progress Progress) {
		if progress.Bytes > 0 {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	assertActiveGeneration(t, manager, good.ID)
}

func TestRedirectToNonAllowlistedHostPreservesActiveGeneration(t *testing.T) {
	goodBody := []byte("current generation")
	replacementBody := []byte("redirected generation")
	other := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(replacementBody)
	}))
	t.Cleanup(other.Close)

	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/good", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(goodBody) })
	mux.HandleFunc("/redirect", func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, other.URL+"/artifact", http.StatusFound)
	})

	good := testGeneration("good", server.URL+"/good", goodBody)
	replacement := testGeneration("bad-redirect", server.URL+"/redirect", replacementBody)
	factory := newFakeFactory()
	manager := openTestManager(t, t.TempDir(), testManifest(good, replacement), factory, server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NoError(t, manager.Install(context.Background(), good.ID, nil))

	err := manager.Install(context.Background(), replacement.ID, nil)
	require.ErrorContains(t, err, "not allowlisted")
	assertActiveGeneration(t, manager, good.ID)
}

func TestFailedProbePreservesActiveGeneration(t *testing.T) {
	goodBody := []byte("current generation")
	replacementBody := []byte("replacement generation")
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/good", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(goodBody) })
	mux.HandleFunc("/replacement", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(replacementBody) })

	good := testGeneration("good", server.URL+"/good", goodBody)
	replacement := testGeneration("bad-probe", server.URL+"/replacement", replacementBody)
	factory := newFakeFactory()
	factory.results[replacement.ID] = admingeo.AdminPath{CountryID: "FR"}
	root := t.TempDir()
	manager := openTestManager(t, root, testManifest(good, replacement), factory, server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NoError(t, manager.Install(context.Background(), good.ID, nil))

	err := manager.Install(context.Background(), replacement.ID, nil)
	require.ErrorContains(t, err, "result mismatch")
	assertActiveGeneration(t, manager, good.ID)
	_, statErr := os.Stat(filepath.Join(root, "generations", replacement.ID))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	assert.Equal(t, int32(1), factory.last(t, replacement.ID).closed.Load())
}

func TestStartupRecoveryRemovesStagingAndReopensActive(t *testing.T) {
	body := []byte("verified geodata")
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)
	generation := testGeneration("generation-1", server.URL+"/artifact", body)
	manifest := testManifest(generation)
	root := t.TempDir()
	firstFactory := newFakeFactory()
	manager := openTestManager(t, root, manifest, firstFactory, server.Client())
	require.NoError(t, manager.Install(context.Background(), generation.ID, nil))
	require.NoError(t, manager.Close())

	stagingDir := filepath.Join(root, ".staging-abandoned")
	require.NoError(t, os.Mkdir(stagingDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "partial"), []byte("partial"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".current-abandoned"), []byte("partial"), 0o600))

	secondFactory := newFakeFactory()
	reopened := openTestManager(t, root, manifest, secondFactory, server.Client())
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	assert.NoDirExists(t, stagingDir)
	assert.NoFileExists(t, filepath.Join(root, ".current-abandoned"))
	assertActiveGeneration(t, reopened, generation.ID)
	assert.NotNil(t, secondFactory.last(t, generation.ID))
}

func TestInvalidActivationDegradesToRecoverableStatus(t *testing.T) {
	root := t.TempDir()
	manager := openTestManager(t, root, testManifest(), newFakeFactory(), nil)
	require.NoError(t, manager.Close())
	require.NoError(t, os.WriteFile(filepath.Join(root, "current.json"), []byte("not JSON"), 0o600))

	reopened := openTestManager(t, root, testManifest(), newFakeFactory(), nil)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	status := reopened.Status()
	assert.Contains(t, status.Error, "decode geodata activation")
	assert.False(t, status.Current.Installed)
	assert.NoFileExists(t, filepath.Join(root, "current.json"))
}

func TestCorruptActiveGenerationCanBeReinstalled(t *testing.T) {
	body := []byte("verified geodata")
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)
	generation := testGeneration("generation-1", server.URL+"/artifact", body)
	manifest := testManifest(generation)
	root := t.TempDir()
	first := openTestManager(t, root, manifest, newFakeFactory(), server.Client())
	require.NoError(t, first.Install(context.Background(), generation.ID, nil))
	require.NoError(t, first.Close())

	artifactPath := filepath.Join(root, "generations", generation.ID, generation.Artifacts[0].Filename)
	require.NoError(t, os.WriteFile(artifactPath, []byte("corrupt geodata!"), 0o600))
	reopened := openTestManager(t, root, manifest, newFakeFactory(), server.Client())
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	status := reopened.Status()
	assert.True(t, status.Current.Installed)
	assert.False(t, status.Current.Valid)
	assert.NotEmpty(t, status.Current.Error)

	require.NoError(t, reopened.Install(context.Background(), generation.ID, nil))
	status = reopened.Status()
	assert.True(t, status.Current.Valid)
	assert.Empty(t, status.Current.Error)
}

func TestRetainedGenerationRecoversFromPreActivationDirectoryMode(t *testing.T) {
	body := []byte("verified geodata")
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)
	generation := testGeneration("generation-1", server.URL+"/artifact", body)
	manifest := testManifest(generation)
	root := t.TempDir()
	first := openTestManager(t, root, manifest, newFakeFactory(), server.Client())
	require.NoError(t, first.Install(context.Background(), generation.ID, nil))
	require.NoError(t, first.Close())
	generationPath := filepath.Join(root, "generations", generation.ID)
	require.NoError(t, os.Chmod(generationPath, 0o700))

	reopened := openTestManager(t, root, manifest, newFakeFactory(), server.Client())
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	server.Close()
	require.NoError(t, reopened.Install(context.Background(), generation.ID, nil))
	info, err := os.Stat(generationPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
	assert.True(t, reopened.Status().Current.Valid)
}

func TestLeaseKeepsRetiredResolverOpenAcrossActivation(t *testing.T) {
	firstBody := []byte("first generation")
	secondBody := []byte("second generation")
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/first", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(firstBody) })
	mux.HandleFunc("/second", func(response http.ResponseWriter, _ *http.Request) { _, _ = response.Write(secondBody) })

	first := testGeneration("first", server.URL+"/first", firstBody)
	second := testGeneration("second", server.URL+"/second", secondBody)
	factory := newFakeFactory()
	manager := openTestManager(t, t.TempDir(), testManifest(first, second), factory, server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	require.NoError(t, manager.Install(context.Background(), first.ID, nil))
	firstResolver := factory.last(t, first.ID)
	lease, err := manager.Acquire()
	require.NoError(t, err)

	require.NoError(t, manager.Install(context.Background(), second.ID, nil))
	assert.Zero(t, firstResolver.closed.Load())
	assert.Equal(t, first.ID, lease.Generation().ID)
	area, err := lease.Resolve(context.Background(), admingeo.Coordinate{Latitude: 41.38, Longitude: 2.17})
	require.NoError(t, err)
	assert.Equal(t, expectedArea, area)
	status := manager.Status()
	assert.Equal(t, second.ID, status.Current.GenerationID)
	assert.Equal(t, first.ID, status.Previous.GenerationID)
	assert.True(t, status.Previous.Valid)

	require.NoError(t, lease.Close())
	assert.Equal(t, int32(1), firstResolver.closed.Load())
	_, err = lease.Resolve(context.Background(), admingeo.Coordinate{Latitude: 41.38, Longitude: 2.17})
	assert.ErrorIs(t, err, ErrClosed)

	server.Close()
	require.NoError(t, manager.Install(context.Background(), first.ID, nil), "retained generations reactivate without network access")
	status = manager.Status()
	assert.Equal(t, first.ID, status.Current.GenerationID)
	assert.Equal(t, second.ID, status.Previous.GenerationID)
}

func TestOnlyOneInstallCanBeInFlight(t *testing.T) {
	body := []byte("generation")
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)
	generation := testGeneration("generation-1", server.URL+"/artifact", body)
	manager := openTestManager(t, t.TempDir(), testManifest(generation), newFakeFactory(), server.Client())
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	installDone := make(chan error, 1)
	go func() { installDone <- manager.Install(context.Background(), generation.ID, nil) }()
	<-started
	assert.ErrorIs(t, manager.Install(context.Background(), generation.ID, nil), ErrInstallInProgress)
	close(release)
	require.NoError(t, <-installDone)
}

func testGeneration(id, downloadURL string, body []byte) Generation {
	return Generation{
		ID:             id,
		DatasetVersion: admingeo.DatasetVersion(id + "-dataset"),
		SourceVersion:  id + "-source",
		Attribution:    "Test data contributors",
		License:        "Test license",
		Artifacts: []Artifact{{
			Role:     "boundaries",
			Filename: "boundaries.db",
			URL:      downloadURL,
			Bytes:    int64(len(body)),
			SHA256:   digest(body),
		}},
		Probes: []Probe{{Latitude: 41.38, Longitude: 2.17, Expected: expectedProbePath()}},
	}
}

func expectedProbePath() ExpectedAdminPath {
	return ExpectedAdminPath{
		Country:   expectedArea.Country,
		CountryID: expectedArea.CountryID,
		Region:    expectedArea.Region,
		County:    expectedArea.County,
		Locality:  expectedArea.Locality,
	}
}

func testManifest(generations ...Generation) Manifest {
	return Manifest{FormatVersion: ManifestFormatVersion, Generations: generations}
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func openTestManager(t *testing.T, root string, manifest Manifest, factory *fakeFactory, client *http.Client) *Manager {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, _ error) error {
			if entry != nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	manager, err := Open(context.Background(), root, manifest, factory.open, Options{
		HTTPClient:       client,
		MaxArtifactBytes: 1024 * 1024,
	})
	require.NoError(t, err)
	return manager
}

func assertActiveGeneration(t *testing.T, manager *Manager, generationID string) {
	t.Helper()
	status := manager.Status()
	assert.Equal(t, generationID, status.Current.GenerationID)
	assert.True(t, status.Current.Valid)
	lease, err := manager.Acquire()
	require.NoError(t, err)
	assert.Equal(t, generationID, lease.Generation().ID)
	require.NoError(t, lease.Close())
}
