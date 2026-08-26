// Package geodata installs and activates verified, immutable geodata generations.
package geodata

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rubiojr/whereami/internal/admingeo"
)

// ManifestFormatVersion is the only manifest format understood by this package.
const ManifestFormatVersion = 1

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Manifest is intended to be decoded from data embedded in the application.
// Generations and their download locations are therefore selected by the
// application build, never by an API caller.
type Manifest struct {
	FormatVersion int          `json:"format_version"`
	Generations   []Generation `json:"generations"`
}

// Generation describes one immutable, independently verifiable dataset.
type Generation struct {
	ID             string                  `json:"id"`
	DatasetVersion admingeo.DatasetVersion `json:"dataset_version"`
	SourceVersion  string                  `json:"source_version"`
	Attribution    string                  `json:"attribution"`
	License        string                  `json:"license"`
	Artifacts      []Artifact              `json:"artifacts"`
	Probes         []Probe                 `json:"probes"`
}

// ArtifactRole identifies the logical purpose of a file for a resolver factory.
type ArtifactRole string

// Artifact gives the exact identity and source of one unarchived file.
type Artifact struct {
	Role     ArtifactRole `json:"role"`
	Filename string       `json:"filename"`
	URL      string       `json:"url"`
	Bytes    int64        `json:"bytes"`
	SHA256   string       `json:"sha256"`
}

// Probe is a known coordinate and its exact expected resolver result.
type Probe struct {
	Latitude  float64           `json:"latitude"`
	Longitude float64           `json:"longitude"`
	Expected  ExpectedAdminPath `json:"expected"`
}

// ExpectedAdminPath is the manifest representation of an admingeo.AdminPath.
type ExpectedAdminPath struct {
	Country      string `json:"country"`
	CountryID    string `json:"country_id"`
	Region       string `json:"region"`
	RegionID     string `json:"region_id"`
	County       string `json:"county"`
	CountyID     string `json:"county_id"`
	LocalAdmin   string `json:"local_admin"`
	LocalAdminID string `json:"local_admin_id"`
	Locality     string `json:"locality"`
	LocalityID   string `json:"locality_id"`
}

func (p ExpectedAdminPath) resolved() admingeo.AdminPath {
	return admingeo.AdminPath{
		Country:      p.Country,
		CountryID:    p.CountryID,
		Region:       p.Region,
		RegionID:     p.RegionID,
		County:       p.County,
		CountyID:     p.CountyID,
		LocalAdmin:   p.LocalAdmin,
		LocalAdminID: p.LocalAdminID,
		Locality:     p.Locality,
		LocalityID:   p.LocalityID,
	}
}

// ParseManifest strictly decodes and validates an embedded manifest.
func ParseManifest(data []byte, maxArtifactBytes int64) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode geodata manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("decode geodata manifest: trailing JSON value")
		}
		return Manifest{}, fmt.Errorf("decode geodata manifest trailing data: %w", err)
	}
	if err := manifest.Validate(maxArtifactBytes); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks all security-sensitive manifest invariants.
func (m Manifest) Validate(maxArtifactBytes int64) error {
	if m.FormatVersion != ManifestFormatVersion {
		return fmt.Errorf("unsupported geodata manifest format %d", m.FormatVersion)
	}
	if maxArtifactBytes <= 0 {
		return errors.New("maximum artifact size must be positive")
	}
	seenGenerations := make(map[string]struct{}, len(m.Generations))
	for _, generation := range m.Generations {
		if !safeName.MatchString(generation.ID) || generation.ID == "." || generation.ID == ".." {
			return fmt.Errorf("invalid generation ID %q", generation.ID)
		}
		if _, exists := seenGenerations[generation.ID]; exists {
			return fmt.Errorf("duplicate generation ID %q", generation.ID)
		}
		seenGenerations[generation.ID] = struct{}{}
		if strings.TrimSpace(string(generation.DatasetVersion)) == "" || strings.TrimSpace(generation.SourceVersion) == "" {
			return fmt.Errorf("generation %q has empty dataset or source version", generation.ID)
		}
		if strings.TrimSpace(generation.Attribution) == "" || strings.TrimSpace(generation.License) == "" {
			return fmt.Errorf("generation %q has empty attribution or license", generation.ID)
		}
		if len(generation.Artifacts) == 0 {
			return fmt.Errorf("generation %q has no artifacts", generation.ID)
		}
		if len(generation.Probes) == 0 {
			return fmt.Errorf("generation %q has no probes", generation.ID)
		}
		seenRoles := make(map[ArtifactRole]struct{}, len(generation.Artifacts))
		seenFiles := make(map[string]struct{}, len(generation.Artifacts))
		for _, artifact := range generation.Artifacts {
			if !safeName.MatchString(string(artifact.Role)) {
				return fmt.Errorf("generation %q has invalid artifact role %q", generation.ID, artifact.Role)
			}
			if _, exists := seenRoles[artifact.Role]; exists {
				return fmt.Errorf("generation %q has duplicate artifact role %q", generation.ID, artifact.Role)
			}
			seenRoles[artifact.Role] = struct{}{}
			if artifact.Filename != filepath.Base(artifact.Filename) || !safeName.MatchString(artifact.Filename) {
				return fmt.Errorf("generation %q has unsafe artifact filename %q", generation.ID, artifact.Filename)
			}
			if _, exists := seenFiles[artifact.Filename]; exists {
				return fmt.Errorf("generation %q has duplicate artifact filename %q", generation.ID, artifact.Filename)
			}
			seenFiles[artifact.Filename] = struct{}{}
			parsedURL, err := url.Parse(artifact.URL)
			if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
				return fmt.Errorf("generation %q artifact %q has invalid HTTPS URL", generation.ID, artifact.Role)
			}
			if artifact.Bytes < 0 || artifact.Bytes > maxArtifactBytes {
				return fmt.Errorf("generation %q artifact %q size is outside limits", generation.ID, artifact.Role)
			}
			digest, err := hex.DecodeString(artifact.SHA256)
			if err != nil || len(digest) != 32 || artifact.SHA256 != strings.ToLower(artifact.SHA256) {
				return fmt.Errorf("generation %q artifact %q has invalid SHA-256", generation.ID, artifact.Role)
			}
		}
		for _, probe := range generation.Probes {
			if math.IsNaN(probe.Latitude) || math.IsInf(probe.Latitude, 0) || probe.Latitude < -90 || probe.Latitude > 90 ||
				math.IsNaN(probe.Longitude) || math.IsInf(probe.Longitude, 0) || probe.Longitude < -180 || probe.Longitude > 180 {
				return fmt.Errorf("generation %q has invalid probe coordinate", generation.ID)
			}
		}
	}
	return nil
}

func (m Manifest) generation(id string) (Generation, bool) {
	for _, generation := range m.Generations {
		if generation.ID == id {
			return generation, true
		}
	}
	return Generation{}, false
}

func cloneManifest(manifest Manifest) Manifest {
	clone := Manifest{FormatVersion: manifest.FormatVersion, Generations: make([]Generation, len(manifest.Generations))}
	for index, generation := range manifest.Generations {
		clone.Generations[index] = cloneGeneration(generation)
	}
	return clone
}

func cloneGeneration(generation Generation) Generation {
	clone := generation
	clone.Artifacts = append([]Artifact(nil), generation.Artifacts...)
	clone.Probes = append([]Probe(nil), generation.Probes...)
	return clone
}
