// Package reports aggregates recorded GPX observations into administrative places.
package reports

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/rubiojr/whereami/internal/admingeo"
	"github.com/rubiojr/whereami/internal/observations"
)

const (
	maxReportPlaces         = 100000
	maxReportTimelineStops  = 100000
	journeySeparationMeters = 100.0
	earthRadiusMeters       = 6371008.8
)

// DatasetMetadata identifies the immutable administrative dataset used by a report.
type DatasetMetadata struct {
	DatasetVersion admingeo.DatasetVersion `json:"dataset_version"`
	SourceVersion  string                  `json:"source_version"`
	Attribution    string                  `json:"attribution"`
	License        string                  `json:"license"`
}

// Period describes the exact UTC half-open interval used for the report.
type Period struct {
	StartUTC  string `json:"start_utc"`
	EndUTC    string `json:"end_utc_exclusive"`
	TimeZone  string `json:"time_zone"`
	Semantics string `json:"semantics"`
}

// Summary describes report coverage without exposing observation coordinates.
type Summary struct {
	RecordedObservations   int64 `json:"recorded_observations"`
	ResolvedObservations   int64 `json:"resolved_observations"`
	UnresolvedObservations int64 `json:"unresolved_observations"`
	InvalidCoordinates     int64 `json:"invalid_coordinates"`
	IndexedValidTimes      int64 `json:"indexed_valid_times"`
	IndexedMissingTimes    int64 `json:"indexed_missing_times"`
	IndexedInvalidTimes    int64 `json:"indexed_invalid_times"`
}

// Place is one administrative hierarchy containing recorded observations.
type Place struct {
	Country              string `json:"country,omitempty"`
	CountryID            string `json:"country_id,omitempty"`
	Region               string `json:"region,omitempty"`
	RegionID             string `json:"region_id,omitempty"`
	County               string `json:"county,omitempty"`
	CountyID             string `json:"county_id,omitempty"`
	LocalAdmin           string `json:"local_admin,omitempty"`
	LocalAdminID         string `json:"local_admin_id,omitempty"`
	Locality             string `json:"locality,omitempty"`
	LocalityID           string `json:"locality_id,omitempty"`
	RecordedObservations int64  `json:"recorded_observations"`
	RecordedDays         int    `json:"recorded_days"`
	SourceFiles          int    `json:"source_files"`
	FirstObservationUTC  string `json:"first_observation_utc"`
	LastObservationUTC   string `json:"last_observation_utc"`
}

// TimelineStop is one chronological journey position separated from the prior
// anchor by at least the report's distance threshold. Completed stops retain
// their anchor coordinate; the final stop uses the latest recorded coordinate.
// DateUTC is the stop's latest UTC date. Source paths are omitted.
type TimelineStop struct {
	DateUTC              string  `json:"date_utc"`
	Latitude             float64 `json:"latitude"`
	Longitude            float64 `json:"longitude"`
	Country              string  `json:"country,omitempty"`
	CountryID            string  `json:"country_id,omitempty"`
	Region               string  `json:"region,omitempty"`
	RegionID             string  `json:"region_id,omitempty"`
	County               string  `json:"county,omitempty"`
	CountyID             string  `json:"county_id,omitempty"`
	LocalAdmin           string  `json:"local_admin,omitempty"`
	LocalAdminID         string  `json:"local_admin_id,omitempty"`
	Locality             string  `json:"locality,omitempty"`
	LocalityID           string  `json:"locality_id,omitempty"`
	RecordedObservations int64   `json:"recorded_observations"`
	FirstObservationUTC  string  `json:"first_observation_utc"`
	LastObservationUTC   string  `json:"last_observation_utc"`
}

// Report contains aggregate place details and a local coordinate-bearing journey.
type Report struct {
	Period                  Period          `json:"period"`
	ObservationRevision     string          `json:"observation_revision"`
	Dataset                 DatasetMetadata `json:"dataset"`
	Summary                 Summary         `json:"summary"`
	JourneySeparationMeters float64         `json:"journey_separation_meters"`
	JourneyTruncated        bool            `json:"journey_truncated,omitempty"`
	Places                  []Place         `json:"places"`
	Timeline                []TimelineStop  `json:"timeline"`
}

type aggregate struct {
	place   Place
	days    map[string]struct{}
	sources map[string]struct{}
	first   time.Time
	last    time.Time
}

type timelineAggregate struct {
	stop            TimelineStop
	anchorLatitude  float64
	anchorLongitude float64
	first           time.Time
	last            time.Time
	latestLatitude  float64
	latestLongitude float64
	latestPath      admingeo.AdminPath
}

type accumulator struct {
	ctx           context.Context
	resolver      admingeo.Resolver
	report        *Report
	aggregates    map[admingeo.AdminPath]*aggregate
	timeline      []*timelineAggregate
	timelineLimit int
	progress      func(int64)
}

// Generate resolves and aggregates observations in [start,end). Progress is
// called periodically with the number of observations scanned.
func Generate(ctx context.Context, snapshot *observations.Snapshot, resolver admingeo.Resolver, dataset DatasetMetadata, start, end time.Time, progress func(int64)) (Report, error) {
	if ctx == nil {
		return Report{}, errors.New("report context is nil")
	}
	if snapshot == nil || resolver == nil {
		return Report{}, errors.New("report data source is unavailable")
	}
	start = start.UTC()
	end = end.UTC()
	if !end.After(start) {
		return Report{}, errors.New("report end must follow start")
	}
	if resolver.Version() != dataset.DatasetVersion {
		return Report{}, fmt.Errorf("resolver dataset version %q does not match report metadata %q", resolver.Version(), dataset.DatasetVersion)
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}

	timeCounts, err := snapshot.TimeStatusCounts()
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Period: Period{
			StartUTC:  start.Format(time.RFC3339Nano),
			EndUTC:    end.Format(time.RFC3339Nano),
			TimeZone:  "UTC",
			Semantics: "recorded observations in UTC [start,end)",
		},
		ObservationRevision:     snapshot.Revision(),
		Dataset:                 dataset,
		JourneySeparationMeters: journeySeparationMeters,
		Summary: Summary{
			IndexedValidTimes:   timeCounts.Valid,
			IndexedMissingTimes: timeCounts.Missing,
			IndexedInvalidTimes: timeCounts.Invalid,
		},
		Places:   make([]Place, 0),
		Timeline: make([]TimelineStop, 0),
	}
	accumulator := accumulator{
		ctx:           ctx,
		resolver:      resolver,
		report:        &report,
		aggregates:    make(map[admingeo.AdminPath]*aggregate),
		timeline:      make([]*timelineAggregate, 0),
		timelineLimit: maxReportTimelineStops,
		progress:      progress,
	}
	err = snapshot.ScanPeriod(start, end, accumulator.record)
	if err != nil {
		return Report{}, err
	}
	if progress != nil {
		progress(report.Summary.RecordedObservations)
	}
	report.Places = accumulator.places()
	report.Timeline = accumulator.timelineStops()
	return report, nil
}

func (a *accumulator) record(observation observations.Observation) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.report.Summary.RecordedObservations++
	if a.progress != nil && a.report.Summary.RecordedObservations%1024 == 0 {
		a.progress(a.report.Summary.RecordedObservations)
	}
	if !observation.CoordinatesValid {
		a.report.Summary.InvalidCoordinates++
		return nil
	}
	path, err := a.resolver.Resolve(a.ctx, admingeo.Coordinate{Latitude: observation.Latitude, Longitude: observation.Longitude})
	if err != nil {
		return fmt.Errorf("resolve recorded observation %d: %w", a.report.Summary.RecordedObservations, err)
	}
	if err := a.recordTimeline(path, observation); err != nil {
		return err
	}
	if path == (admingeo.AdminPath{}) {
		a.report.Summary.UnresolvedObservations++
		return nil
	}
	a.report.Summary.ResolvedObservations++
	item := a.aggregates[path]
	if item == nil {
		if len(a.aggregates) >= maxReportPlaces {
			return fmt.Errorf("report exceeds %d distinct administrative places", maxReportPlaces)
		}
		item = &aggregate{
			place: Place{
				Country: path.Country, CountryID: path.CountryID,
				Region: path.Region, RegionID: path.RegionID,
				County: path.County, CountyID: path.CountyID,
				LocalAdmin: path.LocalAdmin, LocalAdminID: path.LocalAdminID,
				Locality: path.Locality, LocalityID: path.LocalityID,
			},
			days:    make(map[string]struct{}),
			sources: make(map[string]struct{}),
			first:   observation.Time,
			last:    observation.Time,
		}
		a.aggregates[path] = item
	}
	day := observation.Time.UTC().Format(time.DateOnly)
	item.place.RecordedObservations++
	item.days[day] = struct{}{}
	item.sources[observation.Source] = struct{}{}
	if observation.Time.Before(item.first) {
		item.first = observation.Time
	}
	if observation.Time.After(item.last) {
		item.last = observation.Time
	}
	return nil
}

func (a *accumulator) recordTimeline(path admingeo.AdminPath, observation observations.Observation) error {
	var item *timelineAggregate
	if len(a.timeline) > 0 {
		item = a.timeline[len(a.timeline)-1]
	}
	if item == nil || distanceMeters(item.anchorLatitude, item.anchorLongitude, observation.Latitude, observation.Longitude) >= journeySeparationMeters {
		if len(a.timeline) >= a.timelineLimit {
			a.report.JourneyTruncated = true
		} else {
			item = &timelineAggregate{
				stop: TimelineStop{
					DateUTC:  observation.Time.UTC().Format(time.DateOnly),
					Latitude: observation.Latitude, Longitude: observation.Longitude,
				},
				anchorLatitude:  observation.Latitude,
				anchorLongitude: observation.Longitude,
				first:           observation.Time,
				last:            observation.Time,
				latestLatitude:  observation.Latitude,
				latestLongitude: observation.Longitude,
				latestPath:      path,
			}
			setTimelinePath(&item.stop, path)
			a.timeline = append(a.timeline, item)
		}
	}
	if item == nil {
		return nil
	}
	item.stop.RecordedObservations++
	item.latestLatitude = observation.Latitude
	item.latestLongitude = observation.Longitude
	item.latestPath = path
	if observation.Time.Before(item.first) {
		item.first = observation.Time
	}
	if observation.Time.After(item.last) {
		item.last = observation.Time
	}
	return nil
}

func setTimelinePath(stop *TimelineStop, path admingeo.AdminPath) {
	stop.Country, stop.CountryID = path.Country, path.CountryID
	stop.Region, stop.RegionID = path.Region, path.RegionID
	stop.County, stop.CountyID = path.County, path.CountyID
	stop.LocalAdmin, stop.LocalAdminID = path.LocalAdmin, path.LocalAdminID
	stop.Locality, stop.LocalityID = path.Locality, path.LocalityID
}

func distanceMeters(firstLatitude, firstLongitude, secondLatitude, secondLongitude float64) float64 {
	firstLat := firstLatitude * math.Pi / 180
	secondLat := secondLatitude * math.Pi / 180
	latitudeDelta := (secondLatitude - firstLatitude) * math.Pi / 180
	longitudeDelta := (secondLongitude - firstLongitude) * math.Pi / 180
	a := math.Sin(latitudeDelta/2)*math.Sin(latitudeDelta/2) +
		math.Cos(firstLat)*math.Cos(secondLat)*math.Sin(longitudeDelta/2)*math.Sin(longitudeDelta/2)
	a = math.Max(0, math.Min(1, a))
	return earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func (a *accumulator) places() []Place {
	places := make([]Place, 0, len(a.aggregates))
	for _, item := range a.aggregates {
		item.place.RecordedDays = len(item.days)
		item.place.SourceFiles = len(item.sources)
		item.place.FirstObservationUTC = item.first.UTC().Format(time.RFC3339Nano)
		item.place.LastObservationUTC = item.last.UTC().Format(time.RFC3339Nano)
		places = append(places, item.place)
	}
	sort.Slice(places, func(i, j int) bool {
		left, right := places[i], places[j]
		if left.RecordedObservations != right.RecordedObservations {
			return left.RecordedObservations > right.RecordedObservations
		}
		leftKey := left.Country + "\x00" + left.CountryID + "\x00" + left.Region + "\x00" + left.RegionID + "\x00" + left.County + "\x00" + left.CountyID + "\x00" + left.LocalAdmin + "\x00" + left.LocalAdminID + "\x00" + left.Locality + "\x00" + left.LocalityID
		rightKey := right.Country + "\x00" + right.CountryID + "\x00" + right.Region + "\x00" + right.RegionID + "\x00" + right.County + "\x00" + right.CountyID + "\x00" + right.LocalAdmin + "\x00" + right.LocalAdminID + "\x00" + right.Locality + "\x00" + right.LocalityID
		return leftKey < rightKey
	})
	return places
}

func (a *accumulator) timelineStops() []TimelineStop {
	stops := make([]TimelineStop, 0, len(a.timeline))
	for index, item := range a.timeline {
		item.stop.DateUTC = item.last.UTC().Format(time.DateOnly)
		if index == len(a.timeline)-1 {
			item.stop.Latitude = item.latestLatitude
			item.stop.Longitude = item.latestLongitude
			setTimelinePath(&item.stop, item.latestPath)
		}
		item.stop.FirstObservationUTC = item.first.UTC().Format(time.RFC3339Nano)
		item.stop.LastObservationUTC = item.last.UTC().Format(time.RFC3339Nano)
		stops = append(stops, item.stop)
	}
	return stops
}
