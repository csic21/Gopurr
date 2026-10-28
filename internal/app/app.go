package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"gopurr/internal/core"
	"gopurr/internal/kernel"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"
	"gopurr/internal/update"
)

const (
	bars = 72
)

type sample struct {
	up   int64
	down int64
}

type navItem struct {
	id    string
	label string
	icon  *ui.SVG
}

var navItems = []navItem{
	{"overview", "概览", iconOverview},
	{"subs", "订阅", iconSubs},
	{"proxies", "代理", iconProxies},
	{"conns", "连接", iconConns},
	{"rules", "规则", iconRules},
	{"logs", "日志", iconLogs},
	{"settings", "设置", iconSettings},
}

// App is the whole UI state. Every field is read on the UI goroutine and
// written on it or under mu by the sampler.
type App struct {
	softwareUpdates     *update.Controller
	stopSoftwareUpdates context.CancelFunc
	proxyState
	settingsState
	extensionState
	featureState
	subscriptionEditState
	connectionState
	backupState

	mu   sync.Mutex
	hist []sample
	last core.Sample

	initErr    string
	page       string
	proxyQuery string
	modeToken  string
	modePick   string
	cfgPath    string
	cfgText    string
	notice     string

	// async-written state, guarded by bmu
	bmu      sync.Mutex
	delays   map[string]uint16
	testing  string
	subBusy  bool
	subErr   string
	sysBusy  bool
	sysErr   string
	sysState sysproxy.State

	svcInstalled bool
	svcBusy      bool
	svcErr       string
	// tunWanted remembers that TUN was on before its service turned out to be
	// unusable, so a repaired service can pick the preference up again.
	tunWanted bool

	confirmInstall   bool
	confirmUninstall bool

	pmu             sync.Mutex
	prefs           Prefs
	prefsPath       string
	prefsHook       func(Prefs)
	applyAutoLaunch func(bool) error
	subAttempts     map[string]time.Time

	lightOn       bool
	lightDelayIdx int
	autoIdx       int
	lanOn         bool
	ipv6On        bool

	ruleQuery   string
	ruleAdapter string

	logLevelIdx int
	logPaused   bool
	logFrozen   []core.LogLine
	logScroll   ui.ScrollState

	showRaw     bool
	rawDraft    string
	rawReadOnly bool
	rawError    string

	store    *profile.Store
	ov       patch.Overrides
	ovPath   string
	prevPath string
	prev     *sysproxy.State
	proxy    sysproxy.Controller

	sysOn      bool
	sysApplied bool
	tunOn      bool

	deleteSubID   string
	deleteSubName string
	showDeleteSub bool

	subURL  string
	subName string

	dir string
	k   *kernel.Kernel
	mgr *kernel.Manager

	tunErr string

	smu     sync.Mutex
	svc     ui.Services
	ticking bool
	booted  bool
}

// New builds the app with a built-in demo profile so the first launch does
// something visible without a subscription.
func New() *App {
	dir, _ := appDir()

	mgr, err := kernel.NewManager(dir)
	if err != nil {
		// Without a data directory there is no kernel client at all; show the
		// reason instead of dereferencing nil on the first frame.
		log.Printf("kernel manager: %v", err)
	}

	a := &App{
		dir:       dir,
		mgr:       mgr,
		page:      "overview",
		modeToken: "rule",
		modePick:  "规则",
		cfgText:   demoConfig,
		cfgPath:   filepath.Join(dir, "local.yaml"),
		ov:        patch.Default(),
		delays:    map[string]uint16{},
	}
	switch {
	case err != nil:
		a.initErr = "数据目录不可用: " + err.Error()
	case mgr == nil:
		a.initErr = "内核客户端没有初始化"
	default:
		a.k = kernel.New(mgr)
	}

	if p := a.cfgPath; p != "" {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			a.cfgText = string(b)
		} else if os.Getenv("GOPURR_HOME") == "" {
			if legacy, err := os.ReadFile(defaultConfigPath()); err == nil && len(legacy) > 0 {
				a.cfgText = string(legacy)
			}
		}
	}
	if page := os.Getenv("GOPURR_PAGE"); page != "" {
		a.page = page
	}
	if dir != "" {
		a.prefsPath = prefsPathFor(dir)
		a.loadPrefs()
		a.lightDelayIdx = delayIndexOf(a.prefs.LightDelay)
		a.lightOn = a.prefs.LightMode
		a.autoIdx = autoUpdateIndex(a.prefs.AutoUpdateHours)
		a.ruleAdapter = allAdapters
		a.ovPath = filepath.Join(dir, "overrides.json")
		a.prevPath = filepath.Join(dir, "sysproxy.json")
		a.loadOverrides()
		a.lanOn, a.ipv6On = a.ov.AllowLAN, a.ov.IPv6
		a.modeToken = a.ov.Mode
		a.modePick = labelFor(a.modeToken)
		a.modeIdx = modeIndex(a.modeToken)
		a.loadPrevProxy()
		if st, err := profile.Open(dir); err == nil {
			a.store = st
			st.SetProxyURL(a.SubscriptionProxyURL)
			if url := os.Getenv("GOPURR_SUB_URL"); url != "" && len(st.List()) == 0 {
				if p, err := st.Add(url, ""); err == nil {
					_ = st.SetActive(p.ID)
				} else {
					log.Printf("seed subscription: %v", err)
				}
			}
		} else {
			log.Printf("profiles: %v", err)
		}
	}
	return a
}

func appDir() (string, error) {
	// GOPURR_HOME keeps profiles, overrides and logs next to the binary,
	// which is what the tests and a portable install want.
	if home := os.Getenv("GOPURR_HOME"); home != "" {
		abs, err := filepath.Abs(home)
		if err != nil {
			return home, os.MkdirAll(home, 0o700)
		}
		return abs, os.MkdirAll(abs, 0o700)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	// Capitalised, as other apps name their Application Support folder.
	dir := filepath.Join(base, "Gopurr")
	migrateLegacyDir(filepath.Join(base, "myclash"), dir)
	return dir, os.MkdirAll(dir, 0o700)
}

// migrateLegacyDir copies the old data directory once, so renaming the app does
// not lose profiles, overrides or the cached geo database.
func migrateLegacyDir(old, dir string) {
	if _, err := os.Stat(dir); err == nil {
		return // already there, nothing to migrate
	}
	if _, err := os.Stat(old); err != nil {
		return // no previous install
	}
	if err := copyTree(old, dir); err != nil {
		log.Printf("migrate %s: %v", old, err)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil // sockets and the like are not worth copying
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode().Perm())
	})
}

func (a *App) loadOverrides() {
	b, err := os.ReadFile(a.ovPath)
	if err != nil {
		return
	}
	ov := patch.Default()
	if json.Unmarshal(b, &ov) == nil {
		a.ov = ov
	}
}

func (a *App) saveOverrides() {
	if a.ovPath == "" {
		return
	}
	if err := a.saveOverridesAtomic(); err != nil {
		a.notice = "设置已应用，但保存失败: " + err.Error()
	}
}

// configBytes is what the kernel actually runs: the active subscription, with
// this app's overrides applied on top.
func (a *App) configBytes() ([]byte, error) {
	raw := []byte(a.cfgText)
	if a.store != nil {
		if id := a.store.ActiveID(); id != "" {
			b, err := a.store.Content(id)
			if err != nil {
				return nil, err
			}
			raw = b
		}
	}
	if len(raw) == 0 {
		return nil, errors.New("没有可用的配置")
	}
	return patch.Apply(raw, a.ov)
}

// View is the window's root.
func (a *App) View(c *ui.Context) {
	if a.page == "settings" {
		a.reconcileSettings()
	}
	c.SetTheme(theme(appearanceDark(a.Prefs().Appearance, c.Theme().Dark)))
	pal := paletteFor(c)
	if a.initErr != "" {
		ui.Column(c).Fill().Background(pal.Bg).Padding(32).Justify(ui.Center).Gap(8).
			Children(func() {
				ui.Text(c, "无法启动内核客户端").FontSize(16).Bold().TextColor(pal.Text)
				ui.Text(c, a.initErr).FontSize(11).TextColor(pal.Danger)
				ui.Text(c, "检查数据目录是否可写，或用 GOPURR_HOME 指定一个可写目录。").
					FontSize(11.5).TextColor(pal.Muted)
			})
		return
	}

	a.setServices(c.Services())
	a.StartBackground()

	ui.Column(c).Fill().Background(pal.Bg).Children(func() {
		windowTitleBar(c)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			a.sidebar(c)
			a.main(c)
		})
	})
	a.softwareUpdateModal(c)
	a.restoreDialog(c)
}

// StartBackground starts the proxy and maintenance loops without creating a window.
func (a *App) StartBackground() {
	if a.initErr != "" || a.k == nil {
		return
	}
	if !a.ticking {
		a.ticking = true
		go a.sampleLoop()
	}
	if !a.booted {
		a.booted = true
		a.k.WatchLogs()
		go a.autoUpdateLoop()
		a.startSoftwareUpdates()
		a.bmu.Lock()
		a.svcInstalled = service.Installed()
		a.bmu.Unlock()
		if st, err := sysproxy.Current(); err == nil && st.Supported {
			a.sysState = st
			a.sysApplied = ownsProxy(st, a.mixedPort())
			a.sysOn = a.sysApplied
		}
		wantTUN := os.Getenv("GOPURR_TUN") == "1" || a.ov.TunEnable
		if wantTUN && a.k.IsRoot() {
			a.applyTun(true)
		} else {
			a.ov.TunEnable, a.tunOn = false, false
			if wantTUN {
				a.tunWanted = true
				a.setTunErr("TUN 服务未就绪，请安装或修复服务后重新开启")
			}
			if !a.k.Running() && os.Getenv("GOPURR_AUTOSTART") != "0" {
				a.toggle()
			}
		}
		if a.k.Running() {
			a.modeToken = a.k.Mode()
			a.ov.Mode = a.modeToken
			a.modePick = labelFor(a.modeToken)
			a.modeIdx = modeIndex(a.modeToken)
			// A proxy that already points at this kernel keeps working across
			// restarts; re-applying it would only ask for the administrator
			// password again on every launch.
			if !a.sysApplied && a.Prefs().RestoreSystemProxy {
				a.setSystemProxy(true)
			}
		}
		// Smoke-test hook: measure a group without a human clicking.
		if os.Getenv("GOPURR_TEST_DELAY") == "1" {
			go func() {
				time.Sleep(2 * time.Second)
				groups := a.k.Groups()
				if len(groups) == 0 {
					return
				}
				last := groups[len(groups)-1]
				a.testGroup(last.Name, last.Members)
			}()
		}
	}

}

func (a *App) StartupNeedsAttention() bool {
	return a.initErr != "" || a.k == nil || !a.k.Running() || a.tunErrText() != "" || a.sysErrText() != ""
}

func (a *App) sampleLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	tick := 0
	for range t.C {
		s := a.k.Sample()
		tick++
		if os.Getenv("GOPURR_DEBUG") == "1" && tick%5 == 0 {
			log.Printf("rss=%.1fMiB conns=%d up=%d down=%d heap=%dMiB",
				float64(s.Mem)/1048576, s.Conns, s.UpNow, s.DownNow,
				readHeap()/1048576)
		}
		a.mu.Lock()
		a.last = s
		a.hist = append(a.hist, sample{up: s.UpNow, down: s.DownNow})
		if len(a.hist) > bars {
			a.hist = a.hist[len(a.hist)-bars:]
		}
		a.mu.Unlock()
		a.invalidate()
	}
}

func readHeap() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func (a *App) snapshot() (core.Sample, []sample) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]sample, len(a.hist))
	copy(out, a.hist)
	return a.last, out
}

// ---------- chrome ----------

func (a *App) sidebar(c *ui.Context) {
	pal := paletteFor(c)
	ui.Column(c).Width(204).FillHeight().Background(pal.Side).
		BorderWidth(0, 1, 0, 0).BorderColor(pal.Border).Children(func() {
		ui.Row(c).Padding(24, 20).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Image(c, brandLogo).Size(36, 36).Label("Gopurr 蜷睡猫标记")
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, "Gopurr").FontSize(18).FontWeight(650).TextColor(pal.Text)
				ui.Text(c, "让连接简单一点").FontSize(11).TextColor(pal.Faint)
			})
		})

		ui.Text(c, "工作区").FontSize(10.5).FontWeight(600).TextColor(pal.Faint).Margin(4, 0, 8, 22)
		ui.Sidebar(c, &a.page, func() {
			for _, it := range navItems {
				if it.id == "settings" {
					ui.Box(c).FillWidth().Height(1).Background(pal.Border).Margin(12, 8)
				}
				itemTheme := *theme(c.Theme().Dark)
				itemTheme.Accent = pal.Faint
				if a.page == it.id {
					itemTheme.Accent, itemTheme.AccentText = pal.Accent, pal.Accent
				}
				c.SetTheme(&itemTheme)
				item := ui.SidebarItem(c, it.id, it.icon, it.label).
					Height(40).Padding(10, 12).Gap(11).FontSize(13).TextColor(pal.Muted)
				if a.page == it.id {
					item.Background(pal.Selected).TextColor(pal.Text).FontWeight(600)
				}
			}
			c.SetTheme(theme(c.Theme().Dark))
		}).FillWidth().Padding(0, 12).Gap(4).Background(pal.Side)

		ui.Spacer(c)

		ui.Column(c).FillWidth().Padding(20).Children(func() {
			ui.Column(c).FillWidth().Padding(14, 0, 0, 0).
				BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Gap(8).Children(func() {
				running := a.k.Running()
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					dot := pal.Faint
					if running {
						dot = pal.Success
					}
					ui.Box(c).Size(6, 6).Radius(3).Background(dot)
					label := "内核已停止"
					if running {
						label = "内核运行中"
					}
					ui.Text(c, label).FontSize(11.5).TextColor(pal.Muted)
				})
				proxies, rules, _ := a.k.Counts()
				ui.Text(c, fmt.Sprintf("%d 代理 · %d 规则", proxies, rules)).
					FontSize(11).TextColor(pal.Faint)
			})
		})
	})
}

func (a *App) main(c *ui.Context) {
	pal := paletteFor(c)
	paddingY := float32(24)
	if _, height := c.Size(); height < 660 {
		paddingY = 16
	}
	ui.Column(c).Grow(1).FillHeight().MinWidth(0).
		PaddingX(24).PaddingY(paddingY).
		Children(func() {
			// A new key makes the old page leave and the new one fade in, which
			// is the cheapest way to stop page switches from feeling like a
			// hard cut.
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Gap(16).Key(a.page).
				Transition(ui.ElementTransition{
					Duration: 160 * time.Millisecond,
					Enter:    &ui.Motion{Y: 6, Opacity: 0},
				}).
				Children(func() {
					if a.notice != "" {
						ui.Row(c).FillWidth().Padding(12).Radius(10).Background(pal.Hover).Children(func() {
							ui.Text(c, a.notice).FillWidth().FontSize(11).TextColor(pal.Text)
							ui.Button(c, "关闭提示").OnClick(func() { a.notice = "" })
						})
					}
					a.pageBody(c)
				})
		})
}

func (a *App) pageBody(c *ui.Context) {
	switch a.page {
	case "proxies":
		a.proxiesPage(c)
	case "conns":
		a.connsPage(c)
	case "subs":
		a.subsPage(c)
	case "rules":
		a.rulesPage(c)
	case "logs":
		a.logsPage(c)
	case "settings":
		a.settingsPage(c)
	default:
		a.overviewPage(c)
	}
	if a.showDeleteSub {
		idx := ui.AlertDialog(c, &a.showDeleteSub, "删除订阅？",
			"将从本机移除「"+a.deleteSubName+"」。如果正在使用此订阅，请先启用其他配置。", "取消", "删除")
		if idx == 1 {
			a.removeSubscription(a.deleteSubID)
		}
	}
}

func (a *App) pageHeader(c *ui.Context, title, subtitle string, trailing func()) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Text(c, title).FontSize(25).FontWeight(650).TextColor(pal.Text)
			ui.Text(c, subtitle).FontSize(12).TextColor(pal.Faint)
		})
		if trailing != nil {
			trailing()
		}
	})
}

func (a *App) kernelButton(c *ui.Context, running bool) {
	pal := paletteFor(c)
	if running {
		pill(c, "运行中", pal.Success, pal.Success.Alpha(0.10))
		return
	}
	ui.PrimaryButton(c, "启动内核").OnClick(a.toggle)
}

// ---------- overview ----------

func (a *App) overviewPage(c *ui.Context) {
	pal := paletteFor(c)
	snap, hist := a.snapshot()
	running := a.k.Running()
	_, windowHeight := c.Size()
	compact := windowHeight < 660

	a.pageHeader(c, "概览", a.statusLine(), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.modeControl(c)
			a.kernelButton(c, running)
		})
	})

	if a.sysOn != a.sysApplied {
		a.setSystemProxy(a.sysOn)
	}

	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			ui.Row(c).FillWidth().Radius(12).Background(pal.Surface).
				Border(1, pal.Border).Children(func() {
				a.statCard(c, iconDown, "下行速率", fmtRate(snap.DownNow), "累计 "+fmtBytes(snap.DownTotal), pal.Down)
				statDivider(c)
				a.statCard(c, iconUp, "上行速率", fmtRate(snap.UpNow), "累计 "+fmtBytes(snap.UpTotal), pal.Up)
				statDivider(c)
				a.statCard(c, iconActivity, "活跃连接", fmt.Sprintf("%d", snap.Conns), "实时会话", pal.Accent)
				statDivider(c)
				a.statCard(c, iconRules, "生效规则", fmt.Sprintf("%d", len(a.k.Rules())), "按顺序匹配", pal.Muted)
			})

			ui.Column(c).FillWidth().Padding(16).Radius(12).Background(pal.Surface).
				Border(1, pal.Border).Gap(12).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "实时流量").FontSize(14).FontWeight(600).TextColor(pal.Text)
						if !compact {
							ui.Text(c, "最近 72 秒的网络活动").FontSize(11).TextColor(pal.Faint)
						}
					})
					ui.Spacer(c)
					pill(c, "每秒采样", pal.Muted, pal.Hover)
				})
				a.chart(c, hist)
			})

			ui.Row(c).FillWidth().Gap(12).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Height(112).Padding(16).Radius(12).
					Background(pal.Surface).Border(1, pal.Border).Gap(12).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
						controlIcon(c, iconNetwork)
						ui.Text(c, "系统代理").FontSize(13).FontWeight(600).TextColor(pal.Text)
						ui.Spacer(c)
						ui.Switch(c, &a.sysOn).Label("系统代理开关")
					})
					ui.Text(c, a.sysProxyHint()).FillWidth().FontSize(11).TextColor(pal.Faint)
				})

				ui.Column(c).Grow(1).MinWidth(0).Height(112).Padding(16).Radius(12).
					Background(pal.Surface).Border(1, pal.Border).Gap(10).Children(func() {
					installed, busy, svcErr := a.svcState()
					root := a.k.IsRoot()
					// A service repaired in this session can take TUN back over
					// without making the user find the switch again.
					if a.tunWanted && root {
						a.tunWanted = false
						a.applyTun(true)
						root = a.k.IsRoot()
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
						controlIcon(c, iconShield)
						ui.Text(c, "TUN 模式").FontSize(13).FontWeight(600).TextColor(pal.Text)
						ui.Spacer(c)
						ui.Switch(c, &a.tunOn).Disabled(!root || busy).Label("TUN 模式")
						if a.tunOn != a.ov.TunEnable {
							a.applyTun(a.tunOn)
						}
					})
					colour := pal.Faint
					if svcErr != "" || a.tunErrText() != "" {
						colour = pal.Danger
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, a.tunHint()).Grow(1).MinWidth(0).MaxLines(2).Ellipsis("…").
							FontSize(11).TextColor(colour).Tooltip(a.tunHint())
						switch {
						case busy:
							ui.Text(c, "等待授权…").FontSize(11).TextColor(pal.Muted)
						case !service.Available():
							// Windows uses the normal user kernel and system proxy.
						case !installed:
							ui.Button(c, "安装服务…").FontSize(11).OnClick(func() { a.confirmInstall = true })
						case !root || a.mgr.ServiceNeedsUpdate():
							ui.Button(c, "重新安装…").FontSize(11).OnClick(func() { a.confirmInstall = true })
						default:
							ui.Button(c, "卸载服务").FontSize(11).OnClick(func() { a.confirmUninstall = true })
						}
					})
				})
			})
			a.overviewFeatures(c)
		})
	})

	if a.confirmInstall {
		idx := ui.AlertDialog(c, &a.confirmInstall, "安装 TUN 服务",
			service.InstallDescription(),
			"取消", "安装")
		if idx == 1 {
			a.installService()
		}
	}

	if a.confirmUninstall {
		idx := ui.AlertDialog(c, &a.confirmUninstall, "卸载 TUN 服务",
			service.UninstallDescription(),
			"取消", "卸载")
		if idx == 1 {
			a.uninstallService()
		}
	}
}

func (a *App) statCard(c *ui.Context, icon *ui.SVG, label, value, sub string, accent ui.Color) {
	pal := paletteFor(c)
	ui.Column(c).Grow(1).MinWidth(0).Padding(18).Gap(9).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, icon).Size(15, 15).TextColor(accent).Role(ui.RoleNone)
			ui.Text(c, label).FontSize(11.5).TextColor(pal.Muted)
		})
		number, unit := value, ""
		if i := strings.LastIndex(value, " "); i > 0 {
			number, unit = value[:i], value[i+1:]
		}
		ui.Row(c).AlignItems(ui.Baseline).Gap(5).Children(func() {
			ui.Text(c, number).FontSize(29).FontWeight(600).FontFeatures("tnum").TextColor(pal.Text)
			if unit != "" {
				ui.Text(c, unit).FontSize(12).TextColor(pal.Muted)
			}
		})
		ui.Text(c, sub).FontSize(11).TextColor(pal.Faint)
	})
}

func statDivider(c *ui.Context) {
	pal := paletteFor(c)
	ui.Box(c).Width(1).Height(60).AlignSelf(ui.Center).Background(pal.Border)
}

func controlIcon(c *ui.Context, icon *ui.SVG) {
	pal := paletteFor(c)
	ui.Box(c).Size(30, 30).Radius(8).Background(pal.Hover).AlignItems(ui.Center).Justify(ui.Center).
		Children(func() { ui.Icon(c, icon).Size(17, 17).TextColor(pal.Muted).Role(ui.RoleNone) })
}

func (a *App) chart(c *ui.Context, hist []sample) {
	pal := paletteFor(c)
	// Leave room for all four network controls in the normal overview window.
	height := float32(96)
	if _, windowHeight := c.Size(); windowHeight < 660 {
		height = 76
	}

	if len(hist) < 2 {
		ui.Column(c).FillWidth().Height(height).AlignItems(ui.Center).Justify(ui.Center).
			Children(func() {
				ui.Text(c, "等待流量采样…").FontSize(12).TextColor(pal.Faint)
			})
		return
	}

	var max int64 = 48 * 1024
	for _, s := range hist {
		if v := s.down + s.up; v > max {
			max = v
		}
	}

	n := len(hist)
	xs := make([]float32, n)
	down := make([]float32, n)
	up := make([]float32, n)
	zero := make([]float32, n)
	for i, s := range hist {
		xs[i] = float32(i) / float32(n-1)
		down[i] = float32(s.down) / float32(max)
		up[i] = float32(s.up) / float32(max)
	}

	ui.Column(c).FillWidth().Height(height).Draw(func(p *ui.Painter, r ui.Rect) {
		for i := 0; i <= 4; i++ {
			y := r.Y + r.H*float32(i)/4
			p.Line(r.X, y, r.X+r.W, y, 1, pal.Grid)
		}

		area := chart.Area(r, xs, zero, down, chart.CurveMonotone)
		p.FillPathGradient(area, ui.LinearGradient{
			From:  pal.Down.Alpha(0.18),
			To:    pal.Down.Alpha(0.01),
			Angle: 180,
		})
		p.StrokePath(chart.Line(r, xs, down, chart.CurveMonotone), 1.6, pal.Down)
		p.StrokePath(chart.Line(r, xs, up, chart.CurveMonotone), 1.2, pal.Up)
	})

	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("峰值 %s", fmtRate(max))).FontSize(11).TextColor(pal.Faint)
		ui.Spacer(c)
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.Down)
			ui.Text(c, "下行").FontSize(11).TextColor(pal.Muted)
		})
		ui.Row(c).Gap(5).AlignItems(ui.Center).Margin(0, 0, 0, 12).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(pal.Up)
			ui.Text(c, "上行").FontSize(11).TextColor(pal.Muted)
		})
	})
}

// ---------- connections ----------

// ---------- settings ----------

func (a *App) configSource() string {
	if a.store != nil && a.store.ActiveID() != "" {
		return "当前由订阅生成"
	}
	return "当前使用本地配置"
}

func (a *App) revealDataDir() {
	command := "open"
	if runtime.GOOS == "windows" {
		command = "explorer.exe"
	}
	if runtime.GOOS == "linux" {
		command = "xdg-open"
	}
	if err := exec.Command(command, a.dir).Start(); err != nil {
		a.notice = "打开文件夹失败: " + err.Error()
	}
}

func (a *App) openRawConfig() {
	a.showRaw = true
	a.rawError = ""
	a.rawReadOnly = a.store != nil && a.store.ActiveID() != ""
	if a.rawReadOnly {
		if b, err := os.ReadFile(a.mgr.ConfigPath()); err == nil {
			a.rawDraft = string(b)
		} else {
			b, err := a.configBytes()
			a.rawDraft = string(b)
			if err != nil {
				a.rawError = err.Error()
			}
		}
		return
	}
	a.rawDraft = a.cfgText
}

func (a *App) rawConfigModal(c *ui.Context) {
	pal := paletteFor(c)
	if !a.showRaw {
		return
	}
	width, height := c.Size()
	editorHeight := min(float32(360), height-240)

	ui.Modal(c, &a.showRaw, func() {
		ui.Column(c).Width(min(float32(760), width-80)).Padding(20).Radius(14).Background(pal.Surface).
			Border(1, pal.Border).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "运行配置").FontSize(14).Bold().TextColor(pal.Text)
				ui.Spacer(c)
				if a.rawReadOnly {
					pill(c, "只读 · 由订阅生成", pal.Muted, pal.Muted.Alpha(0.10))
				}
			})

			hint := "本地配置可直接编辑；保存到本机并热重载内核。"
			if a.rawReadOnly {
				hint = "由订阅与覆写生成；需要修改时，前往「规则」添加额外规则或 YAML 覆写。"
			}
			ui.Text(c, hint).FontSize(11).TextColor(pal.Faint)

			ui.TextArea(c, &a.rawDraft).FillWidth().Height(editorHeight).FontSize(11).
				ReadOnly(a.rawReadOnly).Label("运行配置内容")
			if a.rawError != "" {
				ui.Text(c, a.rawError).FillWidth().FontSize(11).TextColor(pal.Danger)
			}

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Button(c, "复制").OnClick(func() {
					a.services().WriteClipboard(a.rawDraft)
					a.notice = "配置已复制"
				})
				ui.Spacer(c)
				ui.Button(c, "关闭").OnClick(func() { a.showRaw = false })
				if !a.rawReadOnly {
					ui.PrimaryButton(c, "保存并应用").OnClick(a.saveLocalConfig)
				}
			})
		})
	})
}

// Toggle starts or stops the kernel; the tray calls it too.
func (a *App) Toggle() { a.toggle() }

// Reload applies the current configuration while keeping the daemon available.
func (a *App) Reload() { a.restart() }

// UseMode switches routing mode from the tray menu.
func (a *App) UseMode(token string) {
	if a.k.Running() {
		if err := a.k.SetMode(token); err != nil {
			a.notice = "切换模式失败: " + err.Error()
			a.modePick = labelFor(a.modeToken)
			a.modeIdx = modeIndex(a.modeToken)
			return
		}
	}
	a.modeToken = token
	a.modePick = labelFor(token)
	a.modeIdx = modeIndex(token)
	a.ov.Mode = token
	a.notice = ""
	a.saveOverrides()
}

func (a *App) toggle() {
	if a.k.Running() {
		if err := a.k.Stop(); err != nil {
			a.notice = "停止失败: " + err.Error()
			return
		}
		if a.sysApplied {
			a.setSystemProxy(false)
		}
		a.notice = "内核已停止。"
		return
	}
	if a.ov.TunEnable {
		a.applyTun(true)
		return
	}
	if a.startKernel() {
		if a.sysOn {
			a.setSystemProxy(true)
		}
	}
}

// startKernel applies the active config; it returns false and sets the notice
// when the kernel refuses it.
func (a *App) startKernel() bool {
	if !a.k.Running() && !portFree(a.mixedPort()) {
		a.notice = fmt.Sprintf("端口 %d 已被占用（可能是另一个代理客户端），换一个端口再试",
			a.mixedPort())
		return false
	}
	raw, err := a.configBytes()
	if err != nil {
		a.notice = "配置无法使用: " + err.Error()
		return false
	}
	if err := a.k.Start(raw); err != nil {
		a.notice = "启动失败: " + err.Error()
		return false
	}
	a.cancelDelayTest()
	a.bmu.Lock()
	a.delays = map[string]uint16{}
	a.delayErrors = map[string]string{}
	a.bmu.Unlock()
	a.notice = ""
	return true
}

// restart re-applies the config without touching the system proxy. With TUN
// on, the device is rebuilt first: mihomo closes the descriptor it adopted,
// so the old one cannot be reused.
func (a *App) restart() {
	a.startKernel()
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (a *App) mixedPort() int {
	if a.ov.MixedPort > 0 {
		return a.ov.MixedPort
	}
	return 7890
}

// setSystemProxy flips macOS's proxy settings. It runs on the UI thread
// because the authorization prompt is modal anyway.
func (a *App) setSystemProxy(on bool) {
	if !sysproxy.Available() {
		a.setSysErr("当前平台暂不支持自动设置系统代理")
		return
	}
	port := a.mixedPort()
	if on && !a.k.Running() && !a.startKernel() {
		a.sysOn = false
		return
	}

	var err error
	if on {
		// Whoever owned the system proxy before us gets it back on stop.
		if !a.sysApplied {
			if cur, cerr := sysproxy.Current(); cerr == nil && cur.Enabled && !ownsProxy(cur, port) {
				prev := cur
				a.prev = &prev
				a.savePrevProxy()
			}
		}
		err = a.proxyController().Enable(port, a.prev)
	} else {
		err = a.restoreProxy()
	}
	if st, serr := sysproxy.Current(); serr == nil {
		a.sysState = st
	}
	if err != nil {
		a.sysOn = a.sysApplied
		a.setSysErr(err.Error())
		return
	}
	a.sysApplied = on
	a.sysOn = on
	a.setSysErr("")
}

// restoreProxy hands the system proxy back to its previous owner, or turns it
// off when there was none.
func (a *App) restoreProxy() error {
	if err := a.proxyController().Restore(a.mixedPort(), a.prev); err != nil {
		return err
	}
	a.prev = nil
	a.savePrevProxy()
	return nil
}

func (a *App) proxyController() sysproxy.Controller {
	if a.proxy == nil {
		a.proxy = sysproxy.NewController()
	}
	return a.proxy
}

func ownsProxy(st sysproxy.State, port int) bool {
	if st.RawServer != "" && st.RawServer != fmt.Sprintf("127.0.0.1:%d", port) {
		return false
	}
	return st.Enabled && st.Server == "127.0.0.1" && st.Port == port
}

func (a *App) loadPrevProxy() {
	if a.prevPath == "" {
		return
	}
	b, err := os.ReadFile(a.prevPath)
	if err != nil {
		return
	}
	var st sysproxy.State
	if json.Unmarshal(b, &st) == nil && st.Enabled {
		a.prev = &st
	}
}

func (a *App) savePrevProxy() {
	if a.prevPath == "" {
		return
	}
	if a.prev == nil {
		_ = os.Remove(a.prevPath)
		return
	}
	if b, err := json.MarshalIndent(a.prev, "", "  "); err == nil {
		_ = os.WriteFile(a.prevPath, b, 0o644)
	}
}

// Shutdown puts the machine back the way it was. The window calls it on quit.
func (a *App) Shutdown() error {
	// A failed restore must leave the listener and recovery record available
	// for retry, rather than quitting with a dead system proxy.
	if a.sysApplied {
		a.setSystemProxy(false)
		if a.sysApplied {
			a.page = "overview"
			return fmt.Errorf("无法恢复系统代理，应用保持运行: %s", a.sysErrText())
		}
	}
	if a.proxy != nil {
		if err := a.proxy.Close(); err != nil {
			a.setSysErr(err.Error())
			a.page = "overview"
			return err
		}
	}
	if a.stopSoftwareUpdates != nil {
		a.stopSoftwareUpdates()
	}
	if a.softwareUpdates != nil {
		a.softwareUpdates.Close()
	}
	a.cancelDelayTest()
	if a.k != nil {
		a.k.Shutdown()
	}
	return nil
}

func (a *App) setSysErr(msg string) {
	a.bmu.Lock()
	a.sysErr = msg
	a.bmu.Unlock()
}

func (a *App) sysErrText() string {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.sysErr
}

// ---------- 系统服务（可选，仅 TUN 需要） ----------

func (a *App) svcState() (installed, busy bool, errText string) {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.svcInstalled, a.svcBusy, a.svcErr
}

// installService asks for administrator rights after the user explicitly
// confirms installing the persistent TUN service.
func (a *App) installService() {
	a.bmu.Lock()
	if a.svcBusy {
		a.bmu.Unlock()
		return
	}
	a.svcBusy = true
	a.svcErr = ""
	a.bmu.Unlock()

	go func() {
		defer a.invalidate()

		// The socket belongs to the daemon; hand it over before launchd binds it.
		a.k.Shutdown()

		exe, err := os.Executable()
		if err == nil {
			err = service.Install(exe, a.dir)
		}

		installed := err == nil && service.WaitInstalled(5*time.Second)
		deadline := time.Now().Add(10 * time.Second)
		for installed && time.Now().Before(deadline) {
			if a.k.Running() && a.k.IsRoot() {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		a.bmu.Lock()
		a.svcBusy = false
		a.svcInstalled = installed
		switch {
		case err != nil:
			a.svcErr = err.Error()
		case !installed:
			a.svcErr = "服务没有安装成功，请查看 README 的安装步骤"
		case !a.k.IsRoot():
			if tail := service.LogTail(a.dir); tail != "" {
				a.svcErr = "服务已安装，但内核没有启动：" + tail
			} else {
				a.svcErr = "服务已安装，但内核尚未获得管理员权限，请稍后重试或重新安装"
			}
		}
		a.bmu.Unlock()
	}()
}

func (a *App) uninstallService() {
	a.bmu.Lock()
	if a.svcBusy {
		a.bmu.Unlock()
		return
	}
	a.svcBusy = true
	a.bmu.Unlock()

	go func() {
		defer a.invalidate()
		err := service.Uninstall(a.dir)

		a.bmu.Lock()
		a.svcBusy = false
		a.svcInstalled = service.Installed()
		if err != nil {
			a.svcErr = err.Error()
		} else {
			a.svcErr = ""
		}
		a.bmu.Unlock()

		if err == nil {
			a.ov.TunEnable = false
			a.tunOn = false
			a.saveOverrides()
		}
	}()
}

// ---------- TUN ----------

func (a *App) tunErrText() string {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.tunErr
}

func (a *App) setTunErr(msg string) {
	a.bmu.Lock()
	a.tunErr = msg
	a.bmu.Unlock()
}

// applyTun only edits the config. Whether a device can exist is decided by the
// daemon's privileges, and mihomo itself creates and routes it.
func (a *App) applyTun(on bool) {
	a.tunWanted = false
	if on && !a.k.IsRoot() {
		a.setTunErr("TUN 需要系统服务：先在概览页点「安装服务…」")
		a.ov.TunEnable = false
		a.tunOn = false
		a.saveOverrides()
		return
	}
	a.setTunErr("")
	previous := a.ov.TunEnable
	a.ov.TunEnable = on
	if !a.startKernel() {
		a.ov.TunEnable, a.tunOn = previous, previous
		a.setTunErr(a.notice)
		return
	}
	a.tunOn = on
	if err := a.saveOverridesAtomic(); err != nil {
		a.setTunErr("TUN 已应用，但无法保存设置: " + err.Error())
	}
}

func (a *App) tunHint() string {
	if !service.Available() {
		return "当前平台不支持 TUN 服务"
	}
	installed, busy, svcErr := a.svcState()
	if e := a.tunErrText(); e != "" {
		return e
	}
	if svcErr != "" {
		return svcErr
	}
	if busy {
		return service.AuthorizationHint()
	}
	if !installed {
		return "可选 · 不装也能用系统代理，装了才能全局接管"
	}
	if !a.k.IsRoot() {
		return "服务已安装，但内核尚未获得管理员权限"
	}
	if a.mgr.ServiceNeedsUpdate() {
		return "服务需要更新 · 重新安装以使用新版本内核"
	}
	if a.ov.TunEnable {
		return "已启用 · 路由由内核自己接管"
	}
	return "已就绪 · 打开开关即全局接管"
}

// ---------- subscriptions ----------

func (a *App) setSubErr(msg string) {
	a.bmu.Lock()
	a.subErr = msg
	a.bmu.Unlock()
}

func (a *App) subState() (busy bool, errText string) {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	return a.subBusy, a.subErr
}

func (a *App) addSubscription() {
	url := strings.TrimSpace(a.subURL)
	if url == "" {
		a.setSubErr("先填订阅链接")
		return
	}
	if a.store == nil {
		a.setSubErr("订阅目录不可用")
		return
	}

	a.bmu.Lock()
	if a.subBusy {
		a.bmu.Unlock()
		return
	}
	a.subBusy = true
	a.bmu.Unlock()
	a.setSubErr("")

	name := strings.TrimSpace(a.subName)
	go func() {
		previous := a.store.ActiveID()
		item, err := a.store.Add(url, name)
		if err == nil && a.store.ActiveID() == item.ID {
			if !a.startKernel() {
				err = errors.New(a.notice)
				_ = a.store.SetActive(previous)
			}
		}
		a.bmu.Lock()
		a.subBusy = false
		if err != nil {
			a.subErr = err.Error()
		}
		a.bmu.Unlock()
		if err == nil {
			a.subURL, a.subName = "", ""
		}
		a.invalidate()
	}()
}

func (a *App) refreshSubscription(id string) {
	if a.store == nil || !a.beginProfileWork() {
		return
	}
	go func() {
		defer func() { a.endProfileWork(); a.invalidate() }()
		err := a.refreshManagedProfile(id)
		if err != nil {
			a.setSubErr(err.Error())
		} else {
			a.setSubErr("")
		}
		a.invalidate()
	}()
}

func (a *App) useSubscription(id string) {
	if a.store == nil || !a.beginProfileWork() {
		return
	}
	defer a.endProfileWork()
	previous := a.store.ActiveID()
	if err := a.store.SetActive(id); err != nil {
		a.setSubErr(err.Error())
		return
	}
	a.setSubErr("")
	if !a.startKernel() {
		_ = a.store.SetActive(previous)
		a.setSubErr(a.notice)
		return
	}
	a.proxyGroup = ""
	a.notice = "订阅已启用"
}

func (a *App) removeSubscription(id string) {
	if a.store == nil || !a.beginProfileWork() {
		return
	}
	defer a.endProfileWork()
	if a.store.ActiveID() == id {
		a.setSubErr("正在使用的订阅不能直接删除，请先启用其他配置")
		return
	}
	if err := a.store.Remove(id); err != nil {
		a.setSubErr(err.Error())
		return
	}
	a.setSubErr("")
	a.notice = "订阅已删除"
}

// ---------- helpers ----------

func (a *App) sysProxyHint() string {
	if e := a.sysErrText(); e != "" {
		return e
	}
	if !sysproxy.Available() {
		return "当前平台暂不支持"
	}
	service := a.sysState.Service
	if service == "" {
		service = "默认网络服务"
	}
	if a.sysApplied {
		return fmt.Sprintf("已指向 127.0.0.1:%d · %s", a.mixedPort(), service)
	}
	if a.sysState.Enabled {
		return fmt.Sprintf("当前指向 %s:%d；开启后切到 Gopurr", a.sysState.Server, a.sysState.Port)
	}
	if !a.k.Running() {
		return "未启用 · 内核未运行"
	}
	return "未启用 · " + service
}

func delayLabel(d uint16) string {
	if d == 0 {
		return "超时"
	}
	return fmt.Sprintf("%dms", d)
}

func delayColor(c *ui.Context, d uint16) ui.Color {
	pal := paletteFor(c)
	switch {
	case d == 0:
		return pal.Danger
	case d < 200:
		return pal.Down
	case d < 500:
		return pal.Warn
	default:
		return pal.Danger
	}
}

func (a *App) statusLine() string {
	if a.k.Running() {
		return "内核运行中 · " + a.modeLabel()
	}
	if e := a.k.LastError(); e != "" {
		return "上次启动失败 · " + e
	}
	return "内核未启动 · 点右侧按钮加载配置"
}

func (a *App) modeLabel() string {
	switch a.modeToken {
	case "global":
		return "全局模式"
	case "direct":
		return "直连模式"
	default:
		return "规则模式"
	}
}

var (
	modeLabels = []string{"规则", "全局", "直连"}
	modeTokens = []string{"rule", "global", "direct"}
)

func labelFor(token string) string {
	for i, t := range modeTokens {
		if t == token {
			return modeLabels[i]
		}
	}
	return modeLabels[0]
}

func tokenFor(label string) string {
	for i, l := range modeLabels {
		if l == label {
			return modeTokens[i]
		}
	}
	return "rule"
}

func onOff(b bool) string {
	if b {
		return "运行"
	}
	return "停止"
}

func fmtBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v >= 100 || v == math.Trunc(v) {
		return fmt.Sprintf("%.0f %cB", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGTPE"[exp])
}

func fmtRate(n int64) string { return fmtBytes(n) + "/s" }

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n || n < 1 {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return strings.TrimRight(home, "/") + "/.config/gopurr/config.yaml"
}

const demoConfig = `# Gopurr 内置示例配置：只用内嵌内核跑通链路。
# 换成你自己的订阅内容（Clash / mihomo YAML）即可。
mixed-port: 7890
allow-lan: false
mode: rule
log-level: warning
ipv6: false

proxies: []

proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - DIRECT
  - name: 自动选择
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - DIRECT

rules:
  - DOMAIN-SUFFIX,local,DIRECT
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - MATCH,节点选择
`

// ---------- 订阅自动更新 ----------

// autoUpdateLoop wakes once a minute and refreshes subscriptions when their
// interval has passed. It runs for the life of the process.
func (a *App) autoUpdateLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		a.maybeAutoUpdate()
	}
}

func (a *App) maybeAutoUpdate() {
	if a.store == nil {
		return
	}
	a.bmu.Lock()
	if a.subBusy {
		a.bmu.Unlock()
		return
	}
	a.subBusy = true
	if a.subAttempts == nil {
		a.subAttempts = map[string]time.Time{}
	}
	a.bmu.Unlock()
	defer func() { a.endProfileWork(); a.invalidate() }()
	p := a.Prefs()
	updated, inheritedAttempt := 0, false
	for _, item := range a.store.List() {
		if item.IsFile() {
			continue
		}
		hours := p.AutoUpdateHours
		since := p.LastAutoUpdate
		if item.UpdateHours != nil {
			hours = *item.UpdateHours
			since = item.Updated
		}
		if hours <= 0 || time.Since(since) < time.Duration(hours)*time.Hour {
			continue
		}
		if attempted := a.subAttempts[item.ID]; !attempted.IsZero() && time.Since(attempted) < time.Duration(hours)*time.Hour {
			continue
		}
		a.subAttempts[item.ID] = time.Now()
		if item.UpdateHours == nil {
			inheritedAttempt = true
		}
		if err := a.refreshManagedProfile(item.ID); err != nil {
			a.setSubErr(item.Name + ": " + err.Error())
			continue
		}
		updated++
	}
	if inheritedAttempt {
		if err := a.changePrefs(func(cur *Prefs) { cur.LastAutoUpdate = time.Now() }); err != nil {
			a.setSubErr(err.Error())
		}
	}
	if updated > 0 {
		a.notice = fmt.Sprintf("已自动更新 %d 个订阅", updated)
	}
	a.invalidate()
}

func (a *App) autoUpdateDesc() string {
	p := a.Prefs()
	if p.AutoUpdateHours <= 0 {
		return "关闭；订阅只在手动点击时刷新"
	}
	last := "还没有更新过"
	if !p.LastAutoUpdate.IsZero() {
		last = "上次 " + p.LastAutoUpdate.Format("01-02 15:04")
	}
	return fmt.Sprintf("每 %d 小时刷新一次，更新后自动重载内核 · %s", p.AutoUpdateHours, last)
}

// importLocalFile asks for a file and adds it as a profile.
func (a *App) importLocalFile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   "选择 Clash / mihomo 配置",
			Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		if a.store == nil {
			a.setSubErr("订阅目录不可用")
			return
		}
		if !a.beginProfileWork() {
			a.setSubErr("订阅正在更新，请稍后重试")
			a.invalidate()
			return
		}
		defer func() { a.endProfileWork(); a.invalidate() }()
		var lastErr string
		previous := a.store.ActiveID()
		for _, path := range paths {
			if _, err := a.store.AddFile(path, ""); err != nil {
				lastErr = err.Error()
			}
		}
		if a.store.ActiveID() != previous && !a.startKernel() {
			lastErr = a.notice
			_ = a.store.SetActive(previous)
		}
		a.setSubErr(lastErr)
		a.invalidate()
	}()
}

// services and invalidate give goroutines a race-free handle on the frame's
// window services, which View replaces on every build.
func (a *App) setServices(s ui.Services) {
	a.smu.Lock()
	a.svc = s
	a.smu.Unlock()
}

func (a *App) services() ui.Services {
	a.smu.Lock()
	defer a.smu.Unlock()
	return a.svc
}

func (a *App) invalidate() { a.services().Invalidate() }
