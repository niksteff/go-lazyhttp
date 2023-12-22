package lazyhttp_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/niksteff/lazyhttp"
	"github.com/niksteff/lazyhttp/ratelimit"
)

// TODO: tests
// - test with request without rate limter deadline

// TestBasicRequest tests a basic request with a deadline context
func TestBasicRequest(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		t.Errorf("no deadline set")
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"value": "test"}`))
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	}))
	defer srv.Close()

	httpClient := http.DefaultClient
	httpClient.Timeout = 30 * time.Second

	// test code starts here
	client := lazyhttp.New(
		lazyhttp.WithHttpClient(httpClient),
	)

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr.String(), nil)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	type testResponse struct {
		Value string
	}

	var tr testResponse
	err = lazyhttp.DecodeJson(res.Body, &tr)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if tr.Value != "test" {
		t.Errorf("unexpected response: %s", tr.Value)
	}

	t.Log(tr)
}

// TestBasicRequest tests a basic request with a deadline context
func TestWithHost(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		t.Errorf("no deadline set")
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/some/path/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"value": "test"}`))
		if err != nil {
			t.Errorf("unexpected error: %s", err)
			return
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	// test code starts here
	client := lazyhttp.New(lazyhttp.WithHost(addr))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/some/path/", nil)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	type testResponse struct {
		Value string
	}

	var tr testResponse
	err = lazyhttp.DecodeJson(res.Body, &tr)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}

	if tr.Value != "test" {
		t.Errorf("unexpected response: %s", tr.Value)
	}

	t.Logf("%#v\n", tr)
}

func TestRetryConcept(t *testing.T) {
	type testObj struct {
		n int
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	obj := new(testObj)
	inc := func(obj *testObj) bool {
		return obj.n < 10
	}

	for inc(obj) {
		select {
		case <-ctx.Done():
			t.Log("context done")
			return
		case <-time.After(25 * time.Millisecond):
			// t.Logf("tick %d", obj.n)

			updated := testObj{
				n: obj.n + 1,
			}

			obj = &updated
		}
	}
}

// TestRetryHookMakeRetry adds a simple retry hook and a simple backoff
// implementation. Tests if the retrie count matches what we expect.
func TestRetryHookMakeRetry(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		done = time.Now().Add(30 * time.Second)
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	reqCounter := 0
	expectedTries := 5

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCounter = reqCounter + 1
		
		bb, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		t.Logf("request body: %s", string(bb))
		
		if reqCounter == expectedTries {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("did not expect error parsing url: %+v", err)
		return
	}

	httpTransport := lazyhttp.NewTransport(
		lazyhttp.WithRetryPolicy(func(res *http.Response) bool {
			return res.StatusCode == http.StatusServiceUnavailable
		}),
		lazyhttp.WithBackoffPolicy(func() lazyhttp.Backoff {
			return lazyhttp.NewLimitedTriesBackoff(50*time.Millisecond, expectedTries)
		}),
	)
	httpClient := http.DefaultClient
	httpClient.Transport = httpTransport

	client := lazyhttp.New(
		lazyhttp.WithHttpClient(httpClient),
		lazyhttp.WithHost(addr),
	)

	payload := bytes.NewBuffer([]byte(`{"value": "test"}`))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", payload)
	if err != nil {
		t.Errorf("did not expect error creating request: %+v", err)
		return
	}

	// perform 5 requests in total until we succeed
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("did not expect error making request: %+v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d but got: %d", http.StatusOK, res.StatusCode)
		return
	}

	if reqCounter != expectedTries {
		t.Errorf("expected %d requests but got: %d", expectedTries, reqCounter)
		return
	}
}

type testAuthenticator struct {
	user string
	pass string
}

func (a *testAuthenticator) Authenticate(req *http.Request) error {
	req.SetBasicAuth(a.user, a.pass)
	return nil
}

// TestAuthenticate uses a simple test basic auth authenticator on the request
// to see if the authenticator is called correctly.
func TestAuthenticate(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		t.Errorf("no deadline set")
		return
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if strings.Compare(user, "test") != 0 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if strings.Compare(pass, "test") != 0 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("did not expect error parsing url: %+v", err)
		return
	}

	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithAuthenticator(&testAuthenticator{
			user: "test",
			pass: "test",
		}),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		t.Errorf("did not expect error creating request: %+v", err)
		return
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("did not expect error making request: %+v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d but got: %d", http.StatusOK, res.StatusCode)
		return
	}
}

func TestPreRequestHooks(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		done = time.Now().Add(30 * time.Second)
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("did not expect error parsing url: %+v", err)
		return
	}

	collector := bytes.NewBuffer([]byte{})
	dumpSize := 0

	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithPreRequestHooks(func(req *http.Request) error {
			dump, err := httputil.DumpRequest(req, true)
			if err != nil {
				return fmt.Errorf("error dumping response: %w", err)
			}

			n, err := collector.Write(dump)
			if err != nil {
				return fmt.Errorf("error writing dump to collector: %w", err)
			}

			if n != len(dump) {
				return fmt.Errorf("expected to write %d bytes but wrote %d", len(dump), n)
			}

			dumpSize += len(dump)

			return nil
		}),
	)

	b := bytes.NewBuffer([]byte(`{"value": "test"}`))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", b)
	if err != nil {
		t.Errorf("did not expect error creating request: %+v", err)
		return
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("did not expect error making request: %+v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d but got: %d", http.StatusOK, res.StatusCode)
		return
	}

	if dumpSize == 0 {
		t.Errorf("expected dump size to be greater than 0")
		return
	}

	if len(collector.Bytes()) == 0 {
		t.Errorf("expected collector to have some bytes")
		return
	}

	if dumpSize != len(collector.Bytes()) {
		t.Errorf("expected dump size to be %d but got: %d", dumpSize, len(collector.Bytes()))
		return
	}
}

func TestPostResponseHooks(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		done = time.Now().Add(30 * time.Second)
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("did not expect error parsing url: %+v", err)
		return
	}

	collector := bytes.NewBuffer([]byte{})
	dumpSize := 0

	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithPostResponseHooks(func(res *http.Response) error {
			dump, err := httputil.DumpResponse(res, true)
			if err != nil {
				return fmt.Errorf("error dumping response: %w", err)
			}

			n, err := collector.Write(dump)
			if err != nil {
				return fmt.Errorf("error writing dump to collector: %w", err)
			}

			if n != len(dump) {
				return fmt.Errorf("expected to write %d bytes but wrote %d", len(dump), n)
			}

			dumpSize += len(dump)

			return nil
		}),
	)

	b := bytes.NewBuffer([]byte(`{"value": "test"}`))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", b)
	if err != nil {
		t.Errorf("did not expect error creating request: %+v", err)
		return
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("did not expect error making request: %+v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d but got: %d", http.StatusOK, res.StatusCode)
		return
	}

	if dumpSize == 0 {
		t.Errorf("expected dump size to be greater than 0")
		return
	}

	if len(collector.Bytes()) == 0 {
		t.Errorf("expected collector to have some bytes")
		return
	}

	if dumpSize != len(collector.Bytes()) {
		t.Errorf("expected dump size to be %d but got: %d", dumpSize, len(collector.Bytes()))
		return
	}
}

func TestRateLimiter(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		done = time.Now().Add(30 * time.Second)
	}

	ctx, cancel := context.WithDeadline(context.Background(), done)
	defer cancel()

	requestCount := 0
	expectedReqCount := 5

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount += 1

		// t.Logf("request count: %d", requestCount)

		if requestCount >= expectedReqCount {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusTooManyRequests)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("did not expect error parsing url: %+v", err)
		return
	}

	client := lazyhttp.New(
		lazyhttp.WithHost(addr),
		lazyhttp.WithRateLimiter(ratelimit.NewTokenBucketRateLimiter(*time.NewTicker(50 * time.Millisecond), 1, 100*time.Millisecond)),
		lazyhttp.WithMaxRateLimiterWaitTime(250*time.Millisecond),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		t.Errorf("did not expect error creating request: %+v", err)
		return
	}

	for i := 0; i < expectedReqCount; i++ {
		res, err := client.Do(req)
		if err != nil {
			t.Errorf("did not expect error making request: %+v", err)
		}
		defer func() {
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}()

		if requestCount < expectedReqCount && res.StatusCode != http.StatusTooManyRequests {
			t.Errorf("expected status code %d but got: %d", http.StatusTooManyRequests, res.StatusCode)
		}
	}

	if requestCount != expectedReqCount {
		t.Errorf("expected %d requests but got: %d", expectedReqCount, requestCount)
		return
	}
}
