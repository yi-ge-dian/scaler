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

package model

import (
	"encoding/csv"
	"io"
	"log"
	"os"
	"time"

	pb "github.com/AliyunContainerService/scaler/proto"
	"github.com/jszwec/csvutil"
)

type Meta struct {
	pb.Meta
}

type Instance struct {
	Id               string
	Slot             *Slot
	Meta             *Meta
	CreateTimeInMs   int64
	InitDurationInMs int64
	Busy             bool
	LastIdleTime     time.Time
}

type IdleTimeStats struct {
	MetaKey          string  `csv:"metaKey"`
	InitDurationInMs int     `csv:"initDurationInMs"`
	Count            float64 `csv:"count"`
	P25              float64 `csv:"25%"`
	P50              float64 `csv:"50%"`
	P75              float64 `csv:"75%"`
	P99              float64 `csv:"99%"`
	Cv               float64 `csv:"cv"`
	DurationsInMs    int     `csv:"durationsInMs"`
}

type OfflineMeta struct {
	IsHighConcurrency       bool    // 是否为高并发
	Cv                      float64 // CV 值
	NeedNumber              uint64  // 高并发的情况下，需要的实例数量
	HighConcurrencyDuration uint64  // 高并发的持续时间，100（slot）+ 1000 容错 + 初始化 + 最大执行时间， or 就简单的 gcInterval
	InitDurationInMs        uint64
	P25                     float64
	P50                     float64
	P75                     float64
	P99                     float64
}

// NewOfflineMeta returns a new offline meta map.
func NewOfflineMeta() map[string]*OfflineMeta {
	offlineMetaMap := make(map[string]*OfflineMeta)

	csvFile, err := os.Open("./idle_time_stats.csv")
	if err != nil {
		log.Fatal("0 ", err)
	}
	defer csvFile.Close()

	csvReader := csv.NewReader(csvFile)
	dec, err := csvutil.NewDecoder(csvReader)
	if err != nil {
		log.Fatal("1 ", err)
	}
	_ = dec.Header()
	// log.Println(dec.Header())

	for {
		u := IdleTimeStats{}
		if err := dec.Decode(&u); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal("2 ", err)
		}
		// log.Println(u)
		// 是不是高并发
		if u.P25 == 0 && u.P50 == 0 && u.P75 == 0 {
			offlineMetaMap[u.MetaKey] = &OfflineMeta{
				IsHighConcurrency:       true,
				NeedNumber:              uint64(u.Count) / 2,
				HighConcurrencyDuration: 100 + 1000 + uint64(u.InitDurationInMs) + uint64(u.DurationsInMs),
			}
		} else {
			offlineMetaMap[u.MetaKey] = &OfflineMeta{
				IsHighConcurrency: false,
				InitDurationInMs:  uint64(u.InitDurationInMs),
				Cv:                u.Cv,
				P25:               u.P25,
				P50:               u.P50,
				P75:               u.P75,
				P99:               u.P99,
			}
		}
	}
	return offlineMetaMap
}
