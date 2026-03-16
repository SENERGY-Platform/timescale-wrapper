package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-gonic/gin"
)

func init() {
	endpoints = append(endpoints, RawValueEndpoint)
}

// Query godoc
// @Summary      Raw Value
// @Produce      json
// @Security Bearer
// @Param		 export_id query string false "export_id"
// @Param		 device_id query string false "device_id"
// @Param		 service_id query string false "service_id"
// @Param		 column query string false "column"
// @Param		 source_characteristic_id query string false "source_characteristic_id"
// @Param		 target_characteristic_id query string false "target_characteristic_id"
// @Param		 concept_id query string false "concept_id"
// @Success      200 {object} any "the raw value"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /raw-value [GET]
func RawValueEndpoint(router gin.IRouter, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, lastValueCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	handler := lastValueHandler(config, wrapper, verifier, lastValueCache, converter)
	router.GET("/raw-value", func(c *gin.Context) {
		writer := c.Writer
		request := c.Request
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
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
		request.Body = io.NopCloser(bytes.NewBuffer(b))
		resp, code, err := handler(request)
		if err != nil {
			c.Error(errors.Join(err, model.GetError(code)))
			return
		}
		v := resp[0].Value
		switch v := v.(type) {
		case string:
			b = []byte(v)
		default:
			b, err = json.Marshal(v)
			if err != nil {
				c.Error(errors.Join(err, model.ErrInternalServerError))
				return
			}
		}
		_, err = writer.Write(b)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
