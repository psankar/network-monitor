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

var minionURLs = []struct {
	Hostname string
	URL      string
}{
	{"Machine-9000", "http://127.0.0.1:9000/"},
	{"Machine-9001", "http://127.0.0.1:9001/"},
	{"Machine-9002", "http://127.0.0.1:9002/"},
	{"Machine-9003", "http://127.0.0.1:9003/"},
}

const numWorkers = 2

type Operation struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Check string `json:"check"`
}

const OpTypeDoesExist = "file_exists"
const OpTypeDoesContain = "file_contains"
const OpTypeIsRunning = "is_running"

type OpResp struct {
	OperationID string `json:"-"`

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

func processOperation(k string, v Operation, opsWg *sync.WaitGroup,
	ch chan OpResp, jobs chan contactMinionJob) {
	defer opsWg.Done()

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
			go func(hostname, urlPrefix string) {
				defer wg.Done()

				u := urlPrefix + "does-exist"
				minionReq, err := http.NewRequest("POST",
					u,
					bytes.NewBuffer(jData))
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
				u := urlPrefix + "does-contain"
				minionReq, err := http.NewRequest("POST",
					u,
					bytes.NewBuffer(jData))
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
					url+"is-running",
					bytes.NewBuffer(jData))
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

	accumulateResults(&opResp, passMachines, failedMachines,
		unreachableMachines, errMachines)

	opResp.ErroneousRequest = false
	opResp.ErrorMessage = ""

end:
	ch <- opResp

}

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
		i.done <- true
	}
}

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

var client = &http.Client{}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8000", nil))
}
