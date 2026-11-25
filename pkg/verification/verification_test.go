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

package verification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	serving "github.com/SENERGY-Platform/analytics-serving/client"
	permClient "github.com/SENERGY-Platform/permissions-v2/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// fakePermClient answers CheckPermissionContext from a map and records every call.
type fakePermClient struct {
	permClient.Client
	mu          sync.Mutex
	access      map[string]bool
	errs        map[string]error
	topics      []string
	ids         []string
	permissions [][]permClient.Permission
}

func (f *fakePermClient) CheckPermissionContext(ctx context.Context, token string, topicId string, id string, permissions ...permClient.Permission) (bool, error, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.topics = append(f.topics, topicId)
	f.ids = append(f.ids, id)
	f.permissions = append(f.permissions, permissions)
	return f.access[id], f.errs[id], 0
}

func TestVerifyAccessOnceTopics(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name      string
		element   model.QueriesRequestElement
		allow     bool
		wantTopic string
		wantId    string
		wantPerms []permClient.Permission
	}{
		{
			name:      "device",
			element:   model.QueriesRequestElement{DeviceId: str("dev-1")},
			allow:     true,
			wantTopic: "devices",
			wantId:    "dev-1",
			wantPerms: []permClient.Permission{permClient.Execute},
		},
		{
			name:      "device group",
			element:   model.QueriesRequestElement{DeviceGroupId: str("grp-1")},
			allow:     true,
			wantTopic: "device-groups",
			wantId:    "grp-1",
			wantPerms: []permClient.Permission{permClient.Execute},
		},
		{
			name:      "location",
			element:   model.QueriesRequestElement{LocationId: str("loc-1")},
			allow:     true,
			wantTopic: "locations",
			wantId:    "loc-1",
			wantPerms: []permClient.Permission{permClient.Read, permClient.Execute},
		},
		{
			// Denied on purpose: an allowed export would reach servingClient, which is nil here.
			name:      "export",
			element:   model.QueriesRequestElement{ExportId: str("exp-1")},
			allow:     false,
			wantTopic: "export-instances",
			wantId:    "exp-1",
			wantPerms: []permClient.Permission{permClient.Execute},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePermClient{access: map[string]bool{tc.wantId: tc.allow}}
			v := &Verifier{c: cache.NewLocal(), permClient: fake}
			result, err := v.VerifyAccessOnce(context.Background(), tc.element, "token", "user")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Ok != tc.allow {
				t.Errorf("Ok = %v, want %v", result.Ok, tc.allow)
			}
			if len(fake.topics) != 1 {
				t.Fatalf("got %d permission calls, want 1", len(fake.topics))
			}
			if fake.topics[0] != tc.wantTopic {
				t.Errorf("topic = %q, want %q", fake.topics[0], tc.wantTopic)
			}
			if fake.ids[0] != tc.wantId {
				t.Errorf("id = %q, want %q", fake.ids[0], tc.wantId)
			}
			if !reflect.DeepEqual(fake.permissions[0], tc.wantPerms) {
				t.Errorf("permissions = %v, want %v", fake.permissions[0], tc.wantPerms)
			}
		})
	}

	t.Run("element without id", func(t *testing.T) {
		fake := &fakePermClient{}
		v := &Verifier{c: cache.NewLocal(), permClient: fake}
		result, err := v.VerifyAccessOnce(context.Background(), model.QueriesRequestElement{}, "token", "user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Ok {
			t.Errorf("Ok = true, want false")
		}
		if len(fake.topics) != 0 {
			t.Errorf("got %d permission calls, want 0", len(fake.topics))
		}
	})
}

func TestVerifyAccess(t *testing.T) {
	str := func(s string) *string { return &s }

	exportServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/exp-1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"UserId":"owner-1"}`))
	}))
	defer exportServer.Close()

	tests := []struct {
		name        string
		elements    []model.QueriesRequestElement
		access      map[string]bool
		errs        map[string]error
		wantOk      bool
		wantErr     bool
		wantUserIds []string
	}{
		{
			name: "two allowed",
			elements: []model.QueriesRequestElement{
				{DeviceId: str("dev-1")},
				{DeviceId: str("dev-2")},
			},
			access:      map[string]bool{"dev-1": true, "dev-2": true},
			wantOk:      true,
			wantUserIds: []string{"", ""},
		},
		{
			name: "one denied",
			elements: []model.QueriesRequestElement{
				{DeviceId: str("dev-1")},
				{DeviceId: str("dev-2")},
			},
			access: map[string]bool{"dev-1": true, "dev-2": false},
			wantOk: false,
		},
		{
			name: "one with error",
			elements: []model.QueriesRequestElement{
				{DeviceId: str("dev-1")},
				{DeviceId: str("dev-2")},
			},
			access:  map[string]bool{"dev-1": true, "dev-2": true},
			errs:    map[string]error{"dev-2": errors.New("upstream down")},
			wantOk:  false,
			wantErr: true,
		},
		{
			name:        "export allowed",
			elements:    []model.QueriesRequestElement{{ExportId: str("exp-1")}},
			access:      map[string]bool{"exp-1": true},
			wantOk:      true,
			wantUserIds: []string{"owner-1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePermClient{access: tc.access, errs: tc.errs}
			v := &Verifier{
				c:             cache.NewLocal(),
				permClient:    fake,
				servingClient: serving.New(exportServer.URL),
			}
			ok, userIds, err := v.VerifyAccess(context.Background(), tc.elements, "token", "user")
			if ok != tc.wantOk {
				t.Errorf("ok = %v, want %v", ok, tc.wantOk)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantUserIds != nil {
				if !reflect.DeepEqual(userIds, tc.wantUserIds) {
					t.Errorf("userIds = %v, want %v", userIds, tc.wantUserIds)
				}
			} else if len(userIds) != len(tc.elements) {
				t.Errorf("len(userIds) = %d, want %d", len(userIds), len(tc.elements))
			}
		})
	}
}

func TestVerifyAccessCachesPerUser(t *testing.T) {
	str := func(s string) *string { return &s }
	element := model.QueriesRequestElement{DeviceId: str("dev-1")}
	fake := &fakePermClient{access: map[string]bool{"dev-1": true}}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}
	ctx := context.Background()

	if _, err := v.VerifyAccessOnce(ctx, element, "token", "user-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.ids) != 1 {
		t.Fatalf("after first call: %d permission calls, want 1", len(fake.ids))
	}

	if _, err := v.VerifyAccessOnce(ctx, element, "token", "user-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.ids) != 1 {
		t.Errorf("same user and device: %d permission calls, want 1 (cached)", len(fake.ids))
	}

	if _, err := v.VerifyAccessOnce(ctx, element, "token", "user-b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.ids) != 2 {
		t.Errorf("other user: %d permission calls, want 2", len(fake.ids))
	}
}

// VerifyAccess checks the elements concurrently. Run with -race: the goroutines
// share ok and err and have to synchronise their writes.
func TestVerifyAccessRace(t *testing.T) {
	str := func(s string) *string { return &s }
	const n = 50
	access := map[string]bool{}
	elements := make([]model.QueriesRequestElement, n)
	for i := range elements {
		id := fmt.Sprintf("dev-%d", i)
		elements[i] = model.QueriesRequestElement{DeviceId: str(id)}
		access[id] = i%2 == 0
	}
	fake := &fakePermClient{access: access}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}

	ok, _, err := v.VerifyAccess(context.Background(), elements, "token", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false (half of the elements are denied)")
	}
}

func TestVerifyAccessDeduplicates(t *testing.T) {
	str := func(s string) *string { return &s }
	fake := &fakePermClient{access: map[string]bool{"dev-1": true}}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}
	elements := []model.QueriesRequestElement{
		{DeviceId: str("dev-1")}, {DeviceId: str("dev-1")}, {DeviceId: str("dev-1")},
	}
	ok, userIds, err := v.VerifyAccess(context.Background(), elements, "token", "user")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(fake.ids) != 1 {
		t.Errorf("%d permission calls, want 1", len(fake.ids))
	}
	if len(userIds) != 3 {
		t.Errorf("len(userIds) = %d, want 3", len(userIds))
	}
}

func TestVerifyAccessMixedAllowedDenied(t *testing.T) {
	str := func(s string) *string { return &s }
	fake := &fakePermClient{access: map[string]bool{"dev-1": true, "dev-2": false}}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}
	elements := []model.QueriesRequestElement{
		{DeviceId: str("dev-1")}, {DeviceId: str("dev-2")}, {DeviceId: str("dev-1")},
	}
	ok, _, err := v.VerifyAccess(context.Background(), elements, "token", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("ok = true, want false")
	}
}

func TestVerifyAccessElementWithoutId(t *testing.T) {
	fake := &fakePermClient{}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}
	ok, userIds, err := v.VerifyAccess(context.Background(), []model.QueriesRequestElement{{}}, "token", "user")
	if err != nil || ok || len(userIds) != 1 || len(fake.ids) != 0 {
		t.Errorf("ok=%v err=%v userIds=%v calls=%d", ok, err, userIds, len(fake.ids))
	}
}

func TestVerifyAccessMapsOwnerPerElement(t *testing.T) {
	str := func(s string) *string { return &s }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/instance/exp-1":
			_, _ = w.Write([]byte(`{"UserId":"owner-1"}`))
		case "/instance/exp-2":
			_, _ = w.Write([]byte(`{"UserId":"owner-2"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	fake := &fakePermClient{access: map[string]bool{"exp-1": true, "exp-2": true, "dev-1": true}}
	v := &Verifier{c: cache.NewLocal(), permClient: fake, servingClient: serving.New(server.URL)}
	elements := []model.QueriesRequestElement{
		{ExportId: str("exp-2")}, {DeviceId: str("dev-1")}, {ExportId: str("exp-1")}, {ExportId: str("exp-2")},
	}
	ok, userIds, err := v.VerifyAccess(context.Background(), elements, "token", "user")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := []string{"owner-2", "", "owner-1", "owner-2"}
	if !reflect.DeepEqual(userIds, want) {
		t.Errorf("userIds = %v, want %v", userIds, want)
	}
}

// The same id under two kinds must not share a cache entry.
func TestVerifyAccessKindsDoNotShareCache(t *testing.T) {
	str := func(s string) *string { return &s }
	fake := &fakePermClient{access: map[string]bool{"same": true}}
	v := &Verifier{c: cache.NewLocal(), permClient: fake}
	elements := []model.QueriesRequestElement{
		{DeviceId: str("same")}, {DeviceGroupId: str("same")}, {LocationId: str("same")},
	}
	ok, _, err := v.VerifyAccess(context.Background(), elements, "token", "user")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(fake.topics) != 3 {
		t.Errorf("%d permission calls (%v), want 3", len(fake.topics), fake.topics)
	}
}
