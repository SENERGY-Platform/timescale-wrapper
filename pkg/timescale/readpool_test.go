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
	"context"
	"sync"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
)

// newTestWrapper builds a wrapper through NewWrapper without a database: pgxpool connects on first
// use, so the pools can be inspected as long as nothing queries them.
func newTestWrapper(t *testing.T, config configuration.Config) *Wrapper {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	wrapper, err := NewWrapper(ctx, wg, config)
	if err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func testWriteConfig() *configuration.ConfigStruct {
	return &configuration.ConfigStruct{
		PostgresHost:     "primary.example",
		PostgresPort:     5433,
		PostgresUser:     "writer",
		PostgresPw:       "writer-pw",
		PostgresDb:       "timescale",
		PostgresMaxConns: 7,
	}
}

// TestWithoutReadHostReadsGoThroughTheWritePool is every deployment that has not set
// postgres_read_host: one pool, as before the read connection existed.
func TestWithoutReadHostReadsGoThroughTheWritePool(t *testing.T) {
	wrapper := newTestWrapper(t, testWriteConfig())
	if wrapper.readPool != wrapper.writePool {
		t.Fatal("got a second pool without postgres_read_host")
	}
	got := wrapper.writePool.Config()
	if got.ConnConfig.Host != "primary.example" || got.ConnConfig.Port != 5433 || got.ConnConfig.User != "writer" ||
		got.ConnConfig.Password != "writer-pw" || got.ConnConfig.Database != "timescale" || got.MaxConns != 7 {
		t.Errorf("write pool built from the wrong values: %+v", got.ConnConfig)
	}
}

// TestReadHostAloneKeepsTheWriteCredentials covers the senergy overlay, which names only the
// service of the replicas and logs in as the same role.
func TestReadHostAloneKeepsTheWriteCredentials(t *testing.T) {
	config := testWriteConfig()
	config.PostgresReadHost = "replicas.example"
	wrapper := newTestWrapper(t, config)
	if wrapper.readPool == wrapper.writePool {
		t.Fatal("reads share the write pool although postgres_read_host is set")
	}
	if host := wrapper.writePool.Config().ConnConfig.Host; host != "primary.example" {
		t.Errorf("write pool points at %v, want primary.example", host)
	}
	got := wrapper.readPool.Config()
	if got.ConnConfig.Host != "replicas.example" || got.ConnConfig.Port != 5433 || got.ConnConfig.User != "writer" ||
		got.ConnConfig.Password != "writer-pw" || got.ConnConfig.Database != "timescale" || got.MaxConns != 7 {
		t.Errorf("read pool built from the wrong values: %+v", got.ConnConfig)
	}
}

func TestReadCredentialsOverrideTheWriteOnes(t *testing.T) {
	config := testWriteConfig()
	config.PostgresReadHost = "replicas.example"
	config.PostgresReadUser = "reader"
	config.PostgresReadPw = "reader-pw"
	wrapper := newTestWrapper(t, config)
	got := wrapper.readPool.Config().ConnConfig
	if got.User != "reader" || got.Password != "reader-pw" {
		t.Errorf("read pool logs in as %v with %v, want reader with reader-pw", got.User, got.Password)
	}
	if got := wrapper.writePool.Config().ConnConfig; got.User != "writer" || got.Password != "writer-pw" {
		t.Errorf("write pool logs in as %v with %v, want writer with writer-pw", got.User, got.Password)
	}
}
