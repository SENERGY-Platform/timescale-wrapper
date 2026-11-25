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

package configuration

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestHandleEnvironmentVarsRejectsInvalidValues(t *testing.T) {
	t.Setenv("POSTGRES_PORT", "54x32")
	if err := HandleEnvironmentVars(&ConfigStruct{}); err == nil {
		t.Fatal("expected an error for a port that is not a number")
	}
}

func TestHandleEnvironmentVarsReadsValidValues(t *testing.T) {
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("DEBUG", "true")
	config := &ConfigStruct{}
	if err := HandleEnvironmentVars(config); err != nil {
		t.Fatal(err)
	}
	if config.PostgresPort != 5433 || !config.Debug {
		t.Fatalf("got port %d debug %v", config.PostgresPort, config.Debug)
	}
}

func TestHandleEnvironmentVarsReadsSliceValues(t *testing.T) {
	t.Setenv("MEMCACHED_URLS", "memcached-a:11211, memcached-b:11211 ,memcached-c:11211")
	config := &ConfigStruct{}
	if err := HandleEnvironmentVars(config); err != nil {
		t.Fatal(err)
	}
	want := []string{"memcached-a:11211", "memcached-b:11211", "memcached-c:11211"}
	if !slices.Equal(config.MemcachedUrls, want) {
		t.Fatalf("got %q, want %q", config.MemcachedUrls, want)
	}
}

// HandleEnvironmentVars maps only these field kinds. A field of any other kind
// would be silently skipped, so adding one must come with a branch for it.
func TestConfigFieldKindsHaveEnvironmentHandling(t *testing.T) {
	handled := []reflect.Kind{reflect.Int64, reflect.Uint16, reflect.String, reflect.Bool, reflect.Slice}
	typ := reflect.TypeOf(ConfigStruct{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !slices.Contains(handled, field.Type.Kind()) {
			t.Errorf("field %s has kind %s, which HandleEnvironmentVars does not handle", field.Name, field.Type.Kind())
		}
	}
}

func TestLoadReadsConfigFile(t *testing.T) {
	location := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(location, []byte(`{"postgres_port": 5432, "memcached_urls": ["a:1"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(location)
	if err != nil {
		t.Fatal(err)
	}
	if config.PostgresPort != 5432 || !slices.Equal(config.MemcachedUrls, []string{"a:1"}) {
		t.Fatalf("got port %d urls %q", config.PostgresPort, config.MemcachedUrls)
	}
}
