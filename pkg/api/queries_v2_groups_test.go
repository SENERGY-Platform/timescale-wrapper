/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestVerifyDerivedDeviceGroups(t *testing.T) {
	verify := func(_ context.Context, id string) (bool, error) {
		switch id {
		case "denied":
			return false, nil
		case "broken":
			return false, errors.New("verify failed")
		}
		return true, nil
	}

	if err := verifyDerivedDeviceGroups(context.Background(), []string{"g1", "g2"}, verify); err != nil {
		t.Errorf("allowed groups must pass: %v", err)
	}
	if err := verifyDerivedDeviceGroups(context.Background(), nil, verify); err != nil {
		t.Errorf("no groups must pass: %v", err)
	}
	err := verifyDerivedDeviceGroups(context.Background(), []string{"g1", "denied", "g2"}, verify)
	if err == nil || !errors.Is(err, model.ErrForbidden) {
		t.Errorf("denied group must yield ErrForbidden, got %v", err)
	}
	err = verifyDerivedDeviceGroups(context.Background(), []string{"broken"}, verify)
	if err == nil || !errors.Is(err, model.ErrInternalServerError) || errors.Is(err, model.ErrForbidden) {
		t.Errorf("verify error must yield ErrInternalServerError, got %v", err)
	}
}
