package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"sync"

	// _ "github.com/motemen/go-loghttp/global"
	"github.com/psankar/network-monitor/Minion/minion"
)

// minionURLs refer to the list of URLs of the
// minion machines. In production, this could
// come from zookeeper or some such distributed consensus
// system that maintains membership in a correct way.
var minionURLs = []struct {
	Hostname string
	URL      string
}{
	{"Machine-9000", "http://127.0.0.1:9000/"},
	{"Machine-9001", "http://127.0.0.1:9001/"},
	{"Machine-9002", "http://127.0.0.1:9002/"},
	{"Machine-9003", "http://127.0.0.1:9003/"},
}

// numWorkers is the maximum number of parallel
// HTTP connections that could be opened.
// This should be less than your ulimits
const numWorkers = 2

// Operation is a representation of each incoming query, such as
// a check_virus_file_exists or a check_etc_hosts_has_4488
type Operation struct {
	// Path is the actual file path where the operation
	// should be performed in the minion
	Path string `json:"path"`

	// Type should be one of the OpType constants defined below
	Type string `json:"type"`

	// Check is the parameter that should be used for
	// the completion of the operation
	Check string `json:"check"`
}

const (
	OpTypeDoesExist   = "file_exists"
	OpTypeDoesContain = "file_contains"
	OpTypeIsRunning   = "is_running"
)

type OpResp struct {
	OperationID string `json:"-"`

	// ErroneousRequest indicates whether the operation completed succesfully
	// or not. If this is set to true, ErrorMessage will have a meaningful
	// error description. This is also set when the server faces some kind of
	// internal error too.
	ErroneousRequest bool

	// ErrorMessage contains a valid string only when the above boolean is true
	ErrorMessage string

	// PassedMachines is the list of machines where the operation
	// was succesfully evaluated
	PassedMachines []string

	// FailedMachines is the list of machines where the operation
	// was not succesful. For example, a file is not available
	// in the needed path
	FailedMachines []string

	// ErrorMachines is a list of machines where the result of
	// the operation could not be detected correctly, possibly
	// due to server side issues. The client can retry after sometime.
	ErrorMachines []string

	// UnReachableMachines contain the list of minion machines
	// which are currently not reachable in the network
	UnReachableMachines []string
}

func createDoesExistRequestAndEnqueue(hostname, urlPrefix string,
	wg *sync.WaitGroup, passMachines, failedMachines, unreachableMachines,
	errMachines chan string, jobs chan contactMinionJob, jData []byte) {

	defer wg.Done()

	minionReq, err := http.NewRequest("POST",
		urlPrefix+"does-exist", bytes.NewBuffer(jData))
	if err != nil {
		errMachines <- hostname
		return
	}
	minionReq.Header.Set("Content-Type", "application/json")
	done := make(chan bool)
	jobs <- contactMinionJob{minionReq, hostname, passMachines,
		failedMachines, unreachableMachines, errMachines, done}
	<-done
	return
}

func processOperation(k string, v Operation, opsWg *sync.WaitGroup,
	ch chan OpResp, jobs chan contactMinionJob) {
	defer opsWg.Done()

	// TODO: Since these four are always passed along together,
	// we could create a new resultsChannels struct
	passMachines := make(chan string)
	failedMachines := make(chan string)
	unreachableMachines := make(chan string)
	errMachines := make(chan string)

	var wg sync.WaitGroup
	var opResp OpResp
	opResp.OperationID = k

	switch v.Type {
	case OpTypeDoesExist:
		jData, err := json.Marshal(minion.DoesExistReq{
			Path: v.Path})
		if err != nil {
			log.Println(err)
			opResp.ErroneousRequest = true
			opResp.ErrorMessage = "Internal Server Error"
			goto end
		}

		for _, i := range minionURLs {
			wg.Add(1)
			go createDoesExistRequestAndEnqueue(i.Hostname, i.URL, &wg,
				passMachines, failedMachines, unreachableMachines, errMachines,
				jobs, jData)
		}

	case OpTypeDoesContain:
		jData, err := json.Marshal(minion.DoesContainReq{
			Path:  v.Path,
			Check: v.Check,
		})
		if err != nil {
			log.Println(err)
			opResp.ErroneousRequest = true
			opResp.ErrorMessage = "Internal Server Error"
			goto end
		}

		for _, i := range minionURLs {
			wg.Add(1)
			go func(hostname, urlPrefix string) {
				defer wg.Done()

				minionReq, err := http.NewRequest("POST",
					urlPrefix+"does-contain", bytes.NewBuffer(jData))
				if err != nil {
					errMachines <- hostname
					return
				}
				minionReq.Header.Set("Content-Type", "application/json")
				done := make(chan bool)
				jobs <- contactMinionJob{minionReq, hostname, passMachines,
					failedMachines, unreachableMachines, errMachines, done}
				<-done
				return
			}(i.Hostname, i.URL)
		}
	case OpTypeIsRunning:
		jData, err := json.Marshal(minion.IsRunningReq{
			ProcessName: v.Check,
		})
		if err != nil {
			log.Println(err)
			opResp.ErroneousRequest = true
			opResp.ErrorMessage = "Internal Server Error"
			goto end
		}

		for _, i := range minionURLs {
			wg.Add(1)
			go func(hostname, url string) {
				defer wg.Done()
				minionReq, err := http.NewRequest("POST",
					url+"is-running", bytes.NewBuffer(jData))
				if err != nil {
					errMachines <- hostname
					return
				}
				minionReq.Header.Set("Content-Type", "application/json")
				done := make(chan bool)
				jobs <- contactMinionJob{minionReq, hostname, passMachines,
					failedMachines, unreachableMachines, errMachines, done}
				<-done
				return
			}(i.Hostname, i.URL)
		}
	default:
		opResp.ErroneousRequest = true
		opResp.ErrorMessage = "Unknown type of `Operation`"
		goto end
	}

	// Closes the channel after all the Ops are processed
	go func() {
		wg.Wait()
		close(passMachines)
		close(failedMachines)
		close(unreachableMachines)
		close(errMachines)
	}()

	// accumulateResults gathers the response from all the passed channels
	// and puts them on the opResp parameter.
	accumulateResults(&opResp, passMachines, failedMachines,
		unreachableMachines, errMachines)

	opResp.ErroneousRequest = false
	opResp.ErrorMessage = ""

end:
	// Notify in this channel, after this Operation is fully processed
	ch <- opResp
}

// accumulateResults gathers the response from all the passed channels
// and puts them on the opResp parameter.
func accumulateResults(opResp *OpResp, passMachines, failedMachines,
	unreachableMachines, errMachines chan string) {
	var collectorWG sync.WaitGroup

	collectorWG.Add(1)
	go func() {
		for i := range failedMachines {
			opResp.FailedMachines = append(opResp.FailedMachines, i)
		}
		collectorWG.Done()
	}()

	collectorWG.Add(1)
	go func() {
		for i := range passMachines {
			opResp.PassedMachines = append(opResp.PassedMachines, i)
		}
		collectorWG.Done()
	}()

	collectorWG.Add(1)
	go func() {
		for i := range unreachableMachines {
			opResp.UnReachableMachines = append(opResp.UnReachableMachines, i)
		}
		collectorWG.Done()
	}()

	collectorWG.Add(1)
	go func() {
		for i := range errMachines {
			opResp.ErrorMachines = append(opResp.ErrorMachines, i)
		}
		collectorWG.Done()
	}()

	collectorWG.Wait()
}

func handler(w http.ResponseWriter, r *http.Request) {
	x := make(map[string]Operation)

	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	decodeErr := decoder.Decode(&x)
	if decodeErr != nil {
		http.Error(w, "Invalid Request: "+decodeErr.Error(),
			http.StatusBadRequest)
		return
	}

	ch := make(chan OpResp)

	jobs := make(chan contactMinionJob)
	for wp := 0; wp < numWorkers; wp++ {
		go worker(wp, jobs)
	}

	var opsWg sync.WaitGroup

	for k1, v1 := range x {
		opsWg.Add(1)
		go processOperation(k1, v1, &opsWg, ch, jobs)
	}

	go func() {
		opsWg.Wait()
		close(ch)
		close(jobs)
	}()

	// Assumption is that every operation will have an unique id
	m := make(map[string]OpResp)
	for i := range ch {
		m[i.OperationID] = i
	}

	jData, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		log.Println("JSON Marshal Error: " + err.Error())
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	// log.Println(string(jData))

	w.WriteHeader(http.StatusOK)
	w.Write(jData)

	return
}

// contactMinionJob refers to each task that will be asynchronously
// executed to contact a minion and check the status of the operation.
// Each channel that is a member of this struct, will get the result
// of the operation. The jobs are executed through a simple worker-pool
// as used in the worker function below.
type contactMinionJob struct {
	minionReq           *http.Request
	hostname            string
	passMachines        chan<- string
	failedMachines      chan<- string
	unreachableMachines chan<- string
	errMachines         chan<- string
	done                chan<- bool
}

func worker(id int, jobs <-chan contactMinionJob) {
	for i := range jobs {
		log.Println("WorkerID: ", id, "contacting hostname:",
			i.hostname, " for ", i.minionReq.URL)
		contactMinion(i.minionReq, i.hostname, i.passMachines,
			i.failedMachines, i.unreachableMachines, i.errMachines)
		// Synchronously block until the actual HTTP request is made
		// and then notifies in the done channel of each request
		i.done <- true
	}
}

// Makes the actual HTTP request
func contactMinion(minionReq *http.Request, hostname string,
	passMachines chan<- string, failedMachines chan<- string,
	unreachableMachines chan<- string, errMachines chan<- string) {
	// log.Println("Entering contactMinion", minionReq.URL)
	// defer log.Println("Exiting contactMinion", minionReq.URL)

	minionResp, err := client.Do(minionReq)
	if err != nil {
		unreachableMachines <- hostname
		return
	}

	if minionResp.StatusCode != http.StatusOK {
		errMachines <- hostname
		return
	}

	var minionRespBody []byte
	minionRespBody, err = ioutil.ReadAll(minionResp.Body)
	if err != nil {
		errMachines <- hostname
		return
	}
	// log.Println(string(minionRespBody), minionReq.URL)

	var mr minion.MinionResponse
	if err := json.Unmarshal(minionRespBody, &mr); err != nil {
		log.Println("contactMinion: JSON Unmarshal failed: ", err)
		errMachines <- hostname
		return
	}

	// log.Println(mr)

	if mr.Result == true {
		passMachines <- hostname
		return
	}

	failedMachines <- hostname
	return
}

// A single threadsafe http client that can be reused. This could be
// tweaked to cache sessions, alter timeouts etc.
var client = &http.Client{}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8000", nil))
}
