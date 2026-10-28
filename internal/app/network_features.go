package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
)

type dnsDraft struct {
	Source                                     int
	Enable, IPv6                               bool
	Mode, Servers, Bootstrap, Fallback, Filter string
}

type snifferDraft struct {
	Source                                                     int
	Enable, Override, Mapping, PureIP                          bool
	HTTP, TLS, QUIC, SkipDomain, ForceDomain, SkipSrc, SkipDst string
}

type featureState struct {
	dnsReady, snifferReady                                   bool
	dnsDraft, dnsBase                                        dnsDraft
	snifferDraft, snifferBase                                snifferDraft
	dnsError, dnsStatus, snifferError, snifferStatus         string
	featureCacheAt                                           time.Time
	featureCacheKey, featureCacheConfig, featureCacheProfile string
	featureDNS                                               patch.DNSSettings
	featureSniffer                                           patch.SnifferSettings
	featureCacheError                                        error
}

func (a *App) effectiveFeatures() (patch.DNSSettings, patch.SnifferSettings, error) {
	// Large subscriptions should not be parsed on every UI frame. Changes to
	// app settings/local YAML invalidate immediately; profile updates refresh
	// with the overview's one-second sampling interval.
	key, _ := json.Marshal(a.ov)
	profileID := ""
	if a.store != nil {
		profileID = a.store.ActiveID()
	}
	if string(key) == a.featureCacheKey && a.cfgText == a.featureCacheConfig &&
		profileID == a.featureCacheProfile && time.Since(a.featureCacheAt) < time.Second {
		return a.featureDNS, a.featureSniffer, a.featureCacheError
	}
	a.featureCacheAt = time.Now()
	a.featureCacheKey, a.featureCacheConfig, a.featureCacheProfile = string(key), a.cfgText, profileID
	raw, err := a.configBytes()
	if err != nil {
		a.featureDNS, a.featureSniffer, a.featureCacheError = patch.DefaultDNSSettings(), patch.DefaultSnifferSettings(), err
	} else {
		a.featureDNS, a.featureSniffer, a.featureCacheError = patch.FeatureSettings(raw)
	}
	return a.featureDNS, a.featureSniffer, a.featureCacheError
}

func (a *App) resetDNSDraft() {
	d, _, err := a.effectiveFeatures()
	if err != nil {
		a.dnsError = err.Error()
	}
	source := 0
	if a.ov.DNS != nil {
		d = *a.ov.DNS
		source = 1
	}
	a.dnsDraft = dnsDraft{Source: source, Enable: d.Enable, IPv6: d.IPv6, Mode: d.EnhancedMode,
		Servers: strings.Join(d.Nameserver, "\n"), Bootstrap: strings.Join(d.DefaultNameserver, "\n"),
		Fallback: strings.Join(d.Fallback, "\n"), Filter: strings.Join(d.FakeIPFilter, "\n")}
	a.dnsBase, a.dnsReady = a.dnsDraft, true
}

func (a *App) resetSnifferDraft() {
	_, s, err := a.effectiveFeatures()
	if err != nil {
		a.snifferError = err.Error()
	}
	source := 0
	if a.ov.Sniffer != nil {
		s = *a.ov.Sniffer
		source = 1
	}
	a.snifferDraft = snifferDraft{Source: source, Enable: s.Enable, Override: s.OverrideDestination,
		Mapping: s.ForceDNSMapping, PureIP: s.ParsePureIP,
		HTTP: strings.Join(s.HTTPPorts, ","), TLS: strings.Join(s.TLSPorts, ","), QUIC: strings.Join(s.QUICPorts, ","),
		SkipDomain: strings.Join(s.SkipDomain, "\n"), ForceDomain: strings.Join(s.ForceDomain, "\n"),
		SkipSrc: strings.Join(s.SkipSrcAddress, "\n"), SkipDst: strings.Join(s.SkipDstAddress, "\n")}
	a.snifferBase, a.snifferReady = a.snifferDraft, true
}

func featureLines(raw string) []string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (d dnsDraft) settings() *patch.DNSSettings {
	if d.Source == 0 {
		return nil
	}
	return &patch.DNSSettings{Enable: d.Enable, IPv6: d.IPv6, EnhancedMode: d.Mode,
		Nameserver: featureLines(d.Servers), DefaultNameserver: featureLines(d.Bootstrap),
		Fallback: featureLines(d.Fallback), FakeIPFilter: featureLines(d.Filter)}
}

func (s snifferDraft) settings() (*patch.SnifferSettings, error) {
	if s.Source == 0 {
		return nil, nil
	}
	http, err := patch.ParseSniffPorts(s.HTTP)
	if err != nil {
		return nil, fmt.Errorf("HTTP：%w", err)
	}
	tls, err := patch.ParseSniffPorts(s.TLS)
	if err != nil {
		return nil, fmt.Errorf("TLS：%w", err)
	}
	quic, err := patch.ParseSniffPorts(s.QUIC)
	if err != nil {
		return nil, fmt.Errorf("QUIC：%w", err)
	}
	return &patch.SnifferSettings{Enable: s.Enable, OverrideDestination: s.Override, ForceDNSMapping: s.Mapping,
		ParsePureIP: s.PureIP, HTTPPorts: http, TLSPorts: tls, QUICPorts: quic,
		SkipDomain: featureLines(s.SkipDomain), ForceDomain: featureLines(s.ForceDomain),
		SkipSrcAddress: featureLines(s.SkipSrc), SkipDstAddress: featureLines(s.SkipDst)}, nil
}

func (a *App) saveDNSSettings() {
	a.dnsError, a.dnsStatus = "", ""
	next := a.ov
	next.DNS = a.dnsDraft.settings()
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.dnsError = err.Error()
		return
	}
	a.resetDNSDraft()
	a.dnsStatus = extensionSavedStatus(applied)
}

func (a *App) saveSnifferSettings() {
	a.snifferError, a.snifferStatus = "", ""
	s, err := a.snifferDraft.settings()
	if err != nil {
		a.snifferError = err.Error()
		return
	}
	next := a.ov
	next.Sniffer = s
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.snifferError = err.Error()
		return
	}
	a.resetSnifferDraft()
	a.snifferStatus = extensionSavedStatus(applied)
}

func (a *App) featureSettingsPage(c *ui.Context, dns bool) {
	if dns && (!a.dnsReady || (a.dnsDraft.Source == 0 && a.dnsDraft == a.dnsBase)) {
		a.resetDNSDraft()
	}
	if !dns && (!a.snifferReady || (a.snifferDraft.Source == 0 && a.snifferDraft == a.snifferBase)) {
		a.resetSnifferDraft()
	}
	ui.Scroll(c).Key(fmt.Sprintf("feature-%t", dns)).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(16).Padding(0, 0, 8, 0).Children(func() {
			if dns {
				a.dnsSettingsFields(c)
			} else {
				a.snifferSettingsFields(c)
			}
		})
	})
	message, status, dirty := a.snifferError, a.snifferStatus, a.snifferDraft != a.snifferBase
	save, reset, label := a.saveSnifferSettings, a.resetSnifferDraft, "保存嗅探设置"
	if dns {
		message, status, dirty = a.dnsError, a.dnsStatus, a.dnsDraft != a.dnsBase
		save, reset, label = a.saveDNSSettings, a.resetDNSDraft, "保存 DNS 设置"
	}
	if message != "" {
		extensionError(c, message)
	} else if status != "" && !dirty {
		ui.Text(c, status).FillWidth().FontSize(11).TextColor(paletteFor(c).Success)
	}
	extensionSaveBar(c, dirty, label, func() {
		if dns {
			a.dnsError, a.dnsStatus = "", ""
		} else {
			a.snifferError, a.snifferStatus = "", ""
		}
		reset()
	}, save)
}

func featureSource(c *ui.Context, name string, source *int, enabled *bool, hint string) {
	settingsGroup(c, name, func() {
		settingsRow(c, "配置方式", "跟随配置时使用订阅与 YAML 覆写", func() {
			softSegmented(c, source, "跟随配置", "自定义").Label(name + "配置方式")
		})
		hairline(c)
		settingsRow(c, "启用"+name, hint, func() {
			softSwitch(c, enabled).Disabled(*source == 0).Label(name + "开关")
		})
	})
}

func featureTextArea(c *ui.Context, title, hint string, value *string, placeholder string, disabled bool) {
	pal := paletteFor(c)
	ui.Column(c).FillWidth().Gap(6).Padding(12, 16).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(pal.Text)
		ui.Text(c, hint).FillWidth().FontSize(11).TextColor(pal.Faint)
		ui.TextArea(c, value).FillWidth().Height(76).FontSize(12).Disabled(disabled).Label(title).Placeholder(placeholder)
	})
}

func (a *App) dnsSettingsFields(c *ui.Context) {
	d := &a.dnsDraft
	disabled := d.Source == 0 || !d.Enable
	featureSource(c, "DNS", &d.Source, &d.Enable, "接管域名解析；TUN 模式需要开启")
	settingsGroup(c, "解析方式", func() {
		settingsRow(c, "DNS 模式", "Fake-IP 建立域名映射；redir-host 返回真实 IP", func() {
			ui.Select(c, &d.Mode, []string{"fake-ip", "redir-host"}).Width(130).Disabled(disabled).Label("DNS 模式")
		})
		hairline(c)
		settingsRow(c, "解析 IPv6", "允许 DNS 返回 IPv6 地址", func() {
			softSwitch(c, &d.IPv6).Disabled(disabled).Label("DNS IPv6 开关")
		})
	})
	settingsGroup(c, "解析服务器", func() {
		featureTextArea(c, "DNS 解析服务器", "每行一个，支持 IP、DoH、DoT 等地址", &d.Servers, "https://doh.pub/dns-query\n1.1.1.1", disabled)
		hairline(c)
		featureTextArea(c, "引导 DNS", "用于解析 DNS 服务器自身的域名；填写 IP 地址或 system", &d.Bootstrap, "223.5.5.5\n1.1.1.1", disabled)
		hairline(c)
		featureTextArea(c, "备用 DNS", "可选，每行一个；沿用配置中的回退筛选规则", &d.Fallback, "https://dns.google/dns-query", disabled)
	})
	settingsGroup(c, "Fake-IP 例外", func() {
		featureTextArea(c, "Fake-IP 过滤列表", "每行一项，语法与配置中的过滤模式一致", &d.Filter, "*.lan\n+.local", disabled || d.Mode != "fake-ip")
	})
	featureNote(c, "自定义设置对所有订阅生效；其他 DNS 参数继续沿用配置。切回“跟随配置”并保存可移除自定义设置。")
}

func (a *App) snifferSettingsFields(c *ui.Context) {
	s := &a.snifferDraft
	disabled := s.Source == 0 || !s.Enable
	featureSource(c, "域名嗅探", &s.Source, &s.Enable, "从连接中识别域名，帮助规则准确分流")
	settingsGroup(c, "嗅探行为", func() {
		settingsRow(c, "覆盖连接地址", "使用识别出的域名作为实际连接目标", func() {
			softSwitch(c, &s.Override).Disabled(disabled).Label("覆盖连接地址")
		})
		hairline(c)
		settingsRow(c, "对真实 IP 映射嗅探", "对 redir-host 映射的连接再次识别域名", func() {
			softSwitch(c, &s.Mapping).Disabled(disabled).Label("对真实 IP 映射嗅探")
		})
		hairline(c)
		settingsRow(c, "对未映射 IP 地址嗅探", "尝试识别尚未获取域名的连接", func() {
			softSwitch(c, &s.PureIP).Disabled(disabled).Label("对未映射 IP 地址嗅探")
		})
	})
	settingsGroup(c, "协议与端口", func() {
		for i, field := range []struct {
			name        string
			value       *string
			placeholder string
		}{
			{"HTTP", &s.HTTP, "80,8080-8880"}, {"TLS", &s.TLS, "443,8443"}, {"QUIC", &s.QUIC, "443"},
		} {
			if i > 0 {
				hairline(c)
			}
			settingsRow(c, field.name+" 端口", "逗号分隔，支持范围；留空则不嗅探此协议", func() {
				ui.TextInput(c, field.value).Width(170).Disabled(disabled).Label(field.name + " 嗅探端口").Placeholder(field.placeholder)
			})
		}
	})
	settingsGroup(c, "域名例外", func() {
		featureTextArea(c, "跳过域名嗅探", "每行一个域名或通配规则", &s.SkipDomain, "+.push.apple.com", disabled)
		hairline(c)
		featureTextArea(c, "强制域名嗅探", "每行一个域名或通配规则", &s.ForceDomain, "+.v2ex.com", disabled)
	})
	settingsGroup(c, "地址例外", func() {
		featureTextArea(c, "跳过来源地址", "每行一个 IP 或 CIDR 网段", &s.SkipSrc, "192.168.1.0/24", disabled)
		hairline(c)
		featureTextArea(c, "跳过目标地址", "每行一个 IP 或 CIDR 网段", &s.SkipDst, "91.105.192.0/23", disabled)
	})
	featureNote(c, "嗅探只读取协议中可见的域名信息，不解密 HTTPS 正文。自定义设置对所有订阅生效。")
}

func featureNote(c *ui.Context, text string) {
	ui.Text(c, text).FillWidth().FontSize(11).TextColor(paletteFor(c).Faint)
}

func (a *App) toggleNetworkFeature(isDNS, enabled bool) {
	// A click must use the latest subscription even within the display cache interval.
	a.featureCacheAt = time.Time{}
	d, s, err := a.effectiveFeatures()
	if err != nil {
		a.notice = "配置读取失败：" + err.Error()
		return
	}
	next := a.ov
	if isDNS {
		if next.DNS != nil {
			d = *next.DNS
		}
		d.Enable = enabled
		next.DNS = &d
	} else {
		if next.Sniffer != nil {
			s = *next.Sniffer
		}
		s.Enable = enabled
		next.Sniffer = &s
	}
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.notice = err.Error()
		return
	}
	a.notice = extensionSavedStatus(applied)
	if a.dnsDraft == a.dnsBase {
		a.dnsReady = false
	}
	if a.snifferDraft == a.snifferBase {
		a.snifferReady = false
	}
}

// These rows complete the overview's shared network control group. Settings
// remain available even while following the subscription.
func (a *App) overviewFeatures(c *ui.Context) {
	dns, sniffer, err := a.effectiveFeatures()
	ui.Column(c).FillWidth().Children(func() {
		for i, feature := range []struct {
			name            string
			enabled, custom bool
			tab             int
		}{
			{"DNS", dns.Enable, a.ov.DNS != nil, 4}, {"域名嗅探", sniffer.Enable, a.ov.Sniffer != nil, 5},
		} {
			if i > 0 {
				hairline(c)
			}
			status := "已关闭"
			if feature.enabled {
				status = "已开启"
			}
			if feature.custom {
				status += " · 自定义"
			} else {
				status += " · 跟随配置"
			}
			if err != nil {
				status = "配置读取失败，请检查设置"
			}
			color := paletteFor(c).Muted
			if err != nil {
				color = paletteFor(c).Danger
			}
			overviewControlRow(c, feature.name, status, color, func() {
				softButton(c, feature.name+" 设置").OnClick(func() {
					a.page, a.settingsTab = "settings", feature.tab
				})
				enabled := feature.enabled
				softSwitch(c, &enabled).Disabled(err != nil).Label("概览" + feature.name + "开关").
					Tooltip("切换后使用自定义设置；可在设置中恢复跟随配置").OnChange(func() {
					a.toggleNetworkFeature(feature.tab == 4, enabled)
				})
			})
		}
	})
}
