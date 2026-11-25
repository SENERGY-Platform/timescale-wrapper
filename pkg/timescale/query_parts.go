/*
 *    Copyright 2021 InfAI (CC SES)
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
	"strconv"
	"strings"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// bucketTime is the bucket of a row of the FULL OUTER JOIN over the first n column subqueries.
// The subqueries can hold different buckets: a "last" column reads its rows in descending order
// and the others in ascending order, so with a limit they cover different ends of the window. A
// bucket only a later column has would otherwise get no time, and the next column, joined on
// sub0.time, would not join to it and land on a row of its own.
func bucketTime(n int) string {
	if n <= 1 {
		return "sub0.time"
	}
	subs := make([]string, n)
	for i := range subs {
		subs[i] = "sub" + strconv.Itoa(i) + ".time"
	}
	return "COALESCE(" + strings.Join(subs, ", ") + ")"
}

// subAlias is the name of the subquery of column i in the FROM clause of a grouped query.
func subAlias(i int) string {
	return "sub" + strconv.Itoa(i)
}

// orderLimit is the GROUP BY, ORDER BY and LIMIT clause of a query.
type orderLimit struct {
	// groupBy groups by the first column, the time.
	groupBy bool
	// index is the position of the column to order by, counted from 0; -1 for no ORDER BY.
	index     int
	direction model.Direction
	// limit is nil for no LIMIT.
	limit *int
}

// defaultOrderLimit is the clause the element asks for: its order column and direction, descending
// if it gives none, and its limit.
func defaultOrderLimit(element model.QueriesRequestElement) orderLimit {
	result := orderLimit{index: -1, direction: model.Desc, limit: element.Limit}
	if element.OrderColumnIndex != nil {
		result.index = *element.OrderColumnIndex
	}
	if element.OrderDirection != nil {
		result.direction = *element.OrderDirection
	}
	return result
}

func (o orderLimit) String() string {
	query := ""
	if o.groupBy {
		query += " GROUP BY 1"
	}
	if o.index != -1 {
		query += " ORDER BY " + strconv.Itoa(o.index+1) + " " + strings.ToUpper(string(o.direction))
	}
	if o.limit != nil {
		query += " LIMIT " + strconv.Itoa(*o.limit)
	}
	return query
}
