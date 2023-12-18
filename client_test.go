package lazyhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/niksteff/lazyhttp"
)

// TestBasicRequest tests a basic request with a deadline context
func TestBasicRequest(t *testing.T) {
	done, ok := t.Deadline()
	if !ok {
		t.Errorf("no deadline set")
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

	// test code starts here
	client := lazyhttp.NewClient()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	req, err := lazyhttp.NewRequestWithContext(ctx, http.MethodGet, addr.String())
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

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
		done = time.Now().Add(5 * time.Second)
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
	client := lazyhttp.NewClient(lazyhttp.WithHost(addr))

	req, err := lazyhttp.NewRequestWithContext(ctx, http.MethodGet, "/some/path/")
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}

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
		return obj.n < 100
	}

	for inc(obj) {
		select {
		case <-ctx.Done():
			t.Log("context done")
			return
		case <-time.After(25 * time.Millisecond):
			t.Logf("tick %d", obj.n)

			updated := testObj{
				n: obj.n + 1,
			}

			obj = &updated
		}
	}
}

func TestRetryHookRetry(t *testing.T) {
	reqCounter := 0
	expectedTries := 5

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCounter = reqCounter + 1
		if reqCounter > (expectedTries - 1) {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusServiceUnavailable)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}

	client := lazyhttp.NewClient(
		lazyhttp.WithHost(addr),
		// the retry hook looks for a status code of 503 and will return when found
		lazyhttp.WithRetryHook(func(res *http.Response) bool {
			return res.StatusCode == http.StatusServiceUnavailable
		}),
		// the backoff implementation will wait 25ms between each retry and will try 5 times
		lazyhttp.WithBackoff(func() lazyhttp.Backoff {
			return lazyhttp.NewLimitedTriesBackoff(100*time.Millisecond, expectedTries)
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}

	res, err := client.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
		return
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("unexpected status code: %d", res.StatusCode)
		return
	}

	if reqCounter != expectedTries {
		t.Errorf("unexpected number of requests: %d", reqCounter)
		return
	}
}
