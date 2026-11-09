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
	"sync"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tracing"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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

// ExecuteQuery runs query on the read connection.
func (wrapper *Wrapper) ExecuteQuery(ctx context.Context, query string) (res [][]interface{}, err error) {
	return executeQuery(ctx, wrapper.readPool, query)
}

func executeQuery(ctx context.Context, pool *pgxpool.Pool, query string) (res [][]interface{}, err error) {
	rows, err := pool.Query(ctx, query)
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
				values[i] = numericValue(numeric)
			case *pgtype.Numeric:
				if numeric != nil {
					values[i] = numericValue(*numeric)
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

// numericValue turns a numeric result into a value encoding/json can write. Postgres returns
// numeric for sum and avg over bigint columns and for math on them, so an integral result stays
// an int64 and any other becomes a float64 rather than being cut to an integer. NaN, the
// infinities and values beyond the range of a float64 have no JSON representation and become
// nil.
func numericValue(n pgtype.Numeric) interface{} {
	if !n.Valid || n.NaN || n.InfinityModifier != pgtype.Finite {
		return nil
	}
	if n.Int == nil {
		return int64(0)
	}
	if i, err := n.Int64Value(); err == nil {
		return i.Int64
	}
	f, err := n.Float64Value()
	if err != nil {
		return nil
	}
	return f.Float64
}
