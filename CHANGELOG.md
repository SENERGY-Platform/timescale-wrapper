# Changelog

## Unreleased

### Added

- A second connection for reads: `POSTGRES_READ_HOST`, optionally with `POSTGRES_READ_USER` and
  `POSTGRES_READ_PW`, which fall back to `POSTGRES_USER` and `POSTGRES_PW`. Port, database and
  `POSTGRES_MAX_CONNS` are shared, each pool gets that size. Only the refresh-job cleanup at
  startup and the materialized view, procedure and refresh job of the locate import in
  `POST /queries/v2` use the `POSTGRES_*` connection; every other query reads through the new
  one, so it can point at a read-only replica. Unset, reads stay on the `POSTGRES_*` connection
  as before.
- When the locate import finds no materialized view on the read connection, it asks the write
  connection before creating one, and reads the view it created from there too. A replica may
  not have replayed a view another instance has just created, and creating it again would fail
  with `duplicate_table`.

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
