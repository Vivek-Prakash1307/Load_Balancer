package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "✅ Response from Server 1 - Port 8001\nTimestamp: %s\nPath: %s\n",
			time.Now().Format("15:04:05"), r.URL.Path)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"healthy","server":"server-1","port":"8001"}`)
	})

	fmt.Println("🟢 Server 1 running on :8001")
	log.Fatal(http.ListenAndServe(":8001", nil))
}
