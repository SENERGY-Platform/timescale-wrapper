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
	serving "github.com/SENERGY-Platform/analytics-serving/client"
	importRepo "github.com/SENERGY-Platform/import-repository/lib/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx"
)

const wrapperMaterializedViewPrefix = "_wmv_"
const wrapperMaterializedViewProcedureName = "ts_wrapper_refresh_mat_view"

type Wrapper struct {
	config           configuration.Config
	pool             *pgx.ConnPool
	importRepoClient importRepo.Interface
	servingClient    *serving.Client
}
