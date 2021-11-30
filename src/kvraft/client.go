package kvraft

import (
	"../labrpc"
	"fmt"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
	"time"
)
import "crypto/rand"
import "math/big"

type Clerk struct {
	servers []*labrpc.ClientEnd

	mu            sync.Mutex
	nextCommandId int
	// You will have to modify this struct.
	me uuid.UUID
}
type CommandId struct{
	ClerkId uuid.UUID
	Command int
}

func (ckId *CommandId) String() string{
	return fmt.Sprintf(ckId.ClerkId.String() + "%v", ckId.Command)
}

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	_, _ = DPrintf("new clerk is created.")
	ck := new(Clerk)
	ck.servers = servers


	ck.me= uuid.New()
	ck.nextCommandId = 0
	ck.mu = sync.Mutex{}
	// You'll have to add code here.
	return ck
}

//
// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) Get(key string) string {

	commandId := ck.getCurrentId()

	result := ""
	var error Err
	var replyServer int

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(1)
	var count int32
	count = 0
	for i := 0; i < len(ck.servers); i++ {

		j := i
		go func() {
			for atomic.LoadInt32(&count) != 1{
				args := GetArgs{
					Id:  commandId,
					Key: key,
				}
				reply := GetReply{}
				ok := ck.servers[j].Call("KVServer.Get", &args, &reply)
				if ok && (reply.Err == OK || reply.Err == ErrNoKey) {
					if atomic.CompareAndSwapInt32(&count, 0, 1) {
						result = reply.Value
						error = reply.Err
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
	_, _ = DPrintf("command id: %v server %v get %v return %v err %v ",commandId, replyServer, key, result, error)
	return result

}

//
// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
//
func (ck *Clerk) PutAppend(key string, value string, op string) {
	commandId := ck.getCurrentId()
	var replyServer int

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(1)

	var count int32
	count = 0
	for i := 0; i < len(ck.servers); i++{
		j := i
		go func() {
			for count != 1{
				args := PutAppendArgs{
					Id:  commandId,
					Key: key,
					Value: value,
					Op: op,
				}
				reply := PutAppendReply{}
				ok := ck.servers[j].Call("KVServer.PutAppend", &args, &reply)
				if ok && reply.Err == OK {
					if atomic.CompareAndSwapInt32(&count, 0, 1) {
						waitGroup.Done()
					}
					replyServer = j
					return
				} else{
					time.Sleep(200 * time.Millisecond)
				}
			}
		}()
	}
	waitGroup.Wait()
	_, _ = DPrintf("command id: %v server %v put key %v value %v  ",commandId, replyServer, key, value)

}

func (ck *Clerk) getCurrentId() CommandId{
	ck.mu.Lock()
	commandId := ck.nextCommandId
	ck.nextCommandId += 1
	ck.mu.Unlock()

	id := CommandId{
		ClerkId:   ck.me,
		Command: commandId,
	}
	return id
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}