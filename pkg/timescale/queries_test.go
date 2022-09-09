/*
 * Copyright 2021 InfAI (CC SES)
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

package timescale

import (
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"reflect"
	"testing"
)

func TestQueries(t *testing.T) {
	deviceId := "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
	serviceId := "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
	d1 := "1d"
	time1d := model.QueriesRequestElementTime{
		Last: &d1,
	}
	ten := 10
	zero := 0
	plus5 := "+5"
	plus10 := "+10"
	filter := []model.QueriesRequestElementFilter{{
		Column: "sensor.ENERGY.Total",
		Math:   &plus5,
		Type:   ">",
		Value:  ten,
	}}
	d10 := "10d"
	time10d := model.QueriesRequestElementTime{
		Last: &d10,
	}
	mean := "mean"
	median := "median"
	asc := model.Asc
	desc := model.Desc
	d7 := "7d"
	time7d := model.QueriesRequestElementTime{
		Last: &d7,
	}
	df := "difference-first"
	dl := "difference-last"
	dm := "difference-mean"
	f := "first"
	l := "last"
	start := "2021-06-20T00:00:00Z"
	end := "2021-06-22T00:00:00Z"
	timeFormTo := model.QueriesRequestElementTime{
		Start: &start,
		End:   &end,
	}

	t.Parallel()
	t.Run("Test ShortenId", func(t *testing.T) {
		actual, err := shortenId("urn:infai:ses:device:d42d8d24-f2a2-4dd7-8ad3-4cabfb6f8062")
		if err != nil {
			t.Error(err.Error())
		}
		expected := "1C2NJPKiTdeK00yr-2-AYg"
		if actual != expected {
			t.Error("Mismatched shortId. Expected/Actual\n", expected, "\n", actual)
		}

		actual, err = shortenId("urn:infai:ses:device:e3a9d39f-d833-45df-81c0-e479d17c2e06")
		if err != nil {
			t.Error(err.Error())
		}
		expected = "46nTn9gzRd-BwOR50XwuBg"
		if actual != expected {
			t.Error("Mismatched shortId. Expected/Actual\n", expected, "\n", actual)
		}
		actual, err = shortenId("e3a9d39f-d833-45df-81c0-e479d17c2e06")
		if err != nil {
			t.Error(err.Error())
		}
		expected = "46nTn9gzRd-BwOR50XwuBg"
		if actual != expected {
			t.Error("Mismatched shortId. Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Simple", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      &time1d,
			Limit:     &ten,
			Columns: []model.QueriesRequestElementColumn{
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus5,
				},
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus10,
				}},
			Filters:          &filter,
			OrderColumnIndex: &zero,
			OrderDirection:   &asc,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT \"time\", \"sensor.ENERGY.Total\"+5 AS \"sensor.ENERGY.Total\", \"sensor.ENERGY.Total\"+10" +
			" AS \"sensor.ENERGY.Total\" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
			" \"sensor.ENERGY.Total\" +5 > 10 AND \"time\" > now() - interval '1d' ORDER BY 1 ASC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Group", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      &time10d,
			Columns: []model.QueriesRequestElementColumn{
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &mean,
				},
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &median,
				}},
			GroupTime:        &d1,
			OrderColumnIndex: &zero,
			OrderDirection:   &asc,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT sub0.time AS \"time\", " +
			"(sub0.value) AS \"sensor.ENERGY.Total\", " +
			"(sub1.value) AS \"sensor.ENERGY.Total\" " +
			"FROM (SELECT time_bucket('1d', \"time\") AS \"time\", " +
			"avg(\"sensor.ENERGY.Total\") AS value FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" " +
			"WHERE \"time\" > now() - interval '10d' GROUP BY 1 ORDER BY 1 ASC) sub0 FULL OUTER JOIN " +
			"(SELECT time_bucket('1d', \"time\") AS \"time\", percentile_disc(0.5) WITHIN GROUP (ORDER BY " +
			"\"sensor.ENERGY.Total\") AS value FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" " +
			"WHERE \"time\" > now() - interval '10d' GROUP BY 1 ORDER BY 1 ASC) sub1 on sub0.time = sub1.time " +
			"ORDER BY 1 ASC"

		if actual[0] != expected {
			t.Error("Expected/Actual\n\n", expected, "\n\n", actual[0])
		}
	})

	t.Run("Test GenerateQueries Difference Functions", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      &time7d,
			Limit:     &ten,
			Columns: []model.QueriesRequestElementColumn{
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &dl,
				},
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &df,
					Math:      &plus5,
				},
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &f,
				},
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &l,
				},
				{
					Name:      "sensor.ENERGY.Total",
					GroupType: &dm,
				}},
			GroupTime:        &d1,
			OrderColumnIndex: &zero,
			OrderDirection:   &desc,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT sub0.time AS \"time\", (sub0.value - lag(sub0.value) OVER (ORDER BY 1)) AS \"sensor.ENERGY.Total\"," +
			" (sub1.value - lag(sub1.value) OVER (ORDER BY 1)) +5 AS \"sensor.ENERGY.Total\", (sub2.value) AS \"sensor.ENERGY.Total\"," +
			" (sub3.value) AS \"sensor.ENERGY.Total\", (sub4.value - lag(sub4.value) OVER (ORDER BY 1)) AS \"sensor.ENERGY.Total\" " +
			"FROM (SELECT time_bucket('1d', \"time\") AS \"time\", last(\"sensor.ENERGY.Total\", \"time\") AS value FROM" +
			" \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE \"time\" > now() - interval '8d' GROUP BY " +
			"1 ORDER BY 1 ASC LIMIT 11) sub0 FULL OUTER JOIN (SELECT time_bucket('1d', \"time\") AS \"time\", " +
			"first(\"sensor.ENERGY.Total\", \"time\") AS value FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" " +
			"WHERE \"time\" > now() - interval '8d' GROUP BY 1 ORDER BY 1 ASC LIMIT 11) sub1 on sub0.time = sub1.time FULL OUTER JOIN " +
			"(SELECT time_bucket('1d', \"time\") AS \"time\", first(\"sensor.ENERGY.Total\", \"time\") AS value FROM " +
			"\"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE \"time\" > now() - interval '8d' GROUP BY " +
			"1 ORDER BY 1 ASC LIMIT 11) sub2 on sub0.time = sub2.time FULL OUTER JOIN (SELECT time_bucket('1d', \"time\") AS" +
			" \"time\", last(\"sensor.ENERGY.Total\", \"time\") AS value FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\"" +
			" WHERE \"time\" > now() - interval '8d' GROUP BY 1 ORDER BY 1 ASC LIMIT 11) sub3 on sub0.time = sub3.time FULL OUTER JOIN" +
			" (SELECT time_bucket('1d', \"time\") AS \"time\", avg(\"sensor.ENERGY.Total\") AS value FROM " +
			"\"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE \"time\" > now() - interval '8d' GROUP BY 1" +
			" ORDER BY 1 ASC LIMIT 11) sub4 on sub0.time = sub4.time ORDER BY 1 DESC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n\n", expected, "\n\n", actual[0])
		}
	})

	t.Run("Test GenerateQueries Absolute Timestamps", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      &timeFormTo,
			Limit:     &ten,
			Columns: []model.QueriesRequestElementColumn{{
				Name: "sensor.ENERGY.Total",
			}},
			OrderColumnIndex: &zero,
			OrderDirection:   &asc,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT \"time\", \"sensor.ENERGY.Total\" AS \"sensor.ENERGY.Total\"" +
			" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
			" \"time\" > '2021-06-20T00:00:00Z' AND \"time\" < '2021-06-22T00:00:00Z' ORDER BY 1 ASC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Multiple Queries", func(t *testing.T) {
		filter := []model.QueriesRequestElementFilter{{
			Column: "sensor.ENERGY.Total",
			Math:   &plus5,
			Type:   ">",
			Value:  ten,
		}}
		elements := []model.QueriesRequestElement{
			{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &time1d,
				Limit:     &ten,
				Columns: []model.QueriesRequestElementColumn{
					{
						Name: "sensor.ENERGY.Total",
						Math: &plus5,
					},
					{
						Name: "sensor.ENERGY.Total",
						Math: &plus10,
					}},
				Filters:          &filter,
				OrderDirection:   &asc,
				OrderColumnIndex: &zero,
			},
			{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &time1d,
				Limit:     &ten,
				Columns: []model.QueriesRequestElementColumn{
					{
						Name: "sensor.ENERGY.Total",
					},
					{
						Name: "sensor.ENERGY.Total",
					}},
				Filters:          &filter,
				OrderDirection:   &asc,
				OrderColumnIndex: &zero,
			}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 2 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := []string{"SELECT \"time\", \"sensor.ENERGY.Total\"+5 AS \"sensor.ENERGY.Total\", \"sensor.ENERGY.Total\"+10" +
			" AS \"sensor.ENERGY.Total\" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
			" \"sensor.ENERGY.Total\" +5 > 10 AND \"time\" > now() - interval '1d' ORDER BY 1 ASC LIMIT 10",
			"SELECT \"time\", \"sensor.ENERGY.Total\" AS \"sensor.ENERGY.Total\", \"sensor.ENERGY.Total\"" +
				" AS \"sensor.ENERGY.Total\" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
				" \"sensor.ENERGY.Total\" +5 > 10 AND \"time\" > now() - interval '1d' ORDER BY 1 ASC LIMIT 10",
		}

		if !reflect.DeepEqual(actual, expected) {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Multiple String Filters", func(t *testing.T) {
		isoFormat := "iso_format"
		invalid := "invalid"
		filter := []model.QueriesRequestElementFilter{{
			Column: "sensor.Time_unit",
			Type:   "=",
			Value:  isoFormat,
		}, {
			Column: "sensor.ENERGY.Total_unit",
			Type:   "!=",
			Value:  invalid,
		}}
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      &time1d,
			Limit:     &ten,
			Columns: []model.QueriesRequestElementColumn{{
				Name: "sensor.ENERGY.Total",
			}},
			Filters:          &filter,
			OrderDirection:   &asc,
			OrderColumnIndex: &zero,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT \"time\", \"sensor.ENERGY.Total\" AS \"sensor.ENERGY.Total\"" +
			" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\"" +
			" WHERE \"sensor.Time_unit\" = 'iso_format' AND \"sensor.ENERGY.Total_unit\" != 'invalid'" +
			" AND \"time\" > now() - interval '1d' ORDER BY 1 ASC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Export", func(t *testing.T) {
		exportId := "97805820-ca0a-46c5-9dcf-16c2e386b050"
		elements := []model.QueriesRequestElement{{
			ExportId: &exportId,
			Time:     &time1d,
			Limit:    &ten,
			Columns: []model.QueriesRequestElementColumn{
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus5,
				},
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus10,
				}},
			Filters:          &filter,
			OrderColumnIndex: &zero,
			OrderDirection:   &asc,
		}}

		actual, err := GenerateQueries(elements, "ade1fba6-fa5f-4704-9997-81dc168f62f4")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT \"time\", \"sensor.ENERGY.Total\"+5 AS \"sensor.ENERGY.Total\", \"sensor.ENERGY.Total\"+10" +
			" AS \"sensor.ENERGY.Total\" FROM \"userid:reH7pvpfRwSZl4HcFo9i9A_export:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
			" \"sensor.ENERGY.Total\" +5 > 10 AND \"time\" > now() - interval '1d' ORDER BY 1 ASC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})

	t.Run("Test GenerateQueries Ahead", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time: &model.QueriesRequestElementTime{
				Ahead: &d1,
			},
			Limit: &ten,
			Columns: []model.QueriesRequestElementColumn{
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus5,
				},
				{
					Name: "sensor.ENERGY.Total",
					Math: &plus10,
				}},
			Filters:          &filter,
			OrderColumnIndex: &zero,
			OrderDirection:   &asc,
		}}

		actual, err := GenerateQueries(elements, "")
		if err != nil {
			t.Error(err)
		}
		if len(actual) != 1 {
			t.Error("Unexpected number of queries", len(actual))
		}
		expected := "SELECT \"time\", \"sensor.ENERGY.Total\"+5 AS \"sensor.ENERGY.Total\", \"sensor.ENERGY.Total\"+10" +
			" AS \"sensor.ENERGY.Total\" FROM \"device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA\" WHERE" +
			" \"sensor.ENERGY.Total\" +5 > 10 AND \"time\" > now() AND \"time\" < now() + interval '1d' ORDER BY 1 ASC LIMIT 10"

		if actual[0] != expected {
			t.Error("Expected/Actual\n", expected, "\n", actual)
		}
	})
}
