package handler

import (
	"fmt"
	"net/http"
)

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Server running fine\n")

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Server is healthy"))
}
