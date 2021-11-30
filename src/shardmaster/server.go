package shardmaster


import (
	"../raft"
	"bytes"
	"fmt"
	"log"
	"sort"
	"sync/atomic"
	"time"
)
import "../labrpc"
import "sync"
import "../labgob"

const MaxLogSize = -1
type Snapshot struct {
	Configs []Config
	GroupMap map[int][]string
	Shards [NShards]int
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



type ShardMaster struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	configs []Config // indexed by config num

	// Your data here.
	dead    int32 // set by Kill()

	groupMap map[int][]string
	shards [NShards]int

	lastCommittedCommandCount map[string]int
	snapshotLock int32
}


type Op struct {
	// Your data here.
}



func (sm *ShardMaster) restoreSnapshot(snapshot Snapshot){
	sm.configs = snapshot.Configs
	sm.groupMap = snapshot.GroupMap
	sm.shards = snapshot.Shards
	sm.lastCommittedCommandCount = snapshot.LastCommittedCommandCount
}

func (sm *ShardMaster) checkApplyCommand() {

	for !sm.killed() {
		message := <-sm.applyCh
		if !message.CommandValid{
			sm.mu.Lock()
			snapshot := sm.decodeState(message.Command.([]byte))
			sm.restoreSnapshot(snapshot)
			sm.mu.Unlock()
			continue
		}
		command := message.Command.(Command)
		id := command.Id
		sm.mu.Lock()

		if sm.isCommandCommitted(id){
			sm.mu.Unlock()
			continue
		}

		if command.Type == JoinCommand {
			sm.handleJoin(command.Args.(JoinArgs).Servers)
		}else if command.Type == LeaveCommand {
			sm.handleLeave(command.Args.(LeaveArgs).GIDs)
		}else if command.Type == QueryCommand {
			// do nothing
		}else if command.Type == MoveCommand {
			moveArgs := command.Args.(MoveArgs)
			sm.handleMove(moveArgs.Shard, moveArgs.GID)
		} else{
			panic("Cannot recognize the command")
		}
		sm.updateCommittedCommandMap(id.ClerkId.String(), id.Command)

		if sm.shouldMakeSnapshot(){
			if sm.requireSnapshotLock(){
				state := sm.encodeState()
				go func() {
					sm.rf.SaveSnapshot(message.CommandIndex, message.Term,state)
					sm.releaseSnapshotLock()
				}()
			}
		}
		_, _ = DPrintf("server: %v apply command Id: %v command Type: %v command : %v last state: %v", sm.me, command.Id, command.Type, command.Args, sm.getConfigAtN(sm.getLastConfigNumber()))

		sm.mu.Unlock()
	}
}

func (sm *ShardMaster) startCheckApplyMessageThread() {
	go func() {
		sm.checkApplyCommand()
	}()
}

func (sm *ShardMaster) getLastCommitCommandCount(clerkId string) int{
	commandCount, ok := sm.lastCommittedCommandCount[clerkId]
	if !ok{
		return -1
	}
	return commandCount
}

func (sm *ShardMaster) redistributeShard(){
	groupN := len(sm.groupMap)

	if groupN == 0{
		for j := 0; j < len(sm.shards); j++{
			sm.shards[j] = 0
		}
		return
	}


	// make sure the shard is unique by fixes loop order of keys
	i := 0
	var keys []int
	for gid, _ := range sm.groupMap{
		keys = append(keys, gid)
	}
	sort.Ints(keys)

	shardPerGroup := len(sm.shards) / groupN
	lastGid := 0
	for _, gid := range keys{
		lastGid = gid
		for j := 0; j < shardPerGroup; j++{
			sm.shards[i] = gid
			i += 1
		}
	}
	sm.shards[len(sm.shards) - 1] = lastGid
}
func (sm *ShardMaster) isCommandCommitted(Id CommandId) bool{
	lastCommitCommand, ok := sm.lastCommittedCommandCount[Id.ClerkId.String()]
	if ok && lastCommitCommand >= Id.Command {
		return true
	}
	return false
}

func (sm *ShardMaster) updateCommittedCommandMap(clerkId string, commandN int){
	v, ok := sm.lastCommittedCommandCount[clerkId]
	if ok && v >= commandN{
		panic("Should not update smaller index command number")
	}
	sm.lastCommittedCommandCount[clerkId] = commandN
}

func (sm *ShardMaster) generateConfig() Config{
	shards := [10]int{}
	for i := 0; i < len(sm.shards); i++{
		shards[i] = sm.shards[i]
	}
	groups := CopyGroupMap(sm.groupMap)
	newConfig := Config{
		Num:    sm.getLastConfigNumber() + 1,
		Shards: shards,
		Groups: groups,
	}
	return newConfig
}

func (sm *ShardMaster) addNewConfig(newConfig Config){
	sm.configs = append(sm.configs, newConfig)

}
func (sm *ShardMaster) Join(args *JoinArgs, reply *JoinReply) {
	// Your code here.

	_, _ = DPrintf("master server %v is called Join", sm.me)
	sm.mu.Lock()
	var command Command
	Id := args.Id
	if !sm.isCommandCommitted(Id) {
		command = Command{
			Id:   args.Id,
			Type: JoinCommand,
			Args: *args,
		}
		_, _, isLeader := sm.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			reply.WrongLeader = true
			sm.mu.Unlock()
			return
		}
	}
	sm.mu.Unlock()
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {
		time.Sleep(10 * time.Millisecond)
		sm.mu.Lock()
		if sm.isCommandCommitted(Id){
			reply.WrongLeader = false
			reply.Err = OK
			sm.mu.Unlock()
			return
		}
		sm.mu.Unlock()
	}
	reply.Err = ErrTimeOut
}

func (sm *ShardMaster) handleJoin(newServers map[int][]string) {
	for gid, servers:= range newServers{
		_, ok := sm.groupMap[gid]
		if ok{
			panic("Gid already exist")
		}
		sm.groupMap[gid] = servers
	}
	sm.redistributeShard()
	newConfig := sm.generateConfig()
	sm.addNewConfig(newConfig)
}

func (sm *ShardMaster) handleLeave(leaveGid []int){
	for _, gid := range leaveGid{
		delete(sm.groupMap, gid)
	}
	sm.redistributeShard()
	newConfig := sm.generateConfig()
	sm.addNewConfig(newConfig)
}
func (sm *ShardMaster) handleQuery(N int) Config{
	if N < 0 || N > sm.getLastConfigNumber(){
		return sm.getConfigAtN(sm.getLastConfigNumber())
	}
	return sm.getConfigAtN(N)
}
func (sm *ShardMaster) getLastConfigNumber() int{
	return sm.configs[len(sm.configs) - 1].Num
}
func (sm *ShardMaster) getConfigAtN(N int) Config{
	return sm.configs[N]
}
func (sm *ShardMaster) handleMove(shardN int, gid int){
	_, ok := sm.groupMap[gid]
	if !ok{
		panic(fmt.Sprintf("No group with gid: %v, is found", gid))
	}
	sm.shards[shardN] = gid
	newConfig := sm.generateConfig()
	sm.addNewConfig(newConfig)
}

func (sm *ShardMaster) Leave(args *LeaveArgs, reply *LeaveReply) {
	_, _ = DPrintf("master server %v is called Leave", sm.me)
	sm.mu.Lock()
	var command Command
	Id := args.Id
	if !sm.isCommandCommitted(Id) {
		command = Command{
			Id:   args.Id,
			Type: LeaveCommand,
			Args: *args,
		}
		_, _, isLeader := sm.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			reply.WrongLeader = true
			sm.mu.Unlock()
			return
		}
	}
	sm.mu.Unlock()
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {
		time.Sleep(10 * time.Millisecond)
		sm.mu.Lock()
		if sm.isCommandCommitted(Id){
			reply.WrongLeader = false
			reply.Err = OK
			sm.mu.Unlock()
			return
		}
		sm.mu.Unlock()
	}
	reply.Err = ErrTimeOut
}

func (sm *ShardMaster) Move(args *MoveArgs, reply *MoveReply) {
	// Your code here.
	_, _ = DPrintf("master server %v is called Move", sm.me)
	sm.mu.Lock()
	var command Command
	Id := args.Id
	if !sm.isCommandCommitted(Id) {
		command = Command{
			Id:   args.Id,
			Type: MoveCommand,
			Args: *args,
		}
		_, _, isLeader := sm.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			reply.WrongLeader = true
			sm.mu.Unlock()
			return
		}
	}
	sm.mu.Unlock()
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {
		time.Sleep(10 * time.Millisecond)
		sm.mu.Lock()
		if sm.isCommandCommitted(Id){
			reply.WrongLeader = false
			reply.Err = OK
			sm.mu.Unlock()
			return
		}
		sm.mu.Unlock()
	}
	reply.Err = ErrTimeOut
}

func (sm *ShardMaster) Query(args *QueryArgs, reply *QueryReply) {
	sm.mu.Lock()
	var command Command
	Id := args.Id
	if !sm.isCommandCommitted(Id) {
		command = Command{
			Id:   args.Id,
			Type: QueryCommand,
			Args: *args,
		}
		_, _, isLeader := sm.rf.Start(command)
		if !isLeader {
			reply.Err = ErrWrongLeader
			reply.WrongLeader = true
			sm.mu.Unlock()
			return
		}
	}
	sm.mu.Unlock()
	currentTime := time.Now()
	for time.Now().Sub(currentTime) < CommandTimeOut {
		time.Sleep(10 * time.Millisecond)
		sm.mu.Lock()
		if sm.isCommandCommitted(Id){
			reply.WrongLeader = false
			reply.Err = OK
			reply.Config = sm.handleQuery(args.Num)
			sm.mu.Unlock()
			return
		}
		sm.mu.Unlock()
	}
	reply.Err = ErrTimeOut
}


//
// the tester calls Kill() when a ShardMaster instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (sm *ShardMaster) Kill() {
	atomic.StoreInt32(&sm.dead, 1)
	sm.rf.Kill()
	// Your code here, if desired.
}
func (sm *ShardMaster) killed() bool {
	z := atomic.LoadInt32(&sm.dead)
	return z == 1
}

// needed by shardkv tester
func (sm *ShardMaster) Raft() *raft.Raft {
	return sm.rf
}


func (sm *ShardMaster) isLeader() bool {
	_, isLeader := sm.rf.GetState()
	return isLeader
}

func (sm *ShardMaster) requireSnapshotLock() bool{
	return atomic.CompareAndSwapInt32(&sm.snapshotLock, 0, 1)
}

func (sm *ShardMaster) shouldMakeSnapshot() bool{
	return MaxLogSize > 0 && sm.rf.GetLogLength() >= MaxLogSize
}

func (sm *ShardMaster) releaseSnapshotLock() {
	atomic.StoreInt32(&sm.snapshotLock, 0)
}

func (sm *ShardMaster) lock(methodName string){
	DPrintf(" ShardMaster %v method %v lock", sm.me, methodName)
	sm.mu.Lock()
}
func (sm *ShardMaster) unlock(methodName string){
	DPrintf(" ShardMaster %v method %v unlock", sm.me, methodName)
	sm.mu.Unlock()
}
func (sm *ShardMaster) encodeState() []byte{
	snapshot := Snapshot{
		Configs:                   sm.configs,
		GroupMap:                  sm.groupMap,
		Shards:                    sm.shards,
		LastCommittedCommandCount: sm.lastCommittedCommandCount,
	}


	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if e.Encode(snapshot) != nil{
		panic("snapshot encoding problem")
	}
	data := w.Bytes()
	return data
}

func (sm *ShardMaster) decodeState(data []byte) Snapshot{
	if len(data) == 0{
		return Snapshot{
			Configs:                   []Config{},
			GroupMap:                  make(map[int][]string),
			Shards:                    [NShards]int{},
			LastCommittedCommandCount: make(map[string]int),
		}
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var snapshot Snapshot
	err := d.Decode(&snapshot)
	if err != nil{
		panic(fmt.Sprintf("%v kv server decode state exception: %v", sm.me, err))
	}else{
		return snapshot
	}
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant shardmaster service.
// me is the index of the current server in servers[].
//
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardMaster {
	labgob.Register(Command{})
	labgob.Register(CommandId{})
	labgob.Register(JoinArgs{})
	labgob.Register(JoinReply{})
	labgob.Register(LeaveArgs{})
	labgob.Register(LeaveReply{})
	labgob.Register(MoveArgs{})
	labgob.Register(MoveReply{})
	labgob.Register(QueryArgs{})
	labgob.Register(QueryReply{})
	labgob.Register([]byte{})
	labgob.Register(Op{})

	sm := new(ShardMaster)
	atomic.StoreInt32(&sm.dead, 0)
	atomic.StoreInt32(&sm.snapshotLock, 0)
	sm.me = me

	sm.configs = make([]Config, 1)
	sm.configs[0].Groups = map[int][]string{}
	sm.configs[0].Num = 0

	sm.mu = sync.Mutex{}

	sm.groupMap = make(map[int] []string)
	sm.shards = [NShards]int{}
	sm.lastCommittedCommandCount = make(map[string]int)

	sm.applyCh = make(chan raft.ApplyMsg, 10)
	sm.rf = raft.Make(servers, me, persister, sm.applyCh)

	if MaxLogSize > 0 && persister.SnapshotSize() > 0{
		snapshot := sm.decodeState(sm.rf.GetKVSnapshotByte())
		sm.restoreSnapshot(snapshot)
	}
	sm.startCheckApplyMessageThread()
	// Your code here.

	return sm
}
