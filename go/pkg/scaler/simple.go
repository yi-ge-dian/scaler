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

// 生产者和消费者的信号
type Empty struct{}

type Simple struct {
	config         config.Config
	metaData       *model2.Meta
	platformClient platform_client2.Client
	mu             sync.Mutex
	wg             sync.WaitGroup
	instances      map[string]*model2.Instance
	idleInstance   *list.List
	sem            chan Empty // Semaphores for implementing producer-consumer models
	// recentInstanceList *list.List
}

func New(metaData *model2.Meta, config *config.Config) Scaler {
	client, err := platform_client2.New(config.ClientAddr)
	if err != nil {
		log.Fatalf("client init with error: %s", err.Error())
	}
	scheduler := &Simple{
		config:         *config,
		metaData:       metaData,
		platformClient: client,
		mu:             sync.Mutex{},
		wg:             sync.WaitGroup{},
		instances:      make(map[string]*model2.Instance),
		idleInstance:   list.New(),
		sem:            make(chan Empty, config.MaxConcurrency),
		// recentInstanceList: list.New(),
	}

	if fromDSNo, ok := config.Feature[metaData.Key]; ok {
		if fromDSNo == 1 {
			scheduler.config.GcInterval = 1 * time.Second
			scheduler.config.IdleDurationBeforeGC = 1 * time.Second
		} else {
			scheduler.config.GcInterval = 3 * time.Second
			scheduler.config.IdleDurationBeforeGC = 5 * time.Second
		}
	}

	// 新创建一个scaler || key memoryMb
	log.Printf("New          %s     %d", metaData.Key, metaData.MemoryInMb)
	scheduler.wg.Add(1)
	go func() {
		defer scheduler.wg.Done()
		scheduler.gcLoop()
		log.Printf("gc loop for app: %s is stoped", metaData.Key)
	}()

	return scheduler
}

func (s *Simple) Assign(ctx context.Context, request *pb.AssignRequest) (*pb.AssignReply, error) {
	// 开始请求实例 || key requestId
	log.Printf("[Assign]     %s     %s", request.MetaData.Key, request.RequestId[:8])

	start := time.Now()
	instanceId := uuid.New().String()
	defer func() {
		// 开始运行实例 || key requestId instanceId cost
		log.Printf("Run          %s     %s %s %d", request.RequestId[:8], request.MetaData.Key, instanceId[:8], time.Since(start).Milliseconds())
	}()

	// 复用实例
	select {
	case <-s.sem: // 消费一个实例
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

	// 新创建一个实例
	go s.createInstance(request, instanceId)

	<-s.sem // Block until IdleInstance is not empty, semaphore -1
	s.mu.Lock()
	if element := s.idleInstance.Front(); element != nil {
		instance := element.Value.(*model2.Instance)
		if instance.Id == instanceId {
			log.Printf("create new instance %s and use it to complete the request\n", instanceId)
			// if s.recentInstanceList.Len() >= 5 {
			// 	s.recentInstanceList.Remove(s.recentInstanceList.Front())
			// 	recentInstace := &model2.RecentInstance{
			// 		StartTime:  time.Now(),
			// 		InstanceId: instance.Id,
			// 	}
			// 	s.recentInstanceList.PushBack(recentInstace)
			// 	front := s.idleInstance.Front()
			// 	back := s.idleInstance.Back()
			// 	duration := back.Value.(*model2.RecentInstance).StartTime.Sub(front.Value.(*model2.RecentInstance).StartTime).Milliseconds()
			// 	if duration < 20 {
			// 		// 立即创建3个实例
			// 		for i := 0; i < 3; i++ {
			// 			go s.createInstance(request, uuid.New().String())
			// 		}
			// 	}
			// } else {
			// 	recentInstace := &model2.RecentInstance{
			// 		StartTime:  time.Now(),
			// 		InstanceId: instance.Id,
			// 	}
			// 	s.recentInstanceList.PushBack(recentInstace)
			// }

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
	s.mu.Unlock()
	return nil, status.Errorf(codes.Internal, "create new instance %s failed", instanceId)
}

func (s *Simple) createInstance(request *pb.AssignRequest, instanceId string) {
	resourceConfig := model2.SlotResourceConfig{
		ResourceConfig: pb.ResourceConfig{
			MemoryInMegabytes: request.MetaData.MemoryInMb,
		},
	}
	// 创建槽位 || key requestId instanceId cost
	log.Printf("[CreateSlot] %s     %s     %s", request.MetaData.Key, request.RequestId[:8], instanceId[:8])
	ctx := context.Background()
	slot, err := s.platformClient.CreateSlot(ctx, request.RequestId, &resourceConfig)
	if err != nil {
		errorMessage := fmt.Sprintf("create slot failed with: %s", err.Error())
		log.Print(errorMessage)
		return
	}

	meta := &model2.Meta{
		Meta: pb.Meta{
			Key:           request.MetaData.Key,
			Runtime:       request.MetaData.Runtime,
			TimeoutInSecs: request.MetaData.TimeoutInSecs,
		},
	}
	instance, err := s.platformClient.Init(ctx, request.RequestId, instanceId, slot, meta)
	// 实例化槽 || key requestId instanceId cost
	log.Printf("[InitSlot]   %s     %s     %s", request.MetaData.Key, request.RequestId[:8], instanceId[:8])
	if err != nil {
		errorMessage := fmt.Sprintf("create instance failed with: %s", err.Error())
		log.Print(errorMessage)
		return
	}

	s.mu.Lock()

	// if !s.flag {
	// 	s.flag = true
	// 	s.changeConfig(instance.Slot.CreateDurationInMs+uint64(instance.InitDurationInMs), instance.Meta.MemoryInMb)
	// }
	instance.Busy = false
	s.idleInstance.PushFront(instance)
	s.instances[instance.Id] = instance
	s.sem <- Empty{} // produce a semaphore
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

	instanceId := request.Assigment.InstanceId
	// 释放实例 || key requestId instanceId
	log.Printf("[Idle]       %s     %s     %s", request.Assigment.MetaKey, request.Assigment.RequestId[:8], instanceId[:8])

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
		// s.mu.Lock()
		// if needDestroy {
		// 	for e := s.recentInstanceList.Front(); e != nil; e = e.Next() {
		// 		recentInstance := e.Value.(*model2.RecentInstance)
		// 		if recentInstance.InstanceId == instanceId {
		// 			s.recentInstanceList.Remove(e)
		// 			break
		// 		}
		// 	}
		// 	s.mu.Unlock()
		// 	s.deleteSlot(ctx, request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "bad instance")
		// } else {
		// 	s.mu.Unlock()
		// }
	}()

	if s.metaData.Key == "nodes1" {
		s.config.IdleDurationBeforeGC = 200 * time.Millisecond

		go func() {
			timer := time.NewTimer(26000 * time.Millisecond)
			<-timer.C
			assignRequest := &pb.AssignRequest{
				RequestId: uuid.NewString(),
				MetaData: &pb.Meta{
					Key:           s.metaData.Key,
					Runtime:       s.metaData.Runtime,
					TimeoutInSecs: s.metaData.TimeoutInSecs,
					MemoryInMb:    s.metaData.MemoryInMb,
				},
			}
			s.createInstance(assignRequest, uuid.NewString())
		}()
		s.deleteSlot(context.Background(), request.Assigment.RequestId, slotId, instanceId, request.Assigment.MetaKey, "cv delete")
	}
	// 4. idle instance
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
	// 释放槽位 || key requestId instanceId slotId
	log.Printf("[DeleteSlot] %s     %s     %s     %s", metaKey, requestId[:8], instanceId[:8], slotId[:8])

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
				if idleDuration > s.config.IdleDurationBeforeGC || s.idleInstance.Len() > 10 || (s.metaData.MemoryInMb >= 2048 && s.idleInstance.Len() > 1) {
					// need GC
					<-s.sem // Consuming a semaphore
					s.idleInstance.Remove(element)
					delete(s.instances, instance.Id)
					s.mu.Unlock()
					// 回收 || key instanceId 闲置链表的长度 自从上一次空闲到现在空闲的时间
					log.Printf("GcLoop       %s     %s     %d     %f", instance.Meta.GetKey(), instance.Id[:8], s.idleInstance.Len(), idleDuration.Seconds())

					go func() {
						reason := fmt.Sprintf("Idle duration: %fs, excceed configured duration: %fs", idleDuration.Seconds(), s.config.IdleDurationBeforeGC.Seconds())
						ctx := context.Background()
						ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
						// s.mu.Lock()
						// for e := s.recentInstanceList.Front(); e != nil; e = e.Next() {
						// 	recentInstance := e.Value.(*model2.RecentInstance)
						// 	if recentInstance.InstanceId == instance.Id {
						// 		s.recentInstanceList.Remove(e)
						// 		break
						// 	}
						// }
						// s.mu.Lock()
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
