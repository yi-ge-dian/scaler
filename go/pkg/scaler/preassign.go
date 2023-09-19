package scaler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AliyunContainerService/scaler/go/pkg/model"
	pb "github.com/AliyunContainerService/scaler/proto"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DataSet struct {
	No               uint8  // 数据集编号
	Num              uint8  // 需要提前分配的数量
	MemoryInMb       uint32 // 每个数据集的内存大小
	ExecDurationInMs uint32 // 执行时间
	InitDurationInMs uint32 // 初始化时间
	IdleGurationInMs uint32 // 多久之后开始执行gc
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

var PreMap = map[string]*DataSet{
	"certificatesigningrequests1": {
		No:               1,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 30,
		InitDurationInMs: 20,
		IdleGurationInMs: 200,
	},
	"csinodes1": {
		No:               1,
		Num:              5,
		MemoryInMb:       512,
		ExecDurationInMs: 30,
		InitDurationInMs: 28,
		IdleGurationInMs: 5,
	},
	"nodes1": {
		No:               1,
		Num:              5,
		ExecDurationInMs: 56,
		InitDurationInMs: 49,
		MemoryInMb:       512,
		IdleGurationInMs: 5,
	},
	"rolebindings1": {
		No:               1,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 29,
		InitDurationInMs: 13,
		IdleGurationInMs: 120,
	},
	"roles1": {
		No:               1,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 30,
		InitDurationInMs: 56,
		IdleGurationInMs: 120,
	},
	// "binding1": {
	// 	No:               1,
	// 	Num:              0,
	// 	MemoryInMb:       256,
	// 	ExecDurationInMs: 0,
	// 	InitDurationInMs: 48,
	// 	IdleGurationInMs: 0,
	// },
	"bingding2": {
		No:               2,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 17,
		InitDurationInMs: 48,
		IdleGurationInMs: 200,
	},
	"certificatesigningrequests2": {
		No:               2,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 20,
		InitDurationInMs: 20,
		IdleGurationInMs: 200,
	},
	"nodes2": {
		No:               2,
		Num:              5,
		MemoryInMb:       512,
		ExecDurationInMs: 37,
		InitDurationInMs: 49,
		IdleGurationInMs: 37,
	},
	"rolebindings2": {
		No:               2,
		Num:              7,
		ExecDurationInMs: 18,
		InitDurationInMs: 13,
		MemoryInMb:       256,
		IdleGurationInMs: 200,
	},
	"roles2": {
		No:               2,
		Num:              7,
		MemoryInMb:       256,
		ExecDurationInMs: 20,
		InitDurationInMs: 56,
	},
	// "csinodes2": {
	// 	No:               2,
	// 	Num:              0,
	// 	MemoryInMb:       512,
	// 	ExecDurationInMs: 0,
	// 	InitDurationInMs: 28,
	// 	IdleGurationInMs: 0,
	// },
}

/************************************************************* batch pre assign *******************************************************************************/
func (s *Simple) BatchPreAssign(ctx context.Context, groupNum int) {
	for i := 0; i < groupNum; i++ {
		go s.PreAssign(ctx)
	}
}

func (s *Simple) PreAssign(ctx context.Context) error {
	instanceId := uuid.New().String()

	resourceConfig := model.SlotResourceConfig{
		ResourceConfig: pb.ResourceConfig{
			MemoryInMegabytes: s.metaData.GetMemoryInMb(),
		},
	}

	slot, err := s.platformClient.CreateSlot(ctx, uuid.NewString(), &resourceConfig)
	if err != nil {
		errorMessage := fmt.Sprintf("create slot failed with: %s", err.Error())
		return status.Errorf(codes.Internal, errorMessage)
	}

	meta := &model.Meta{
		Meta: pb.Meta{
			Key:           s.metaData.GetKey(),
			Runtime:       s.metaData.GetRuntime(),
			TimeoutInSecs: s.metaData.GetTimeoutInSecs(),
		},
	}

	instance, err := s.platformClient.Init(ctx, uuid.NewString(), instanceId, slot, meta)
	if err != nil {
		errorMessage := fmt.Sprintf("create instance failed with: %s", err.Error())
		log.Printf("create instance failed with: %s", err.Error())
		return status.Errorf(codes.Internal, errorMessage)
	}

	s.mu.Lock()
	instance.Busy = false
	s.instances[instance.Id] = instance
	s.idleInstance.PushFront(instance)
	s.mu.Unlock()

	return nil
}

/************************************************* gc pre assign for dataset1and2 *****************************************/
func (s *Simple) gcLoopPreAssignForDataSet1and2() {

	ticker := time.NewTicker(s.config.GcInterval)

	for range ticker.C {
		for {
			s.mu.Lock()
			if element := s.idleInstance.Back(); element != nil {
				instance := element.Value.(*model.Instance)
				idleDuration := time.Since(instance.LastIdleTime)
				if idleDuration > s.config.IdleDurationBeforeGC {
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
