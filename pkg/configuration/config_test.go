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

import "testing"

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
