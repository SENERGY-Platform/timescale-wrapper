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

package meta

import convmodel "github.com/SENERGY-Platform/converter/lib/model"

type DeviceTypeCommand struct {
	Command    Command    `json:"command"`
	Id         string     `json:"id"`
	Owner      string     `json:"owner"`
	DeviceType DeviceType `json:"device_type"`
}

type Command string

type DeviceType struct {
	Id            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Services      []Service `json:"services"`
	DeviceClassId string    `json:"device_class_id"`
	RdfType       string    `json:"rdf_type"`
}

type Attribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Service struct {
	Id          string      `json:"id"`
	LocalId     string      `json:"local_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Interaction Interaction `json:"interaction"`
	AspectIds   []string    `json:"aspect_ids"`
	ProtocolId  string      `json:"protocol_id"`
	Inputs      []Content   `json:"inputs"`
	Outputs     []Content   `json:"outputs"`
	FunctionIds []string    `json:"function_ids"`
	RdfType     string      `json:"rdf_type"`
	Attributes  []Attribute `json:"attributes"`
}

type Interaction string

type Content struct {
	Id                string          `json:"id"`
	ContentVariable   ContentVariable `json:"content_variable"`
	Serialization     string          `json:"serialization"`
	ProtocolSegmentId string          `json:"protocol_segment_id"`
}

type Type string

type ContentVariable struct {
	Id                   string            `json:"id"`
	Name                 string            `json:"name"`
	Type                 Type              `json:"type"`
	SubContentVariables  []ContentVariable `json:"sub_content_variables"`
	CharacteristicId     string            `json:"characteristic_id"`
	Value                interface{}       `json:"value"`
	SerializationOptions []string          `json:"serialization_options"`
	UnitReference        string            `json:"unit_reference,omitempty"`
}

type Envelope struct {
	DeviceId  string                 `json:"device_id,omitempty"`
	ServiceId string                 `json:"service_id,omitempty"`
	Value     map[string]interface{} `json:"value"`
}

type Concept struct {
	Id                   string                         `json:"id"`
	Name                 string                         `json:"name"`
	CharacteristicIds    []string                       `json:"characteristic_ids"`
	BaseCharacteristicId string                         `json:"base_characteristic_id"`
	Conversions          []convmodel.ConverterExtension `json:"conversions"`
}
