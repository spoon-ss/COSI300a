package shardmaster

import (
	"fmt"
	"github.com/google/uuid"
)

//
// Master shard server: assigns shards to replication groups.
//
// RPC interface:
// Join(servers) -- add a set of groups (gid -> server-list mapping).
// Leave(gids) -- delete a set of groups.
// Move(shard, gid) -- hand off one shard from current owner to gid.
// Query(num) -> fetch Config # num, or latest config if num==-1.
//
// A Config (configuration) describes a set of replica groups, and the
// replica group responsible for each shard. Configs are numbered. Config
// #0 is the initial configuration, with no groups and all shards
// assigned to group 0 (the invalid group).
//
// You will need to add fields to the RPC argument structs.
//

// The number of shards.
const NShards = 10

// A configuration -- an assignment of shards to groups.
// Please don't change this.
type Config struct {
	Num    int              // config number
	Shards [NShards]int     // shard -> gid
	Groups map[int][]string // gid -> servers[]
}

/* origin version
const (
	OK = "OK"
)

type Err string

*/

type ErrState interface {
	GetErr() Err
}
type JoinArgs struct {
	Id CommandId
	Servers map[int][]string // new GID -> servers mappings
}

type JoinReply struct {
	WrongLeader bool
	Err         Err
}
func (reply *JoinReply) GetErr() Err{
	return reply.Err
}

type LeaveArgs struct {
	Id CommandId
	GIDs []int
}

type LeaveReply struct {
	WrongLeader bool
	Err         Err
}

func (reply *LeaveReply) GetErr() Err{
	return reply.Err
}

type MoveArgs struct {
	Id CommandId
	Shard int
	GID   int
}

type MoveReply struct {
	WrongLeader bool
	Err         Err
}
func (reply *MoveReply) GetErr() Err{
	return reply.Err
}

type QueryArgs struct {
	Id CommandId
	Num int // desired config number
}

type QueryReply struct {
	WrongLeader bool
	Err         Err
	Config      Config
}
func (reply *QueryReply) GetErr() Err{
	return reply.Err
}

type CommandType string
const(
	MoveCommand  = "Move"
	JoinCommand  = "Join"
	LeaveCommand = "Leave"
	QueryCommand = "Query"
)

const (
	OK             = "OK"
	ErrNoKey       = "ErrNoKey"
	ErrWrongLeader = "ErrWrongLeader"
	ErrTimeOut = "ErrTimeOut"
)

type Err string

type Command struct {
	Id     CommandId
	Type   CommandType
	Args interface{}

	Error Err
}



type CommandId struct{
	ClerkId uuid.UUID
	Command int
}
func (ckId *CommandId) String() string{
	return fmt.Sprintf(ckId.ClerkId.String() + " %v", ckId.Command)
}

func CopyGroupMap(groupMap map[int][]string) map[int][]string{
	groups := make(map[int][]string)
	for gid, servers := range groupMap{
		copyServers := make([]string, len(servers))
		copy(copyServers, servers)
		groups[gid] = copyServers
	}
	return groups
}
