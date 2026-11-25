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

package model

import (
	"errors"
	"time"
)

// Bounds returns the start and end of the time window as of now: last is the window [now-last, now],
// ahead [now, now+ahead], start and end are parsed as RFC3339 and returned as given. The result says
// nothing about which ends belong to the window, that is up to the caller. A nil receiver, a window with
// only one of start and end, an unparseable start or end and an interval AddTimeInterval rejects are
// errors; all but the interval wrap ErrBadRequest.
func (elementTime *QueriesRequestElementTime) Bounds(now time.Time) (start, end time.Time, err error) {
	if elementTime == nil {
		return start, end, errors.Join(errors.New("no time window"), ErrBadRequest)
	}
	switch {
	case elementTime.Last != nil:
		start, err = AddTimeInterval(now, *elementTime.Last, -1)
		return start, now, errors.Join(err)
	case elementTime.Ahead != nil:
		end, err = AddTimeInterval(now, *elementTime.Ahead, 1)
		return now, end, errors.Join(err)
	case elementTime.Start != nil && elementTime.End != nil:
		if start, err = time.Parse(time.RFC3339, *elementTime.Start); err != nil {
			return start, end, errors.Join(err, ErrBadRequest)
		}
		if end, err = time.Parse(time.RFC3339, *elementTime.End); err != nil {
			return start, end, errors.Join(err, ErrBadRequest)
		}
		return start, end, nil
	default:
		return start, end, errors.Join(errors.New("incomplete time window"), ErrBadRequest)
	}
}
