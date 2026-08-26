package geodata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/rubiojr/whereami/internal/admingeo"
)

const (
	defaultMaxArtifactBytes int64 = 2 << 30
	maxResponseHeaderBytes        = 64 << 10
)

var (
	// ErrInstallInProgress means this manager is already installing a generation.
	ErrInstallInProgress = errors.New("geodata install already in progress")
	// ErrNoActiveGeneration means no verified generation has been activated.
	ErrNoActiveGeneration = errors.New("no active geodata generation")
	// ErrClosed means the manager or lease can no longer be used.
	ErrClosed = errors.New("geodata manager is closed")
)

// ResolverFactory opens a resolver over verified files keyed by logical role.
// The map and its paths must not be retained for writes.
type ResolverFactory func(context.Context, Generation, map[ArtifactRole]string) (admingeo.Resolver, error)

// Progress reports verified transfer progress. Callbacks run synchronously.
type Progress struct {
	GenerationID  string
	Role          ArtifactRole
	Filename      string
	ArtifactIndex int
	ArtifactCount int
	Bytes         int64
	TotalBytes    int64
}

// ProgressFunc receives download progress, including an initial zero update.
type ProgressFunc func(Progress)

// Options controls process-local dependencies and hard resource limits.
type Options struct {
	HTTPClient       *http.Client
	MaxArtifactBytes int64
}

// GenerationStatus is a local verification result for one retained generation.
type GenerationStatus struct {
	GenerationID   string                  `json:"generation_id"`
	DatasetVersion admingeo.DatasetVersion `json:"dataset_version,omitempty"`
	SourceVersion  string                  `json:"source_version,omitempty"`
	Attribution    string                  `json:"attribution,omitempty"`
	License        string                  `json:"license,omitempty"`
	Installed      bool                    `json:"installed"`
	Valid          bool                    `json:"valid"`
	Error          string                  `json:"error,omitempty"`
}

// Status describes persisted activation metadata and local artifact integrity.
type Status struct {
	Current    GenerationStatus `json:"current"`
	Previous   GenerationStatus `json:"previous"`
	Installing bool             `json:"installing"`
	Error      string           `json:"error,omitempty"`
}

type activationState struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}

type openGeneration struct {
	generation Generation
	resolver   admingeo.Resolver
	refs       int
	retired    bool
	closed     bool
}

// Manager owns installation, activation, and process-local resolver leases.
type Manager struct {
	mu               sync.Mutex
	root             string
	manifest         Manifest
	factory          ResolverFactory
	httpClient       *http.Client
	maxArtifactBytes int64
	currentID        string
	previousID       string
	active           *openGeneration
	installing       bool
	closed           bool
	verified         map[string]bool
	statusErrors     map[string]string
	startupError     string
}

// Open initializes a manager, removes abandoned staging files, and opens the
// persisted active generation if one exists.
func Open(ctx context.Context, root string, manifest Manifest, factory ResolverFactory, options Options) (*Manager, error) {
	if ctx == nil {
		return nil, errors.New("geodata context is nil")
	}
	if factory == nil {
		return nil, errors.New("geodata resolver factory is nil")
	}
	maxBytes := options.MaxArtifactBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxArtifactBytes
	}
	manifest = cloneManifest(manifest)
	if err := manifest.Validate(maxBytes); err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve geodata root: %w", err)
	}
	if err := prepareRoot(absRoot); err != nil {
		return nil, err
	}
	client, err := hardenedHTTPClient(options.HTTPClient)
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		root:             absRoot,
		manifest:         manifest,
		factory:          factory,
		httpClient:       client,
		maxArtifactBytes: maxBytes,
		verified:         make(map[string]bool),
		statusErrors:     make(map[string]string),
	}
	if err := manager.recoverStaging(); err != nil {
		return nil, err
	}
	state, err := manager.readActivation()
	if err != nil {
		manager.startupError = err.Error()
		if removeErr := manager.removeActivation(); removeErr != nil {
			return nil, fmt.Errorf("recover invalid geodata activation: %w", removeErr)
		}
		state = activationState{}
	}
	manager.currentID = state.Current
	manager.previousID = state.Previous
	if state.Current != "" {
		generation, ok := manifest.generation(state.Current)
		if !ok {
			manager.statusErrors[state.Current] = fmt.Sprintf("generation %q is absent from manifest", state.Current)
		} else {
			resolver, openErr := manager.openAndProbe(ctx, generation, manager.generationDir(generation.ID))
			if openErr != nil {
				manager.statusErrors[state.Current] = fmt.Sprintf("open active generation: %v", openErr)
			} else {
				manager.active = &openGeneration{generation: generation, resolver: resolver}
				manager.verified[generation.ID] = true
			}
		}
	}
	return manager, nil
}

// Install downloads, verifies, probes, and atomically activates a manifest generation.
func (m *Manager) Install(ctx context.Context, generationID string, progress ProgressFunc) error {
	if ctx == nil {
		return errors.New("geodata context is nil")
	}
	generation, ok := m.manifest.generation(generationID)
	if !ok {
		return fmt.Errorf("unknown geodata generation %q", generationID)
	}
	if err := m.beginInstall(); err != nil {
		return err
	}
	defer m.endInstall()

	if err := ctx.Err(); err != nil {
		return err
	}
	target := m.generationDir(generation.ID)
	if _, err := os.Lstat(target); err == nil {
		if err := normalizeRetainedGeneration(target, generation); err == nil {
			return m.activatePrepared(ctx, generation, target)
		} else {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if removeErr := removeGeneration(target); removeErr != nil {
				return fmt.Errorf("replace invalid retained geodata generation %q: %w", generation.ID, removeErr)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect retained geodata generation: %w", err)
	}
	stage, err := os.MkdirTemp(m.root, ".staging-")
	if err != nil {
		return fmt.Errorf("create geodata staging directory: %w", err)
	}
	if err := os.Chmod(stage, 0o700); err != nil {
		os.RemoveAll(stage)
		return fmt.Errorf("secure geodata staging directory: %w", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.Chmod(stage, 0o700)
			_ = os.RemoveAll(stage)
		}
	}()

	allowedAuthorities := generationAuthorities(generation)
	for index, artifact := range generation.Artifacts {
		if err := m.download(ctx, generation, artifact, index, stage, allowedAuthorities, progress); err != nil {
			return err
		}
	}
	if err := writeGenerationMetadata(stage, generation); err != nil {
		return err
	}
	if err := syncDir(stage); err != nil {
		return fmt.Errorf("sync geodata staging directory: %w", err)
	}
	if err := os.Rename(stage, target); err != nil {
		return fmt.Errorf("publish geodata generation: %w", err)
	}
	keepStage = true
	cleanupTarget := true
	defer func() {
		if cleanupTarget {
			_ = os.Chmod(target, 0o700)
			_ = os.RemoveAll(target)
			_ = syncDir(filepath.Dir(target))
		}
	}()
	if err := syncDir(filepath.Dir(target)); err != nil {
		return fmt.Errorf("sync geodata generations directory: %w", err)
	}
	if err := os.Chmod(target, 0o500); err != nil {
		return fmt.Errorf("make geodata generation immutable: %w", err)
	}
	if err := syncDir(target); err != nil {
		return fmt.Errorf("sync immutable geodata generation: %w", err)
	}
	resolver, err := m.openAndProbe(ctx, generation, target)
	if err != nil {
		return err
	}
	resolverOwned := true
	defer func() {
		if resolverOwned {
			_ = resolver.Close()
		}
	}()
	if err := m.activate(generation, resolver); err != nil {
		return err
	}
	resolverOwned = false
	cleanupTarget = false
	return nil
}

func (m *Manager) activatePrepared(ctx context.Context, generation Generation, target string) error {
	m.mu.Lock()
	alreadyActive := !m.closed && m.currentID == generation.ID && m.active != nil
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if alreadyActive {
		return nil
	}
	resolver, err := m.openAndProbe(ctx, generation, target)
	if err != nil {
		return err
	}
	if err := m.activate(generation, resolver); err != nil {
		_ = resolver.Close()
		return err
	}
	return nil
}

func normalizeRetainedGeneration(target string, generation Generation) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode().Perm() == 0o700 {
		if err := verifyGenerationWithMode(target, generation, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(target, 0o500); err != nil {
			return err
		}
		if err := syncDir(target); err != nil {
			return err
		}
	}
	return verifyGeneration(target, generation)
}

// Acquire returns a reference-counted lease on the active resolver.
func (m *Manager) Acquire() (*Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if m.active == nil {
		return nil, ErrNoActiveGeneration
	}
	m.active.refs++
	return &Lease{manager: m, generation: m.active}, nil
}

// Status reports local generation metadata using verification results cached
// when generations are opened. It does not hash large artifacts per call.
func (m *Manager) Status() Status {
	m.mu.Lock()
	currentID := m.currentID
	previousID := m.previousID
	installing := m.installing
	currentVerified := m.verified[currentID]
	previousVerified := m.verified[previousID]
	currentError := m.statusErrors[currentID]
	previousError := m.statusErrors[previousID]
	startupError := m.startupError
	m.mu.Unlock()
	return Status{
		Current:    m.localStatus(currentID, currentVerified, currentError),
		Previous:   m.localStatus(previousID, previousVerified, previousError),
		Installing: installing,
		Error:      startupError,
	}
}

// Close retires the active generation. Its resolver closes after the final lease.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	active := m.active
	m.active = nil
	var resolver admingeo.Resolver
	if active != nil {
		active.retired = true
		if active.refs == 0 && !active.closed {
			active.closed = true
			resolver = active.resolver
		}
	}
	m.mu.Unlock()
	if resolver != nil {
		return resolver.Close()
	}
	return nil
}

// Lease keeps a generation's resolver alive across later activations.
type Lease struct {
	mu         sync.RWMutex
	manager    *Manager
	generation *openGeneration
	closed     bool
}

var _ admingeo.Resolver = (*Lease)(nil)

// Generation returns the immutable metadata associated with this lease.
func (l *Lease) Generation() Generation {
	return cloneGeneration(l.generation.generation)
}

// Resolve performs a lookup while the lease is open.
func (l *Lease) Resolve(ctx context.Context, coordinate admingeo.Coordinate) (admingeo.AdminPath, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed {
		return admingeo.AdminPath{}, ErrClosed
	}
	return l.generation.resolver.Resolve(ctx, coordinate)
}

// Version returns the immutable dataset version held by this lease.
func (l *Lease) Version() admingeo.DatasetVersion {
	return l.generation.generation.DatasetVersion
}

// Close releases the generation and closes a retired resolver after its final lease.
func (l *Lease) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	m := l.manager
	m.mu.Lock()
	generation := l.generation
	generation.refs--
	var resolver admingeo.Resolver
	if generation.retired && generation.refs == 0 && !generation.closed {
		generation.closed = true
		resolver = generation.resolver
	}
	m.mu.Unlock()
	if resolver != nil {
		return resolver.Close()
	}
	return nil
}

func (m *Manager) beginInstall() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.installing {
		return ErrInstallInProgress
	}
	m.installing = true
	return nil
}

func (m *Manager) endInstall() {
	m.mu.Lock()
	m.installing = false
	m.mu.Unlock()
}

func (m *Manager) download(ctx context.Context, generation Generation, artifact Artifact, index int, stage string, allowed map[string]struct{}, progress ProgressFunc) error {
	parsedURL, _ := url.Parse(artifact.URL)
	if err := validateDownloadURL(parsedURL, allowed); err != nil {
		return fmt.Errorf("download artifact %q: %w", artifact.Role, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return fmt.Errorf("create artifact %q request: %w", artifact.Role, err)
	}
	client := *m.httpClient
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return validateDownloadURL(request.URL, allowed)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download artifact %q: %w", artifact.Role, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download artifact %q: unexpected HTTP status %s", artifact.Role, response.Status)
	}
	if response.ContentLength >= 0 && response.ContentLength != artifact.Bytes {
		return fmt.Errorf("download artifact %q: content length %d, expected %d", artifact.Role, response.ContentLength, artifact.Bytes)
	}

	path := filepath.Join(stage, artifact.Filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create artifact %q: %w", artifact.Role, err)
	}
	fileOwned := true
	defer func() {
		if fileOwned {
			_ = file.Close()
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure artifact %q: %w", artifact.Role, err)
	}
	hasher := sha256.New()
	reporter := &progressWriter{
		ctx:      ctx,
		writer:   io.MultiWriter(file, hasher),
		callback: progress,
		progress: Progress{
			GenerationID:  generation.ID,
			Role:          artifact.Role,
			Filename:      artifact.Filename,
			ArtifactIndex: index,
			ArtifactCount: len(generation.Artifacts),
			TotalBytes:    artifact.Bytes,
		},
	}
	reporter.report()
	written, copyErr := io.Copy(reporter, io.LimitReader(response.Body, artifact.Bytes+1))
	if copyErr != nil {
		return fmt.Errorf("download artifact %q body: %w", artifact.Role, copyErr)
	}
	if written != artifact.Bytes {
		return fmt.Errorf("download artifact %q: received %d bytes, expected %d", artifact.Role, written, artifact.Bytes)
	}
	if digest := hex.EncodeToString(hasher.Sum(nil)); digest != artifact.SHA256 {
		return fmt.Errorf("download artifact %q: SHA-256 mismatch", artifact.Role)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync artifact %q: %w", artifact.Role, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close artifact %q: %w", artifact.Role, err)
	}
	fileOwned = false
	return nil
}

type progressWriter struct {
	ctx      context.Context
	writer   io.Writer
	callback ProgressFunc
	progress Progress
}

func (w *progressWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(data)
	w.progress.Bytes += int64(n)
	w.report()
	return n, err
}

func (w *progressWriter) report() {
	if w.callback != nil {
		w.callback(w.progress)
	}
}

func (m *Manager) openAndProbe(ctx context.Context, generation Generation, dir string) (admingeo.Resolver, error) {
	if err := verifyGeneration(dir, generation); err != nil {
		return nil, err
	}
	paths := make(map[ArtifactRole]string, len(generation.Artifacts))
	for _, artifact := range generation.Artifacts {
		paths[artifact.Role] = filepath.Join(dir, artifact.Filename)
	}
	resolver, err := m.factory(ctx, cloneGeneration(generation), paths)
	if err != nil {
		return nil, fmt.Errorf("open resolver: %w", err)
	}
	if resolver == nil {
		return nil, errors.New("open resolver: factory returned nil")
	}
	if resolver.Version() != generation.DatasetVersion {
		_ = resolver.Close()
		return nil, fmt.Errorf("resolver dataset version %q, expected %q", resolver.Version(), generation.DatasetVersion)
	}
	for index, probe := range generation.Probes {
		if err := ctx.Err(); err != nil {
			_ = resolver.Close()
			return nil, err
		}
		actual, err := resolver.Resolve(ctx, admingeo.Coordinate{Latitude: probe.Latitude, Longitude: probe.Longitude})
		if err != nil {
			_ = resolver.Close()
			return nil, fmt.Errorf("probe %d: %w", index, err)
		}
		expected := probe.Expected.resolved()
		if !reflect.DeepEqual(actual, expected) {
			_ = resolver.Close()
			return nil, fmt.Errorf("probe %d: result mismatch: got %+v, expected %+v", index, actual, expected)
		}
	}
	return resolver, nil
}

func (m *Manager) activate(generation Generation, resolver admingeo.Resolver) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	state := activationState{Current: generation.ID, Previous: m.currentID}
	if err := m.writeActivation(state); err != nil {
		m.mu.Unlock()
		return err
	}
	old := m.active
	m.active = &openGeneration{generation: generation, resolver: resolver}
	m.currentID = state.Current
	m.previousID = state.Previous
	m.verified[generation.ID] = true
	delete(m.statusErrors, generation.ID)
	m.startupError = ""
	var retiredResolver admingeo.Resolver
	if old != nil {
		old.retired = true
		if old.refs == 0 && !old.closed {
			old.closed = true
			retiredResolver = old.resolver
		}
	}
	m.mu.Unlock()
	if retiredResolver != nil {
		_ = retiredResolver.Close()
	}
	return nil
}

func (m *Manager) localStatus(id string, verified bool, statusError string) GenerationStatus {
	status := GenerationStatus{GenerationID: id}
	if id == "" {
		return status
	}
	generation, ok := m.manifest.generation(id)
	if !ok {
		if statusError != "" {
			status.Error = statusError
		} else {
			status.Error = "generation absent from manifest"
		}
		return status
	}
	status.DatasetVersion = generation.DatasetVersion
	status.SourceVersion = generation.SourceVersion
	status.Attribution = generation.Attribution
	status.License = generation.License
	dir := m.generationDir(id)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if err != nil {
			status.Error = err.Error()
		} else {
			status.Error = "generation path is not a directory"
		}
		return status
	}
	status.Installed = true
	if err := verifyGenerationLayout(dir, generation); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Valid = verified
	if statusError != "" {
		status.Error = statusError
	} else if !verified {
		status.Error = "generation has not been verified in this session"
	}
	return status
}

func (m *Manager) removeActivation() error {
	path := filepath.Join(m.root, "current.json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(m.root)
}

func prepareRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create geodata root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect geodata root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("geodata root is not a real directory")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("secure geodata root: %w", err)
	}
	generations := filepath.Join(root, "generations")
	if err := os.Mkdir(generations, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create geodata generations directory: %w", err)
	}
	info, err = os.Lstat(generations)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("geodata generations path is not a real directory")
	}
	return os.Chmod(generations, 0o700)
}

func (m *Manager) recoverStaging() error {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return fmt.Errorf("scan geodata root: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".staging-") && !strings.HasPrefix(entry.Name(), ".current-") {
			continue
		}
		path := filepath.Join(m.root, entry.Name())
		if entry.IsDir() {
			if err := os.Chmod(path, 0o700); err != nil {
				return fmt.Errorf("recover geodata staging directory: %w", err)
			}
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove abandoned geodata staging path: %w", err)
		}
	}
	return syncDir(m.root)
}

func (m *Manager) readActivation() (activationState, error) {
	path := filepath.Join(m.root, "current.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return activationState{}, nil
	}
	if err != nil {
		return activationState{}, fmt.Errorf("inspect geodata activation: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return activationState{}, errors.New("geodata activation has wrong type or mode")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return activationState{}, fmt.Errorf("read geodata activation: %w", err)
	}
	var state activationState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return activationState{}, fmt.Errorf("decode geodata activation: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return activationState{}, errors.New("decode geodata activation: trailing data")
	}
	if state.Current == "" || !safeName.MatchString(state.Current) || (state.Previous != "" && !safeName.MatchString(state.Previous)) {
		return activationState{}, errors.New("geodata activation contains invalid generation IDs")
	}
	return state, nil
}

func (m *Manager) writeActivation(state activationState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode geodata activation: %w", err)
	}
	temporary, err := os.CreateTemp(m.root, ".current-")
	if err != nil {
		return fmt.Errorf("create geodata activation: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		_ = temporary.Close()
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure geodata activation: %w", err)
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write geodata activation: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync geodata activation: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close geodata activation: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(m.root, "current.json")); err != nil {
		return fmt.Errorf("activate geodata generation: %w", err)
	}
	remove = false
	if err := syncDir(m.root); err != nil {
		return fmt.Errorf("sync geodata activation directory: %w", err)
	}
	return nil
}

func writeGenerationMetadata(dir string, generation Generation) error {
	data, err := json.Marshal(generation)
	if err != nil {
		return fmt.Errorf("encode generation metadata: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "generation.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create generation metadata: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure generation metadata: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("write generation metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync generation metadata: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close generation metadata: %w", err)
	}
	return nil
}

func verifyGeneration(dir string, generation Generation) error {
	return verifyGenerationWithMode(dir, generation, 0o500)
}

func verifyGenerationLayout(dir string, generation Generation) error {
	return verifyGenerationLayoutWithMode(dir, generation, 0o500)
}

func verifyGenerationWithMode(dir string, generation Generation, directoryMode os.FileMode) error {
	if err := verifyGenerationLayoutWithMode(dir, generation, directoryMode); err != nil {
		return err
	}
	for _, artifact := range generation.Artifacts {
		path := filepath.Join(dir, artifact.Filename)
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open artifact %q: %w", artifact.Role, err)
		}
		hasher := sha256.New()
		read, copyErr := io.Copy(hasher, io.LimitReader(file, artifact.Bytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("verify artifact %q: %w", artifact.Role, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close artifact %q: %w", artifact.Role, closeErr)
		}
		if read != artifact.Bytes || hex.EncodeToString(hasher.Sum(nil)) != artifact.SHA256 {
			return fmt.Errorf("artifact %q failed verification", artifact.Role)
		}
	}
	return nil
}

func verifyGenerationLayoutWithMode(dir string, generation Generation, directoryMode os.FileMode) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect generation directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != directoryMode {
		return errors.New("generation path is not an immutable directory")
	}
	expectedMetadata, err := json.Marshal(generation)
	if err != nil {
		return fmt.Errorf("encode expected generation metadata: %w", err)
	}
	expectedMetadata = append(expectedMetadata, '\n')
	metadata, err := readExactRegularFile(filepath.Join(dir, "generation.json"), int64(len(expectedMetadata)))
	if err != nil {
		return fmt.Errorf("verify generation metadata: %w", err)
	}
	if !bytes.Equal(metadata, expectedMetadata) {
		return errors.New("generation metadata does not match manifest")
	}
	for _, artifact := range generation.Artifacts {
		path := filepath.Join(dir, artifact.Filename)
		fileInfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect artifact %q: %w", artifact.Role, err)
		}
		if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 || fileInfo.Size() != artifact.Bytes {
			return fmt.Errorf("artifact %q has wrong type, mode, or size", artifact.Role)
		}
	}
	return nil
}

func removeGeneration(path string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func readExactRegularFile(path string, size int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != size {
		return nil, errors.New("file has wrong type, mode, or size")
	}
	return os.ReadFile(path)
}

func generationAuthorities(generation Generation) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, artifact := range generation.Artifacts {
		parsedURL, _ := url.Parse(artifact.URL)
		allowed[strings.ToLower(parsedURL.Host)] = struct{}{}
	}
	return allowed
}

func validateDownloadURL(downloadURL *url.URL, allowed map[string]struct{}) error {
	if downloadURL == nil || downloadURL.Scheme != "https" || downloadURL.Host == "" || downloadURL.User != nil || downloadURL.Fragment != "" {
		return errors.New("download URL is not valid HTTPS")
	}
	if _, ok := allowed[strings.ToLower(downloadURL.Host)]; !ok {
		return fmt.Errorf("download authority %q is not allowlisted", downloadURL.Host)
	}
	return nil
}

func hardenedHTTPClient(client *http.Client) (*http.Client, error) {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	var transport *http.Transport
	switch configured := client.Transport.(type) {
	case nil:
		transport = http.DefaultTransport.(*http.Transport).Clone()
	case *http.Transport:
		transport = configured.Clone()
	default:
		return nil, errors.New("geodata HTTP client must use *http.Transport")
	}
	transport.DisableCompression = true
	transport.MaxResponseHeaderBytes = maxResponseHeaderBytes
	clone.Transport = transport
	clone.CheckRedirect = nil
	clone.Jar = nil
	return &clone, nil
}

func (m *Manager) generationDir(id string) string {
	return filepath.Join(m.root, "generations", id)
}

func syncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return err
	}
	return directory.Close()
}
