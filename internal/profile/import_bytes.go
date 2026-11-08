package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// AddBytesChecked imports a document supplied by a native file picker or editor.
// The cache is its source: no desktop path needs to survive a mobile sandbox.
func (s *Store) AddBytesChecked(name string, body []byte, accept func(Profile, []byte) error) (Profile, error) {
	return s.AddBytesCheckedContext(context.Background(), name, body, accept)
}
func (s *Store) AddBytesCheckedContext(ctx context.Context, name string, body []byte, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	sum := sha256.Sum256(body)
	id := "local-" + hex.EncodeToString(sum[:])[:16]
	p := Profile{ID: id, Name: firstNonEmpty(strings.TrimSpace(name), "配置"), Kind: KindFile, File: id + ".yaml", Updated: time.Now()}
	return s.importCheckedContext(ctx, p, body, name != "", accept)
}

// AddDownloadedChecked commits an already downloaded HTTP subscription. Hosts
// own downloading so their lifecycle context can cancel it before any commit.
func (s *Store) AddDownloadedChecked(address, name string, body []byte, headers http.Header, accept func(Profile, []byte) error) (Profile, error) {
	return s.AddDownloadedCheckedContext(context.Background(), address, name, body, headers, accept)
}
func (s *Store) AddDownloadedCheckedContext(ctx context.Context, address, name string, body []byte, headers http.Header, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	id := idOf(address)
	p := Profile{ID: id, Name: firstNonEmpty(strings.TrimSpace(name), decodeTitle(headers.Get("profile-title")), hostOf(address)), Kind: KindURL, URL: address, File: id + ".yaml", Updated: time.Now(), Info: parseUserinfo(headers.Get("subscription-userinfo"))}
	return s.importCheckedContext(ctx, p, body, name != "", accept)
}
