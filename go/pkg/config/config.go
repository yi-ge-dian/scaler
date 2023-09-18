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

package config2

import (
	"time"
)

type DataSetInfo struct {
	No                    int     // 序号
	ExecutionDurationInMs float32 // 执行时间
	InitDurationInMs      int64   // 初始化时间
	MemoryInMb            int64   // 内存
}

// certificatesigningrequests1    29.958949
// csinodes1                      30.000000
// nodes1                         56.263234
// rolebindings1                  28.868354
// roles1                         29.578194
// {"key": "nodes1", "runtime": "go", "timeoutInSecs": 627, "memoryInMb": 512, "initDurationInMs": 49}
// {"key": "roles1", "runtime": "go", "timeoutInSecs": 129, "memoryInMb": 256, "initDurationInMs": 56}
// {"key": "rolebindings1", "runtime": "go", "timeoutInSecs": 491, "memoryInMb": 256, "initDurationInMs": 13}
// {"key": "certificatesigningrequests1", "runtime": "go", "timeoutInSecs": 541, "memoryInMb": 256, "initDurationInMs": 20}
// {"key": "binding1", "runtime": "go", "timeoutInSecs": 885, "memoryInMb": 256, "initDurationInMs": 48}
// {"key": "csinodes1", "runtime": "go", "timeoutInSecs": 366, "memoryInMb": 512, "initDurationInMs": 28}

// binding2                       16.928627
// certificatesigningrequests2    19.727118
// nodes2                         36.614693
// rolebindings2                  18.454575
// roles2                         19.762240
// {"key": "nodes2", "runtime": "go", "timeoutInSecs": 627, "memoryInMb": 512, "initDurationInMs": 49}
// {"key": "roles2", "runtime": "go", "timeoutInSecs": 129, "memoryInMb": 256, "initDurationInMs": 56}
// {"key": "rolebindings2", "runtime": "go", "timeoutInSecs": 491, "memoryInMb": 256, "initDurationInMs": 13}
// {"key": "certificatesigningrequests2", "runtime": "go", "timeoutInSecs": 541, "memoryInMb": 256, "initDurationInMs": 20}
// {"key": "binding2", "runtime": "go", "timeoutInSecs": 885, "memoryInMb": 256, "initDurationInMs": 48}
// {"key": "csinodes2", "runtime": "go", "timeoutInSecs": 366, "memoryInMb": 512, "initDurationInMs": 28}
var InfoMap = map[string]*DataSetInfo{
	"certificatesigningrequests1": {
		No:                    1,
		ExecutionDurationInMs: 29.958949,
		InitDurationInMs:      20,
		MemoryInMb:            256,
	},
	"csinodes1": {
		No:                    1,
		ExecutionDurationInMs: 30.000000,
		InitDurationInMs:      28,
		MemoryInMb:            512,
	},
	"nodes1": {
		No:                    1,
		ExecutionDurationInMs: 56.263234,
		InitDurationInMs:      49,
		MemoryInMb:            512,
	},
	"rolebindings1": {
		No:                    1,
		ExecutionDurationInMs: 28.868354,
		InitDurationInMs:      13,
		MemoryInMb:            256,
	},
	"roles1": {
		No:                    1,
		ExecutionDurationInMs: 29.578194,
		InitDurationInMs:      56,
		MemoryInMb:            256,
	},
	"binding1": {
		No:                    1,
		ExecutionDurationInMs: 0.00,
		InitDurationInMs:      48,
		MemoryInMb:            256,
	},
	"bingding2": {
		No:                    2,
		ExecutionDurationInMs: 16.928627,
		InitDurationInMs:      48,
		MemoryInMb:            256,
	},
	"certificatesigningrequests2": {
		No:                    2,
		ExecutionDurationInMs: 19.727118,
		InitDurationInMs:      20,
		MemoryInMb:            256,
	},
	"nodes2": {
		No:                    2,
		ExecutionDurationInMs: 36.614693,
		InitDurationInMs:      49,
		MemoryInMb:            512,
	},
	"rolebindings2": {
		No:                    2,
		ExecutionDurationInMs: 18.454575,
		InitDurationInMs:      13,
		MemoryInMb:            256,
	},
	"roles2": {
		No:                    2,
		ExecutionDurationInMs: 19.762240,
		InitDurationInMs:      56,
		MemoryInMb:            256,
	},
	"csinodes2": {
		No:                    2,
		ExecutionDurationInMs: 0.00,
		InitDurationInMs:      28,
		MemoryInMb:            512,
	},
}

type Config struct {
	ClientAddr           string
	GcInterval           time.Duration
	IdleDurationBeforeGC time.Duration
}

// todo : 调节数据集3的参数
var DefaultConfig = Config{
	ClientAddr:           "127.0.0.1:50051",
	GcInterval:           1 * time.Microsecond,
	IdleDurationBeforeGC: 5 * time.Second, // original is 5 * time.Minute
}
