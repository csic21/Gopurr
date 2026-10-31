package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gopurr/internal/backup"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

type backupState struct {
	backupError    string
	backupStatus   string
	restorePreview *backup.Snapshot
	restoreName    string
	restoreCount   int
	showRestore    bool
}

func (a *App) beginProfileWork() bool {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if a.subBusy || a.shutdownPending.Load() {
		return false
	}
	a.subBusy = true
	return true
}
func (a *App) endProfileWork() { a.bmu.Lock(); a.subBusy = false; a.bmu.Unlock() }

func (a *App) backupSnapshot() (*backup.Snapshot, error) {
	if a.store == nil {
		return nil, fmt.Errorf("%s", tr("订阅目录不可用"))
	}
	s := &backup.Snapshot{Version: 1, Created: time.Now(), Files: map[string][]byte{}}
	add := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		s.Files[name] = b
		return err
	}
	if err := add("prefs.json", a.Prefs()); err != nil {
		return nil, err
	}
	if err := add("overrides.json", a.ov); err != nil {
		return nil, err
	}
	items := a.store.List()
	if err := add("profiles.json", struct {
		Profiles []profile.Profile `json:"profiles"`
		Active   string            `json:"active"`
	}{items, a.store.ActiveID()}); err != nil {
		return nil, err
	}
	s.Files["local.yaml"] = []byte(a.cfgText)
	for _, p := range items {
		body, err := a.store.Content(p.ID)
		if err != nil {
			return nil, err
		}
		s.Files["profiles/"+p.File] = body
	}
	return s, s.Validate()
}

// prepareRestore validates every file before any configuration is changed.
func (a *App) prepareRestore(s *backup.Snapshot) (Prefs, patch.Overrides, int, error) {
	prefs, ov := defaultPrefs(), patch.Default()
	fail := func(err error) (Prefs, patch.Overrides, int, error) { return prefs, ov, 0, err }
	if err := s.Validate(); err != nil {
		return fail(err)
	}
	if err := json.Unmarshal(s.Files["prefs.json"], &prefs); err != nil {
		return fail(fmt.Errorf(tr("偏好文件无效: %w"), err))
	}
	if err := validatePrefs(prefs); err != nil {
		return fail(err)
	}
	if err := json.Unmarshal(s.Files["overrides.json"], &ov); err != nil {
		return fail(fmt.Errorf(tr("覆写文件无效: %w"), err))
	}
	// Login registration and active network listeners belong to this machine.
	prefs.AutoLaunch = a.Prefs().AutoLaunch
	ov.MixedPort, ov.TunEnable = a.ov.MixedPort, a.ov.TunEnable
	if a.ov.TunEnable {
		ov.TunStack = a.ov.TunStack
	}
	if _, err := patch.Apply(s.Files["local.yaml"], patch.Default()); err != nil {
		return fail(fmt.Errorf(tr("本地配置无效: %w"), err))
	}
	var index struct {
		Profiles []profile.Profile `json:"profiles"`
		Active   string            `json:"active"`
	}
	if err := json.Unmarshal(s.Files["profiles.json"], &index); err != nil {
		return fail(err)
	}
	seenID, seenFile := map[string]bool{}, map[string]bool{}
	activeFound := index.Active == ""
	for i, p := range index.Profiles {
		if p.ID == "" || seenID[p.ID] || seenFile[p.File] || filepath.Base(p.File) != p.File || !strings.HasSuffix(p.File, ".yaml") {
			return fail(fmt.Errorf("%s", tr("订阅索引包含无效或重复条目")))
		}
		seenID[p.ID], seenFile[p.File] = true, true
		if p.Kind == "" {
			p.Kind = profile.KindURL
		}
		if p.Kind != profile.KindFile && p.Kind != profile.KindURL {
			return fail(fmt.Errorf("%s", tr("未知配置类型")))
		}
		if err := profile.ValidateSettings(p); err != nil {
			return fail(fmt.Errorf("%s: %w", p.Name, err))
		}
		body, exists := s.Files["profiles/"+p.File]
		if !exists {
			return fail(fmt.Errorf(tr("备份缺少订阅 %s 的内容"), p.Name))
		}
		// Non-active subscriptions may require different rule targets. Validate
		// their source YAML separately; apply global overrides to the active one.
		var local patch.ProfileOverrides
		if p.Overrides != nil {
			local = *p.Overrides
		}
		check := ov
		check.ExtraRules, check.TunEnable = nil, false
		if _, err := patch.ApplyLayers(body, check, local); err != nil {
			return fail(fmt.Errorf(tr("订阅 %s 无效: %w"), p.Name, err))
		}
		if p.ID == index.Active {
			activeFound = true
			if _, err := patch.ApplyLayers(body, ov, local); err != nil {
				return fail(err)
			}
		}
		if p.IsFile() {
			p.Path = ""
			index.Profiles[i] = p
		} // Restored local profiles are self-contained.
	}
	if !activeFound {
		return fail(fmt.Errorf("%s", tr("备份中的当前订阅不存在")))
	}
	s.Files["prefs.json"], _ = json.MarshalIndent(prefs, "", "  ")
	s.Files["overrides.json"], _ = json.MarshalIndent(ov, "", "  ")
	s.Files["profiles.json"], _ = json.MarshalIndent(index, "", "  ")
	return prefs, ov, len(index.Profiles), nil
}

func (a *App) restoreBackup(s *backup.Snapshot) error {
	if !a.beginProfileWork() {
		return fmt.Errorf("%s", tr("订阅正在更新，请完成后重试"))
	}
	defer a.endProfileWork()
	prefs, ov, _, err := a.prepareRestore(s)
	if err != nil {
		return err
	}
	oldStore, oldConfig, oldOverrides := a.store, a.cfgText, a.ov
	previous, err := a.configBytes()
	if err != nil {
		return fmt.Errorf(tr("无法保存恢复前的配置: %w"), err)
	}
	tx, err := backup.Install(a.dir, s)
	if err != nil {
		return err
	}
	running := a.k != nil && a.k.Running()
	rollback := func(cause error) error {
		a.store, a.cfgText, a.ov = oldStore, oldConfig, oldOverrides
		diskErr := tx.Rollback()
		var runtimeErr error
		if running {
			runtimeErr = a.k.Start(previous)
		}
		return errors.Join(cause, diskErr, runtimeErr)
	}
	nextStore, err := profile.Open(a.dir)
	if err != nil {
		return rollback(err)
	}
	nextStore.SetProxyURL(a.SubscriptionProxyURL)
	a.store, a.cfgText, a.ov = nextStore, string(s.Files["local.yaml"]), ov
	if running && !a.startKernel() {
		return rollback(fmt.Errorf(tr("内核未接受备份配置: %s"), a.notice))
	}
	// AutoLaunch is preserved, so publishing imported preferences cannot alter
	// OS login registration or request system proxy authorization.
	a.pmu.Lock()
	a.prefs = prefs
	hook := a.prefsHook
	a.pmu.Unlock()
	if hook != nil {
		hook(prefs)
	}
	a.settingsReady, a.extensionReady, a.dnsReady, a.snifferReady = false, false, false, false
	a.appRulesReady, a.showAppPicker, a.showAppEditor = false, false, false
	a.appRulesError, a.appRulesStatus = "", ""
	a.extensionDrafts = nil
	a.showEditSub, a.showRaw, a.showDeleteSub = false, false, false
	a.subAttempts = map[string]time.Time{}
	a.proxyGroup = ""
	a.connFetched = time.Time{}
	a.modeToken = ov.Mode
	a.modePick = labelFor(ov.Mode)
	a.modeIdx = modeIndex(ov.Mode)
	if err := tx.Commit(); err != nil {
		a.backupStatus = tr("配置已恢复；旧文件清理失败: ") + err.Error()
	} else {
		a.backupStatus = tr("已恢复订阅、规则与偏好")
	}
	a.notice = ""
	return nil
}

func (a *App) backupSettings(c *ui.Context) {
	busy, _ := a.subState()
	settingsGroup(c, tr("本地备份"), func() {
		settingsRow(c, tr("备份与恢复"), tr("保存订阅、规则、DNS 与偏好，便于迁移或恢复"), func() {
			softButton(c, tr("导出备份…")).Disabled(busy).OnClick(a.exportBackupDialog)
		})
		hairline(c)
		settingsRow(c, tr("从备份恢复"), tr("恢复前可查看摘要；当前配置将被替换"), func() {
			softButton(c, tr("选择备份…")).Disabled(busy).OnClick(a.importBackupDialog)
		})
		if a.backupError != "" {
			settingsFeedback(c, a.backupError, true)
		} else if a.backupStatus != "" {
			settingsFeedback(c, a.backupStatus, false)
		}
	})
}

func (a *App) exportBackupDialog() {
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: tr("导出 Gopurr 备份"), DefaultPath: "Gopurr-" + time.Now().Format("2006-01-02") + ".zip", Filters: []mygo.FileFilter{{Name: tr("Gopurr 备份"), Extensions: []string{"zip"}}}, Message: tr("备份包含订阅链接与配置凭据，请妥善保存。")})
		if err != nil || path == "" {
			return
		}
		mygo.RunOnMain(func() {
			a.backupError, a.backupStatus = "", ""
			if !a.beginProfileWork() {
				a.backupError = tr("订阅正在更新，请稍后重试")
				return
			}
			defer a.endProfileWork()
			s, err := a.backupSnapshot()
			if err == nil {
				err = backup.Write(path, s)
			}
			if err != nil {
				a.backupError = tr("导出失败: ") + err.Error()
			} else {
				a.backupStatus = tr("备份已保存到 ") + path
			}
			a.invalidate()
		})
	}()
}
func (a *App) importBackupDialog() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: tr("选择 Gopurr 备份"), Filters: []mygo.FileFilter{{Name: tr("Gopurr 备份"), Extensions: []string{"zip"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		snapshot, readErr := backup.Read(paths[0])
		mygo.RunOnMain(func() {
			a.backupError, a.backupStatus = "", ""
			if readErr != nil {
				a.backupError = readErr.Error()
				a.invalidate()
				return
			}
			_, _, count, err := a.prepareRestore(snapshot)
			if err != nil {
				a.backupError = tr("无法恢复: ") + err.Error()
				a.invalidate()
				return
			}
			a.restorePreview, a.restoreName, a.restoreCount, a.showRestore = snapshot, filepath.Base(paths[0]), count, true
			a.invalidate()
		})
	}()
}
func (a *App) restoreDialog(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "restore-backup", &a.showRestore, func() {
		ui.Column(c).Width(480).Padding(20).Gap(14).Children(func() {
			ui.Text(c, tr("恢复本地备份？")).FontSize(16).Bold().TextColor(pal.Text)
			ui.Text(c, a.restoreName).FillWidth().SingleLine().Ellipsis("…").Tooltip(a.restoreName).FontSize(12).TextColor(pal.Text)
			created := ""
			if a.restorePreview != nil {
				created = a.restorePreview.Created.Local().Format("2006-01-02 15:04")
			}
			ui.Text(c, fmt.Sprintf(tr("备份于 %s · %d 个订阅"), created, a.restoreCount)).FontSize(11).TextColor(pal.Muted)
			ui.Text(c, tr("将替换当前订阅、规则、DNS 和偏好。建议先导出当前备份。")).FillWidth().FontSize(12).TextColor(pal.Text)
			ui.Text(c, tr("登录自启、当前代理端口与 TUN 开关沿用本机设置。恢复的本地配置使用备份内容。")).FillWidth().FontSize(11).TextColor(pal.Muted)
			if a.backupError != "" {
				settingsFeedback(c, a.backupError, true)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				softButton(c, tr("取消")).OnClick(func() { a.showRestore = false })
				softPrimaryButton(c, tr("恢复备份")).OnClick(func() {
					if a.restorePreview == nil {
						return
					}
					if err := a.restoreBackup(a.restorePreview); err != nil {
						a.backupError = tr("恢复失败: ") + err.Error()
						return
					}
					a.showRestore = false
					a.backupError = ""
				})
			})
		})
	})
}
