/*
 *    Copyright 2021 InfAI (CC SES)
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

import "log"

func(wrapper *Wrapper) ExecuteQueries(queries []string) (res [][][]interface{}, err error) {
	res = make([][][]interface{}, len(queries))
	for i, query := range queries {
		if wrapper.config.Debug {
			log.Println("DEBUG: Query ", i, query)
		}
		rows, err := wrapper.pool.Query(query)
		if err != nil {
			return nil, err
		}
		subResults := [][]interface{}{}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, err
			}
			subResults = append(subResults, values)
		}
		res[i] = subResults
	}
	return
}
  /* TODO
  SELECT time_bucket('1d', "time") AS "time",
  (lead(last("sensor.ENERGY.Total", "time"), 1) OVER (PARTITION BY 1 ORDER BY 1 ASC) - last("sensor.ENERGY.Total", "time")) * -1 AS "sensor.ENERGY.Total"
  FROM "device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA" WHERE "time" > now() - interval '7d' GROUP BY 1 ORDER BY 1 DESC LIMIT 182

  SELECT time_bucket('1d', "time") AS "time",
  (lead(last("sensor.ENERGY.Total", "time"), 1) OVER (PARTITION BY 1 ORDER BY 1 ASC) - last("sensor.ENERGY.Total", "time")) * -1 AS "sensor.ENERGY.Total"
  FROM "device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA" WHERE "time" > now() - interval '7d' GROUP BY 1 ORDER BY 1 DESC LIMIT 10

  These queries yield way different results due to query optimization by postgres? Maybe bad SQL statement?
   */
