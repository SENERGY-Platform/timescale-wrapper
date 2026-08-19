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

import "testing"

func TestParseViewDescription(t *testing.T) {
	t.Run("quoted column", func(t *testing.T) {
		groupType, groupTime, err := parseViewDescription(` SELECT time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text) AS "time",
    last("POWER", "time") AS "POWER"
   FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:Mo9HsF47Rx60mXwn9-80Tg"
  GROUP BY (time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text));`)
		if err != nil {
			t.Fatal(err)
		}
		if groupType != "last" {
			t.Error("Expected/Actual", "last", groupType)
		}
		if groupTime != "1 day" {
			t.Error("Expected/Actual", "1 day", groupTime)
		}
	})

	// Postgres omits the quotes for column names that do not require them, which used to make
	// the type regex fail and the endpoint answer 500.
	t.Run("unquoted column", func(t *testing.T) {
		groupType, groupTime, err := parseViewDescription(` SELECT time_bucket('12:00:00'::interval, "time", 'Europe/Berlin'::text) AS "time",
    first(lwt, "time") AS lwt
   FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:8Poq-YNIREmO_5Gl8uebCA"
  GROUP BY (time_bucket('12:00:00'::interval, "time", 'Europe/Berlin'::text));`)
		if err != nil {
			t.Fatal(err)
		}
		if groupType != "first" {
			t.Error("Expected/Actual", "first", groupType)
		}
		if groupTime != "12:00:00" {
			t.Error("Expected/Actual", "12:00:00", groupTime)
		}
	})

	t.Run("multiple columns", func(t *testing.T) {
		groupType, groupTime, err := parseViewDescription(` SELECT time_bucket('1 mon'::interval, "time", 'Europe/Berlin'::text) AS "time",
    last(lwt, "time") AS lwt,
    last("sensor.Wifi.RSSI", "time") AS "sensor.Wifi.RSSI",
    last("sensor.ENERGY.Total", "time") AS "sensor.ENERGY.Total"
   FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:iQA5Hii2ScK5DaV6a6AjFQ"
  GROUP BY (time_bucket('1 mon'::interval, "time", 'Europe/Berlin'::text));`)
		if err != nil {
			t.Fatal(err)
		}
		if groupType != "last" {
			t.Error("Expected/Actual", "last", groupType)
		}
		if groupTime != "1 mon" {
			t.Error("Expected/Actual", "1 mon", groupTime)
		}
	})

	t.Run("no aggregation", func(t *testing.T) {
		_, _, err := parseViewDescription(` SELECT time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text) AS "time"
   FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:8Poq-YNIREmO_5Gl8uebCA"
  GROUP BY (time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text));`)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("no bucket", func(t *testing.T) {
		_, _, err := parseViewDescription(` SELECT "time",
    last(lwt, "time") AS lwt
   FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:8Poq-YNIREmO_5Gl8uebCA";`)
		if err == nil {
			t.Error("expected error")
		}
	})
}
