package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/psankar/network-monitor/pkg/minion"
)

func doesExistHandler(w http.ResponseWriter, r *http.Request) {
	x := minion.DoesExistReq{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	err := decoder.Decode(&x)
	if err != nil {
		http.Error(w, "Invalid Request: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	var res minion.MinionResponse
	_, statErr := os.Stat(x.Path)
	if statErr != nil {
		res.Result = false
	} else {
		res.Result = true
	}

	var jData []byte
	jData, err = json.Marshal(res)
	if err != nil {
		http.Error(w, "JSON Marshal Error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	// log.Println("MINION: Does-exist response success!!!!!!!", string(jData))
	w.WriteHeader(http.StatusOK)
	w.Write(jData)
}

func doesContainHandler(w http.ResponseWriter, r *http.Request) {
	x := minion.DoesContainReq{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	decodeErr := decoder.Decode(&x)
	if decodeErr != nil {
		http.Error(w, "Invalid Request: "+decodeErr.Error(),
			http.StatusInternalServerError)
		return
	}

	var res minion.MinionResponse
	res.Result = false // Which would already be the default value

	file, err := os.Open(x.Path)
	if err != nil {
		http.Error(w, "Fileopen Error: "+decodeErr.Error(),
			http.StatusInternalServerError)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), x.Check) {
			res.Result = true
			// We can exit at the first chance.
			goto end
		}
	}

	if err := scanner.Err(); err != nil {
		http.Error(w, "Fileread Error: "+decodeErr.Error(),
			http.StatusInternalServerError)
		return
	}

end:
	var jData []byte
	jData, err = json.Marshal(res)
	if err != nil {
		http.Error(w, "JSON Marshal Error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	// log.Println("MINION: Does-contain response success!!!!!!!", string(jData))
	w.WriteHeader(http.StatusOK)
	w.Write(jData)
}

func isRunningHandler(w http.ResponseWriter, r *http.Request) {
	x := minion.IsRunningReq{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	err := decoder.Decode(&x)
	if err != nil {
		http.Error(w, "Invalid Request: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	var res minion.MinionResponse
	// This is an ugly hack to get the list of processes,
	// but we do this to as we cannot use any 3rd party libraries
	// In actual production code, we will use github.com/mitchellh/go-ps
	cmd := exec.Command("ps", "-e", "-o", "command")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		http.Error(w, "Error getting process list: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	// This is an approximate matching. Not precise.
	if strings.Contains(out.String(), x.ProcessName) {
		res.Result = true
	} else {
		res.Result = false
	}

	var jData []byte
	jData, err = json.Marshal(res)
	if err != nil {
		http.Error(w, "JSON Marshal Error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(jData)
}

func main() {
	http.HandleFunc("/does-exist", doesExistHandler)
	http.HandleFunc("/does-contain", doesContainHandler)
	http.HandleFunc("/is-running", isRunningHandler)

	var port string
	var found bool
	if port, found = os.LookupEnv("PORT"); !found {
		log.Fatal("No PORT env variable set")
	}

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
