package kvraft

type CommandType string
const(
	GetCommand = "GET"
	PutCommand = "PUT"
	AppendCommand = "Append"
)
const (
	OK             = "OK"
	ErrNoKey       = "ErrNoKey"
	ErrWrongLeader = "ErrWrongLeader"
	ErrTimeOut = "ErrTimeOut"
)

type Err string



// Put or Append
type PutAppendArgs struct {
	Id CommandId
	Key   string
	Value string
	Op    string // "Put" or "Append"
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Id CommandId
	Key string
	// You'll have to add definitions here.
}

type GetReply struct {
	Value string
	Err   Err
	//CommandMap map[string]Command
}
