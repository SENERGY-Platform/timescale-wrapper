package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, RawValueEndpoint)
}

func RawValueEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, lastValueCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	handler := lastValueHandler(config, wrapper, verifier, lastValueCache, converter)
	router.GET("/raw-value", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		exportId := request.URL.Query().Get("export_id")
		deviceId := request.URL.Query().Get("device_id")
		serviceId := request.URL.Query().Get("service_id")
		math := request.URL.Query().Get("math")
		sourceCharacteristicId := request.URL.Query().Get("source_characteristic_id")
		targetCharacteristicId := request.URL.Query().Get("target_characteristic_id")
		conceptId := request.URL.Query().Get("concept_id")

		elem := model.LastValuesRequestElement{
			ColumnName: request.URL.Query().Get("column"),
		}
		if len(exportId) > 0 {
			elem.ExportId = &exportId
		}
		if len(deviceId) > 0 {
			elem.DeviceId = &deviceId
		}
		if len(serviceId) > 0 {
			elem.ServiceId = &serviceId
		}
		if len(math) > 0 {
			elem.Math = &math
		}
		if len(sourceCharacteristicId) > 0 {
			elem.SourceCharacteristicId = &sourceCharacteristicId
		}
		if len(targetCharacteristicId) > 0 {
			elem.TargetCharacteristicId = &targetCharacteristicId
		}
		if len(conceptId) > 0 {
			elem.ConceptId = &conceptId
		}

		b, err := json.Marshal([]model.LastValuesRequestElement{elem})
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		request.Body = io.NopCloser(bytes.NewBuffer(b))
		resp, code, err := handler(request, params)
		if err != nil {
			http.Error(writer, err.Error(), code)
			return
		}
		v := resp[0].Value
		switch v.(type) {
		case string:
			b = []byte(v.(string))
			break
		default:
			b, err = json.Marshal(v)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		_, err = writer.Write(b)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
