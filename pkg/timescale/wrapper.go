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
	poolConfig.MaxConns = 5

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
