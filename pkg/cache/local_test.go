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

package cache

import (
	"errors"
	"testing"
)

type localTestValue struct {
	Name string
	N    int
}

func TestUseLocalMissThenHit(t *testing.T) {
	c := NewLocal()
	calls := 0
	getter := func() (localTestValue, error) {
		calls++
		return localTestValue{Name: "a", N: 7}, nil
	}
	for i := 0; i < 2; i++ {
		got, err := UseLocal(c, "k", getter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != (localTestValue{Name: "a", N: 7}) {
			t.Errorf("call %d: got %+v", i, got)
		}
	}
	if calls != 1 {
		t.Errorf("getter called %d times, want 1", calls)
	}
}

func TestUseLocalGetterErrorNotCached(t *testing.T) {
	c := NewLocal()
	calls := 0
	boom := errors.New("boom")
	_, err := UseLocal(c, "k", func() (localTestValue, error) {
		calls++
		return localTestValue{N: 1}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	got, err := UseLocal(c, "k", func() (localTestValue, error) {
		calls++
		return localTestValue{N: 2}, nil
	})
	if err != nil || got.N != 2 {
		t.Errorf("got %+v, %v; want N=2, nil", got, err)
	}
	if calls != 2 {
		t.Errorf("getter called %d times, want 2", calls)
	}
}

func TestUseLocalKeysAreIndependent(t *testing.T) {
	c := NewLocal()
	a, _ := UseLocal(c, "a", func() (int, error) { return 1, nil })
	b, _ := UseLocal(c, "b", func() (int, error) { return 2, nil })
	if a != 1 || b != 2 {
		t.Errorf("a=%d b=%d", a, b)
	}
}
