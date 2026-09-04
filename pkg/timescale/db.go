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
	"context"
	"math"
	"sync"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tracing"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (wrapper *Wrapper) ExecuteQueries(ctx context.Context, queries []string) (res [][][]interface{}, err error) {
	res = make([][][]interface{}, len(queries))
	// otelpgx traces every query and every pool acquire, which is one span pair
	// per query of the batch. The count goes on the span of the calling phase
	// instead.
	trace.SpanFromContext(ctx).SetAttributes(attribute.Int("db.queries", len(queries)))
	ctx = tracing.SuppressChildren(ctx, wrapper.config.DetailedTracing)
	wg := sync.WaitGroup{} // handle multiple queries in parallel
	mux := sync.Mutex{}    // prevent overwriting error in case of multiple queries
	for i, query := range queries {
		wg.Add(1)
		i := i         // make thread safe
		query := query // make thread safe
		go func() {
			if wrapper.config.Debug {
				log.Logger.DebugContext(ctx, "Query", "index", i, "query", query)
			}
			resS, errS := wrapper.ExecuteQuery(ctx, query)
			if errS != nil { // Prevents overwriting with nil
				mux.Lock()
				err = errS
				mux.Unlock()
			} else {
				mux.Lock()
				res[i] = resS
				mux.Unlock()
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return
}

func (wrapper *Wrapper) ExecuteQuery(ctx context.Context, query string) (res [][]interface{}, err error) {
	rows, err := wrapper.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res = [][]interface{}{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, v := range values {
			switch numeric := v.(type) {
			case pgtype.Numeric:
				if numeric.Valid {
					values[i] = int64(float64(numeric.Int.Int64()) * math.Pow10(int(numeric.Exp)))
				} else {
					values[i] = nil
				}
			case *pgtype.Numeric:
				if numeric != nil && numeric.Valid {
					values[i] = int64(float64(numeric.Int.Int64()) * math.Pow10(int(numeric.Exp)))
				} else {
					values[i] = nil
				}
			}
		}
		res = append(res, values)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(res) == 0 { // no results --> append nil for each requested field
		res = append(res, make([]interface{}, len(rows.FieldDescriptions())))
	}
	return res, nil
}
