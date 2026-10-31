// Package profile stores subscription-backed configurations on disk.
package profile

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"gopurr/internal/localize"
	"gopurr/internal/patch"
	"gopurr/internal/securefs"
)

// Info is the traffic quota some providers put in the subscription-userinfo
// response header: "upload=1; download=2; total=3; expire=1700000000".
type Info struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}

// Used is the traffic spent so far.
func (i Info) Used() int64 { return i.Upload + i.Download }

// Kinds of profile source.
const (
	KindURL       = "url"
	KindFile      = "file"
	KindAggregate = "aggregate"
)

// Profile is one saved configuration: a subscription link or a local file.
type Profile struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Kind        string                  `json:"kind,omitempty"` // empty on entries from before local files
	URL         string                  `json:"url,omitempty"`
	Path        string                  `json:"path,omitempty"`
	File        string                  `json:"file"`
	Updated     time.Time               `json:"updated"`
	Info        Info                    `json:"info"`
	UserAgent   string                  `json:"userAgent,omitempty"`
	UpdateHours *int                    `json:"updateHours,omitempty"`
	UseProxy    bool                    `json:"useProxy,omitempty"`
	Overrides   *patch.ProfileOverrides `json:"overrides,omitempty"`
	Aggregate   *AggregateSpec          `json:"aggregate,omitempty"`
}

// IsFile reports a local file rather than a subscription.
func (p Profile) IsFile() bool { return p.Kind == KindFile }

func (p Profile) IsAggregate() bool { return p.Kind == KindAggregate }

// Store is the profiles directory plus its index.
type Store struct {
	mu       sync.Mutex
	dir      string
	Items    []Profile `json:"profiles"`
	Active   string    `json:"active"`
	proxyURL func() *url.URL
	last     string // last error from a background refresh
}

// Open loads (or creates) the store under dir.
func Open(dir string) (*Store, error) {
	if err := securefs.PrivateDir(dir); err != nil {
		return nil, err
	}
	if err := securefs.PrivateDir(filepath.Join(dir, "profiles")); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if err == nil {
		if err := securefs.PrivateFile(filepath.Join(dir, "profiles.json")); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf(localize.Text("profiles.json 损坏: %w"), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Entries written before local files existed have no kind; they are URLs.
	for i := range s.Items {
		if filepath.Base(s.Items[i].File) != s.Items[i].File || !strings.HasSuffix(s.Items[i].File, ".yaml") {
			return nil, errors.New(localize.Text("订阅文件路径无效"))
		}
		if err := securefs.PrivateFile(filepath.Join(dir, "profiles", s.Items[i].File)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if s.Items[i].Kind == "" {
			s.Items[i].Kind = KindURL
		}
	}
	return s, nil
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return securefs.WriteFile(filepath.Join(s.dir, "profiles.json"), b, 0600)
}

// List returns the profiles in a stable order.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Profile, len(s.Items))
	for i, p := range s.Items {
		out[i] = cloneProfile(p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// ActiveID is the profile the kernel should run.
func (s *Store) ActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Active
}

// SetActive marks a profile as the one to run.
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.Active
	commit := func() error {
		s.Active = id
		if err := s.save(); err != nil {
			s.Active = previous
			return err
		}
		return nil
	}
	if id == "" {
		return commit()
	}
	for _, p := range s.Items {
		if p.ID == id {
			return commit()
		}
	}
	return errors.New(localize.Text("没有这个订阅"))
}

// Remove deletes a profile and its file.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Items {
		if p.Aggregate == nil {
			continue
		}
		for _, source := range p.Aggregate.Sources {
			if source.ID == id {
				return fmt.Errorf(localize.Text("配置仍被聚合订阅 %q 使用，请先修改或删除该聚合"), p.Name)
			}
		}
	}
	for i, p := range s.Items {
		if p.ID != id {
			continue
		}
		_ = os.Remove(filepath.Join(s.dir, "profiles", p.File))
		s.Items = append(s.Items[:i], s.Items[i+1:]...)
		if s.Active == id {
			s.Active = ""
		}
		return s.save()
	}
	return nil
}

// Content returns the stored YAML of a profile.
func (s *Store) Content(id string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Items {
		if p.ID == id {
			return os.ReadFile(filepath.Join(s.dir, "profiles", p.File))
		}
	}
	return nil, errors.New(localize.Text("没有这个订阅"))
}

// Add fetches a subscription URL and stores it. name may be empty when the
// provider sends a profile-title header.
func (s *Store) Add(address, name string) (Profile, error) {
	return s.AddChecked(address, name, nil)
}

// AddFile imports a local configuration file.
func (s *Store) AddFile(path, name string) (Profile, error) {
	return s.AddFileChecked(path, name, nil)
}

// validate rejects anything that is not a usable mihomo config.
func validate(body []byte) error {
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return errors.New(localize.Text("不是合法 YAML: ") + err.Error())
	}
	if len(doc) == 0 {
		return errors.New(localize.Text("配置为空"))
	}
	return nil
}

// Refresh re-downloads an existing profile.
func (s *Store) Refresh(id string) error { return s.RefreshChecked(id, nil) }

// RefreshChecked validates/applies the candidate before replacing the cache.
func (s *Store) RefreshChecked(id string, accept func([]byte) error) error {
	s.mu.Lock()
	var target Profile
	found := false
	for _, p := range s.Items {
		if p.ID == id {
			target, found = p, true
		}
	}
	s.mu.Unlock()
	if !found {
		return errors.New(localize.Text("没有这个订阅"))
	}

	var body []byte
	var info Info
	var title string
	var err error

	if target.IsAggregate() {
		if target.Aggregate == nil {
			return errors.New(localize.Text("聚合配置缺少来源设置"))
		}
		body, _, err = s.BuildAggregate(*target.Aggregate)
		if err != nil {
			return err
		}
	} else if target.IsFile() {
		path := target.Path
		if path == "" {
			path = filepath.Join(s.dir, "profiles", target.File)
		}
		body, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf(localize.Text("读取本地文件失败: %w"), err)
		}
	} else {
		body, info, title, err = s.fetchProfile(target)
		if err != nil {
			return err
		}
	}
	if err := validate(body); err != nil {
		return err
	}
	if accept != nil {
		if err := accept(body); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.Items {
		if p.ID != id {
			continue
		}
		path := filepath.Join(s.dir, "profiles", p.File)
		previous, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writeContent(path, body); err != nil {
			return err
		}
		s.Items[i].Updated = time.Now()
		s.Items[i].Info = info
		if p.Name == "" || p.Name == hostOf(p.URL) {
			if title != "" {
				s.Items[i].Name = title
			}
		}
		if err := s.save(); err != nil {
			s.Items[i] = p
			return errors.Join(err, writeContent(path, previous))
		}
		return nil
	}
	return errors.New(localize.Text("没有这个订阅"))
}

func writeContent(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func fetch(url string) (body []byte, info Info, title string, err error) {
	return fetchWith(url, "", nil)
}

func fetchWith(address, userAgent string, proxy *url.URL) (body []byte, info Info, title string, err error) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, Info{}, "", err
	}
	if userAgent == "" {
		userAgent = "clash.meta/mihomo"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Explicit direct download unless this profile opts in.
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 30 * time.Second, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, Info{}, "", fmt.Errorf(localize.Text("下载失败: %w"), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, Info{}, "", fmt.Errorf(localize.Text("下载失败: HTTP %d"), resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, Info{}, "", err
	}
	if len(body) == 0 {
		return nil, Info{}, "", errors.New(localize.Text("订阅内容为空"))
	}

	info = parseUserinfo(resp.Header.Get("subscription-userinfo"))
	title = decodeTitle(resp.Header.Get("profile-title"))
	return body, info, title, nil
}

func parseUserinfo(raw string) Info {
	var info Info
	for _, part := range strings.Split(raw, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, ok := usageNumber(strings.TrimSpace(value))
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "upload":
			info.Upload = n
		case "download":
			info.Download = n
		case "total":
			info.Total = n
		case "expire":
			info.Expire = n
		}
	}
	return info
}

var usageDecimal = regexp.MustCompile(`^\+?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]{1,3})?$`)

func usageNumber(raw string) (int64, bool) {
	if len(raw) > 128 || !usageDecimal.MatchString(raw) {
		return 0, false
	}
	n, ok := new(big.Rat).SetString(raw)
	if !ok || n.Sign() < 0 {
		return 0, false
	}
	whole := new(big.Int).Quo(n.Num(), n.Denom())
	return whole.Int64(), whole.IsInt64()
}

// decodeTitle accepts both "base64:..." and plain text, as providers disagree.
func decodeTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "base64:") {
		if b, err := base64Decode(strings.TrimPrefix(raw, "base64:")); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return raw
}

func idOf(url string) string {
	sum := sha1.Sum([]byte(url))
	return hex.EncodeToString(sum[:])[:12]
}

func hostOf(url string) string {
	s := url
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return localize.Text("订阅")
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
