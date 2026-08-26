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
)

func openObservationIndex(dataRoot, bookmarksPath string) error {
	dbPath := filepath.Join(effectiveCacheDir(), "observations", "observations.sqlite")
	repo, err := observations.Open(dbPath)
	if err != nil {
		return err
	}

	observationRepo = repo
	observationDataRoot = dataRoot
	observationBookmarksPath = bookmarksPath
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
	return observationRepo.Rebuild(observationDataRoot, observationBookmarksPath)
}

func closeObservationIndex() error {
	if observationRepo == nil {
		return nil
	}
	return observationRepo.Close()
}
