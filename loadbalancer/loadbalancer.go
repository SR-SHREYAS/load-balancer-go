package loadbalancer

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
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
	return &ServerPool{
		backends: backends,
		current:  0,
	}
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
	peer := s.GetNextPeer()
	if peer != nil {
		peer.ReverseProxy.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}
