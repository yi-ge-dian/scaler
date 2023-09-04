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
	config         *config.Config              // config
	metaData       *model2.Meta                // meta data
	platformClient platform_client2.Client     // interface to call simulator
	mu             sync.Mutex                  // lock
	wg             sync.WaitGroup              // wait group
	instances      map[string]*model2.Instance // instanceId => instance
	idleInstance   *list.List                  // idle instance list
	sem            chan Empty                  // Semaphores for implementing producer-consumer models
	flag           bool
}

func New(metaData *model2.Meta, config *config.Config) Scaler {
	client, err := platform_client2.New(config.ClientAddr)
	if err != nil {
		log.Fatalf("client init with error: %s", err.Error())
	}
	scheduler := &Simple{
		config:         config,
		metaData:       metaData,
		platformClient: client,
		mu:             sync.Mutex{},
		wg:             sync.WaitGroup{},
		instances:      make(map[string]*model2.Instance),
		idleInstance:   list.New(),
		sem:            make(chan Empty, config.MaxConcurrency),
		flag:           false,
	}
	log.Printf("New scaler for app: %s is created", metaData.Key)
	scheduler.wg.Add(1)
	go func() {
		defer scheduler.wg.Done()
		scheduler.gcLoop()
		log.Printf("gc loop for app: %s is stoped", metaData.Key)
	}()

	return scheduler
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

	<-s.sem // Block until IdleInstance is not empty, semaphore -1

	s.mu.Lock() // Lock to prevent two requests from accessing the same instance at the same time
	if element := s.idleInstance.Front(); element != nil {
		instance := element.Value.(*model2.Instance)
		if instance.Id == instanceId {
			log.Printf("create new instance %s and use it to complete the request\n", instanceId)
		} else {
			log.Printf("Assign, request id: %s, instance %s reused(wait instance)", request.RequestId, instance.Id)
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
		},
	}
	instance, err := s.platformClient.Init(ctx, request.RequestId, instanceId, slot, meta)
	if err != nil {
		errorMessage := fmt.Sprintf("create instance failed with: %s", err.Error())
		log.Print(errorMessage)
		return
	}
	s.mu.Lock()
	if !s.flag {
		s.flag = true
		s.changeConfig(instance.Slot.CreateDurationInMs+uint64(instance.InitDurationInMs), instance.Meta.MemoryInMb)
	}

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

		// 4.3 if not need destroy, add it to idle list
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

func (s *Simple) changeConfig(init uint64, memsize uint64) {

	// var priority = float32(init) / float32(memsize)
	// if priority <= 1 {
	// 	s.config.IdleDurationBeforeGC = 30 * time.Second
	// } else if priority > 1 && priority <= 10 {
	// 	s.config.IdleDurationBeforeGC = 35 * time.Second
	// } else if priority > 10 && priority <= 50 {
	// 	s.config.IdleDurationBeforeGC = 40 * time.Minute
	// } else if priority > 50 && priority <= 100 {
	// 	s.config.IdleDurationBeforeGC = 1 * time.Minute
	// }

	// if init < 200 && memsize > 1000 {
	// 	s.config.GcInterval = time.Second * 5
	// 	s.config.IdleDurationBeforeGC = time.Second * 10
	// }

	if init <= 512 && memsize > 1000 {
		s.config.GcInterval = time.Second * 3
		s.config.IdleDurationBeforeGC = time.Second * 5
	}

	// } else if init >= 200 && init < 1000 {
	// 	s.config.GcInterval = time.Second * 5
	// 	s.config.IdleDurationBeforeGC = time.Second * 5
	// } else if init >= 1000 && init < 10000 {
	// 	s.config.GcInterval = time.Second * 5
	// 	s.config.IdleDurationBeforeGC = time.Second * 15
	// } else if init >= 10000 && init < 60000 {
	// 	s.config.GcInterval = time.Second * 10
	// 	s.config.IdleDurationBeforeGC = time.Second * 10000
	// }
}
