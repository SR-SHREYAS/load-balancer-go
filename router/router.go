package router

import (
	"net/http"

	"github.com/SR-SHREYAS/load-balancer-go/handler"
)

func RegisterRoutes(lb http.Handler) http.Handler {
	mux := http.NewServeMux()

	// health check route
	mux.HandleFunc("/health", handler.HealthCheckHandler)

	// reverse proxy route
	mux.Handle("/test", lb)

	return mux

}
