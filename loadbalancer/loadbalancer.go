package loadbalancer

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type contextKey int

const (
	attemptsKey contextKey = iota
	retriesKey
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	mux          sync.RWMutex
	ReverseProxy *httputil.ReverseProxy
}

type ServerPool struct {
	backends []*Backend
	current  uint64
}

// func NewReverseProxy() http.Handler {
// 	u, err := url.Parse("http://localhost:8081")
// 	if err != nil {
// 		panic(err)
// 	}
// 	return httputil.NewSingleHostReverseProxy(u)
// }

func NewBackend(rawURL string) (*Backend, error) {
	url, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if url.Scheme != "http" && url.Scheme != "https" {
		return nil, fmt.Errorf("unsupported backend URL scheme: %q", url.Scheme)
	}

	if url.Host == "" {
		return nil, fmt.Errorf("backend URL must have a host")
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(url)
	backend := &Backend{
		URL:          url,
		Alive:        true,
		ReverseProxy: reverseProxy,
	}

	return backend, nil
}

func NewServerPool(backends ...*Backend) *ServerPool {
	pool := &ServerPool{
		backends: backends,
		current:  0,
	}

	for _, backend := range pool.backends {
		b := backend

		b.ReverseProxy.ErrorHandler = func(
			w http.ResponseWriter,
			r *http.Request,
			err error,
		) {
			log.Printf("[%s] %v, b.URL.Host, err")

			retries := getRetriesFromContext(r)

			if retries < 3 {
				time.Sleep(10 * time.Millisecond)

				ctx := context.WithValue(
					r.Context(),
					retriesKey,
					retries+1,
				)

				b.ReverseProxy.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// the retries for this backend have been exhausted.
			b.SetAlive(false)

			attempts := getAttemptsFromContext(r)

			log.Printf(
				"%s (%s) : backend %s failed; switching backend (attempt %d)",
				r.RemoteAddr,
				r.URL.Path,
				b.URL.Host,
				attempts+1,
			)

			ctx := context.WithValue(
				r.Context(),
				attemptsKey,
				attempts+1,
			)

			ctx = context.WithValue(ctx, retriesKey, 0)

			pool.ServeHTTP(w, r.WithContext(ctx))
		}
	}

	return pool
}

func (s *ServerPool) NextIndex() int {
	return int(atomic.AddUint64(&s.current, uint64(1)) % uint64(len(s.backends)))
}

func (s *ServerPool) GetNextPeer() *Backend {
	if len(s.backends) == 0 {
		return nil
	}

	// loop entire backends to find alive backend
	next := s.NextIndex()
	l := len(s.backends) + next // start from the next and move a full cycle
	for i := next; i < l; i++ {
		idx := i % len(s.backends) // take an index by modding with length
		// if we have an alive backend, use it and store if its not the original one
		if s.backends[idx].IsAlive() {
			if i != next {
				atomic.StoreUint64(&s.current, uint64(idx)) // mark the current one
			}
			return s.backends[idx]
		}
	}
	return nil
}

func (b *Backend) SetAlive(IsAlive bool) {
	b.mux.Lock()
	b.Alive = IsAlive
	b.mux.Unlock()
}

func (b *Backend) IsAlive() (alive bool) {
	b.mux.RLock()
	alive = b.Alive
	b.mux.RUnlock()
	return
}

// lb load balances the incoming request
func (s *ServerPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	attempts := getAttemptsFromContext(r)

	if attempts > 3 {
		log.Printf(
			"%s (%s): maximum backend attempts reached",
			r.RemoteAddr,
			r.URL.Path,
		)

		http.Error(
			w,
			"Service not available",
			http.StatusServiceUnavailable,
		)
		return
	}

	peer := s.GetNextPeer()
	if peer != nil {
		peer.ReverseProxy.ServeHTTP(w, r)
		return
	}

	http.Error(
		w,
		"Service not available",
		http.StatusServiceUnavailable,
	)
}

func getAttemptsFromContext(r *http.Request) int {
	attempts, ok := r.Context().Value(attemptsKey).(int)
	if !ok {
		return 0
	}
	return attempts
}

func getRetriesFromContext(r *http.Request) int {
	retires, ok := r.Context().Value(retriesKey).(int)
	if !ok {
		return 0
	}
	return retires
}

func isBackendAlive(u *url.URL) bool {
	timeout := 2 * time.Second

	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return false
	}

	_ = conn.Close()
	return true
}

func (s *ServerPool) HealthCheck() {
	for _, b := range s.backends {
		alive := isBackendAlive(b.URL)

		b.SetAlive(alive)

		status := "down"
		if alive {
			status = "up"
		}

		log.Printf("%s [%s]", b.URL.Host, status)
	}
}

func (s *ServerPool) RunHealthChecks(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Starting health check...")
		s.HealthCheck()
		log.Println("Health check completed")
	}
}
