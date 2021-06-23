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

import (
	"log"
	"sync"
)

func (wrapper *Wrapper) ExecuteQueries(queries []string) (res [][][]interface{}, err error) {
	res = make([][][]interface{}, len(queries))
	wg := sync.WaitGroup{} // handle multiple queries in parallel
	for i, query := range queries {
		wg.Add(1)
		i := i         // make thread safe
		query := query // make thread safe
		go func() {
			if wrapper.config.Debug {
				log.Println("DEBUG: Query ", i, query)
			}
			resS, errS := wrapper.executeQuery(query)
			if errS != nil { // Prevents overwriting with nil
				err = errS
			} else {
				res[i] = resS
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return
}

func (wrapper *Wrapper) executeQuery(query string) (res [][]interface{}, err error) {
	rows, err := wrapper.pool.Query(query)
	if err != nil {
		return nil, err
	}
	res = [][]interface{}{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			rows.Close()
			return nil, err
		}
		res = append(res, values)
	}
	return res, nil
}
