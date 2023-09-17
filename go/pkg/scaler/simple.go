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
	"sync"
	"time"

	config2 "github.com/AliyunContainerService/scaler/go/pkg/config"
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
	config             config2.Config
	metaData           *model2.Meta
	platformClient     platform_client2.Client
	mu                 sync.Mutex
	wg                 sync.WaitGroup
	instances          map[string]*model2.Instance
	idleInstance       *list.List
	sem                chan Empty // Semaphores for implementing producer-consumer models
	dataSetInfo        *config2.DataSetInfo
	// recentInstanceList *list.List
	// flag           bool
}

func New(metaData *model2.Meta, config *config2.Config) Scaler {
	client, _ := platform_client2.New(config.ClientAddr)
	scheduler := &Simple{
		config:             *config,
		metaData:           metaData,
		platformClient:     client,
		mu:                 sync.Mutex{},
		wg:                 sync.WaitGroup{},
		instances:          make(map[string]*model2.Instance),
		idleInstance:       list.New(),
		sem:                make(chan Empty, config.MaxConcurrency),
		dataSetInfo:        nil,
		// recentInstanceList: list.New(),
		// flag:           false,
	}

	if dataSetInfo, ok := config2.InfoMap[metaData.Key]; ok {
		scheduler.dataSetInfo = dataSetInfo
		switch dataSetInfo.No {
		case 1:
			scheduler.config.GcInterval = 1 * time.Second
			scheduler.config.IdleDurationBeforeGC = 1 * time.Second
			// todo: 调节数据集2的参数
		case 2:
			scheduler.config.GcInterval = 10 * time.Second
			scheduler.config.IdleDurationBeforeGC = 40 * time.Second
		default:
		}
	}

	scheduler.wg.Add(1)
	go func() {
		defer scheduler.wg.Done()
		scheduler.gcLoop()
	}()

	return scheduler
}

func (s *Simple) Assign(ctx context.Context, request *pb.AssignRequest) (*pb.AssignReply, error) {
	instanceId := uuid.New().String()

	// 复用实例
	select {
	case <-s.sem: // 消费一个实例
		s.mu.Lock()
		if element := s.idleInstance.Front(); element != nil {
			instance := element.Value.(*model2.Instance)
			instance.Busy = true
			s.idleInstance.Remove(element)
			s.mu.Unlock()
			// instanceId = instance.Id
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
		// 实例是新创建的
		// if instance.Id == instanceId {
		// 	if s.recentInstanceList.Len() >= 5 {
		// 		s.recentInstanceList.Remove(s.recentInstanceList.Front())
		// 		recentInstace := &model2.RecentInstance{
		// 			StartTime:  time.Now(),
		// 			InstanceId: instance.Id,
		// 		}
		// 		s.recentInstanceList.PushBack(recentInstace)
		// 		front := s.idleInstance.Front()
		// 		back := s.idleInstance.Back()
		// 		duration := back.Value.(*model2.RecentInstance).StartTime.Sub(front.Value.(*model2.RecentInstance).StartTime).Milliseconds()
		// 		if duration < 3000 {
		// 			// 立即创建 3 个实例
		// 			for i := 0; i < 3; i++ {
		// 				go s.createInstance(request, uuid.New().String())
		// 			}
		// 		}
		// 	} else {
		// 		recentInstace := &model2.RecentInstance{
		// 			StartTime:  time.Now(),
		// 			InstanceId: instance.Id,
		// 		}
		// 		s.recentInstanceList.PushBack(recentInstace)
		// 	}
		// }
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
	ctx := context.Background()
	slot, err := s.platformClient.CreateSlot(ctx, request.RequestId, &resourceConfig)
	if err != nil {
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
	if err != nil {
		return
	}

	s.mu.Lock()

	// if !s.flag && s.DataSetInfo == nil {
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

	// 4. idle instance
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance := s.instances[instanceId]; instance != nil {
		slotId = instance.Slot.Id
		instance.LastIdleTime = time.Now()
		// 4.1 if need destroy, then delete slot at defer function
		if needDestroy {
			return reply, nil
		}

		// 4.2 if not need destroy, the instance has been freed
		if !instance.Busy {
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
	s.platformClient.DestroySLot(ctx, requestId, slotId, reason)
}

func (s *Simple) gcLoop() {
	ticker := time.NewTicker(s.config.GcInterval)
	for range ticker.C {
		for {
			s.mu.Lock()
			if element := s.idleInstance.Back(); element != nil {
				instance := element.Value.(*model2.Instance)
				idleDuration := time.Since(instance.LastIdleTime)
				if idleDuration > s.config.IdleDurationBeforeGC || s.idleInstance.Len() > 5 || (s.metaData.MemoryInMb >= 2048 && s.idleInstance.Len() > 1) {
					// need GC
					<-s.sem // Consuming a semaphore
					s.idleInstance.Remove(element)
					delete(s.instances, instance.Id)
					s.mu.Unlock()

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
						// s.mu.Unlock()
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

// func (s *Simple) changeConfig(init uint64, memsize uint64) {

// }
