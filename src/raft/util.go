package raft

import (
	"log"
	"sync"
)

// Debugging
const Debug = -2
var mutex = sync.Mutex{}
func DPrintf(format string, a ...interface{}) (n int) {
	if Debug > 1 {
		mutex.Lock()
		log.Printf(format, a...)
		mutex.Unlock()
	}
	return
}

func LPrintf(format string, a ...interface{}) (n int) {
	if Debug > -1 {
		mutex.Lock()
		log.Printf(format, a...)
		mutex.Unlock()
	}
	return
}
func CPrintf(format string, a ...interface{}) (n int) {
	if Debug > 0 {
		mutex.Lock()
		log.Printf(format, a...)
		mutex.Unlock()
	}
	return
}


func Min(a, b int) int{
	if a <= b{
		return a
	}else{
		return b
	}
}

func Max(a, b int) int{
	if a <= b{
		return b
	}else{
		return a
	}
}