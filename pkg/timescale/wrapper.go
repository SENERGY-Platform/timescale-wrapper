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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx"
	"sync"
)

func NewWrapper(ctx context.Context, wg *sync.WaitGroup, config configuration.Config) (wrapper *Wrapper, err error) {
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     config.PostgresHost,
			Port:     config.PostgresPort,
			Database: config.PostgresDb,
			User:     config.PostgresUser,
			Password: config.PostgresPw,
		},
		MaxConnections: 50,
		AcquireTimeout: 0,
	})
	if err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		<-ctx.Done()
		pool.Close()
		wg.Done()
	}()
	return &Wrapper{config: config, pool: pool}, nil
}
