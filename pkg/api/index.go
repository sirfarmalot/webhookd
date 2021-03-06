package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/ncarlier/webhookd/pkg/config"
	"github.com/ncarlier/webhookd/pkg/logger"
	"github.com/ncarlier/webhookd/pkg/tools"
	"github.com/ncarlier/webhookd/pkg/worker"
)

var (
	defaultTimeout int
	scriptDir      string
)

func atoiFallback(str string, fallback int) int {
	if value, err := strconv.Atoi(str); err == nil && value > 0 {
		return value
	}
	return fallback
}

// index is the main handler of the API.
func index(conf *config.Config) http.Handler {
	defaultTimeout = *conf.Timeout
	scriptDir = *conf.ScriptDir
	return http.HandlerFunc(webhookHandler)
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported!", http.StatusInternalServerError)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get script location
	p := strings.TrimPrefix(r.URL.Path, "/")
	script, err := tools.ResolveScript(scriptDir, p)
	if err != nil {
		logger.Error.Println(err.Error())
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Error.Printf("Error reading body: %v", err)
		http.Error(w, "can't read body", http.StatusBadRequest)
		return
	}

	params := tools.QueryParamsToShellVars(r.URL.Query())
	params = append(params, tools.HTTPHeadersToShellVars(r.Header)...)

	// logger.Debug.Printf("API REQUEST: \"%s\" with params %s...\n", p, params)

	// Create work
	timeout := atoiFallback(r.Header.Get("X-Hook-Timeout"), defaultTimeout)
	work := worker.NewWorkRequest(p, script, string(body), params, timeout)

	// Put work in queue
	worker.WorkQueue <- *work

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	for {
		msg, open := <-work.MessageChan

		if !open {
			break
		}

		fmt.Fprintf(w, "data: %s\n\n", msg)

		// Flush the data immediatly instead of buffering it for later.
		flusher.Flush()
	}
}
