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

package pkg

import (
	"context"
	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/device-repository/lib/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api"
	cache "github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"sync"
)

func Start(ctx context.Context, config configuration.Config) (wg *sync.WaitGroup, err error) {
	wg = &sync.WaitGroup{}
	influxClient, err := timescale.NewWrapper(ctx, wg, config)
	if err != nil {
		return wg, err
	}
	verifier := verification.New(config)
	deviceRepoClient := client.NewClient(config.DeviceRepoUrl)
	lastValueCache := cache.NewRemote(config, deviceRepoClient)
	conv, err := converter.New()
	if err != nil {
		return wg, err
	}
	err = api.Start(ctx, wg, config, influxClient, verifier, lastValueCache, conv)
	return
}
