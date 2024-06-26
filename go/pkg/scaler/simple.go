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

package scaler

import (
	"container/list"
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/AliyunContainerService/scaler/go/pkg/config"
	model2 "github.com/AliyunContainerService/scaler/go/pkg/model"
	platform_client2 "github.com/AliyunContainerService/scaler/go/pkg/platform_client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/AliyunContainerService/scaler/proto"
	"github.com/google/uuid"
)

type Empty struct{}

type Simple struct {
	config                   *config.Config              // config
	metaData                 *model2.Meta                // meta data
	platformClient           platform_client2.Client     // interface to call simulator
	mu                       sync.Mutex                  // lock
	wg                       sync.WaitGroup              // wait group
	instances                map[string]*model2.Instance // instanceId => instance
	idleInstance             *list.List                  // idle instance list
	sem                      chan Empty                  // semaphores for implementing producer-consumer models
	isOfflineHighConcurrency bool                        // 是否为离线高并发
	offlineMeta              *model2.OfflineMeta         // 离线元数据
	offlineMu                sync.RWMutex                // 离线元数据锁
}

func New(metaData *model2.Meta, config *config.Config, offline map[string]*model2.OfflineMeta) Scaler {
	client, err := platform_client2.New(config.ClientAddr)
	if err != nil {
		log.Fatalf("client init with error: %s", err.Error())
	}
	// new scaler
	s := &Simple{
		config:                   config,                                  // default config
		metaData:                 metaData,                                // request metadata
		platformClient:           client,                                  // platform client
		mu:                       sync.Mutex{},                            // lock
		wg:                       sync.WaitGroup{},                        // wait group
		instances:                make(map[string]*model2.Instance),       // instanceId => instance
		idleInstance:             list.New(),                              // idle instance list
		sem:                      make(chan Empty, config.MaxConcurrency), // semaphores for implementing producer-consumer models
		isOfflineHighConcurrency: false,                                   // 是否为离线高并发
		offlineMeta:              nil,
		offlineMu:                sync.RWMutex{},
	}

	// 如果是离线类型，我们需要判断是不是高并发
	if offlineMeta, ok := offline[metaData.Key]; ok {
		log.Println("是离线类型, key是" + metaData.Key)
		s.offlineMeta = offlineMeta
		// 如果是高并发，在第一次来的时候快速创建很多，后面这个类型的不会再来很多了，自己来手动 gc，
		// 如果又来了几个了呢？
		// 过一小段时间后，我们把这个离线的flag改成false
		// **** (todo)如果是这个情况呢？  1  11111  1， 我们可能需要一个ticker来重置计数
		// **** 但是这种情况好像不会发生
		if offlineMeta.IsHighConcurrency {
			s.isOfflineHighConcurrency = true
			// after a period of time, cancel the flag
			go func() {
				ticker := time.NewTicker(time.Duration(s.offlineMeta.HighConcurrencyDuration))
				<-ticker.C
				s.offlineMu.Lock()
				s.isOfflineHighConcurrency = false
				s.offlineMu.Unlock()
				log.Print("离线高并发模式结束")
			}()
		} else {
			//TODO: 改一下idle_time_stats.csv，bins=200,重写cv
			s.isOfflineHighConcurrency = false
			// keep alive 计算： 取得IT的分位数 25% 75%； 75%为keep alive结束时间，25%为keep alive开始时间
			keep_alive_min := time.Duration(s.offlineMeta.P25) * time.Millisecond
			keep_alive_max := time.Duration(s.offlineMeta.P75) * time.Millisecond
			keep_alive := keep_alive_max - keep_alive_min + time.Duration(1+rand.Intn(10))*time.Millisecond
			// 75% < ( init + 100 ) * 2, idle = 75% * 2,prewarm =0
			if keep_alive_max < (100*time.Millisecond+time.Duration(s.offlineMeta.InitDurationInMs)*time.Millisecond)*2 {
				s.config.IdleDurationBeforeGC = keep_alive_max * 2
				s.config.PreWarm = 0
			} else {
				if s.offlineMeta.Cv > 10 {
					// pre warm 计算： 100ms(slot create) + initTime = pre warm 窗口；
					// keep alive min - pre warm 窗口 - random (1,10)ms  = pre warm 开始时间
					pre_warm_start := keep_alive_min -
						(100*time.Millisecond + time.Duration(s.offlineMeta.InitDurationInMs)*time.Millisecond) -
						(time.Duration(1+rand.Intn(10)) * time.Millisecond)
					s.config.PreWarm = pre_warm_start
					s.config.IdleDurationBeforeGC = keep_alive
				} else {
					// default config
				}
			}

			// 修改config，采用 gc Loop，如何修改，应该看图用 CV 值，pre warm 和 keep alive 的情况下直接看图可以得到
			// todo
		}
	} else {
		// 如果不是 offline 类型 ，采用在线算法
		// 1. 是不是高并发，在第一次来的时候快速创建很多，自己手动 gc
		// 2. 修改config，采用 gc Loop，如何修改，每隔一段请求去算 CV 值，pre warm 和 keep alive 的情况下，采用在线算法
	}

	// 我们是不是还应该考虑一下内存和初始化的关系

	log.Printf("New scaler for app: %s is created", metaData.Key)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.gcLoop()
		log.Printf("gc loop for app: %s is stoped", metaData.Key)
	}()

	return s
}

func (s *Simple) Assign(ctx context.Context, request *pb.AssignRequest) (*pb.AssignReply, error) {
	start := time.Now()
	instanceId := uuid.New().String()
	defer func() {
		log.Printf("Assign, request id: %s, instance id: %s, cost %dms", request.RequestId, instanceId, time.Since(start).Milliseconds())
	}()
	log.Printf("Assign, request id: %s", request.RequestId)

	// reuse instance
	select {
	case <-s.sem: // consume a semaphore
		s.mu.Lock()
		if element := s.idleInstance.Front(); element != nil {
			instance := element.Value.(*model2.Instance)
			instance.Busy = true
			s.idleInstance.Remove(element)
			s.mu.Unlock()
			log.Printf("Assign, request id: %s, instance %s reused", request.RequestId, instance.Id)
			instanceId = instance.Id
			return &pb.AssignReply{
				Status: pb.Status_Ok,
				Assigment: &pb.Assignment{
					RequestId:  request.RequestId,
					MetaKey:    instance.Meta.Key,
					InstanceId: instance.Id,
				},
				ErrorMessage: nil,
			}, nil
		}
		s.mu.Unlock()
	default:
	}

	// 2. create new Instance
	go s.createInstance(request, instanceId)

	// 3. hign concurrent create
	s.offlineMu.RLock()
	if s.isOfflineHighConcurrency {
		s.offlineMu.RUnlock()
		for i := 0; i < int(s.offlineMeta.NeedNumber); i++ {
			go s.createInstance(request, uuid.New().String())
		}
	} else {
		s.offlineMu.RUnlock()
	}

	<-s.sem // Block until IdleInstance is not empty, semaphore -1

	s.mu.Lock() // Lock to prevent two requests from accessing the same instance at the same time
	if element := s.idleInstance.Front(); element != nil {
		instance := element.Value.(*model2.Instance)
		if instance.Id == instanceId {
			log.Printf("create new instance %s and use it to complete the request\n", instanceId)
		} else {
			log.Printf("Assign, request id: %s, instance %s reused(wait instance)\n", request.RequestId, instance.Id)
		}
		instance.Busy = true
		s.idleInstance.Remove(element)
		s.mu.Unlock()
		return &pb.AssignReply{
			Status: pb.Status_Ok,
			Assigment: &pb.Assignment{
				RequestId:  request.RequestId,
				MetaKey:    instance.Meta.Key,
				InstanceId: instance.Id,
			},
			ErrorMessage: nil,
		}, nil
	}
	return nil, status.Errorf(codes.Internal, "create new instance %s failed", instanceId)
}

func (s *Simple) createInstance(request *pb.AssignRequest, instanceId string) {
	// 1. create slot
	resourceConfig := model2.SlotResourceConfig{
		ResourceConfig: pb.ResourceConfig{
			MemoryInMegabytes: request.MetaData.MemoryInMb,
		},
	}
	ctx := context.Background()
	slot, err := s.platformClient.CreateSlot(ctx, request.RequestId, &resourceConfig)
	if err != nil {
		errorMessage := fmt.Sprintf("create slot failed with: %s", err.Error())
		log.Print(errorMessage)
		return
	}

	// 2. init instance
	meta := &model2.Meta{
		Meta: pb.Meta{
			Key:           request.MetaData.Key,
			Runtime:       request.MetaData.Runtime,
			TimeoutInSecs: request.MetaData.TimeoutInSecs,
			MemoryInMb:    request.MetaData.MemoryInMb,
		},
	}
	instance, err := s.platformClient.Init(ctx, request.RequestId, instanceId, slot, meta)
	if err != nil {
		errorMessage := fmt.Sprintf("create instance failed with: %s", err.Error())
		log.Print(errorMessage)
		return
	}
	s.mu.Lock()

	instance.Busy = false
	s.idleInstance.PushFront(instance)
	s.instances[instance.Id] = instance
	s.sem <- Empty{} // produce a semaphore
	log.Printf("create new instance %s success by request %s\n", instanceId, request.RequestId)
	s.mu.Unlock()
}

func (s *Simple) Idle(ctx context.Context, request *pb.IdleRequest) (*pb.IdleReply, error) {
	// 1. check request
	if request.Assigment == nil {
		return nil, status.Errorf(codes.InvalidArgument, "assignment is nil")
	}

	// 2. get instanceId from request assignment
	reply := &pb.IdleReply{
		Status:       pb.Status_Ok,
		ErrorMessage: nil,
	}
	start := time.Now()
	instanceId := request.Assigment.InstanceId
	defer func() {
		log.Printf("Idle, request id: %s, instance: %s, cost %dus", request.Assigment.RequestId, instanceId, time.Since(start).Microseconds())
	}()

	// 3. check instance whether need destroy
	needDestroy := false
	slotId := ""
	if request.Result != nil && request.Result.NeedDestroy != nil && *request.Result.NeedDestroy {
		needDestroy = true
	}
	defer func() {
		if needDestroy {
			s.deleteSlot(ctx, request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "bad instance")
		}
	}()

	// 4. idle instance
	log.Printf("Idle, request id: %s", request.Assigment.RequestId)
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance := s.instances[instanceId]; instance != nil {
		slotId = instance.Slot.Id
		instance.LastIdleTime = time.Now()
		// 4.1 if need destroy, then delete slot at defer function
		if needDestroy {
			log.Printf("request id %s, instance %s need be destroy", request.Assigment.RequestId, instanceId)
			return reply, nil
		}

		// 4.2 if not need destroy, the instance has been freed
		if !instance.Busy {
			log.Printf("request id %s, instance %s already freed", request.Assigment.RequestId, instanceId)
			return reply, nil
		}

		// 4.3 offline concurrency gc
		if s.isOfflineHighConcurrency {
			delete(s.instances, instance.Id)
			go func() {
				reason := ("offline concurrency gc")
				ctx := context.Background()
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				s.deleteSlot(ctx, uuid.NewString(), instance.Slot.Id, instance.Id, instance.Meta.Key, reason)
			}()
			return reply, nil
		}

		// 4.4 CV
		if s.config.PreWarm != 0 {
			delete(s.instances, instance.Id)
			go func() {
				reason := ("offline cv gc")
				ctx := context.Background()
				ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				s.deleteSlot(ctx, uuid.NewString(), instance.Slot.Id, instance.Id, instance.Meta.Key, reason)
				ticker := time.NewTicker(s.config.PreWarm)
				<-ticker.C
				assignRequest := &pb.AssignRequest{
					RequestId: uuid.New().String(),
					MetaData: &pb.Meta{
						Key:           s.metaData.Key,
						Runtime:       s.metaData.Runtime,
						TimeoutInSecs: s.metaData.TimeoutInSecs,
						MemoryInMb:    s.metaData.MemoryInMb,
					},
				}
				s.createInstance(assignRequest, uuid.NewString())
			}()
		}

		// 4.5 if not need destroy, add it to idle list
		instance.Busy = false
		s.idleInstance.PushFront(instance)
		s.sem <- Empty{} // Idle list gets longer, semaphore + 1
	} else {
		return nil, status.Errorf(codes.NotFound, fmt.Sprintf("request id %s, instance %s not found", request.Assigment.RequestId, instanceId))
	}

	// 5. return reply
	return &pb.IdleReply{
		Status:       pb.Status_Ok,
		ErrorMessage: nil,
	}, nil
}

func (s *Simple) deleteSlot(ctx context.Context, requestId, slotId, instanceId, metaKey, reason string) {
	log.Printf("start delete Instance %s (Slot: %s) of app: %s,reason %s,request: %s", instanceId, slotId, metaKey, reason, requestId)
	if err := s.platformClient.DestroySLot(ctx, requestId, slotId, reason); err != nil {
		log.Printf("delete Instance %s (Slot: %s) of app: %s failed with: %s", instanceId, slotId, metaKey, err.Error())
	}
}

func (s *Simple) gcLoop() {
	log.Printf("gc loop for app: %s is started", s.metaData.Key)
	ticker := time.NewTicker(s.config.GcInterval)
	for range ticker.C {
		for {
			s.mu.Lock()
			if element := s.idleInstance.Back(); element != nil {
				instance := element.Value.(*model2.Instance)
				idleDuration := time.Since(instance.LastIdleTime)
				if idleDuration > s.config.IdleDurationBeforeGC {
					//need GC
					<-s.sem // Consuming a semaphore
					s.idleInstance.Remove(element)
					delete(s.instances, instance.Id)
					s.mu.Unlock()
					go func() {
						reason := fmt.Sprintf("Idle duration: %fs, excceed configured duration: %fs", idleDuration.Seconds(), s.config.IdleDurationBeforeGC.Seconds())
						ctx := context.Background()
						ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						s.deleteSlot(ctx, uuid.NewString(), instance.Slot.Id, instance.Id, instance.Meta.Key, reason)
					}()

					continue
				}
			}
			s.mu.Unlock()
			break
		}
	}
}

func (s *Simple) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		TotalInstance:     len(s.instances),
		TotalIdleInstance: s.idleInstance.Len(),
	}
}

// todo: 每隔 100 个请求，看用了多长时间，如果小于了 执行时间 ms，就批量创建 100 个, 来个 flag，是这个类型，在 Idle 中做flag，自己手动gc。
// todo: 如果 大于了 512 ms ，就用 CV 判断一下是否具有典型性，具有典型，走 pre-warm 和 keep-alive. CV =2
// 偏差 / 均值
// 如果 大于 1ms 小于 512ms，需不需要考虑 init 和 mem

// func (s *Simple) changeConfig(init uint64, memsize uint64) {

// 	// var priority = float32(init) / float32(memsize)
// 	// if priority <= 1 {
// 	// 	s.config.IdleDurationBeforeGC = 30 * time.Second
// 	// } else if priority > 1 && priority <= 10 {
// 	// 	s.config.IdleDurationBeforeGC = 35 * time.Second
// 	// } else if priority > 10 && priority <= 50 {
// 	// 	s.config.IdleDurationBeforeGC = 40 * time.Minute
// 	// } else if priority > 50 && priority <= 100 {
// 	// 	s.config.IdleDurationBeforeGC = 1 * time.Minute
// 	// }

// 	// if init < 200 && memsize > 1000 {
// 	// 	s.config.GcInterval = time.Second * 5
// 	// 	s.config.IdleDurationBeforeGC = time.Second * 10
// 	// }

// 	if init <= 512 && memsize > 1000 {
// 		s.config.GcInterval = time.Second * 3
// 		s.config.IdleDurationBeforeGC = time.Second * 5
// 	}

// 	// } else if init >= 200 && init < 1000 {
// 	// 	s.config.GcInterval = time.Second * 5
// 	// 	s.config.IdleDurationBeforeGC = time.Second * 5
// 	// } else if init >= 1000 && init < 10000 {
// 	// 	s.config.GcInterval = time.Second * 5
// 	// 	s.config.IdleDurationBeforeGC = time.Second * 15
// 	// } else if init >= 10000 && init < 60000 {
// 	// 	s.config.GcInterval = time.Second * 10
// 	// 	s.config.IdleDurationBeforeGC = time.Second * 10000
// 	// }
// }
