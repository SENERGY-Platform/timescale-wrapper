/*
 *    Copyright 2026 InfAI (CC SES)
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *        http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 */

package timescale

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
)

// timeWeightedQuery renders the subquery of a time-weighted-mean-locf or time-weighted-mean-linear
// column: one row per bucket with its "time" and the time-weighted mean as "value".
//
// The mean of a bucket covers the whole bucket, not only the stretch between its first and last
// point. Between two points the value is held (LOCF) or interpolated (linear); after the newest
// point it is held until now. The points read are those inside the buckets plus the last point
// before the first bucket and the first point after the last bucket, so a bucket without a point
// of its own still gets the value of the stretch it lies in. A device that sends less often than
// once per bucket therefore yields a value for every bucket after its first point. The toolkit's
// average(time_weight(...)) only sees the points inside a bucket and returns nothing for a bucket
// with fewer than two of them.
//
// The buckets are generated rather than taken from the points, from times passed through
// time_bucket, so they join with the buckets of the element's other columns. The window is the element's time window, which a time-weighted column
// requires, and it may hold at most maxTimeWeightedBuckets buckets: every one of them is computed
// whether or not it holds a point.
//
// groupOrderLimit is the GROUP BY, ORDER BY and LIMIT clause the element's other columns use, see
// GenerateQueries.
func timeWeightedQuery(element model.QueriesRequestElement, groupType string, value string, table string, timezone string, groupOrderLimit string) (string, error) {
	conditions, err := getFilterConditions(element)
	if err != nil {
		return "", err
	}
	conditions = append(conditions, value+" IS NOT NULL")
	from := " FROM " + sqlliteral.QuoteIdentifier(table)
	where := " WHERE " + strings.Join(conditions, " AND ")
	tz := sqlliteral.QuoteString(timezone)
	width := sqlliteral.QuoteString(*element.GroupTime) + "::interval"

	window, err := timeWeightedWindow(element, time.Now())
	if err != nil {
		return "", err
	}
	var start, end string
	switch {
	case element.Time.Last != nil:
		start = "now() - interval " + sqlliteral.QuoteString(*element.Time.Last)
		end = "now()"
	case element.Time.Ahead != nil:
		start = "now()"
		end = "now() + interval " + sqlliteral.QuoteString(*element.Time.Ahead)
	default:
		start = sqlliteral.QuoteString(*element.Time.Start) + "::timestamptz"
		end = sqlliteral.QuoteString(*element.Time.End) + "::timestamptz"
	}
	if err := timeWeightedBucketsAllowed(window, *element.GroupTime); err != nil {
		return "", err
	}

	var mean string
	switch groupType {
	case "time-weighted-mean-locf":
		mean = "SUM(v0 * " + twDuration + ")"
	case "time-weighted-mean-linear":
		mean = "SUM(" + twDuration + " * (" + twLinearAt("a") + " + " + twLinearAt("e") + ") / 2)"
	default:
		return "", errors.New("unknown time-weighted group type " + groupType)
	}

	// A bucket is what time_bucket assigns a time to. Around a change of the offset to UTC,
	// time_bucket labels times in ways that do not follow the local clock: two stretches of real
	// time can share a bucket, and a bucket need not start at its own time. The buckets are
	// therefore built as pieces of real time with the bucket of their start: [c0, c1) lies wholly
	// in bucket "time". A piece starts wherever the bucket changes. The candidates for that are:
	//   - away from a change of the offset, the times where the local time reaches a multiple of
	//     the group time, at every real time that local time stands for;
	//   - every change of the offset, found by the day and then by the minute;
	//   - around a change, from an hour plus the group time (at most a day) before it to as long
	//     after it, every whole minute, or every whole second for a group time that is not a whole
	//     number of minutes.
	// Consecutive candidates with the same bucket are merged, so a piece ends where the next bucket
	// starts. The window holds the buckets of the pieces that overlap [time_bucket(start), end),
	// each with all of its pieces: a bucket split by a change of the offset counts as a whole even
	// where one of its pieces lies outside the window. The candidates reach a group time plus two
	// hours before the window and two group times past it, so every such piece starts and ends
	// where its bucket does. The end of the window is exclusive like in getWhereString: a piece
	// starting exactly there does not bring its bucket into the window. A window that does not end
	// after its start holds no bucket, like it holds no point for the other columns.
	offset := func(x string) string {
		return "((" + x + ") AT TIME ZONE " + tz + " - (" + x + ") AT TIME ZONE 'UTC')"
	}
	firstBucket := "time_bucket(" + width + ", " + start + ", " + tz + ")"
	label := "time_bucket(" + width + ", c, " + tz + ")"
	boundaries := "SELECT l AT TIME ZONE " + tz + " + d AS c" +
		" FROM generate_series(time_bucket(" + width + ", (" + start + ") AT TIME ZONE " + tz + " - " + width + " - interval '2 hours'), (" + end + ") AT TIME ZONE " + tz + " + 2 * " + width + ", " + width + ") l," +
		" (VALUES (interval '0'), (interval '-1 hour'), (interval '-30 minutes'), (interval '30 minutes'), (interval '1 hour')) v(d)" +
		" WHERE (l AT TIME ZONE " + tz + " + d) AT TIME ZONE " + tz + " = l"
	changes := "SELECT m FROM generate_series(date_trunc('day', " + firstBucket + " - " + width + " - interval '2 hours') - interval '1 day', " + end + " + 2 * " + width + " + interval '1 day', interval '1 day') day" +
		" CROSS JOIN LATERAL generate_series(day, day + interval '1 day', interval '1 minute') m" +
		" WHERE " + offset("day") + " <> " + offset("day + interval '1 day'") + " AND " + offset("m") + " <> " + offset("m - interval '1 minute'")
	reach := "(LEAST(" + width + ", interval '1 day') + interval '1 hour')"
	step := "CASE WHEN extract(epoch FROM " + width + ") % 60 = 0 THEN interval '1 minute' ELSE interval '1 second' END"
	aroundChanges := "SELECT m FROM t UNION ALL SELECT c FROM t CROSS JOIN LATERAL generate_series(date_trunc('minute', t.m - " + reach + "), t.m + " + reach + ", " + step + ") c"
	labelled := "SELECT c, " + label + " AS label, lag(" + label + ") OVER (ORDER BY c) AS previous" +
		" FROM (SELECT DISTINCT c FROM (" + boundaries + " UNION ALL " + aroundChanges + ") k) k"
	pieces := "SELECT \"time\", c0, c1 FROM (SELECT label AS \"time\", c AS c0, lead(c) OVER (ORDER BY c) AS c1" +
		" FROM (" + labelled + ") l WHERE previous IS DISTINCT FROM label) r WHERE c1 IS NOT NULL"
	buckets := "SELECT \"time\", c0, c1 FROM r WHERE \"time\" IN (SELECT \"time\" FROM r WHERE c1 > " + firstBucket + " AND c0 < " + end + " AND " + start + " < " + end + ")"
	point := "SELECT \"time\", " + value + " AS value" + from + where
	points := "(" + point + " AND \"time\" < (SELECT min(c0) FROM b) ORDER BY \"time\" DESC LIMIT 1)" +
		" UNION ALL (" + point + " AND \"time\" >= (SELECT min(c0) FROM b) AND \"time\" < (SELECT max(c1) FROM b))" +
		" UNION ALL (" + point + " AND \"time\" >= (SELECT max(c1) FROM b) ORDER BY \"time\" ASC LIMIT 1)"
	// t1 and v1 are the next point, t_end where the stretch of a point ends. Points sharing a time
	// count as one with their mean value.
	segments := "SELECT \"time\" AS t0, value AS v0, lead(\"time\") OVER w AS t1, lead(value) OVER w AS v1, COALESCE(lead(\"time\") OVER w, GREATEST(\"time\", now())) AS t_end FROM (SELECT \"time\", avg(value)::double precision AS value FROM p GROUP BY 1) p WINDOW w AS (ORDER BY \"time\")"
	// A stretch is cut where it crosses the start of a piece: the part from its point on lies in
	// the piece the point lies in, and every piece starting inside the stretch gets the part from
	// the piece's start on. Pieces and points are merged into one stream in time order, a piece
	// before a point where both meet. g counts the pieces up to a row and h the points, so each row
	// knows the piece and the point before it as the first row of its partition. Window functions
	// over one sorted stream keep the query at n log n whatever the planner estimates for the CTEs,
	// which it estimates at a handful of rows; a join it plans from such an estimate can turn into
	// a nested loop over every piece and every point.
	stream := "SELECT c0 AS k, 0 AS kind, \"time\" AS label, c1, NULL::timestamptz AS t0, NULL::double precision AS v0, NULL::timestamptz AS t1, NULL::double precision AS v1, NULL::timestamptz AS t_end FROM b" +
		" UNION ALL SELECT t0, 1, NULL, NULL, t0, v0, t1, v1, t_end FROM s"
	counted := "SELECT *, count(*) FILTER (WHERE kind = 0) OVER o AS g, count(*) FILTER (WHERE kind = 1) OVER o AS h FROM m WINDOW o AS (ORDER BY k, kind)"
	before := func(column string, partition string) string {
		return "first_value(" + column + ") OVER (PARTITION BY " + partition + " ORDER BY k, kind) AS " + partition + "_" + column
	}
	known := "SELECT k, kind, g, h, label, c1, t0, v0, t1, v1, t_end, " + before("label", "g") + ", " + before("c1", "g") + ", " +
		before("t0", "h") + ", " + before("v0", "h") + ", " + before("t1", "h") + ", " + before("v1", "h") + ", " + before("t_end", "h") + " FROM n"
	// a and e are where a stretch starts and ends within a piece
	overlaps := "SELECT g_label AS \"time\", t0, v0, t1, v1, t0 AS a, LEAST(t_end, g_c1) AS e FROM q WHERE kind = 1 AND g > 0 AND t0 < g_c1" +
		" UNION ALL SELECT label, h_t0, h_v0, h_t1, h_v1, k, LEAST(h_t_end, c1) FROM q WHERE kind = 0 AND h > 0 AND h_t_end > k"

	return "WITH t AS (" + changes + "), r AS (" + pieces + "), b AS (" + buckets + "), p AS (" + points + "), s AS (" + segments + "), m AS (" + stream + "), n AS (" + counted + "), q AS (" + known + "), o AS (" + overlaps + ")" +
		" SELECT \"time\", (" + mean + " / NULLIF(SUM(" + twDuration + "), 0))::double precision AS value FROM o" + groupOrderLimit, nil
}

// twDuration is the length in seconds of a stretch within a bucket, see timeWeightedQuery.
const twDuration = "extract(epoch FROM e - a)"

// twLinearAt renders the value interpolated between a point and its successor at the time x. A
// point without a successor, or with one at the same time, holds its value.
func twLinearAt(x string) string {
	return "CASE WHEN t1 IS NULL OR t1 = t0 THEN v0 ELSE v0 + (v1 - v0) * extract(epoch FROM " + x + " - t0) / extract(epoch FROM t1 - t0) END"
}

// maxTimeWeightedBuckets bounds the buckets of a time-weighted column, see timeWeightedQuery.
const maxTimeWeightedBuckets = 100000

// maxTimeWeightedDays bounds the days in the window of a time-weighted column: the changes of the
// offset to UTC are searched day by day whatever the group time, see timeWeightedQuery.
const maxTimeWeightedDays = 100000

// timeWeightedWindow returns the start and end of the element's time window as of now.
func timeWeightedWindow(element model.QueriesRequestElement, now time.Time) (window [2]time.Time, err error) {
	if element.Time == nil {
		return window, errors.Join(errors.New("time-weighted group types need a time window"), model.ErrBadRequest)
	}
	switch {
	case element.Time.Last != nil:
		start, err := model.AddTimeInterval(now, *element.Time.Last, -1)
		return [2]time.Time{start, now}, errors.Join(err)
	case element.Time.Ahead != nil:
		end, err := model.AddTimeInterval(now, *element.Time.Ahead, 1)
		return [2]time.Time{now, end}, errors.Join(err)
	case element.Time.Start != nil && element.Time.End != nil:
		if window[0], err = time.Parse(time.RFC3339, *element.Time.Start); err != nil {
			return window, errors.Join(err, model.ErrBadRequest)
		}
		if window[1], err = time.Parse(time.RFC3339, *element.Time.End); err != nil {
			return window, errors.Join(err, model.ErrBadRequest)
		}
		return window, nil
	default:
		return window, errors.Join(errors.New("incomplete time window"), model.ErrBadRequest)
	}
}

// timeWeightedBucketsAllowed rejects a window holding more than maxTimeWeightedBuckets buckets of
// groupTime or more than maxTimeWeightedDays days. Calendar units count with the length they have
// at the start of the window.
func timeWeightedBucketsAllowed(window [2]time.Time, groupTime string) error {
	bucketEnd, err := model.AddTimeInterval(window[0], groupTime, 1)
	if err != nil {
		return errors.Join(err, model.ErrBadRequest)
	}
	width := bucketEnd.Sub(window[0])
	if width <= 0 {
		return errors.Join(fmt.Errorf("invalid group time %q", groupTime), model.ErrBadRequest)
	}
	// the buckets are searched on a grid of whole seconds, see timeWeightedQuery
	if width%time.Second != 0 {
		return errors.Join(fmt.Errorf("time-weighted group types need a group time of whole seconds, got %q", groupTime), model.ErrBadRequest)
	}
	if window[1].Sub(window[0])/width > maxTimeWeightedBuckets {
		return errors.Join(fmt.Errorf("time-weighted group types allow at most %d buckets, choose a larger group time or a shorter time window", maxTimeWeightedBuckets), model.ErrBadRequest)
	}
	if window[1].Sub(window[0]) > maxTimeWeightedDays*24*time.Hour {
		return errors.Join(fmt.Errorf("time-weighted group types allow a time window of at most %d days", maxTimeWeightedDays), model.ErrBadRequest)
	}
	return nil
}
