package update

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestProxyRoutingOnlyAppliesToUpdateRequests(t *testing.T) {
	proxy, _ := url.Parse("http://127.0.0.1:7890")
	var direct, proxied int
	response := func() *http.Response {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}
	}
	router := &proxyTransport{
		selectProxy: func() *url.URL { return proxy },
		base:        roundTripFunc(func(*http.Request) (*http.Response, error) { direct++; return response(), nil }),
		proxied: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			proxied++
			if req.Context().Value(updateProxyKey{}) != proxy {
				t.Fatal("selected proxy was not attached to request")
			}
			return response(), nil
		}),
	}
	for _, ctx := range []context.Context{context.Background(), requestContext(context.Background())} {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://github.com/csic21/Gopurr/releases/latest", nil)
		resp, err := router.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if direct != 1 || proxied != 1 {
		t.Fatalf("routing direct=%d proxy=%d", direct, proxied)
	}
	router.selectProxy = func() *url.URL { return nil }
	req, _ := http.NewRequestWithContext(requestContext(context.Background()), "GET", "https://github.com", nil)
	resp, err := router.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if direct != 2 {
		t.Fatal("no-proxy fallback failed")
	}
}
