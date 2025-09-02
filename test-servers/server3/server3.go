package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "🟡 Response from Server 3 - Port 8003\nTimestamp: %s\nPath: %s\n",
			time.Now().Format("15:04:05"), r.URL.Path)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"healthy","server":"server-3","port":"8003"}`)
	})

	fmt.Println("🟡 Server 3 running on :8003")
	log.Fatal(http.ListenAndServe(":8003", nil))
}
