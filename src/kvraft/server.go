package kvraft

import (
	"../labgob"
	"../labrpc"
	"../raft"
	"bytes"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type Command struct {
	Id     CommandId
	Result string
	Type   CommandType
	Key    string
	Value  string

	Error Err
}

type Snapshot struct {
	KvMap map[string]string
	LastCommittedCommandCount map[string]int
}

const CommandTimeOut = time.Second * 5

var mu = sync.Mutex{}

const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		mu.Lock()
		log.Printf(format, a...)
		mu.Unlock()
	}
	return
}

type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	kvMap      map[string]string

	lastCommittedCommandCount map[string]int

	maxraftstate int // snapshot if log grows this big

	snapshotLock int32

	// Your definitions here.
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	_, _ = DPrintf("KV server %v is called get", kv.me)
	kv.mu.Lock()
	var command Command
	lastCommitCommand, ok := kv.lastCommittedCommandCount[args.Id.ClerkId.String()]
	// not committed
	if !ok || lastCommitCommand < args.Id.Command {
		command = Command{
			Id:     args.Id,
			Result: "",
			Type:   GetCommand,
			Key:    args.Key,
			Error:  "",
		}
		_, _, isLeader := kv.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			reply.Value = ""
			kv.mu.Unlock()
			return
		}
	}
	kv.mu.Unlock()
	// check if committed
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {
		time.Sleep(10 * time.Millisecond)
		kv.mu.Lock()
		lastCommitCommand := kv.getLastCommitCommandCount(args.Id.ClerkId.String())
		if lastCommitCommand >= args.Id.Command{
			result, ok := kv.kvMap[args.Key]
			if !ok {
				reply.Value = ""
				reply.Err = ErrNoKey
				kv.mu.Unlock()
				return
			}
			reply.Value = result
			reply.Err = OK
			kv.mu.Unlock()
			return
		}
		kv.mu.Unlock()
	}
	reply.Value = ""
	reply.Err = ErrTimeOut
	return
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	var command Command
	lastCommitCommand, ok := kv.lastCommittedCommandCount[args.Id.ClerkId.String()]

	// not committed
	if !ok || lastCommitCommand < args.Id.Command {
		command = Command{
			Id:     args.Id,
			Result: "",
			Key:    args.Key,
			Value: args.Value,
			Error:  "",
		}
		if args.Op == "Put" {
			command.Type = PutCommand
		} else if args.Op == "Append" {
			command.Type = AppendCommand
		} else {
			panic("Cannot recognize command")
		}
		_, _, isLeader := kv.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			kv.mu.Unlock()
			return
		}
	}
	kv.mu.Unlock()
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {

		time.Sleep(50 * time.Millisecond)
		kv.mu.Lock()
		lastCommitCommand := kv.getLastCommitCommandCount(args.Id.ClerkId.String())
		if lastCommitCommand >= args.Id.Command{
			reply.Err = OK
			kv.mu.Unlock()
			return
		}
		kv.mu.Unlock()
	}
	reply.Err = ErrTimeOut
	return
}

func (kv *KVServer) checkApplyCommand() {

	for !kv.killed() {
		message := <-kv.applyCh

		if !message.CommandValid{
			kv.mu.Lock()
			snapshot := kv.decodeState(message.Command.([]byte))
			kv.kvMap = snapshot.KvMap
			kv.lastCommittedCommandCount = snapshot.LastCommittedCommandCount
			kv.mu.Unlock()
			continue
		}

		command := message.Command.(Command)
		kv.mu.Lock()

		commandId := command.Id
		lastCommand := kv.getLastCommitCommandCount(commandId.ClerkId.String())
		if lastCommand >= commandId.Command {
			kv.mu.Unlock()
			continue
		}

		if command.Type == GetCommand{

		}else if command.Type == PutCommand{
			kv.kvMap[command.Key] = command.Value
		}else if command.Type == AppendCommand{
			value, ok := kv.kvMap[command.Key]
			if !ok {
				value = command.Value
			} else {
				value = value + command.Value
			}
			command.Error = OK
			kv.kvMap[command.Key] = value
		}else{
			panic("Cannot recognize the command")
		}
		kv.lastCommittedCommandCount[commandId.ClerkId.String()] = commandId.Command

		if kv.maxraftstate > 0 && kv.rf.GetLogLength() >= kv.maxraftstate{
			if kv.canSnapshot(){
				state := kv.encodeState()
				go func() {
					kv.rf.SaveSnapshot(message.CommandIndex, message.Term,state)
					kv.releaseSnapshotLock()
				}()
			}
		}
		_, _ = DPrintf("server: %v apply command Id: %v command Type: %v command Key: %v command Value: %v, result is: %v",
			kv.me, command.Id, command.Type, command.Key, command.Value, command.Result)

		kv.mu.Unlock()
	}
}

func (kv *KVServer) startCheckApplyMessageThread() {
	go func() {
		kv.checkApplyCommand()
	}()
}

func (kv *KVServer) getLastCommitCommandCount(clerkId string) int{
	commandCount, ok := kv.lastCommittedCommandCount[clerkId]
	if !ok{
		return -1
	}
	return commandCount
}

//
// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
//
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

func (kv *KVServer) isLeader() bool {
	_, isLeader := kv.rf.GetState()
	return isLeader
}

func (kv *KVServer) canSnapshot() bool{
	return atomic.CompareAndSwapInt32(&kv.snapshotLock, 0, 1)
}

func (kv *KVServer) releaseSnapshotLock() {
	atomic.StoreInt32(&kv.snapshotLock, 0)
}

func (kv *KVServer) lock(methodName string){
	DPrintf(" KVserver %v method %v lock", kv.me, methodName)
	kv.mu.Lock()
}
func (kv *KVServer) unlock(methodName string){
	DPrintf(" KVserver %v method %v unlock", kv.me, methodName)
	kv.mu.Unlock()
}
func (kv *KVServer) encodeState() []byte{
	snapshot := Snapshot{
		KvMap:                     kv.kvMap,
		LastCommittedCommandCount: kv.lastCommittedCommandCount,
	}

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if e.Encode(snapshot) != nil{
		panic("snapshot encoding problem")
	}
	data := w.Bytes()
	return data
}

func (kv *KVServer) decodeState(data []byte) Snapshot{
	if len(data) == 0{
		return Snapshot{
			KvMap:                     make(map[string]string),
			LastCommittedCommandCount: make(map[string]int),
		}
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var snapshot Snapshot
	err := d.Decode(&snapshot)
	if err != nil{
		panic(fmt.Sprintf("%v kv server decode state exception: %v", kv.me, err))
	}else{
		return snapshot
	}
}
//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Command{})
	labgob.Register(CommandId{})
	labgob.Register([]byte{})
	kv := new(KVServer)
	atomic.StoreInt32(&kv.dead, 0)
	atomic.StoreInt32(&kv.snapshotLock, 0)
	kv.me = me

	kv.maxraftstate = maxraftstate
	kv.mu = sync.Mutex{}

	kv.kvMap = make(map[string]string)
	kv.lastCommittedCommandCount = make(map[string]int)


	// You may need initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg, 100)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	// You may need initialization code here.
	if maxraftstate > 0 && persister.SnapshotSize() > 0{
		snapshot := kv.decodeState(kv.rf.GetKVSnapshotByte())
		kv.kvMap = snapshot.KvMap
		kv.lastCommittedCommandCount = snapshot.LastCommittedCommandCount
	}
	kv.startCheckApplyMessageThread()

	return kv
}
