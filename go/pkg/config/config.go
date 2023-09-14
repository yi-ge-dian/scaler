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
	"math"
	"time"
)

type Trait struct {
	DataSetNo        int
	ExecDurationInMs int64
	InitDurationInMs int64
}

type Config struct {
	ClientAddr           string
	GcInterval           time.Duration
	IdleDurationBeforeGC time.Duration
	MaxConcurrency       int
	Feature              map[string]Trait
}

var traitsMap = map[string]Trait{
	"nodes2": {
		DataSetNo:        2, // 你的数据集编号
		ExecDurationInMs: 37,
		InitDurationInMs: 49,
	},
	"roles2": {
		DataSetNo:        2, // 你的数据集编号
		ExecDurationInMs: 20,
		InitDurationInMs: 56,
	},
	"rolebindings2": {
		DataSetNo:        2, // 你的数据集编号
		ExecDurationInMs: 19,
		InitDurationInMs: 13,
	},
	"certificatesigningrequests2": {
		DataSetNo:        2, // 你的数据集编号
		ExecDurationInMs: 20,
		InitDurationInMs: 20,
	},
	"binding2": {
		DataSetNo:        2, // 你的数据集编号
		ExecDurationInMs: 17,
		InitDurationInMs: 48,
	},
	"nodes1": {
		DataSetNo:        1, // 你的数据集编号
		ExecDurationInMs: 57,
		InitDurationInMs: 49,
	},
	"roles1": {
		DataSetNo:        1, // 你的数据集编号
		ExecDurationInMs: 30,
		InitDurationInMs: 56,
	},
	"rolebindings1": {
		DataSetNo:        1, // 你的数据集编号
		ExecDurationInMs: 29,
		InitDurationInMs: 13,
	},
	"certificatesigningrequests1": {
		DataSetNo:        1, // 你的数据集编号
		ExecDurationInMs: 30,
		InitDurationInMs: 20,
	},
	"csinodes1": {
		DataSetNo:        1, // 你的数据集编号
		ExecDurationInMs: 30,
		InitDurationInMs: 28,
	},
}

var DefaultConfig = Config{
	ClientAddr:           "127.0.0.1:50051",
	GcInterval:           3 * time.Second,
	IdleDurationBeforeGC: 5 * time.Second, // original is 5 * time.Minute
	MaxConcurrency:       math.MaxInt32,
	Feature:              traitsMap,
}
