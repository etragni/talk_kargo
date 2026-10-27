package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	version := os.Getenv("APP_VERSION")
	if version == "" {
		version = "v1.2.0"
	}

	// DEMO HOOK: set FORCE_ERROR=true to simulate a broken release
	// Used during the talk to show staging blocking promotion
	forceError := os.Getenv("FORCE_ERROR") == "true"

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if forceError {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "ERROR: something went wrong")
			return
		}
		fmt.Fprintf(w, "Hello from Kargo demo — version %s\n", version)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if forceError {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "unhealthy")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	http.ListenAndServe(":8080", nil)
}
