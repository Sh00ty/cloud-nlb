package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

func main() {
	mux := http.NewServeMux()

	mu := sync.Mutex{}
	lastHcReq := time.Now()
	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		mu.Lock()
		defer mu.Unlock()

		log.Printf(
			"got hc request %s from %s since %s",
			r.RequestURI,
			r.UserAgent(),
			time.Since(lastHcReq),
		)
		lastHcReq = time.Now()
		w.WriteHeader(http.StatusOK)
		// filename := r.URL.Query().Get("file")
		// f, err := os.Open(filename)
		// if err != nil {
		// 	w.WriteHeader(http.StatusOK)
		// 	return
		// }
		// f.Close()
		// w.WriteHeader(http.StatusInternalServerError)
	})
	err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%s", os.Args[1]), mux)
	if err != nil {
		fmt.Println(err)
	}
}
