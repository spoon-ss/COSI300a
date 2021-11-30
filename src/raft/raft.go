package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)
import "sync/atomic"
import "../labrpc"
import "../labgob"

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
	Term int
}

type LogItem struct {
	Command interface{}
	Term    int
	Index 	int
}

type PersistState struct {
	Term int
	VoteFor int
	Log []LogItem
}

type SnapshotRaft struct{
	StateMachineState []byte
	LastIncludedIndex int
	LastIncludedTerm int
}

type InstallSnapshotArg struct{
	Term int
	LeaderId int
	LastIncludedIndex int
	LastIncludedTerm int
	Data []byte
}

type InstallSnapshotReply struct{
	Term int
}


func isMoreUpdated(term1, index1, term2, index2 int) bool {
	if term1 > term2 {
		return true
	} else if term1 == term2 && index1 >= index2 {
		return true
	} else {
		return false
	}

}

type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	applyCh   chan ApplyMsg
	me        int   // this peer's index into peers[]
	dead      int32 // set by Kill()

	state NodeState

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// persistent
	currentTerm int
	voteFor     int
	log         []LogItem

	// volatile
	commitIndex int
	lastApplied int

	// volatile leader
	nextIndex  []int
	matchIndex []int

	electionTimeStamp time.Time
	voteCount         int

	electionTimeout time.Duration
}

func (rf *Raft) getPreviousIndexForServer(i int) int {
	return rf.nextIndex[i] - 1
}

func (rf *Raft) getPreviousTermForServer(i int) int {
	return rf.getLogItemAtIndex(rf.nextIndex[i] - 1).Term
}

func (rf *Raft) getAppendLogItem(i int) []LogItem {
	var newLog []LogItem
	for i := rf.nextIndex[i]; i <= rf.getLastLogIndex(); i++ {
		newLog = append(newLog, rf.getLogItemAtIndex(i))
	}
	return newLog
}

func (rf *Raft) getLastLogTerm() int {
	return rf.getLogItemAtIndex(rf.getLastLogIndex()).Term
}

func (rf *Raft) getLastLogIndex() int {
	return rf.log[len(rf.log) - 1].Index
}

func (rf *Raft) getDummyLogIndex() int{
	return rf.log[0].Index
}

func (rf *Raft) getLogItemAtIndex(i int) LogItem{
	head := rf.log[0].Index
	// fmt.Printf(" get index at %v \n", i)
	if i < head || i > rf.getLastLogIndex(){
		panic("Index out of bound")
	}
	return rf.log[i - head]

}

func (rf *Raft) logContainIndex(i int) bool{
	return  rf.getDummyLogIndex() <= i && i <= rf.getLastLogIndex()
}

func (rf *Raft) getLogItemSliceInclusive(i, j int) []LogItem{
	head := rf.getDummyLogIndex()
	return rf.log[i - head : j - head + 1]

}

func (rf *Raft) getMajorityCount() int {
	return len(rf.peers)/2 + 1
}

func (rf *Raft) deleteLogFromIndexInclusive(i int) {
	length := i - rf.getDummyLogIndex()
	newLog := make([]LogItem, length)
	copy(newLog, rf.getLogItemSliceInclusive(rf.getDummyLogIndex(), i - 1))
	rf.log = newLog
}

func (rf *Raft) findMatch() int {
	match := make([]int, len(rf.matchIndex))
	copy(match, rf.matchIndex)
	sort.Ints(match)
	middle := match[len(match)/2]
	return middle
}

func (rf *Raft) refreshTimestamp(){
	rf.electionTimeStamp = time.Now()
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	// Your code here (2A).
	return rf.currentTerm, rf.state == Leader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:

	/*
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	state := PersistState{
		Term:    rf.currentTerm,
		VoteFor: rf.voteFor,
		Log:     rf.log,
	}
	_ = e.Encode(state)
	data := w.Bytes()
	 */

	data := rf.getPersistStateByte()
	rf.persister.SaveRaftState(data)
}

func (rf *Raft) getPersistStateByte() []byte{

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	state := PersistState{
		Term:    rf.currentTerm,
		VoteFor: rf.voteFor,
		Log:     rf.log,
	}
	if e.Encode(state) != nil{
		panic("Raft encode problem")
	}
	data := w.Bytes()
	return data
}

func (rf *Raft) getPersistSnapshotByte(lastIncludedIndex int, lastIncludedTerm int, kvSnapshot []byte) []byte{
	snapshot := SnapshotRaft{
		StateMachineState: kvSnapshot,
		LastIncludedIndex: lastIncludedIndex,
		LastIncludedTerm:  lastIncludedTerm,
	}

	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	if e.Encode(snapshot) != nil{
		panic("Raft Encode problem")
	}
	data := w.Bytes()
	return data
}
func (rf *Raft) getSnapshotRaftObject() SnapshotRaft{
	if rf.persister.SnapshotSize() > 0{
		data := rf.persister.ReadSnapshot()
		r := bytes.NewBuffer(data)
		d := labgob.NewDecoder(r)
		var snapshot SnapshotRaft
		if d.Decode(&snapshot) != nil{
			panic(fmt.Sprintf("%v server decode state exception", rf.me))
		}else{
			return snapshot
		}
	}
	panic("No snapshot is read")
}
//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	 r := bytes.NewBuffer(data)
	 d := labgob.NewDecoder(r)
	 var state PersistState
	 if d.Decode(&state) != nil{
	 	panic(fmt.Sprintf("%v server decode state exception", rf.me))
	 }else{
	 	rf.currentTerm = state.Term
	 	rf.voteFor = state.VoteFor
		rf.log = state.Log
	 }
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogItem
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

//
// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).

	rf.lock("RequestVote")
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		rf.unlock("RequestVote")
		return
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}

	if rf.voteFor == -1 || rf.voteFor == args.CandidateId {
		lastTerm := rf.getLastLogTerm()
		lastIndex := rf.getLastLogIndex()
		if isMoreUpdated(args.LastLogTerm, args.LastLogIndex, lastTerm, lastIndex) {
			rf.voteFor = args.CandidateId
			reply.Term = rf.currentTerm
			reply.VoteGranted = true
			rf.persist()
			rf.unlock("RequestVote")
			return
		}
	}
	reply.Term = rf.currentTerm
	reply.VoteGranted = false
	rf.persist()
	rf.unlock("RequestVote")
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {

	rf.lock("AppendEntries")
	if args.Term > rf.currentTerm {
		rf.becomeFollower(args.Term)
	}else if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		rf.unlock("AppendEntries")
		return
	}

	reply.Term = rf.currentTerm
	rf.electionTimeStamp = time.Now()

	if args.PrevLogIndex > rf.getLastLogIndex() || args.PrevLogIndex < rf.getDummyLogIndex(){
		reply.Success = false
		rf.persist()
		rf.unlock("AppendEntries")
		return
	}

	previousItem := rf.getLogItemAtIndex(args.PrevLogIndex)

	if previousItem.Term != args.PrevLogTerm {
		rf.deleteLogFromIndexInclusive(args.PrevLogIndex)
		reply.Success = false
		rf.persist()
		rf.unlock("AppendEntries")
		return
	}else{
		rf.deleteLogFromIndexInclusive(args.PrevLogIndex + 1)
	}

	for _, v := range args.Entries {
		rf.log = append(rf.log, v)
	}
	reply.Success = true

	if args.LeaderCommit > rf.commitIndex {
		lastCommit := rf.commitIndex
		rf.commitIndex = Min(rf.getLastLogIndex(), args.LeaderCommit)
		rf.sendCommitMessage(lastCommit+1, rf.commitIndex)
		rf.lastApplied = Max(rf.commitIndex, rf.lastApplied)
	}
	DPrintf("%v server log state is: %v+", rf.me, rf.log)

	//DPrintf("%v server commit state is: %v", rf.me, rf.commitIndex)

	rf.persist()
	rf.unlock("AppendEntries")
}

func (rf *Raft) InstallSnapshot(arg *InstallSnapshotArg, reply *InstallSnapshotReply){
	rf.lock("InstallSnapshot")
	if rf.currentTerm > arg.Term{
		reply.Term = rf.currentTerm
		rf.unlock("InstallSnapshot")
		return
	}
	if rf.currentTerm < arg.Term{
		rf.becomeFollower(arg.Term)
	}
	// skip step 6

	rf.rebuildLogFromSnapshot(arg.LastIncludedIndex, arg.LastIncludedTerm, arg.Data)
	rf.persister.SaveStateAndSnapshot(rf.getPersistStateByte(),
		rf.getPersistSnapshotByte(arg.LastIncludedIndex, arg.LastIncludedTerm, arg.Data))
	rf.unlock("InstallSnapshot")
}

func (rf *Raft) rebuildLogFromSnapshot(lastIndex, lastTerm int, data []byte){
	dummyItem := LogItem{
		Command: "dummy node",
		Term:    lastTerm,
		Index:   lastIndex,
	}
	newLog := []LogItem{dummyItem}
	rf.log = newLog
	rf.commitIndex = lastIndex
	rf.applyCh <- ApplyMsg{
		CommandValid: false,
		Command:      data,
		CommandIndex: -1,
		Term:         -1,
	}
}

func (rf *Raft) becomeFollower(term int) {
	rf.state = Follower
	rf.currentTerm = term
	rf.voteCount = 0
	rf.voteFor = -1
	rf.refreshTimestamp()
}
func (rf *Raft) becomeCandidate() {
	rf.state = Candidate
	rf.voteFor = rf.me
	rf.voteCount = 1
	rf.currentTerm += 1
	rf.refreshTimestamp()
}
func (rf *Raft) becomeLeader() {
	rf.state = Leader
	rf.voteCount = 0
	rf.matchIndex = []int{}
	rf.nextIndex = []int{}
	for i := 0; i < len(rf.peers); i++ {
		rf.nextIndex = append(rf.nextIndex, rf.getLastLogIndex()+1)
		rf.matchIndex = append(rf.matchIndex, 0)
	}
	rf.refreshTimestamp()
}

func (rf *Raft) startElection() {
	for !rf.killed() {
		time.Sleep(time.Millisecond * time.Duration(50))
		CPrintf("raft %v do election check before lock", rf.me)
		rf.lock( "startElection")
		CPrintf("raft %v do election check", rf.me)
		if time.Now().Sub(rf.electionTimeStamp) < rf.randomElectionTimeout() {
			rf.unlock("startElection")
			continue
		}

		rf.becomeCandidate()
		rf.persist()
		for i := 0; i < len(rf.peers); i++ {
			j := i
			if j == rf.me {
				continue
			}
			args := &RequestVoteArgs{
				Term:         rf.currentTerm,
				CandidateId:  rf.me,
				LastLogIndex: rf.getLastLogIndex(),
				LastLogTerm:  rf.getLastLogTerm(),
			}
			reply := &RequestVoteReply{}
			go func() {
				ok := rf.sendRequestVote(j, args, reply)
				if !ok {
					return
				}
				rf.lock("startElection")
				if reply.Term < rf.currentTerm{
					rf.unlock( "startElection")
					return
				}
				if reply.Term > rf.currentTerm {
					rf.becomeFollower(reply.Term)
					rf.persist()
					rf.unlock("startElection")
					return
				}

				if reply.VoteGranted {
					rf.voteCount += 1
				}
				if rf.state != Leader && rf.voteCount >= rf.getMajorityCount() {
					rf.becomeLeader()
					rf.persist()
				}
				rf.unlock("startElection")
			}()
		}
		rf.unlock("startElection")
	}
}
func (rf *Raft) startAppend() {
	for !rf.killed() {
		time.Sleep(time.Millisecond * 100)

		rf.lock( "startAppend")
		CPrintf("raft %v do start append check", rf.me)
		if rf.state != Leader {
			rf.unlock( "startAppend")
			continue
		}
		for i, _ := range rf.peers {
			if i == rf.me {
				continue
			}
			if rf.getPreviousIndexForServer(i) < rf.getDummyLogIndex(){
				continue
			}

			args := &AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: rf.getPreviousIndexForServer(i),
				PrevLogTerm:  rf.getPreviousTermForServer(i),
				Entries:      rf.getAppendLogItem(i),
				LeaderCommit: rf.commitIndex,
			}
			reply := &AppendEntriesReply{}
			j := i
			go func() {
				ok := rf.sendAppend(j, args, reply)
				if !ok {
					return
				}
				rf.lock("start Append")

				if reply.Term > rf.currentTerm {
					rf.becomeFollower(reply.Term)
					rf.persist()
					rf.unlock( "startAppend")
					return
				}
				rf.refreshTimestamp()
				if reply.Term < rf.currentTerm {
					rf.unlock( "startAppend")
					return
				}
				if reply.Success {
					rf.nextIndex[j] = args.PrevLogIndex + len(args.Entries) + 1
					rf.matchIndex[j] = args.PrevLogIndex + len(args.Entries)
					newCommit := rf.findMatch()
					if rf.getLastLogTerm() == rf.currentTerm {
						lastCommit := rf.commitIndex
						rf.commitIndex = Max(rf.commitIndex, newCommit)
						rf.sendCommitMessage(lastCommit+1, rf.commitIndex)
						rf.lastApplied = rf.commitIndex
					}
				} else {
					if args.PrevLogIndex == rf.getDummyLogIndex(){
						rf.nextIndex[j] = rf.getDummyLogIndex()
					}else{
						rf.nextIndex[j] = Max(args.PrevLogIndex - len(args.Entries), rf.getDummyLogIndex() + 1)
					}
				}
				rf.unlock("startAppend")
			}()
		}
		rf.unlock("startAppend")
	}

}

func (rf *Raft) startInstallSnapshot(){

	for i := 0; i < len(rf.peers); i++{
		if i == rf.me{
			continue
		}
		go func(server int) {
			for !rf.killed(){
				time.Sleep(200 * time.Millisecond)
				rf.lock("startInstallSnapshot")
				CPrintf("raft %v do install snapshot check", rf.me)
				if rf.state != Leader{
					rf.unlock("startInstallSnapshot")
					continue
				}
				previous := rf.getPreviousIndexForServer(server)
				DPrintf("server %v preivous index is %v, dummy index is %v", server, previous, rf.getDummyLogIndex())
				if previous >= rf.getDummyLogIndex(){

					rf.unlock("startInstallSnapshot")
					continue
				}
				snapshotRaft := rf.getSnapshotRaftObject()
				args := InstallSnapshotArg{
					Term:              rf.currentTerm,
					LeaderId:          rf.me,
					LastIncludedIndex: snapshotRaft.LastIncludedIndex,
					LastIncludedTerm:  snapshotRaft.LastIncludedTerm,
					Data:              snapshotRaft.StateMachineState,
				}
				reply := InstallSnapshotReply{}

				rf.unlock("startInstallSnapshot")
				ok := rf.sendInstallSnapshot(server, &args, &reply)
				if !ok{
					continue
				}

				rf.lock("startInstallSnapshot")
				if rf.state != Leader || reply.Term < rf.currentTerm{
					rf.unlock( "startInstallSnapshot")
					continue
				}
				if reply.Term > rf.currentTerm{
					rf.becomeFollower(reply.Term)
					rf.persist()
					rf.unlock( "startInstallSnapshot")
					continue
				}
				rf.nextIndex[server] = args.LastIncludedIndex + 1
				rf.matchIndex[server] = args.LastIncludedIndex
				rf.unlock( "startInstallSnapshot")
			}
		}(i)
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	DPrintf("%v send a RequestVote to %v\n Args: %+v \n", rf.me, server, args)

	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if ok{
		 DPrintf("%v receive a RequestVote from %v \n Reply: %+v\n", rf.me, server, reply)
	}else{
		DPrintf("%v cannot receive a  RequestVote from %v \n Reply: %+v\n", rf.me, server, reply)

	}

	return ok
}


func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArg, reply *InstallSnapshotReply) bool {
	DPrintf("%v send a InstallSnapshot to %v\n Args: %+v \n", rf.me, server, args)

	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	if ok{
		DPrintf("%v receive a InstallSnapshot from %v \n Reply: %+v\n", rf.me, server, reply)
	}else{
		DPrintf("%v cannot receive a InstallSnapshot from %v \n Reply: %+v\n", rf.me, server, reply)
	}

	return ok
}

func (rf *Raft) sendAppend(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	 DPrintf("%v send a AppendEntries to %v \n Args: %+v \n", rf.me, server, args)
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	if ok{
		DPrintf("%v receive a AppendEntries from %v \n Reply: %+v \n", rf.me, server, reply)
	} else{
		DPrintf("%v cannot receive a AppendEntries from %v \n Reply: %+v\n", rf.me, server, reply)
	}
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {

	// Your code here (2B).
	rf.lock("Start")
	if rf.state != Leader {
		rf.unlock("Start")
		return -1, -1, false
	}

	newItem := LogItem{
		Command: command,
		Term:    rf.currentTerm,
		Index: rf.getLastLogIndex() + 1,
	}
	rf.log = append(rf.log, newItem)
	rf.matchIndex[rf.me] = rf.getLastLogIndex()
	resultIndex := rf.getLastLogIndex()
	resultCurrentTerm := rf.currentTerm
	rf.persist()
	rf.unlock("Start")

	DPrintf("%v leader return a start result index: %v, current term: %v, is leader: %v", rf.me, resultIndex, resultCurrentTerm, true)

	return resultIndex, resultCurrentTerm, true
}
func (rf *Raft) sendCommitMessage(from, to int) {
	i := Max(from, rf.getDummyLogIndex() + 1)
	for ; i <= to; i++ {
		j := i
		logItem := rf.getLogItemAtIndex(j)
		DPrintf("%v server apply commend index: %v, commend: %v", rf.me, j, logItem.Command)
		rf.applyCh <- ApplyMsg{true, logItem.Command, logItem.Index, logItem.Term}
	}
}

func (rf *Raft) GetLogLength() int{
	return rf.persister.RaftStateSize()
}

func (rf *Raft) GetKVSnapshotByte() []byte{
	rf.lock("GetKVSnapshot")
	if rf.persister.SnapshotSize() <= 0{
		return []byte{}
	}
	rf.unlock("GetKVSnapshot")
	return rf.getSnapshotRaftObject().StateMachineState
}
func (rf *Raft) SaveSnapshot(lastIncludedIndex int, lastIncludedTerm int, snapshot []byte){
	rf.lock("SaveSnapshot")
	if !rf.logContainIndex(lastIncludedIndex){
		rf.unlock("SaveSnapshot-release 1")
		return
	}
	newLogLength := rf.getLastLogIndex() - lastIncludedIndex + 1
	newLog := make([]LogItem, newLogLength)
	copy(newLog, rf.getLogItemSliceInclusive(lastIncludedIndex, rf.getLastLogIndex()))
	rf.log = newLog
	snapshotRaft := rf.getPersistSnapshotByte(lastIncludedIndex, lastIncludedTerm, snapshot)
	rf.persister.SaveStateAndSnapshot(rf.getPersistStateByte(), snapshotRaft)

	rf.unlock("SaveSnapshot-release 2")
}



//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids th/e
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}
func (rf *Raft) randomElectionTimeout() time.Duration{
	return time.Millisecond * time.Duration(200+rand.Intn(150))
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {



	rf := &Raft{}
	rf.mu = sync.Mutex{}
	atomic.StoreInt32(&rf.dead, 0)
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.applyCh = applyCh

	// Your initialization code here (2A, 2B, 2C).

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	rf.commitIndex = 0
	rf.state = Follower

	rf.nextIndex = []int{}
	rf.matchIndex = []int{}

	rf.electionTimeStamp = time.Now()
	rf.voteCount = 0
	// initialize from state persisted before a crash
	// uncomment the next line for 2c
	//rf.readPersist(persister.ReadRaftState())

	// persistent
	rf.currentTerm = 0
	rf.log = []LogItem{}
	rf.log = append(rf.log, LogItem{
		Command: "dummy node",
		Term:    0,
		Index: 0,
	})
	rf.voteFor = -1

	rf.electionTimeout = time.Millisecond * time.Duration(200 + rand.Intn(250))

	rf.commitIndex = 0
	rf.lastApplied = 0


	if persister.RaftStateSize() > 0{
		rf.readPersist(persister.ReadRaftState())
	}else{
		rf.persist()
	}
	rf.commitIndex = rf.getDummyLogIndex()
	rf.lastApplied = rf.getDummyLogIndex()
	/*
	if persister.SnapshotSize() > 0{
		snapshotRaft := rf.getSnapshotRaftObject()
		applyCh <- ApplyMsg{
			CommandValid: false,
			Command:     snapshotRaft.StateMachineState,
			CommandIndex: snapshotRaft.LastIncludedIndex,
			Term:         snapshotRaft.LastIncludedTerm,
		}
	}
	 */
	rf.startInstallSnapshot()
	go rf.startElection()
	go rf.startAppend()

	return rf
}

func (rf *Raft) unlock(methodName string){
	LPrintf(" server %v method %v unlock", rf.me, methodName)
	rf.mu.Unlock()
}

func (rf *Raft) lock(methodName string){
	LPrintf(" server %v method %v lock", rf.me, methodName)
	rf.mu.Lock()
}


