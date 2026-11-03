package loadbalancer

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// testing is a go package that provides support for automated testing of Go packages
// uviversal features like running tests , benchmarking and profiling etc
func createTestBackend(t *testing.T, rawURL string) *Backend {

	t.Helper()
	// if a assertion fails inside this function, go reports the calling test location rather than treating the helper as the primary failure location

	backend, err := NewBackend(rawURL)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	return backend
}

func TestNewBackend(t *testing.T) {

	// table driven tests :  a table slice of all the scenarios we want our api to face and handle without breaking
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{
			name:    "Valid HTTP URL",
			rawURL:  "http://localhost:8081",
			wantErr: false,
		},
		{
			name:    "Valid HTTPS URL ",
			rawURL:  "https://localhost:8082",
			wantErr: false,
		},
		{
			name:    "unsupported scheme",
			rawURL:  "ftp://localhost:8083",
			wantErr: true,
		},
		{
			name:    "missing host",
			rawURL:  "http://",
			wantErr: true,
		},
		{
			name:    "missing scheme",
			rawURL:  "localhost:8081",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend, err := NewBackend(tt.rawURL)

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewBackend() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil {
				return
			}

			if backend.URL.String() != tt.rawURL {
				t.Errorf(
					"expected URL %q, got %q",
					tt.rawURL,
					backend.URL.String(),
				)
			}

			if !backend.IsAlive() {
				t.Error("new backend should be alive by default( should be initially alive)")
			}

			if backend.ReverseProxy == nil {
				t.Error("backend reverse proxy should not be nil")
			}
		})
	}
}

// a unit test for backend liveness check
// we only care about the state of current backend , hence no need for a HTTP server trigger (hence a unit test(one part of a feature))
// check on SETALIVE and ISALIVE methods of backend struct
func TestBackendLiveness(t *testing.T) {
	backend := createTestBackend(t, "http://localhost:8081")

	if !backend.IsAlive() {
		t.Fatal("new Backend should be alive by default( should be initially alive)")
	}

	backend.SetAlive(false)

	if backend.IsAlive() {
		t.Error("backend should be marked down")
	}

	backend.SetAlive(true)

	if !backend.IsAlive() {
		t.Error("backend should be marked up alive again")
	}
}

// test on round robin load balancing algorithm
func TestGetNextPeerRoundRobin(t *testing.T) {
	backend1 := createTestBackend(t, "http://localhost:8081")
	backend2 := createTestBackend(t, "http://localhost:8082")
	backend3 := createTestBackend(t, "http://localhost:8083")

	pool := NewServerPool(backend1, backend2, backend3)

	selected := make(map[string]bool)

	for i := 0; i < 3; i++ {
		peer := pool.GetNextPeer()

		if peer == nil {
			t.Fatal("expected a backend, got nil")
		}

		selected[peer.URL.Host] = true
	}

	if len(selected) != 3 {
		t.Errorf(
			"expected all 3 backends to be selected, got %d",
			len(selected),
		)
	}
}

// to skip unhealthy backends
func TestGetNextPeerSkipsUnhealthy(t *testing.T) {
	backend1 := createTestBackend(t, "http://localhost:8081")
	backend2 := createTestBackend(t, "http://localhost:8082")
	backend3 := createTestBackend(t, "http://localhost:8083")

	pool := NewServerPool(backend1, backend2, backend3)

	backend2.SetAlive(false)

	for i := 0; i < 6; i++ {
		peer := pool.GetNextPeer()

		if peer == nil {
			t.Fatal("expected a backend, got nil")
		}

		if peer == backend2 {
			t.Error("selected an unhealthy backend")
		}
	}
}

// test when all backends are unhealthy
func TestGetNextPeerNoHealthyBackends(t *testing.T) {
	backend1 := createTestBackend(t, "http://localhost:8081")
	backend2 := createTestBackend(t, "http://localhost:8082")
	backend3 := createTestBackend(t, "http://localhost:8083")

	pool := NewServerPool(backend1, backend2, backend3)

	backend1.SetAlive(false)
	backend2.SetAlive(false)
	backend3.SetAlive(false)

	peer := pool.GetNextPeer()

	if peer != nil {
		t.Error("expected nil, all backend unhealthy, still got a backend")
	}
}

// Integration tests

func TestServeHTTPForwardsRequest(t *testing.T) {
	// Arrange: create a temporary backend HTTP server
	// httptest.NewServer creates a temporary new server locally
	backendServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("backend received : " + r.URL.Path))
		},
	))

	defer backendServer.Close()

	// Create a backend and put it in a server pool
	backend := createTestBackend(t, backendServer.URL)
	pool := NewServerPool(backend)

	// send a request through the load balancer
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	recorder := httptest.NewRecorder()
	// http new recorder creates a new ResponseRecorder (which satisfies http.ResponseWriter) to record the response

	pool.ServeHTTP(recorder, request)

	// Assert: verify response from the backend
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			recorder.Code,
		)
	}

	expectedBody := "backend received : /test"
	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}
}

// Backend failover test and retry mechanism test , request landing on next available server
func TestServeHTTPFailsOverToHealthyBackend(t *testing.T) {
	// create a healthy backend .
	healthyServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("response from healthy backend"))
		},
	))
	defer healthyServer.Close()

	healthyBackend := createTestBackend(t, healthyServer.URL)

	// create a backend and shut it down to simulate a failure
	failedServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	failedBackendURL := failedServer.URL
	failedServer.Close() // shuting down the server

	failedBackend := createTestBackend(t, failedBackendURL)

	// the failed backend is deliberately placed second
	// the current round robin selection implements from 1st index forward

	pool := NewServerPool(failedBackend, healthyBackend)

	// send a request through LB
	request := httptest.NewRequest(http.MethodGet, "/test", nil)
	recorder := httptest.NewRecorder()

	pool.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusOK,
			recorder.Code,
		)
	}

	expectedBody := "response from healthy backend"
	if recorder.Body.String() != expectedBody {
		t.Errorf(
			"expected body %q, got %q",
			expectedBody,
			recorder.Body.String(),
		)
	}

	// verify that the failed backend is marked as unhealthy
	if failedBackend.IsAlive() {
		t.Error("expected failed bakend to be marked as down but it is still marked as alive")
	}

	// verify healthy backend are remains available
	if !healthyBackend.IsAlive() {
		t.Error("expected healthy backend to be marked as alive but it is marked as down")
	}
}
