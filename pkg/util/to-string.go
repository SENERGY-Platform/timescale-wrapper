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

package util

import (
	"errors"
	"strconv"
)

func String(val interface{}) (string, error) {
	str, valid := val.(string)
	if valid {
		return str, nil
	}
	flt, valid := val.(float64)
	if valid {
		return strconv.FormatFloat(flt, 'f', -1, 64), nil
	}
	in, valid := val.(int)
	if valid {
		return strconv.Itoa(in), nil
	}
	b, valid := val.(bool)
	if valid {
		return strconv.FormatBool(b), nil
	}
	return "", errors.New("could not convert to string")
}
