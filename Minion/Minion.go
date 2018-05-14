package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/psankar/network-monitor/Minion/minion"
)

func DoesExistHandler(w http.ResponseWriter, r *http.Request) {
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

func DoesContainHandler(w http.ResponseWriter, r *http.Request) {
	x := minion.DoesContainReq{}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	err := decoder.Decode(&x)
	if err != nil {
		http.Error(w, "Invalid Request: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	var res minion.MinionResponse

	// Reads entire file into memory
	fileBody, statErr := ioutil.ReadFile(x.Path)
	if statErr != nil {
		res.Result = false
		goto end
	}

	// Would work only for plain text files
	if strings.Contains(string(fileBody), x.Check) {
		res.Result = true
	} else {
		res.Result = false
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

func IsRunningHandler(w http.ResponseWriter, r *http.Request) {
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
	http.HandleFunc("/does-exist", DoesExistHandler)
	http.HandleFunc("/does-contain", DoesContainHandler)
	http.HandleFunc("/is-running", IsRunningHandler)

	var port string
	var found bool
	if port, found = os.LookupEnv("PORT"); !found {
		log.Fatal("No PORT env variable set")
	}

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
