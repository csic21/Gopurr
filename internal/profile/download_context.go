package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"gopurr/internal/localize"
)

const MaxDocumentBytes = 16 << 20

var ErrDocumentTooLarge = errors.New("profile document exceeds 16 MiB")

// DownloadError distinguishes transport/HTTP failures from configuration
// validation, allowing API adapters to redact URLs while the desktop retains
// its established localized error messages and wrapped diagnostic cause.
type DownloadError struct {
	StatusCode int
	Cause      error
}

func (e *DownloadError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf(localize.Text("下载失败: HTTP %d"), e.StatusCode)
	}
	return fmt.Errorf(localize.Text("下载失败: %w"), e.Cause).Error()
}
func (e *DownloadError) Unwrap() error { return e.Cause }

func fetchWithContext(ctx context.Context, address, userAgent string, proxy *url.URL) (body []byte, info Info, title string, err error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, Info{}, "", err
	}
	if userAgent == "" {
		userAgent = "clash.meta/mihomo"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return ValidateSettings(Profile{Name: "download", URL: req.URL.String()})
	}}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, Info{}, "", ctx.Err()
		}
		return nil, Info{}, "", &DownloadError{Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, Info{}, "", &DownloadError{StatusCode: response.StatusCode}
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, MaxDocumentBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, Info{}, "", ctx.Err()
		}
		return nil, Info{}, "", err
	}
	if len(body) > MaxDocumentBytes {
		return nil, Info{}, "", ErrDocumentTooLarge
	}
	if len(body) == 0 {
		return nil, Info{}, "", errors.New(localize.Text("订阅内容为空"))
	}
	if err := ctx.Err(); err != nil {
		return nil, Info{}, "", err
	}
	return body, parseUserinfo(response.Header.Get("subscription-userinfo")), decodeTitle(response.Header.Get("profile-title")), nil
}
