package lazyhttp

import (
	"log"
	"net/http"
	"time"
)

type TransportOption func(t *transport)

func WithTransport(httpTransport *http.Transport) TransportOption {
	return func(t *transport) {
		t.transport = httpTransport
	}
}

func WithRetryPolicy(retryPolicy func(res *http.Response) bool) TransportOption {
	return func(t *transport) {
		t.retryPolicy = retryPolicy
	}
}

func WithBackoffPolicy(backoffPolicy func() Backoff) TransportOption {
	return func(t *transport) {
		t.backoffPolicy = backoffPolicy
	}
}

type transport struct {
	transport     *http.Transport
	retryPolicy   func(res *http.Response) bool
	backoffPolicy func() Backoff
}

func NewTransport(opts ...TransportOption) *transport {
	t := &transport{
		transport:     http.DefaultTransport.(*http.Transport).Clone(),
		retryPolicy:   func(res *http.Response) bool { return false },
		backoffPolicy: func() Backoff { return NewNoopBackoff() },
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// RoundTrip executes a single HTTP transaction, returning
// a Response for the provided Request.
//
// RoundTrip should not attempt to interpret the response. In
// particular, RoundTrip must return err == nil if it obtained
// a response, regardless of the response's HTTP status code.
// A non-nil err should be reserved for failure to obtain a
// response. Similarly, RoundTrip should not attempt to
// handle higher-level protocol details such as redirects,
// authentication, or cookies.
//
// RoundTrip should not modify the request, except for
// consuming and closing the Request's Body. RoundTrip may
// read fields of the request in a separate goroutine. Callers
// should not mutate or reuse the request until the Response's
// Body has been closed.
//
// RoundTrip must always close the body, including on errors, // TODO:
// but depending on the implementation may do so in a separate
// goroutine even after RoundTrip returns. This means that
// callers wanting to reuse the body for subsequent requests
// must arrange to wait for the Close call before doing so.
//
// The Request's URL and Header fields must be initialized.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// TODO: perform roundtrip with backoff and retry
	res, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// get a fresh instance of the backoff policy for this round trip
	backoffPolicy := t.backoffPolicy()

	// TODO: buffer the body for retries?
	for t.retryPolicy(res) {
		log.Printf("retrying request %s %s", req.Method, req.URL)
		td, ok := backoffPolicy.Backoff()
		if !ok {
			return res, nil
		}

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(td):
			res, err = t.transport.RoundTrip(req)
			if err != nil {
				return nil, err
			}
		}
	}

	return res, nil
}
