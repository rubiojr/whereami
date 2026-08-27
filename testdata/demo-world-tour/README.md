# Demo World Tour

Import this directory recursively to populate WhereAmI with a deterministic
multi-year travel history:

```text
testdata/demo-world-tour
```

The dataset contains 72 observations in 36 stays across 2021-2026. Each stay
has a morning and evening waypoint less than 100 meters apart, so the Timeline
groups them into one stop while retaining multiple observations. Transitions
between stays cover cities on six continents.

Useful demos:

- Open Timeline and select any year from 2021 through 2026.
- Step through chronological stops and watch the map move around the world.
- Filter the main map by year, month, or custom UTC date range.
- Search for city names such as `Kyoto`, `Cape Town`, `Reykjavik`, or `Quito`.
- Open waypoint cards to show timestamps, descriptions, and elevations.

The GPX files use only `<wpt>` elements because that is the observation format
accepted by WhereAmI's importer. Locations and timestamps are synthetic demo
data, not a record of an actual person's travel.
