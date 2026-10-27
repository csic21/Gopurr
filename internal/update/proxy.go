package update

import (
	"context"
	"net/http"
	"net/url"
)

type updateRequestKey struct{}
type updateProxyKey struct{}

func requestContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, updateRequestKey{}, true)
}

// ConfigureHTTPClient is called once before the UI starts. Only requests from
// Native.Check/Install use the selected proxy; unrelated HTTP clients keep
// their existing behavior. This is necessary for GUI apps launched without
// HTTP_PROXY/HTTPS_PROXY in their environment.
func ConfigureHTTPClient(selectProxy func() *url.URL) {
	client := *http.DefaultClient
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	proxied := http.DefaultTransport.(*http.Transport).Clone()
	proxied.Proxy = func(req *http.Request) (*url.URL, error) {
		proxy, _ := req.Context().Value(updateProxyKey{}).(*url.URL)
		return proxy, nil
	}
	client.Transport = &proxyTransport{base: base, proxied: proxied, selectProxy: selectProxy}
	http.DefaultClient = &client
}

type proxyTransport struct {
	base, proxied http.RoundTripper
	selectProxy   func() *url.URL
}

func (p *proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if marked, _ := req.Context().Value(updateRequestKey{}).(bool); marked {
		if proxy := p.selectProxy(); proxy != nil {
			ctx := context.WithValue(req.Context(), updateProxyKey{}, proxy)
			return p.proxied.RoundTrip(req.Clone(ctx))
		}
	}
	return p.base.RoundTrip(req)
}
