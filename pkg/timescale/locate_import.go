/*
 *    Copyright 2024 InfAI (CC SES)
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
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	importModel "github.com/SENERGY-Platform/import-repository/lib/model"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx"
	"github.com/umahmood/haversine"
)

type distance struct {
	identifier interface{}
	km         float64
}

func (wrapper *Wrapper) CreateFiltersForImport(exportId string, userId string, token string, lat float64, lon float64) ([]model.QueriesRequestElementFilter, error) {
	if wrapper.config.Debug {
		start := time.Now()
		defer func() {
			log.Printf("DEBUG: CreateFiltersForImport took %v, is included in query generation\n", time.Since(start))
		}()
	}
	exportInstance, err := wrapper.servingClient.GetInstance(token, exportId)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(exportInstance.ServiceName, "urn:infai:ses:import-type:") {
		return nil, errors.New("can not locate export which is not based on an import")
	}
	importType, err, _ := wrapper.importRepoClient.ReadImportType(exportInstance.ServiceName, jwt.Token{Token: token})
	if err != nil {
		return nil, err
	}
	var output *importModel.ContentVariable
	for _, sub := range importType.Output.SubContentVariables {
		if sub.Name == "value" {
			output = &sub
			break
		}
	}
	if output == nil {
		return nil, errors.New("unknown import type output format")
	}
	identifierPath, err := findImportTypeContentVariable(*output, findImportTypeContentVariableSearchOptions{identifiesMeasurement: true}, "")
	if err != nil {
		return nil, errors.Join(errors.New("import type has no measurement identifier"), err)
	}

	latPath, err := findImportTypeContentVariable(*output, findImportTypeContentVariableSearchOptions{characteristicId: "urn:infai:ses:characteristic:63bb46ea-64f1-4a60-aabb-67b9febdb588"}, "")
	if err != nil {
		return nil, errors.Join(errors.New("import type has no output with characteristic for lat"), err)
	}

	lonPath, err := findImportTypeContentVariable(*output, findImportTypeContentVariableSearchOptions{characteristicId: "urn:infai:ses:characteristic:d8a73a4d-8745-40b9-87e5-50c5d31f745a"}, "")
	if err != nil {
		return nil, errors.Join(errors.New("import type has no output with characteristic for lon"), err)
	}

	var identifierPathTs, latPathTs, lonPathTs string
	for _, v := range exportInstance.Values {
		if v.Path == identifierPath {
			identifierPathTs = v.Name
			continue
		}
		if v.Path == latPath {
			latPathTs = v.Name
			continue
		}
		if v.Path == lonPath {
			lonPathTs = v.Name
			continue
		}
	}

	if len(identifierPathTs) == 0 || len(lonPathTs) == 0 || len(latPathTs) == 0 {
		return nil, errors.New("missing identifier, lat or lon path in export")
	}

	tableName, err := wrapper.tableName(model.QueriesRequestElement{ExportId: &exportId}, exportInstance.UserId, wrapper.config.DefaultTimezone)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT \"%v\", \"%v\", \"%v\" FROM \"%v\";", identifierPathTs, latPathTs, lonPathTs, wrapperMaterializedViewPrefix+tableName)
	if wrapper.config.Debug {
		log.Println("DEBUG: Querying export of import locations with: " + query)
	}
	table, err := wrapper.ExecuteQuery(query)
	if err != nil {
		err2, ok := err.(pgx.PgError)
		if !ok || err2.Code != pgerrcode.UndefinedTable {
			return nil, err
		}
		if wrapper.config.Debug {
			log.Printf("DEBUG: setting up materialized view for table %v\n", tableName)
		}
		err = wrapper.setupMaterializedRefreshJob(identifierPathTs, latPathTs, lonPathTs, tableName)
		if err != nil {
			return nil, err
		}
		table, err = wrapper.ExecuteQuery(query)
		if err != nil {
			return nil, err
		}
	}

	if len(table) == 0 {
		return []model.QueriesRequestElementFilter{}, nil
	}

	distances := []distance{}
	source := haversine.Coord{Lat: lat, Lon: lon}

	for _, t := range table {
		dLat, ok := t[1].(float64)
		if !ok {
			return nil, errors.New("lat coorindate not float64")
		}
		dLon, ok := t[2].(float64)
		if !ok {
			return nil, errors.New("lon coorindate not float64")
		}
		destination := haversine.Coord{Lat: dLat, Lon: dLon}
		_, km := haversine.Distance(source, destination)
		distances = append(distances, distance{
			identifier: t[0],
			km:         km,
		})
	}

	sort.Slice(distances, func(i, j int) bool { return distances[i].km < distances[j].km })

	if wrapper.config.Debug {
		log.Printf("DEBUG: Found %v options. Smallest distance %vkm (identifier %v), longest %vkm (identifier %v)", len(distances), distances[0].km, distances[0].identifier, distances[len(distances)-1].km, distances[len(distances)-1].identifier)
	}

	return []model.QueriesRequestElementFilter{{
		Column: identifierPathTs,
		Type:   "=",
		Value:  distances[0].identifier,
	}}, nil

}

type findImportTypeContentVariableSearchOptions struct {
	identifiesMeasurement bool   // can only use one at a time
	characteristicId      string // can only use one at a time
}

func findImportTypeContentVariable(content importModel.ContentVariable, find findImportTypeContentVariableSearchOptions, path string) (string, error) {
	if len(path) > 0 {
		path += "."
	}
	path += content.Name
	if find.identifiesMeasurement && content.UseAsTag {
		return path, nil
	}
	if len(find.characteristicId) > 0 && find.characteristicId == content.CharacteristicId {
		return path, nil
	}
	for _, sub := range content.SubContentVariables {
		path, err := findImportTypeContentVariable(sub, find, path)
		if err == nil {
			return path, nil
		}
	}
	return "", errors.New("content variable not found")

}

func (wrapper *Wrapper) setupMaterializedRefreshJob(identifierPathTs, latPathTs, lonPathTs, tableName string) error {
	_, err := wrapper.pool.Exec("CREATE OR REPLACE PROCEDURE " + wrapperMaterializedViewProcedureName + `(job_id INT, view JSONB) LANGUAGE PLPGSQL
		AS $$
		BEGIN
			EXECUTE format('REFRESH MATERIALIZED VIEW %s', view);
		END
		$$;
	`)
	if err != nil {
		return err
	}
	_, err = wrapper.pool.Exec(fmt.Sprintf("CREATE MATERIALIZED VIEW \"%v\" AS SELECT DISTINCT \"%v\", \"%v\", \"%v\" FROM \"%v\"", wrapperMaterializedViewPrefix+tableName, identifierPathTs, latPathTs, lonPathTs, tableName))
	if err != nil {
		return err
	}
	_, err = wrapper.pool.Exec(fmt.Sprintf("SELECT add_job('ts_wrapper_refresh_mat_view', '1day', config => '\"%v\"');", tableName))
	if err != nil {
		return err
	}
	return nil
}

func (wrapper *Wrapper) removeOutdatedMaterializedRefreshJobs() error {
	rows, err := wrapper.pool.Query(fmt.Sprintf("SELECT job_id FROM timescaledb_information.jobs WHERE proc_name = '%v' AND config::text NOT IN (SELECT '\"' || table_name || '\"' FROM information_schema.tables);", wrapperMaterializedViewProcedureName))
	if err != nil {
		return err
	}

	var jobId int64
	for rows.Next() {
		err = rows.Scan(&jobId)
		if err != nil {
			return err
		}
		if wrapper.config.Debug {
			log.Printf("DEBUG: Deleting job for materialized view refresh %v (no longer needed)\n", jobId)
		}

		_, err = wrapper.pool.Exec(fmt.Sprintf("SELECT delete_job(%v);", jobId))
		if err != nil {
			return err
		}
	}

	return nil
}
