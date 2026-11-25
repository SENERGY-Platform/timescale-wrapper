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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresDSNKeepsCredentials(t *testing.T) {
	for _, pw := range []string{"with space", "a+b", "p@ss:w/rd?#%", "plain"} {
		config, err := pgxpool.ParseConfig(postgresDSN("db.example", 5432, "my db", "us er", pw))
		if err != nil {
			t.Fatalf("%q: %v", pw, err)
		}
		if config.ConnConfig.Password != pw || config.ConnConfig.User != "us er" || config.ConnConfig.Database != "my db" || config.ConnConfig.Port != 5432 {
			t.Fatalf("%q: got user %q password %q database %q port %d", pw, config.ConnConfig.User, config.ConnConfig.Password, config.ConnConfig.Database, config.ConnConfig.Port)
		}
	}
}
