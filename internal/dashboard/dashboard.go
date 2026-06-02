package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/mubeng/mubeng/common"
)

//go:embed static
var staticFiles embed.FS

// Run starts the dashboard HTTP server on addr.
func Run(addr string, opt *common.Options) error {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		req, rotates, errs, proxy, uptime := opt.Stats.Get()
		rps := 0.0
		if uptime > 0 {
			rps = float64(req) / uptime
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"proxies":   opt.ProxyManager.Count(),
			"requests":  req,
			"rotates":   rotates,
			"errors":    errs,
			"rps":       rps,
			"proxy":     proxy,
			"rotate":    opt.Rotate,
			"method":    opt.Method,
			"delay":     opt.Delay.String(),
			"rateLimit": opt.RateLimit,
		})
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Rotate    int     `json:"rotate"`
			Method    string  `json:"method"`
			Delay     string  `json:"delay"`
			RateLimit float64 `json:"rateLimit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Rotate > 0 {
			opt.Rotate = body.Rotate
		}
		if body.Method == "random" || body.Method == "sequent" {
			opt.Method = body.Method
		}
		if body.RateLimit >= 0 {
			opt.RateLimit = body.RateLimit
		}
		if d, err := time.ParseDuration(body.Delay); err == nil {
			opt.Delay = d
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		ch := LogBroadcaster.Subscribe()
		defer LogBroadcaster.Unsubscribe(ch)

		for {
			select {
			case msg := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	mux.Handle("/", http.FileServer(http.FS(sub)))

	return http.ListenAndServe(addr, mux)
}
