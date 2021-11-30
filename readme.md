# Distributed Sharded Key Value Storage System

## Project Summary
This project is an implementation of distributed key-value storage system. It supports a set of interfaces (PUT, GET and APPEND) to manipulate string type pairs. This system uses an implementation of Raft algorithm to achieve high availability, strong consistency and fault tolerance.

## Project Structure

* /src/kvraft

    This package contains the implementation of key value servers, which use raft algorithm to communicate.

* /labgob

    The package that is used to serialize and deserialize data structure.

* /labrpc

    The package provides the remote procedure call functionality which simulates the real network message passing.

* /models

    The key value pair model

* /porcupine

    Test utilities

* /raft

    The implementation of raft algorithm

* /shardmaster

    The implementation of configuration cluster

## Usage
```bash
$ go test raft/...
$ go test kvraft/...
$ go test shardmaster/...
```