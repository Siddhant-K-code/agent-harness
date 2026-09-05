package main

import (
	"github.com/Siddhant-K-code/agent-harness/internal/artifact"
	"log"
	"net/http"
	"time"
)

func main() {
	s := &http.Server{Addr: ":8080", Handler: &artifact.Service{Root: "/harness-root"}, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute, WriteTimeout: time.Minute, MaxHeaderBytes: 16384}
	log.Fatal(s.ListenAndServe())
}
