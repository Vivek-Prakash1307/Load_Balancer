package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "🔵 Response from Server 2 - Port 8002\nTimestamp: %s\nPath: %s\n",
			time.Now().Format("15:04:05"), r.URL.Path)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"healthy","server":"server-2","port":"8002"}`)
	})

	fmt.Println("🔵 Server 2 running on :8002")
	log.Fatal(http.ListenAndServe(":8002", nil))
}
