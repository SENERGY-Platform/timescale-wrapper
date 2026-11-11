# Paths into lists of variable length

A content variable of type list whose first sub variable is named `*` has a variable length.
[timescale-tableworker](https://github.com/SENERGY-Platform/timescale-tableworker) stores such a
list as one `JSONB` column named after the list without the `*`, for example `root.List`. Since
v0.5.0 the wrapper reads values inside that column by the same dotted path the rest of the
platform uses, e.g. `root.List.0.Value`.

## Scope

Applies to column and filter names of `/queries`, `/queries/v2`, `/last-values` and the download
endpoints, for elements that name a device and a service, and for the columns `/queries/v2`
builds from device group or location criteria. An element with an `exportId` has no service
description to resolve against; a `*` in its names is rejected.

Not this: lists of fixed length. Their elements are ordinary columns, one per leaf
(`root.List.0.Value`), and behave as before. A name with a list index is resolved against the
service either way, so both kinds look the same in a request; only the device type tells them
apart.

## Resolving a name

`meta.ResolveColumn` walks the service output's content variables along the path. The first
segment is the name of the output's root variable. Structures and lists of fixed length are
entered by the name of the sub variable. At the first list of variable length the path up to and
including that list is the column; every later segment addresses the JSON value:

| Segment after a list of variable length | Meaning |
|---|---|
| `0`, `1`, ... | element at that index |
| `*` | every element |
| a field name | field of a structure inside the list |

Nested lists of variable length continue the same JSON path. A negative index, a field name on a
list, a field name made of digits only, or a path that is not in the service description is
rejected with 400. A path without any list of variable length on it is a plain column and is
queried as before.

Only names with an index or a `*` segment are resolved, so only they cost a service lookup
(`GetService`, cached in memcached for five minutes). When the service cannot be read, names
without `*` are used as plain columns, because an index may as well address a list of fixed
length; a name with `*` then fails with 500.

## Values and their types

A path to a single value is read with `jsonb_path_query_first` and cast to the type the
tableworker gives a column of the same content variable: `bigint`, `double precision`, `boolean`
or `text`; a structure or list stays `jsonb`. The value then behaves like a column of that type:
every `groupType`, filters, `math` and characteristic conversion apply. An index past the end of
the list or a missing field gives `null`. A value that does not match its declared type, such as
`1.5` in an integer field, fails the cast and the whole request with 400.

## All elements: `*`

Without a `groupType`, a path with `*` returns one array per message, from
`jsonb_path_query_array`: flat across nested `*`, missing fields left out, JSON `null` elements
kept.

With a `groupType`, the column's subquery joins the elements as rows
(`LEFT JOIN LATERAL jsonb_path_query(...) WITH ORDINALITY ... ON TRUE`), so the aggregation runs
over every element of every message in the bucket. `first` and `last`, also inside
`difference-*`, take the element ordered by time and then by its position in the list.

Rejected with 400: `*` in a filter, `*` with `time-weighted-*`, and `*` without a `groupType`
together with `math` or a `targetCharacteristicId`, since both would apply to an array.

## Cache and database answer alike

A request the last-value cache can answer reads the same path from the cached message
(`getDeepEntry`): an index past the end gives `null`, `*` collects the matches flat and keeps JSON
`null`, like the database. Values from the cache are not cast; a JSON number arrives as a float.

## Rejected alternatives

- **An operator syntax such as `root.List->0->Value`.** It needs no service lookup, since the
  column boundary is in the name. But it is a second syntax next to the dotted path the
  marshaller, the cache and the last value store on the gateway already use, every client would
  have to learn it, and without the content variable there is no type to cast to, so aggregations
  would need another parameter.
- **`CROSS JOIN LATERAL` for `*`.** A message with an empty or `null` list produces no row, so its
  bucket disappears from that column only. The `FULL OUTER JOIN` that combines the columns then
  gave rows without a time.
- **TimescaleDB's `first(value, time)` and `last(value, time)` over the elements.** All elements of
  one message share its time, so which of them wins is not defined. `array_agg` ordered by time and
  position, filtered to non-null values, is.
