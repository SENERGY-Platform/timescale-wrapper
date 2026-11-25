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

package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/meta"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// columnConverter converts the values of the columns of a request element list from the characteristic of the
// source to the target characteristic of the column.
type columnConverter struct {
	request    []model.QueriesRequestElement
	conv       *converter.Converter
	sources    map[int]map[int]*string                     // seriesIndex to seriesColumnIndex to sourceCharacteristicId
	extensions map[int]map[int][]models.ConverterExtension // seriesIndex to seriesColumnIndex to ConverterExtensions
}

// newColumnConverter looks up the source characteristic and the concept extensions of every column with a
// target characteristic. The source is the one of the column, or else the one of the variable of the service
// output the column name addresses.
func newColumnConverter(ctx context.Context, lookups *requestLookups, request []model.QueriesRequestElement, conv *converter.Converter) (*columnConverter, error) {
	c := &columnConverter{
		request:    request,
		conv:       conv,
		sources:    map[int]map[int]*string{},
		extensions: map[int]map[int][]models.ConverterExtension{},
	}
	for seriesIndex := range request {
		c.sources[seriesIndex] = make(map[int]*string)
		c.extensions[seriesIndex] = make(map[int][]models.ConverterExtension)
		for seriesColumnIndex, column := range request[seriesIndex].Columns {
			if column.TargetCharacteristicId == nil {
				continue
			}
			sourceCharId := column.SourceCharacteristicId
			if sourceCharId == nil {
				serviceId := request[seriesIndex].ServiceId
				if serviceId == nil {
					return nil, errors.New("service id cant be nil")
				}
				service, err := lookups.GetService(ctx, *serviceId)
				if err != nil {
					return nil, err
				}
				if len(service.Outputs) != 1 {
					return nil, errors.New("service doesnt have exactly one output")
				}
				pathParts := strings.Split(column.Name, ".")
				if len(pathParts) > 1 {
					pathParts = pathParts[1:] // skip root
				}
				contentVariable := meta.GetDeepContentVariable(service.Outputs[0].ContentVariable, pathParts)
				if contentVariable == nil {
					return nil, errors.Join(fmt.Errorf("column %q not found in service output", column.Name), model.ErrBadRequest)
				}
				sourceCharId = &contentVariable.CharacteristicId
			}
			c.sources[seriesIndex][seriesColumnIndex] = sourceCharId
			if column.ConceptId == nil {
				return nil, errors.New("concept id cant be nil")
			}
			concept, err := lookups.GetConcept(ctx, *column.ConceptId)
			if err != nil {
				return nil, err
			}
			c.extensions[seriesIndex][seriesColumnIndex] = concept.Conversions
		}
	}
	return c, nil
}

// convert returns the point of the column unchanged if it has no target characteristic, no known source or
// the same characteristic as target.
func (c *columnConverter) convert(seriesIndex int, seriesColumnIndex int, point interface{}) (interface{}, error) {
	column := c.request[seriesIndex].Columns[seriesColumnIndex]
	source := c.sources[seriesIndex][seriesColumnIndex]
	if column.TargetCharacteristicId == nil || source == nil || *source == *column.TargetCharacteristicId {
		return point, nil
	}
	return c.conv.CastWithExtension(point, *source, *column.TargetCharacteristicId, c.extensions[seriesIndex][seriesColumnIndex])
}

// formatPerQuery brings the rows of each element into their final form: trailing rows without a value are
// removed, the rows are sorted, cut at the end of the time range and at the limit, characteristics are converted
// and the time is formatted. The results are changed in place.
func formatPerQuery(ctx context.Context, lookups *requestLookups, request []model.QueriesRequestElement, results [][][]interface{},
	orderColumnIndex int, orderDirection model.Direction, timeFormat string, conv *converter.Converter) ([][][]interface{}, error) {

	columns, err := newColumnConverter(ctx, lookups, request, conv)
	if err != nil {
		return nil, err
	}
	for seriesIndex := range results {
		for len(results[seriesIndex]) > 0 && isRowEmpty(results[seriesIndex][len(results[seriesIndex])-1]) {
			results[seriesIndex] = results[seriesIndex][:len(results[seriesIndex])-1]
		}
		err = model.Sort2D(results[seriesIndex], orderColumnIndex, orderDirection)
		if err != nil {
			return nil, err
		}
		if request[seriesIndex].Time != nil && request[seriesIndex].Time.End != nil {
			// if tables do not contain enough data, LIMIT clauses miss their intention.
			// therefore, result are filtered in this post-processing step again. The rows at or
			// after the end can be anywhere in the sorted series, and the filter has to run
			// before the limit, or it would remove rows the limit counted.
			ts, err := time.Parse(time.RFC3339, *request[seriesIndex].Time.End)
			if err != nil {
				return nil, err
			}
			kept := results[seriesIndex][:0]
			for _, row := range results[seriesIndex] {
				if len(row) > 0 {
					if t, ok := row[0].(time.Time); ok && !t.Before(ts) {
						continue
					}
				}
				kept = append(kept, row)
			}
			results[seriesIndex] = kept
		}
		if limit := request[seriesIndex].Limit; limit != nil && *limit >= 0 && len(results[seriesIndex]) > *limit {
			results[seriesIndex] = results[seriesIndex][:*limit]
		}
		for rowIndex := range results[seriesIndex] {
			for j := range results[seriesIndex][rowIndex] {
				if j == 0 {
					continue // time column
				}
				results[seriesIndex][rowIndex][j], err = columns.convert(seriesIndex, j-1, results[seriesIndex][rowIndex][j])
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if len(timeFormat) > 0 {
		for i := range results {
			formatTime2D(results[i], timeFormat)
		}
	}
	return results, nil
}

// formatTable joins the series into one table, see joinSeriesAsTable, formats the time and removes trailing rows
// without a value.
func formatTable(ctx context.Context, lookups *requestLookups, request []model.QueriesRequestElement, results [][][]interface{},
	orderColumnIndex int, orderDirection model.Direction, timeFormat string, conv *converter.Converter) ([][]interface{}, error) {

	columns, err := newColumnConverter(ctx, lookups, request, conv)
	if err != nil {
		return nil, err
	}
	formatted, err := joinSeriesAsTable(request, results, orderColumnIndex, orderDirection, columns)
	if err != nil {
		return nil, err
	}
	if len(timeFormat) > 0 {
		formatTime2D(formatted, timeFormat)
	}
	for len(formatted) > 0 && isRowEmpty(formatted[len(formatted)-1]) {
		formatted = formatted[:len(formatted)-1]
	}
	return formatted, nil
}

// joinSeriesAsTable joins the series into one table with a row per timestamp: column 0 is the
// time, then the columns of every request element in order. A row of an earlier series takes the
// first row of each later series with the same timestamp, rows with the same timestamp within a
// series pair up in their order. Rows without any value are dropped.
//
// The rows of the later series are looked up in a map per series instead of by binary search
// with a re-sort after every match, which made the join quadratic in the number of rows and let
// requests over a few days run into the gateway timeout (SNRGY-1722).
func joinSeriesAsTable(request []model.QueriesRequestElement, data [][][]interface{}, orderColumnIndex int, orderDirection model.Direction, columns *columnConverter) (formatted [][]interface{}, err error) {
	totalColumns := 1
	baseIndex := map[int]int{}
	for requestIndex, element := range request {
		baseIndex[requestIndex] = totalColumns
		totalColumns += len(element.Columns)
	}

	// row indices per timestamp of every series, in row order; consumed tracks rows that were
	// already joined into a row of an earlier series
	rowsByTime := make([]map[int64][]int, len(data))
	consumed := make([][]bool, len(data))
	for seriesIndex := range data {
		rowsByTime[seriesIndex] = map[int64][]int{}
		consumed[seriesIndex] = make([]bool, len(data[seriesIndex]))
		for rowIndex, row := range data[seriesIndex] {
			if timestamp, ok := row[0].(time.Time); ok {
				key := timestamp.UnixNano()
				rowsByTime[seriesIndex][key] = append(rowsByTime[seriesIndex][key], rowIndex)
			}
		}
	}

	for seriesIndex := range data {
		for rowIndex := range data[seriesIndex] {
			if consumed[seriesIndex][rowIndex] {
				continue
			}
			formattedRow := make([]interface{}, totalColumns)
			formattedRow[0] = data[seriesIndex][rowIndex][0]
			if formattedRow[0] == nil { // no data in series
				continue
			}
			anyData := false
			for seriesColumnIndex := range request[seriesIndex].Columns {
				point, err := columns.convert(seriesIndex, seriesColumnIndex, data[seriesIndex][rowIndex][seriesColumnIndex+1])
				if err != nil {
					return nil, err
				}
				if point != nil {
					formattedRow[baseIndex[seriesIndex]+seriesColumnIndex] = point
					anyData = true
				}
			}
			timestamp, ok := data[seriesIndex][rowIndex][0].(time.Time)
			if ok {
				key := timestamp.UnixNano()
				for subSeriesIndex := seriesIndex + 1; subSeriesIndex < len(data); subSeriesIndex++ {
					candidates := rowsByTime[subSeriesIndex][key]
					for len(candidates) > 0 && consumed[subSeriesIndex][candidates[0]] {
						candidates = candidates[1:]
					}
					rowsByTime[subSeriesIndex][key] = candidates
					if len(candidates) == 0 {
						continue
					}
					subRowIndex := candidates[0]
					consumed[subSeriesIndex][subRowIndex] = true
					for subSeriesColumnIndex := range request[subSeriesIndex].Columns {
						point, err := columns.convert(subSeriesIndex, subSeriesColumnIndex, data[subSeriesIndex][subRowIndex][subSeriesColumnIndex+1])
						if err != nil {
							return nil, err
						}
						if point != nil {
							formattedRow[baseIndex[subSeriesIndex]+subSeriesColumnIndex] = point
							anyData = true
						}
					}
				}
			}
			if anyData {
				formatted = append(formatted, formattedRow)
			}
		}
	}
	err = model.Sort2D(formatted, orderColumnIndex, orderDirection)
	if err != nil {
		return formatted, err
	}
	return
}

func formatTime2D(data [][]interface{}, timeFormat string) {
	for i := range data {
		if len(data[i]) > 0 && data[i][0] != nil {
			data[i][0] = data[i][0].(time.Time).Format(timeFormat)
		}
	}
}

func isRowEmpty(data []interface{}) bool {
	for i := range data {
		if i == 0 {
			continue
		}
		if data[i] != nil {
			return false
		}
	}
	return true
}

// writeCsv writes a table (2D) or the series of a per query response (3D) as csv rows.
func writeCsv(data interface{}, writer *csv.Writer) (err error) {
	switch d := data.(type) {
	case [][][]interface{}:
		for i := range d {
			if err = writeCsvRows(d[i], writer); err != nil {
				return err
			}
		}
	case [][]interface{}:
		if err = writeCsvRows(d, writer); err != nil {
			return err
		}
	default:
		return errors.New("data is not 2d or 3d")
	}
	writer.Flush()
	return nil
}

// writeCsvRows writes each row as a csv row of its json encoded cells without quotes.
func writeCsvRows(rows [][]interface{}, writer *csv.Writer) error {
	for _, row := range rows {
		vals := make([]string, len(row))
		for k := range row {
			b, err := json.Marshal(row[k])
			if err != nil {
				return err
			}
			vals[k] = strings.Replace(string(b), "\"", "", -1)
		}
		if err := writer.Write(vals); err != nil {
			return err
		}
	}
	return nil
}
