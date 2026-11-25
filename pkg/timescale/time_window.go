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

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
)

// sqlBounds renders the start and end of the time window as SQL expressions: now() based for last and
// ahead, timestamptz literals for start and end. It is the SQL counterpart of
// model.QueriesRequestElementTime.Bounds, which gives the same window as Go times. It lives here and not in pkg/model because rendering needs
// pkg/sqlliteral, whose pgx dependency pkg/model's other importers should not get. The time has to be
// valid (QueriesRequestElementTime.Valid), start and end are empty for one that has neither
// last, ahead nor both start and end.
func sqlBounds(elementTime *model.QueriesRequestElementTime) (start, end string) {
	start, end, _ = sqlWindow(elementTime, "::timestamptz")
	return start, end
}

// sqlWindow renders start and end like sqlBounds, with cast appended to the literals of an absolute
// window, and reports whether the window has an end of its own: last has none that a row condition
// needs, it reads up to now.
func sqlWindow(elementTime *model.QueriesRequestElementTime, cast string) (start, end string, bounded bool) {
	switch {
	case elementTime.Last != nil:
		return "now() - interval " + sqlliteral.QuoteString(*elementTime.Last), "now()", false
	case elementTime.Ahead != nil:
		return "now()", "now() + interval " + sqlliteral.QuoteString(*elementTime.Ahead), true
	case elementTime.Start != nil && elementTime.End != nil:
		return sqlliteral.QuoteString(*elementTime.Start) + cast, sqlliteral.QuoteString(*elementTime.End) + cast, true
	default:
		return "", "", false
	}
}

// timeCondition renders the condition on "time" that selects the rows of the time window: the start
// is exclusive, and so is the end of ahead and of start and end. last reads up to the newest row.
// A window with neither last, ahead nor both start and end is rejected.
func timeCondition(elementTime *model.QueriesRequestElementTime) (string, error) {
	return timeConditionFrom(elementTime, "")
}

// timeConditionFrom renders the condition like timeCondition, with the SQL expression start as the
// start of the window instead of the window's own if it is not empty. That start is inclusive: it is
// the start of a bucket, see differenceWindowFor, and a reading on it belongs to the bucket.
func timeConditionFrom(elementTime *model.QueriesRequestElementTime, start string) (string, error) {
	ownStart, end, bounded := sqlWindow(elementTime, "")
	if ownStart == "" {
		return "", errors.Join(errors.New("incomplete time window"), model.ErrBadRequest)
	}
	comparison := " >= "
	if start == "" {
		start, comparison = ownStart, " > "
	}
	condition := "\"time\"" + comparison + start
	if bounded {
		condition += " AND \"time\" < " + end
	}
	return condition, nil
}

// differenceWindow is the time window a grouped element with a difference-* column reads: it starts
// one bucket early, so that the first requested bucket has a predecessor for lag() to subtract. The
// end stays where it is.
type differenceWindow struct {
	// start is the SQL expression of the start of the bucket before the first requested one.
	start string
	// firstBucket is the SQL expression of the first requested bucket: the bucket the requested
	// start lies in. Buckets before it hold the predecessor and are dropped after lag().
	firstBucket string
}

// differenceWindowFor returns the window of an element query, nil if it has no difference-* column
// or no time window.
//
// The start moves back to the start of the bucket before the one the requested start lies in, not
// by a group time from the requested start: a start inside a bucket would otherwise leave the
// predecessor with only part of its rows, which changes every aggregate but last and, for last,
// still needs data in that part. The predecessor starts where its bucket does, which is
// the bucket of the instant just before time_bucket(start). That is not time_bucket(start) minus
// the group time on either clock: on the absolute time, in the session's zone, a day on the days the
// zone changes its offset is not a bucket wide, and on the local time the result can be a local time
// that does not exist, which postgres moves forward and thereby into the first bucket. Asking
// time_bucket for the bucket of the instant before follows its own rules in both cases. Both
// expressions are SQL and stay SQL for a window of start and end: a literal computed in Go would
// need the zone's rules in Go too. The start is inclusive, a reading on the start of a bucket
// belongs to it; the rows of the continuous aggregate of a bucket are labelled with its start.
func differenceWindowFor(q elementQuery) *differenceWindow {
	if q.element.Time == nil || q.element.GroupTime == nil {
		return nil
	}
	hasDifference := false
	for _, column := range q.columns {
		hasDifference = hasDifference || column.agg.Kind == model.Difference
	}
	start, _, _ := sqlWindow(q.element.Time, "::timestamptz")
	if !hasDifference || start == "" {
		return nil
	}
	width := sqlliteral.QuoteString(*q.element.GroupTime)
	tz := sqlliteral.QuoteString(q.timezone)
	first := "time_bucket(" + width + ", " + start + ", " + tz + ")"
	return &differenceWindow{
		start:       "time_bucket(" + width + ", " + first + " - interval '1 microsecond', " + tz + ")",
		firstBucket: first,
	}
}
