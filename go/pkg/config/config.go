/*
Copyright 2023 The Alibaba Cloud Serverless Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"time"
)

type Config struct {
	ClientAddr           string
	GcInterval           time.Duration
	IdleDurationBeforeGC time.Duration
	Feature              map[string]int
}

var DefaultConfig = Config{
	ClientAddr:           "127.0.0.1:50051",
	GcInterval:           500 * time.Millisecond,
	IdleDurationBeforeGC: 5 * time.Second, // original is 5 * time.Minute
	Feature: map[string]int{
		"nodes1":                      1,
		"roles1":                      1,
		"rolebindings1":               1,
		"certificatesigningrequests1": 1,
		"binding1":                    1,
		"csinodes1":                   1,
		"nodes2":                      2,
		"roles2":                      2,
		"rolebindings2":               2,
		"certificatesigningrequests2": 2,
		"binding2":                    2,
		"csinodes2":                   2,
	},
}
