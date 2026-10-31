// Package diagnostic checks individual network paths without changing proxy,
// DNS or routing settings. Reports contain classified errors, never raw URLs,
// credentials, resolver answers or local file paths.
package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type State string

var ErrOldController = errors.New("installed controller needs update")

const (
	Pending State = "等待"
	Running State = "检测中"
	Passed  State = "通过"
	Failed  State = "失败"
	Info    State = "提示"
	Skipped State = "跳过"
)

type Step struct {
	Name     string
	State    State
	Detail   string
	Duration time.Duration
}

type Options struct {
	URL         string
	Port        int
	Probe       func(context.Context) error
	Direct      func(context.Context, string) error // kernel DIRECT adapter bypasses TUN interception
	ProxyStatus func(context.Context) (enabled, matches bool, err error)
	KernelDNS   func(context.Context, string) error // nil when built-in DNS is disabled
	Lookup      func(context.Context, string) ([]string, error)
	Timeout     time.Duration
}

func Steps() []Step {
	names := []string{"内核控制接口", "本地代理端口", "系统代理配置", "系统 DNS 解析", "内核 DNS 解析", "直连请求", "代理请求"}
	steps := make([]Step, len(names))
	for i, name := range names {
		steps[i] = Step{Name: name, State: Pending}
	}
	return steps
}

// Run publishes copies after each transition. Failure in one path never hides
// the other paths, so a blocked direct route can coexist with a working proxy.
func Run(ctx context.Context, opt Options, publish func([]Step)) []Step {
	steps := Steps()
	emit := func() {
		if publish != nil {
			publish(append([]Step(nil), steps...))
		}
	}
	parsed, err := url.Parse(opt.URL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(opt.URL, " \t\r\n") || opt.Port < 1 || opt.Port > 65535 {
		for i := range steps {
			steps[i].State, steps[i].Detail = Skipped, "请填写有效的 HTTP / HTTPS 测试地址及代理端口"
		}
		emit()
		return steps
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 5 * time.Second
	}
	if opt.Lookup == nil {
		opt.Lookup = net.DefaultResolver.LookupHost
	}
	host := parsed.Hostname()
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(opt.Port))}
	domain := true
	if _, err := netip.ParseAddr(host); err == nil {
		domain = false
	}
	check := []func(context.Context) (State, string){
		func(ctx context.Context) (State, string) {
			if opt.Probe == nil {
				return Failed, "内核客户端不可用，请重启应用"
			}
			if err := opt.Probe(ctx); err != nil {
				return Failed, "内核未响应，请在概览启动内核或检查 TUN 服务"
			}
			return Passed, "内核已响应，控制接口可用"
		},
		func(ctx context.Context) (State, string) {
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyURL.Host)
			if err != nil {
				return Failed, "代理端口不可连接，请检查内核状态与端口设置"
			}
			conn.Close()
			return Passed, "本地 TCP 监听可连接；代理请求将验证转发能力"
		},
		func(ctx context.Context) (State, string) {
			if opt.ProxyStatus == nil {
				return Info, "当前平台不支持读取系统代理；TUN 不依赖此设置"
			}
			enabled, matches, err := opt.ProxyStatus(ctx)
			if err != nil {
				return Failed, "无法读取系统代理，请检查系统网络设置"
			}
			if !enabled {
				return Info, "系统代理未开启；使用 TUN 时可忽略"
			}
			if !matches {
				return Info, "系统代理指向其他地址；如需接管，请在概览开启系统代理"
			}
			return Passed, "系统代理已指向 Gopurr"
		},
		func(ctx context.Context) (State, string) {
			if !domain {
				return Skipped, "测试地址使用 IP，无需域名解析"
			}
			answers, err := opt.Lookup(ctx, host)
			if err != nil || len(answers) == 0 {
				return Failed, "系统 DNS 解析失败，请检查当前网络与系统 DNS"
			}
			return Passed, "系统解析器已返回地址；结果不会写入报告"
		},
		func(ctx context.Context) (State, string) {
			if opt.KernelDNS == nil {
				return Skipped, "当前配置未开启内置 DNS"
			}
			if !domain {
				return Skipped, "测试地址使用 IP，无需域名解析"
			}
			if err := opt.KernelDNS(ctx, host); errors.Is(err, ErrOldController) {
				return Info, "旧服务不支持内核 DNS 诊断，请在概览重新安装 TUN 服务"
			} else if err != nil {
				return Failed, "内核 DNS 解析失败，请检查 DNS 服务器与解析规则"
			}
			return Passed, "内核 DNS 已返回地址；结果不会写入报告"
		},
		func(ctx context.Context) (State, string) {
			if opt.Direct != nil {
				if err := opt.Direct(ctx, opt.URL); err != nil {
					return Failed, "DIRECT 出站未能收到响应，请检查网络、DNS 与测试地址"
				}
				return Passed, "内核 DIRECT 出站已收到响应，不经过策略规则"
			}
			return checkHTTP(ctx, opt.URL, nil)
		},
		func(ctx context.Context) (State, string) { return checkHTTP(ctx, opt.URL, proxyURL) },
	}
	emit()
	for i, run := range check {
		if ctx.Err() != nil {
			steps[i].State, steps[i].Detail = Skipped, "已取消"
			emit()
			continue
		}
		steps[i].State = Running
		emit()
		started := time.Now()
		stepCtx, cancel := context.WithTimeout(ctx, opt.Timeout)
		state, detail := run(stepCtx)
		cancel()
		if ctx.Err() != nil {
			state, detail = Skipped, "已取消"
		}
		steps[i].State, steps[i].Detail, steps[i].Duration = state, detail, time.Since(started)
		emit()
	}
	return steps
}

func checkHTTP(ctx context.Context, address string, proxy *url.URL) (State, string) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // Never inherit HTTP_PROXY for the direct check.
	if proxy != nil {
		transport.Proxy = http.ProxyURL(proxy)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return Failed, "测试地址无效，请检查设置"
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Failed, "请求超时，请检查网络或更换测试地址"
		}
		return Failed, "连接或 TLS 握手失败，请检查网络、节点与测试地址"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return Failed, fmt.Sprintf("测试服务器返回 HTTP %d，可尝试更换测试地址", resp.StatusCode)
	}
	return Passed, fmt.Sprintf("收到 HTTP %d 响应", resp.StatusCode)
}

func Report(steps []Step) string {
	var b strings.Builder
	b.WriteString("Gopurr 连通诊断\n报告不包含测试地址、订阅链接、密钥、进程路径和 DNS 解析结果。\n")
	for _, step := range steps {
		fmt.Fprintf(&b, "\n%s：%s\n%s\n", step.Name, step.State, step.Detail)
	}
	return b.String()
}
