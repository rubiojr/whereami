// Package admingeo defines application-owned administrative geocoding types.
package admingeo

import "context"

// Coordinate is a WGS84 coordinate in degrees.
type Coordinate struct {
	Longitude float64
	Latitude  float64
}

// AdminPath is the administrative hierarchy containing a coordinate.
type AdminPath struct {
	Country      string
	CountryID    string
	Region       string
	RegionID     string
	County       string
	CountyID     string
	LocalAdmin   string
	LocalAdminID string
	Locality     string
	LocalityID   string
}

// DatasetVersion identifies the immutable dataset used by a Resolver.
type DatasetVersion string

// Resolver maps WGS84 coordinates to administrative hierarchies.
type Resolver interface {
	Resolve(context.Context, Coordinate) (AdminPath, error)
	Version() DatasetVersion
	Close() error
}
