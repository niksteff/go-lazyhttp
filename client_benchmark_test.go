package lazyhttp_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/niksteff/lazyhttp"
	"github.com/niksteff/lazyhttp/ratelimit"
)

const (
	COUNT = 50 // how many requests to send in a single benchmark
)

func startServer(b *testing.B) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

func BenchmarkDefaultClient(b *testing.B) {
	// stop the timer for the setup process
	b.StopTimer()

	srv := startServer(b)
	defer srv.Close()
	addr := srv.URL + "/"

	// client trace to log whether the request's underlying tcp connection was re-used
	// reusedCounter := 0
	// clientTrace := &httptrace.ClientTrace{
	// 	GotConn: func(info httptrace.GotConnInfo) {
	// 		if info.Reused {
	// 			reusedCounter += 1
	// 		}
	// 	},
	// }

	// ctx := httptrace.WithClientTrace(context.Background(), clientTrace)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 50
	transport.MaxIdleConnsPerHost = 50
	transport.MaxIdleConns = 50

	httpClient := http.DefaultClient
	httpClient.Timeout = 30 * time.Second
	httpClient.Transport = transport

	for i := 0; i < COUNT; i++ {
		b.StartTimer()

		// this buffer holds our benchmark requests until we run them
		buf := make(chan *http.Request, COUNT)
		for i := 0; i < COUNT; i++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
			if err != nil {
				b.Errorf("error creating new benchmark request: %v", err)
				continue
			}

			// write the request to our benchmark buffer
			buf <- req
		}
		close(buf)

		var wg sync.WaitGroup
		for req := range buf {
			request := req
			wg.Add(1)
			go func(wg *sync.WaitGroup) {
				defer wg.Done()

				res, err := httpClient.Do(request)
				if err != nil {
					b.Errorf("error sending benchmark request: %v", err)
				}
				defer func() {
					_, _ = io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}()
			}(&wg)
		}
		wg.Wait()
	}

	// b.Logf("reused connections: %d", reusedCounter)
}

func BenchmarkClient(b *testing.B) {
	// stop the timer for the setup process
	b.StopTimer()

	srv := startServer(b)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	addr, err := url.Parse(srv.URL)
	if err != nil {
		b.Errorf("unexpected error: %v", err)
		return
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 50
	transport.MaxIdleConnsPerHost = 50
	transport.MaxIdleConns = 50

	httpClient := http.DefaultClient
	httpClient.Timeout = 30 * time.Second
	httpClient.Transport = transport

	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithHttpClient(httpClient),
	)

	for i := 0; i < b.N; i++ {
		// benchmark code starts here
		// this buffer holds our benchmark requests until we run them
		buf := make(chan *http.Request, COUNT)

		b.StartTimer()
		for i := 0; i < COUNT; i++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			if err != nil {
				b.Errorf("error creating new benchmark request: %v", err)
				continue
			}

			// write the request to our benchmark buffer
			buf <- req
		}
		close(buf)

		var wg sync.WaitGroup
		for req := range buf {
			request := req
			wg.Add(1)
			go func(wg *sync.WaitGroup) {
				defer wg.Done()

				res, err := client.Do(request)
				if err != nil {
					b.Errorf("error sending benchmark request: %v", err)
				}
				defer func() {
					_, _ = io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}()
			}(&wg)
		}
		wg.Wait()
	}
}

func BenchmarkClientComplex(b *testing.B) {
	// stop the timer for the setup process
	b.StopTimer()

	srv := startServer(b)
	defer srv.Close()
	addr, err := url.Parse(srv.URL)
	if err != nil {
		b.Errorf("unexpected error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 50
	transport.MaxIdleConnsPerHost = 50
	transport.MaxIdleConns = 50

	httpClient := http.DefaultClient
	httpClient.Timeout = 30 * time.Second
	httpClient.Transport = transport

	// benchmark code starts here
	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithHttpClient(httpClient),
		lazyhttp.WithRateLimiter(ratelimit.NewTokenBucketRateLimiter(*time.NewTicker(time.Millisecond * 250), 1000, time.Second*30)),
		lazyhttp.WithPreRequestHooks(func(req *http.Request) error {
			return nil
		}),
		lazyhttp.WithPostResponseHooks(func(resp *http.Response) error {
			return nil
		}),
	)

	for i := 0; i < b.N; i++ {
		// this buffer holds our benchmark requests until we run them
		buf := make(chan *http.Request, COUNT)

		b.StartTimer()
		for i := 0; i < COUNT; i++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
			if err != nil {
				b.Errorf("error creating new benchmark request: %v", err)
				continue
			}

			// write the request to our benchmark buffer
			buf <- req
		}
		close(buf)

		var wg sync.WaitGroup
		for req := range buf {
			request := req
			wg.Add(1)
			go func(wg *sync.WaitGroup) {
				defer wg.Done()

				res, err := client.Do(request)
				if err != nil {
					b.Errorf("error sending benchmark request: %v", err)
				}
				defer func() {
					_, _ = io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}()
			}(&wg)
		}
		wg.Wait()
	}
}
