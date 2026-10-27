package app

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/patch"
)

func TestFeatureSettingsSaveAndFollow(t *testing.T) {
	a := extensionTestApp(t)
	a.resetDNSDraft()
	a.resetSnifferDraft()
	a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
	a.dnsDraft.Servers = " 1.1.1.1 \n\nhttps://dns.example.com/dns-query"
	a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
	a.snifferDraft.TLS = "443,8443"
	a.snifferDraft.SkipDomain = "+.push.apple.com"
	pendingSniff := a.snifferDraft
	a.saveDNSSettings()
	if a.dnsError != "" || a.ov.DNS == nil || !a.ov.DNS.Enable || a.snifferDraft != pendingSniff || a.ov.Sniffer != nil {
		t.Fatalf("DNS save applied another draft: %q %+v", a.dnsError, a.ov)
	}
	a.saveSnifferSettings()
	if a.snifferError != "" || a.ov.Sniffer == nil || !a.ov.Sniffer.Enable {
		t.Fatalf("sniffer save: %s", a.snifferError)
	}
	var saved patch.Overrides
	if err := json.Unmarshal(readExtensionFile(t, a.ovPath), &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, a.ov) || !strings.Contains(a.snifferStatus, "下次启动") {
		t.Fatal("saved state or stopped-kernel feedback lost")
	}
	// Reopening drafts uses persisted custom values, independently of current profile.
	a.ov = saved
	a.resetDNSDraft()
	a.resetSnifferDraft()
	if a.dnsDraft.Source != 1 || a.snifferDraft.Source != 1 || a.snifferDraft.SkipDomain != "+.push.apple.com" {
		t.Fatal("custom settings not restored")
	}
	a.dnsDraft.Source = 0
	a.saveDNSSettings()
	if a.ov.DNS != nil || a.ov.Sniffer == nil || a.ov.MergeYAML == "" || len(a.ov.ExtraRules) == 0 {
		t.Fatal("following DNS cleared unrelated overrides")
	}
	a.snifferDraft.Source = 0
	a.saveSnifferSettings()
	if a.ov.Sniffer != nil || a.dnsError != "" || a.snifferError != "" {
		t.Fatal("cannot return to following the profile")
	}
}

func TestFeatureSettingsValidationKeepsDraftAndFile(t *testing.T) {
	a := extensionTestApp(t)
	a.resetDNSDraft()
	a.dnsDraft.Source, a.dnsDraft.Enable = 1, true
	a.dnsDraft.Bootstrap = "https://bootstrap.example.com/dns-query"
	before := readExtensionFile(t, a.ovPath)
	a.saveDNSSettings()
	if a.dnsError == "" || a.ov.DNS != nil || a.dnsDraft == a.dnsBase || !bytes.Equal(before, readExtensionFile(t, a.ovPath)) {
		t.Fatal("invalid DNS saved or draft discarded")
	}
	a.resetSnifferDraft()
	a.snifferDraft.Source, a.snifferDraft.Enable, a.snifferDraft.QUIC = 1, true, "65536"
	a.saveSnifferSettings()
	if a.snifferError == "" || a.ov.Sniffer != nil || !bytes.Equal(before, readExtensionFile(t, a.ovPath)) {
		t.Fatal("invalid sniff ports saved")
	}
	a.ov.TunEnable = true
	a.resetDNSDraft()
	a.dnsDraft.Source, a.dnsDraft.Enable = 1, false
	a.saveDNSSettings()
	if !strings.Contains(a.dnsError, "TUN") || a.ov.DNS != nil {
		t.Fatal("disabled DNS accepted while TUN is enabled")
	}
}

func TestFeatureSettingsRunningTransaction(t *testing.T) {
	for _, failure := range []string{"none", "rejected", "write"} {
		t.Run(failure, func(t *testing.T) {
			a, reloads := newSettingsTestApp(t, func(raw string) bool { return failure == "rejected" && strings.Contains(raw, "sniffer:") })
			originalPath := a.ovPath
			before := readExtensionFile(t, originalPath)
			a.resetSnifferDraft()
			a.snifferDraft.Source, a.snifferDraft.Enable = 1, true
			if failure == "write" {
				a.ovPath = t.TempDir()
			}
			a.saveSnifferSettings()
			requests := reloads()
			if failure == "none" {
				if a.snifferError != "" || a.ov.Sniffer == nil || len(requests) != 1 || !strings.Contains(a.snifferStatus, "重新载入") {
					t.Fatalf("save: %q %+v %q", a.snifferError, a.ov, requests)
				}
			} else {
				if a.snifferError == "" || a.ov.Sniffer != nil || a.snifferDraft == a.snifferBase || !bytes.Equal(before, readExtensionFile(t, originalPath)) {
					t.Fatal("failed transaction damaged saved state")
				}
				raw, err := os.ReadFile(a.mgr.ConfigPath())
				if err != nil || strings.Contains(string(raw), "sniffer:") || !a.k.Running() {
					t.Fatalf("runtime not restored: %v\n%s", err, raw)
				}
				if failure == "write" && len(requests) != 2 {
					t.Fatalf("write failure did not reload old config: %q", requests)
				}
			}
		})
	}
}

func TestFeatureSettingsControlsAtBothSizes(t *testing.T) {
	for _, size := range [][2]int{{1060, 720}, {840, 560}} {
		a := extensionTestApp(t)
		a.prefs, a.page = defaultPrefs(), "settings"
		tr := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(theme(c.Theme().Dark))
			ui.Row(c).Fill().Children(func() {
				ui.Box(c).Width(204).FillHeight()
				ui.Column(c).Grow(1).MinWidth(0).FillHeight().Padding(22, 24).Gap(16).Children(func() { a.settingsPage(c) })
			})
		}, size[0], size[1])
		click := func(label string) {
			t.Helper()
			if err := tr.Click(label); err != nil {
				t.Fatal(err)
			}
			tr.Frame()
		}
		visible := func(label string) {
			t.Helper()
			r, ok := tr.Find(label)
			if !ok || r.X < 228 || r.X+r.W > float32(size[0]-24)+1 || r.Y < 0 || r.Y+r.H > float32(size[1]) {
				t.Fatalf("%q outside %v: %+v, %v", label, size, r, ok)
			}
		}
		click("DNS")
		visible("DNS配置方式")
		visible("DNS开关")
		visible("保存 DNS 设置")
		click("自定义")
		click("DNS开关")
		if !a.dnsDraft.Enable || a.ov.DNS != nil {
			t.Fatal("DNS switch did not edit only the draft")
		}
		pending := a.dnsDraft
		click("域名嗅探")
		visible("域名嗅探配置方式")
		visible("域名嗅探开关")
		visible("保存嗅探设置")
		click("自定义")
		click("域名嗅探开关")
		click("保存嗅探设置")
		if a.ov.Sniffer == nil || !a.ov.Sniffer.Enable || a.snifferError != "" {
			t.Fatalf("sniffer controls failed to save: %s", a.snifferError)
		}
		click("DNS")
		if a.dnsDraft != pending {
			t.Fatal("switching tabs lost DNS draft")
		}
		click("保存 DNS 设置")
		if a.ov.DNS == nil || !a.ov.DNS.Enable || a.dnsError != "" {
			t.Fatalf("DNS controls failed to save: %s", a.dnsError)
		}
		click("跟随配置")
		click("保存 DNS 设置")
		if a.ov.DNS != nil {
			t.Fatal("follow control did not remove custom settings")
		}
	}
}

func TestOverviewFeatureSwitchUsesEffectiveProfile(t *testing.T) {
	a := extensionTestApp(t)
	a.cfgText = "dns: {enable: true, nameserver: [9.9.9.9]}\nsniffer: {enable: false}\nrules: [\"MATCH,DIRECT\"]\n"
	tr := ui.NewTester(a.overviewFeatures, 588, 240)
	if err := tr.Click("概览域名嗅探开关"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.ov.Sniffer == nil || !a.ov.Sniffer.Enable || a.ov.DNS != nil {
		t.Fatal("overview switch failed or changed DNS source")
	}
	if err := tr.Click("概览DNS开关"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if a.ov.DNS == nil || a.ov.DNS.Enable || !reflect.DeepEqual(a.ov.DNS.Nameserver, []string{"9.9.9.9"}) {
		t.Fatal("overview DNS switch discarded profile settings")
	}
	if err := tr.Click("DNS 设置"); err != nil {
		t.Fatal(err)
	}
	if a.page != "settings" || a.settingsTab != 4 {
		t.Fatal("overview settings entry did not navigate")
	}
}
