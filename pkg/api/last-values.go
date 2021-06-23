/*
 *    Copyright 2020 InfAI (CC SES)
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

package api

func init() {
	// TODO endpoints = append(endpoints, LastValuesEndpoint)
}

/* TODO

func LastValuesEndpoint(router *httprouter.Router, config configuration.Config, influx *influxdb.Influx) {
	router.POST("/last-values", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()

		db := request.Header.Get(userHeader)
		if db == "" {
			http.Error(writer, "Missing header "+userHeader, http.StatusBadRequest)
			return
		}

		var requestElements []influxdb.RequestElement
		err := json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		if config.Debug {
			log.Print("Request:")
			err = json.NewEncoder(log.Writer()).Encode(requestElements)
			if err != nil {
				log.Println(err)
			}
		}

		responseElements, err := influx.GetLatestValues(db, requestElements)

		if err != nil {
			switch err {
			case influxdb.ErrInfluxConnection, influxdb.ErrNULL:
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			case influxdb.ErrNotFound:
				http.Error(writer, err.Error(), http.StatusNotFound)
				return
			default:
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(responseElements)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}

		if config.Debug {
			log.Println("Took " + time.Since(start).String())
		}
	})

}

 */
