package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/admingeo/xiangshan"
	"github.com/rubiojr/whereami/internal/geodata"
)

const (
	indexFilename    = "divisions.xs-index.gz"
	polygonsFilename = "divisions.xs-poly"
)

func main() {
	var (
		inputDir       = flag.String("input", "", "directory containing the Xiangshan split files")
		outputDir      = flag.String("output", "", "new versioned distribution directory")
		baseURL        = flag.String("base-url", "", "public geodata base URL")
		generationID   = flag.String("id", "", "safe generation identifier")
		dataset        = flag.String("dataset-version", "", "Xiangshan dataset version")
		source         = flag.String("source-version", "", "Overture source version")
		licensePath    = flag.String("license", "licenses/ODbL-1.0.txt", "ODbL license file")
		provenancePath = flag.String("provenance", "docs/GEODATA.md", "geodata provenance document")
	)
	flag.Parse()
	if *inputDir == "" || *outputDir == "" || *baseURL == "" || *generationID == "" || *dataset == "" || *source == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*inputDir, *outputDir, *baseURL, *generationID, *dataset, *source, *licensePath, *provenancePath); err != nil {
		fmt.Fprintln(os.Stderr, "geodata-package:", err)
		os.Exit(1)
	}
}

func run(inputDir, outputDir, baseURL, generationID, dataset, source, licensePath, provenancePath string) error {
	indexPath := filepath.Join(inputDir, indexFilename)
	polygonsPath := filepath.Join(inputDir, polygonsFilename)
	indexBytes, indexDigest, err := fileIdentity(indexPath)
	if err != nil {
		return err
	}
	polygonsBytes, polygonsDigest, err := fileIdentity(polygonsPath)
	if err != nil {
		return err
	}

	resolver, err := xiangshan.New(indexPath, polygonsPath, admingeo.DatasetVersion(dataset))
	if err != nil {
		return fmt.Errorf("open candidate: %w", err)
	}
	probeCoordinate := admingeo.Coordinate{Latitude: 48.8584, Longitude: 2.2945}
	probe, resolveErr := resolver.Resolve(context.Background(), probeCoordinate)
	closeErr := resolver.Close()
	if resolveErr != nil {
		return fmt.Errorf("resolve package probe: %w", resolveErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close candidate: %w", closeErr)
	}
	if probe.Country == "" {
		return errors.New("package probe did not resolve a country")
	}

	versionURL, err := url.JoinPath(strings.TrimRight(baseURL, "/"), dataset)
	if err != nil {
		return fmt.Errorf("build generation URL: %w", err)
	}
	generation := geodata.Generation{
		ID:             generationID,
		DatasetVersion: admingeo.DatasetVersion(dataset),
		SourceVersion:  source,
		Attribution:    "© OpenStreetMap contributors, Overture Maps Foundation",
		License:        "ODbL-1.0",
		Artifacts: []geodata.Artifact{
			{Role: "index", Filename: indexFilename, URL: versionURL + "/" + indexFilename, Bytes: indexBytes, SHA256: indexDigest},
			{Role: "polygons", Filename: polygonsFilename, URL: versionURL + "/" + polygonsFilename, Bytes: polygonsBytes, SHA256: polygonsDigest},
		},
		Probes: []geodata.Probe{{
			Latitude: probeCoordinate.Latitude, Longitude: probeCoordinate.Longitude,
			Expected: geodata.ExpectedAdminPath{
				Country: probe.Country, CountryID: probe.CountryID,
				Region: probe.Region, RegionID: probe.RegionID,
				County: probe.County, CountyID: probe.CountyID,
				LocalAdmin: probe.LocalAdmin, LocalAdminID: probe.LocalAdminID,
				Locality: probe.Locality, LocalityID: probe.LocalityID,
			},
		}},
	}
	manifest := geodata.Manifest{FormatVersion: geodata.ManifestFormatVersion, Generations: []geodata.Generation{generation}}
	if err := manifest.Validate(1 << 30); err != nil {
		return fmt.Errorf("validate generated manifest: %w", err)
	}

	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create distribution parent: %w", err)
	}
	if _, err := os.Lstat(outputDir); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("output directory already exists: %s", outputDir)
		}
		return fmt.Errorf("inspect output directory: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".geodata-package-")
	if err != nil {
		return fmt.Errorf("create package staging directory: %w", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()

	for _, file := range []struct{ source, name string }{
		{indexPath, indexFilename},
		{polygonsPath, polygonsFilename},
		{licensePath, "ODbL-1.0.txt"},
		{provenancePath, "GEODATA.md"},
	} {
		if err := copyFile(file.source, filepath.Join(stage, file.name)); err != nil {
			return err
		}
	}
	if err := writeJSON(filepath.Join(stage, "generation.json"), generation); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		return err
	}
	if err := writeChecksums(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, outputDir); err != nil {
		return fmt.Errorf("publish package directory: %w", err)
	}
	keepStage = true
	return nil
}

func fileIdentity(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, "", fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return 0, "", fmt.Errorf("not a regular file: %s", path)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return 0, "", fmt.Errorf("hash %s: %w", path, err)
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open package input %s: %w", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create package file %s: %w", destination, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return fmt.Errorf("copy package file %s: %w", destination, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close package file %s: %w", destination, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeChecksums(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list package files: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "SHA256SUMS" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	var checksums strings.Builder
	for _, name := range names {
		_, digest, err := fileIdentity(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&checksums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(checksums.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	return nil
}
