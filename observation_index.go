package main

import (
	"errors"
	"path/filepath"

	"github.com/rubiojr/whereami/internal/observations"
)

var (
	observationRepo          *observations.Repository
	observationDataRoot      string
	observationBookmarksPath string
	observationIndexRebuilt  func()
)

func openObservationIndex(dataRoot, bookmarksPath string) error {
	canonicalDataRoot, err := filepath.EvalSymlinks(dataRoot)
	if err != nil {
		return err
	}
	canonicalDataRoot, err = filepath.Abs(canonicalDataRoot)
	if err != nil {
		return err
	}
	canonicalBookmarksPath := bookmarksPath
	if bookmarksPath != "" {
		if resolved, resolveErr := filepath.EvalSymlinks(bookmarksPath); resolveErr == nil {
			canonicalBookmarksPath = resolved
		}
		canonicalBookmarksPath, err = filepath.Abs(canonicalBookmarksPath)
		if err != nil {
			return err
		}
	}
	dbPath := filepath.Join(effectiveCacheDir(), "observations", "observations.sqlite")
	repo, err := observations.Open(dbPath)
	if err != nil {
		return err
	}

	observationRepo = repo
	observationDataRoot = canonicalDataRoot
	observationBookmarksPath = canonicalBookmarksPath
	if err := rebuildObservationIndex(); err != nil {
		_ = repo.Close()
		observationRepo = nil
		return err
	}
	return nil
}

func rebuildObservationIndex() error {
	if observationRepo == nil {
		return errors.New("observation repository is unavailable")
	}
	if err := observationRepo.Rebuild(observationDataRoot, observationBookmarksPath); err != nil {
		return err
	}
	if observationIndexRebuilt != nil {
		observationIndexRebuilt()
	}
	return nil
}

func addObservationSources(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if observationRepo == nil {
		return errors.New("observation repository is unavailable")
	}
	if err := observationRepo.AddSources(observationDataRoot, observationBookmarksPath, paths); err != nil {
		return err
	}
	if observationIndexRebuilt != nil {
		observationIndexRebuilt()
	}
	return nil
}

func closeObservationIndex() error {
	if observationRepo == nil {
		return nil
	}
	return observationRepo.Close()
}
