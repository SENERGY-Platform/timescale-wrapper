/*
 *    Copyright 2020 InfAI (CC SES)
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
	"fmt"
	"math"
	"net/url"
	"sync"

	serving "github.com/SENERGY-Platform/analytics-serving/client"
	"github.com/SENERGY-Platform/gin-middleware/otelx"
	importRepo "github.com/SENERGY-Platform/import-repository/lib/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
)

// DefaultMaxConns is the pool size a configuration that names none gets.
//
// Twenty-five, and the number comes from a measurement rather than a habit.
// One request element is one query, so a batched read offers the pool as many
// queries as it has elements at once; with the pool at five, a reader sending
// 108 elements per call spent its whole latency in the queue - a year's
// window measured 1512 queries in 28.40 seconds of wall clock, which is
// exactly 5 connections at 94 ms per query, and no other term mattered.
//
// Not larger, because these connections point at a database several services
// share and this bound is per replica: what the database sees is this times
// however many of them are running.
const DefaultMaxConns = 25

// MaxConns is the configured pool size, narrowed to what pgxpool takes.
//
// Guarded rather than passed through: pgxpool's own pool panics below a size
// of one, so a configuration file that simply omits the field - which is
// every deployment until this one is rolled out - would take the process down
// at startup instead of falling back.
func MaxConns(config configuration.Config) int32 {
	if config.PostgresMaxConns <= 0 {
		return DefaultMaxConns
	}
	if config.PostgresMaxConns > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(config.PostgresMaxConns)
}

func NewWrapper(ctx context.Context, wg *sync.WaitGroup, config configuration.Config) (wrapper *Wrapper, err error) {
	servingClient := serving.New(config.ServingUrl)
	importRepoClient := importRepo.NewClient(config.ImportRepoUrl)
	baseDsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		url.QueryEscape(config.PostgresUser),
		url.QueryEscape(config.PostgresPw),
		config.PostgresHost,
		config.PostgresPort,
		url.PathEscape(config.PostgresDb),
	)
	poolConfig, err := pgxpool.ParseConfig(baseDsn)
	if err != nil {
		return nil, err
	}
	otelx.GinOpenTelemetry(ctx, "timescale-wrapper", "") // Initialize OpenTelemetry with default settings. Required for otelpgx
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()
	poolConfig.MaxConns = MaxConns(config)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		<-ctx.Done()
		pool.Close()
		wg.Done()
	}()
	return &Wrapper{config: config, pool: pool, servingClient: servingClient, importRepoClient: importRepoClient}, nil
}

func (wrapper *Wrapper) Migrate(ctx context.Context) error {
	tracer := otel.Tracer("timescale/db.go:Migrate")
	c, span := tracer.Start(ctx, "Migrate")
	defer span.End()
	return wrapper.removeOutdatedMaterializedRefreshJobs(c)
}
