// NTN-in-a-Box Sample: Message Server (Go)
//
// A simple HTTP server that receives messages and echoes them back.
// Run this OUTSIDE ntnbox (on the host) so it's always reachable.
// The client runs INSIDE ntnbox and experiences NTN conditions.
//
// Usage:
//   go run samples/go-messenger/server/main.go
//
// Then in another terminal:
//   ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- go run samples/go-messenger/client/main.go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

var messageCount atomic.Int64

type message struct {
	ID      int    `json:"id"`
	From    string `json:"from"`
	Text    string `json:"text"`
	SentAt  string `json:"sent_at"`
}

type response struct {
	Status   string `json:"status"`
	ServerTs string `json:"server_ts"`
	MsgID    int    `json:"msg_id"`
}

func main() {
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var msg message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		count := messageCount.Add(1)
		fmt.Printf("  [%s] received msg#%d from %s: %q\n",
			time.Now().Format("15:04:05"), msg.ID, msg.From, msg.Text)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response{
			Status:   "delivered",
			ServerTs: time.Now().Format(time.RFC3339),
			MsgID:    int(count),
		})
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	addr := ":9090"
	fmt.Printf("  ntn-messenger-server: listening on %s\n", addr)
	fmt.Printf("  waiting for messages from client...\n\n")
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}
