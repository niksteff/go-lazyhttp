package lazyhttp_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/niksteff/lazyhttp"
)

const (
	B  = 1
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB
)

func TestDecodeToBytes(t *testing.T) {
	testData := []byte(`{"value": "foo"}`)
	r := bytes.NewBuffer(testData)
	rc := io.NopCloser(r)

	decoded, err := lazyhttp.DecodeBytes(rc)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if !bytes.Equal(decoded, testData) {
		t.Errorf("unexpected value: %s expected %s", decoded, testData)
	}
}

func TestDecodeJson(t *testing.T) {
	d := `{
		"foo": "bar"
	}`

	type res struct {
		Foo string `json:"foo"`
	}

	var tmp res
	err := lazyhttp.DecodeJson(io.NopCloser(strings.NewReader(d)), &tmp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	t.Logf("result: %#v", tmp)
}

func TestDecodeJsonLimit(t *testing.T) {
	d := `{
		"foo": "bar"
	}`

	type res struct {
		Foo string `json:"foo"`
	}

	var tmp res
	err := lazyhttp.DecodeJson(io.NopCloser(io.LimitReader(strings.NewReader(d), 1*KB)), &tmp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	t.Logf("result: %#v", tmp)
}

func TestDecodeJsonLimitTooLong(t *testing.T) {
	d := `{
		"foo": "bar"
	}`

	type res struct {
		Foo string `json:"foo"`
	}

	var tmp res
	err := lazyhttp.DecodeJson(io.NopCloser(io.LimitReader(strings.NewReader(d), 1*B)), &tmp)
	if err != nil {
		if tmp != (res{}) {
			t.Errorf("expected empty result but got: %#v", tmp)
		}
	}
}

func TestDecodeJsonFromResponse(t *testing.T) {
	type testRes struct {
		Foo string `json:"foo"`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"foo": "bar"}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := url.Parse(srv.URL)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	c := lazyhttp.New(lazyhttp.WithHost(addr))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	res, err := c.Do(req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	var resObj testRes
	err = lazyhttp.DecodeJson(res.Body, &resObj)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
}
