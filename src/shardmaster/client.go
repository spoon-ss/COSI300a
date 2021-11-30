package shardmaster

//
// Shardmaster clerk.
//

import (
	"../labrpc"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
)
import "time"
import "crypto/rand"
import "math/big"

type Clerk struct {
	servers []*labrpc.ClientEnd
	// Your data here.

	mu sync.Mutex
	nextCommandN int
	me uuid.UUID
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	_, _ = DPrintf("new clerk is created")


	ck := new(Clerk)
	ck.servers = servers

	ck.me = uuid.New()
	ck.nextCommandN = 0
	ck.mu = sync.Mutex{}
	// Your code here.
	return ck
}

func (ck *Clerk) getCurrentId() CommandId{
	ck.mu.Lock()
	commandN := ck.nextCommandN
	ck.nextCommandN += 1
	ck.mu.Unlock()

	id := CommandId{
		ClerkId: ck.me,
		Command: commandN,
	}
	return id
}
func (ck *Clerk) concurrentCall(RPCName string, args []interface{}, reply []interface{}) int{
	var replyServer int

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(1)

	var count int32
	count = 0
	for i := 0; i < len(ck.servers); i++{
		j := i
		go func() {
			for atomic.LoadInt32(&count) != 1{
				ok := ck.servers[j].Call(RPCName, args[j], reply[j])
				replyErrState := reply[j].(ErrState)
				if ok && replyErrState.GetErr() == OK{
					if atomic.CompareAndSwapInt32(&count, 0, 1){
						replyServer = j
						waitGroup.Done()
					}
					return
				}else{
					time.Sleep(200 * time.Millisecond)
				}
			}
		}()
	}
	waitGroup.Wait()
	return replyServer
}

func (ck *Clerk) Query(num int) Config {

	commandId := ck.getCurrentId()

	var argsArray []interface{}
	var replyArray  []interface{}

	for i := 0; i < len(ck.servers); i++{
		argsArray = append(argsArray, &QueryArgs{
			Id:  commandId,
			Num: num,
		})
		replyArray = append(replyArray, &QueryReply{})
	}
	replyServer := ck.concurrentCall("ShardMaster.Query", argsArray, replyArray)

	if replyArray == nil{
		panic("Nil pointer exception")
	}
	reply := replyArray[replyServer]
	DPrintf("Query %v get result %v", commandId.Command, reply.(*QueryReply).Config)
	return reply.(*QueryReply).Config
}
func (ck *Clerk) Join(servers map[int][]string) {
	commandId := ck.getCurrentId()

	var argsArray []interface{}
	var replyArray  []interface{}

	for i := 0; i < len(ck.servers); i++{
		argsArray = append(argsArray, &JoinArgs{
			Id:      commandId,
			Servers: CopyGroupMap(servers),
		})
		replyArray = append(replyArray, &JoinReply{})
	}
	ck.concurrentCall("ShardMaster.Join", argsArray, replyArray)
}

func (ck *Clerk) Leave(gids []int) {
	commandId := ck.getCurrentId()

	var argsArray []interface{}
	var replyArray  []interface{}

	for i := 0; i < len(ck.servers); i++{
		argsArray = append(argsArray, &LeaveArgs{
			Id:   commandId,
			GIDs: gids,
		})
		replyArray = append(replyArray, &LeaveReply{})
	}
	ck.concurrentCall("ShardMaster.Leave", argsArray, replyArray)
}

func (ck *Clerk) Move(shard int, gid int) {
	commandId := ck.getCurrentId()

	var argsArray []interface{}
	var replyArray  []interface{}

	for i := 0; i < len(ck.servers); i++{
		argsArray = append(argsArray, &MoveArgs{
			Id:    commandId,
			Shard: shard,
			GID:   gid,
		})
		replyArray = append(replyArray, &MoveReply{})
	}
	ck.concurrentCall("ShardMaster.Move", argsArray, replyArray)
}
