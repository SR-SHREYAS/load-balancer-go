package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/SR-SHREYAS/load-balancer-go/loadbalancer"
	"github.com/SR-SHREYAS/load-balancer-go/router"
)

func main() {
	// create new backend instances
	// to be replaced with constructor APIs

	backend1, err := loadbalancer.NewBackend("http://localhost:8081")
	if err != nil {
		panic(err)
	}

	backend2, err := loadbalancer.NewBackend("http://localhost:8082")
	if err != nil {
		panic(err)
	}

	backend3, err := loadbalancer.NewBackend("http://localhost:8083")
	if err != nil {
		panic(err)
	}

	// Assemble the backends into a pool
	pool := loadbalancer.NewServerPool(
		backend1,
		backend2,
		backend3,
	)

	go pool.RunHealthChecks(10 * time.Second)

	// the router receives the pool as a HTTP handler
	mux := router.RegisterRoutes(pool)

	port := 8080
	server := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
