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
func timeCondition(elementTime *model.QueriesRequestElementTime) string {
	start, end, bounded := sqlWindow(elementTime, "")
	condition := "\"time\" > " + start
	if bounded {
		condition += " AND \"time\" < " + end
	}
	return condition
}
