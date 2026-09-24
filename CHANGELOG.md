# Changelog

## Unreleased

### Changed

- Extracted the nearest-station lookup used by `CreateFiltersForImport` into a new leaf package
  `pkg/locate` (`ViewName`, `LocationsQuery`, `Candidate`, `Nearest`, `ValueLiteral`), so a caller
  outside this module can reuse it without pulling in `pkg/timescale`'s pgxpool, otel,
  import-repository and analytics-serving dependencies. `pkg/timescale` now calls `pkg/locate`
  instead of keeping a second copy.
- `CreateFiltersForImport` picks a tie between two equally distant stations deterministically, by
  comparing the stations' identifiers. Previously the tie went to whichever station a `sort.Slice`
  happened to place first, which is not guaranteed to be stable, so the choice between two
  equally distant stations could differ between otherwise identical calls.

### Removed

- The `github.com/umahmood/haversine` dependency. The great-circle distance calculation is now
  inline in `pkg/locate`, using the same formula and Earth radius.
