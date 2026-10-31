package profile

import (
	"fmt"
	"net/url"
	"strings"

	"gopurr/internal/localize"
)

func (s *Store) SetProxyURL(fn func() *url.URL) { s.mu.Lock(); s.proxyURL = fn; s.mu.Unlock() }

func (s *Store) fetchProfile(p Profile) ([]byte, Info, string, error) {
	var proxy *url.URL
	if p.UseProxy {
		s.mu.Lock()
		fn := s.proxyURL
		s.mu.Unlock()
		if fn != nil {
			proxy = fn()
		}
		if proxy == nil {
			return nil, Info{}, "", fmt.Errorf("%s", localize.Text("使用代理更新需要先启动内核，请启动后重试或关闭此选项"))
		}
	}
	return fetchWith(p.URL, p.UserAgent, proxy)
}

func ValidateSettings(p Profile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%s", localize.Text("请填写配置名称"))
	}
	if !p.IsFile() {
		u, err := url.Parse(p.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || strings.ContainsAny(p.URL, " \t\r\n") {
			return fmt.Errorf("%s", localize.Text("请输入完整的 HTTP 或 HTTPS 订阅链接"))
		}
		if strings.ContainsAny(p.UserAgent, "\r\n") || len(p.UserAgent) > 512 {
			return fmt.Errorf("%s", localize.Text("UA 不能包含换行，且不能超过 512 字节"))
		}
		if p.UpdateHours != nil && (*p.UpdateHours < 0 || *p.UpdateHours > 8760) {
			return fmt.Errorf("%s", localize.Text("更新间隔须为 0–8760 小时；0 表示关闭"))
		}
	}
	return nil
}

// Edit changes only source metadata. Cached YAML and the active selection stay
// intact until the next successful refresh, even when the URL is changed.
func (s *Store) Edit(p Profile) error {
	if err := ValidateSettings(p); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.Items {
		if old.ID != p.ID {
			continue
		}
		next := old
		next.Name = strings.TrimSpace(p.Name)
		next.URL, next.UserAgent, next.UpdateHours, next.UseProxy = p.URL, p.UserAgent, p.UpdateHours, p.UseProxy
		s.Items[i] = next
		if err := s.save(); err != nil {
			s.Items[i] = old
			return err
		}
		return nil
	}
	return fmt.Errorf("%s", localize.Text("没有这个订阅"))
}
