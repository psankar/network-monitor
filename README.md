# Problem Statement
```
Let’s assume you have hundreds of thousands of servers under your command and
all servers must be within the following standards:

* Given some files, must be in defined paths
* Given some strings, must be in given path's content (eg: in a log file)
* Given some process names, must be running

Write an example of how you would meet these standards (see operation example
below). How to distribute these operations will be your choice of distribution
strategy. Your solution should come with scaling/distribution in-place.
Eg: your solution should be able to send M requests from N machines to Y
destinations. M, N and Y respectively would be millions.

You should know which clients are online/sending results.
You should persist the responses coming from clients.

Operation example:
{
  "check_etc_hosts_has_4488": {
    "path": "/etc/hosts",
    "type": "file_contains",
    "check": "4.4.8.8"
  },
  "check_virus_file_exists": {
    "path": "/var/log/virus.txt",
    "type": "file_exists"
  }
}

This example is here only for demo purposes, and does not mean that the server
will only get these checks, we could send many different checks in any unsorted
order.

FAQ 

1) Language? 
Code should be written in Go (golang). You will only use Golang's std packages except database driver package(s) and/or its dependencies.

2) How should I submit the solution?
Code should be pushed to a git repository.

3) Should I host the server somewhere?
You do not need to host/serve the code in a server

4) How long can I take to solve this challenge ?
In 1 weeks, after receiving this message.
```

# Design of my solution
There are two programs.

* A Minion: This runs on all the millions of nodes.
* An API Server: This can run on any number of nodes. For now, we are going to run in only one node.

When an user makes a HTTP request to the `API server`, it parses the requests and identifies the individual `type` of operation requests. There could be three different types of operations: `file_contains`, `file_exists`, `is_running`.

The `API server` then makes HTTP requests to all the `Minion` nodes.  The IP Addresses of the minions are now stored in a global variable in the `APIServer.go` file. 

Managing members in a network is a distributed consensus problem. We would be using Apache Zookeeper or some such tool. It needs a Raft like algorithm to avoid split-brain like situations. For now, we will just go with a global variable.

The `API Server` parses the incoming JSON request and creates HTTP Request objects for talking to all the `Minion` nodes. However, the API server talks to only 2 Minion nodes at a time, to not run out of sockets, as per your ulimit imposed limits. This number 2 is hardcoded now but can be tweaked.

# API Response
For every JSON HTTP Request that comes to the `API Server`, it will respond with the following fields, in JSON format:
```
OpResp {
	ErroneousRequest bool   // Will be true if the request is invalid
	ErrorMessage     string // Contains valid string explaining why the request
	// is invalid, if the above bool is true. Should ignore if the above value
	// is false.

	PassedMachines []string // The Test passed in these machines
	FailedMachines []string // The Test failed in these machines

	ErrorMachines []string // Could not find out if the Test passed
	// in these machines, due to some operational issues

	UnReachableMachines []string // These machines are unreachable at the moment
}
```

# Usage Instruction
We will run 4 instances of the Minions to simulate 4 machines in the network and will run one instance of the API Server.
```
Minion$ PORT=9000 go run Minion.go
Minion$ PORT=9001 go run Minion.go
Minion$ PORT=9002 go run Minion.go
Minion$ PORT=9003 go run Minion.go
APIServer$ go run APIServer.go
$ curl --header "Content-Type: application/json" \
  --request POST \
  --data '{
  "check_etc_hosts_has_4488": {
    "path": "/etc/hosts",
    "type": "file_contains",
    "check": "4.4.8.8"
  },
  "check_virus_file_exists": {
    "path": "/var/log/virus.txt",
    "type": "file_exists"
  }
}' \
  http://localhost:8000/


$ # To ensure that the system works fine, We can \
bring the Minion server that is running on port 9000 and \
then re give the same curl command as above. Now we can \
find that what came in "FailedMachines" will now come in \
the "UnReachableMachines" for the 9000 server
```
