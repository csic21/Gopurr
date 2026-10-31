package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
	"gopurr/internal/securefs"
)

type overrideLibraryState struct {
	showOverrideLibrary bool
	libraryTab          int
	libraryDraft        []patch.OverrideTemplate
	libraryBase         []patch.OverrideTemplate
	librarySelected     string
	libraryScopeID      string
	libraryScopeName    string
	libraryIDs          []string
	libraryIDsBase      []string
	libraryPick         string
	libraryError        string
	libraryStatus       string
	libraryFileBusy     bool
	libraryGeneration   uint64
}

func (a *App) openOverrideLibrary() {
	a.libraryGeneration++
	a.showOverrideLibrary, a.libraryTab = true, 0
	a.libraryDraft = slices.Clone(a.ov.Library)
	a.libraryBase = slices.Clone(a.ov.Library)
	a.librarySelected, a.libraryPick = "", ""
	if len(a.libraryDraft) > 0 {
		a.librarySelected = a.libraryDraft[0].ID
	}
	a.libraryScopeID, a.libraryScopeName = "", tr("全局")
	a.libraryIDs = slices.Clone(a.ov.TemplateIDs)
	if a.extensionScope == 1 {
		a.libraryScopeID = a.extensionProfileID
		if a.store != nil {
			if p, err := a.store.Get(a.libraryScopeID); err == nil {
				a.libraryScopeName = p.Name
			}
		}
		a.libraryIDs = slices.Clone(a.profileOverrides(a.libraryScopeID).TemplateIDs)
	}
	a.libraryIDsBase = slices.Clone(a.libraryIDs)
	a.libraryError, a.libraryStatus, a.libraryFileBusy = "", "", false
}

func (a *App) closeOverrideLibrary() {
	a.showOverrideLibrary = false
	a.libraryGeneration++
}

func (a *App) libraryDirty() bool    { return !reflect.DeepEqual(a.libraryDraft, a.libraryBase) }
func (a *App) libraryIDsDirty() bool { return !slices.Equal(a.libraryIDs, a.libraryIDsBase) }

func (a *App) selectedLibraryIndex() int {
	return slices.IndexFunc(a.libraryDraft, func(item patch.OverrideTemplate) bool { return item.ID == a.librarySelected })
}

func templateID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "template-" + hex.EncodeToString(b[:]), nil
}

func (a *App) newOverrideTemplate() {
	id, err := templateID()
	if err != nil {
		a.libraryError = err.Error()
		return
	}
	a.libraryDraft = append(a.libraryDraft, patch.OverrideTemplate{ID: id, Name: tr("未命名覆写")})
	a.librarySelected, a.libraryError, a.libraryStatus = id, "", ""
}

func (a *App) templateReferenced(id string) bool {
	if slices.Contains(a.ov.TemplateIDs, id) {
		return true
	}
	if a.store != nil {
		for _, p := range a.store.List() {
			if p.Overrides != nil && slices.Contains(p.Overrides.TemplateIDs, id) {
				return true
			}
		}
	}
	return false
}

func (a *App) removeOverrideTemplate() {
	index := a.selectedLibraryIndex()
	if index < 0 {
		return
	}
	id := a.libraryDraft[index].ID
	if a.templateReferenced(id) {
		a.libraryError = tr("此模板仍被全局或订阅使用，请先在对应作用范围移除并保存应用顺序")
		return
	}
	a.libraryDraft = slices.Delete(a.libraryDraft, index, index+1)
	a.libraryIDs = slices.DeleteFunc(a.libraryIDs, func(value string) bool { return value == id })
	a.librarySelected, a.libraryError, a.libraryStatus = "", "", ""
	if len(a.libraryDraft) > 0 {
		a.librarySelected = a.libraryDraft[min(index, len(a.libraryDraft)-1)].ID
	}
}

// commitOverrideLibrary validates all saved subscriptions while profile work
// is locked, not just the active one. An edit cannot silently break an unused
// subscription, nor remove a referenced template from its configuration.
func (a *App) commitOverrideLibrary(next patch.Overrides) (bool, error) {
	if !a.beginProfileWork() {
		return false, errors.New(tr("订阅正在更新，请稍后重试"))
	}
	defer a.endProfileWork()
	if err := patch.ValidateTemplateLibrary(next.Library); err != nil {
		return false, err
	}
	if err := patch.ValidateTemplateIDs(next.Library, next.TemplateIDs); err != nil {
		return false, err
	}
	global := next
	runtime := a.runtimeOverrides()
	global.MeshIPs, global.MeshProxyAddress, global.MeshProxyPassword = runtime.MeshIPs, runtime.MeshProxyAddress, runtime.MeshProxyPassword
	if a.store != nil {
		for _, p := range a.store.List() {
			raw, err := a.store.Content(p.ID)
			if err != nil {
				return false, err
			}
			local := patch.ProfileOverrides{}
			if p.Overrides != nil {
				local = *p.Overrides
			}
			check := global
			if p.ID != a.store.ActiveID() {
				check.ExtraRules, check.AppRules, check.TunEnable = nil, nil, false
			}
			if _, err := patch.ApplyLayers(raw, check, local); err != nil {
				return false, fmt.Errorf(tr("模板修改会使订阅 %q 无法使用：%w"), p.Name, err)
			}
		}
	}
	if (a.store == nil || a.store.ActiveID() == "") && strings.TrimSpace(a.cfgText) != "" {
		if _, err := patch.Apply([]byte(a.cfgText), global); err != nil {
			return false, fmt.Errorf(tr("本地配置验证失败：%w"), err)
		}
	}
	previous := a.ov
	running := a.k != nil && a.k.Running()
	var previousRaw []byte
	if running {
		var err error
		previousRaw, err = a.lastAcceptedConfig()
		if err != nil {
			return false, err
		}
	}
	a.ov = next
	if running && !a.startKernel() {
		a.ov = previous
		return false, errors.Join(fmt.Errorf(tr("内核未接受配置，未保存：%s"), a.notice), a.k.Start(previousRaw))
	}
	if err := a.saveOverridesAtomic(); err != nil {
		a.ov = previous
		if running {
			if rollbackErr := a.k.Start(previousRaw); rollbackErr != nil {
				return false, fmt.Errorf(tr("保存失败：%w；旧配置重新载入也失败：%s"), err, rollbackErr.Error())
			}
		}
		return false, fmt.Errorf(tr("保存失败，已保留原配置：%w"), err)
	}
	a.featureCacheAt = time.Time{}
	return running, nil
}

func (a *App) saveOverrideLibraryDraft() {
	next := a.ov
	next.Library = slices.Clone(a.libraryDraft)
	applied, err := a.commitOverrideLibrary(next)
	if err != nil {
		a.libraryError, a.libraryStatus = err.Error(), ""
		return
	}
	a.libraryBase = slices.Clone(a.libraryDraft)
	a.libraryError, a.libraryStatus = "", extensionSavedStatus(applied)
}

func (a *App) saveOverrideTemplateSelection() {
	if a.libraryDirty() {
		a.libraryError = tr("请先保存模板库，再保存应用顺序")
		return
	}
	if err := patch.ValidateTemplateIDs(a.ov.Library, a.libraryIDs); err != nil {
		a.libraryError = err.Error()
		return
	}
	var applied bool
	var err error
	if a.libraryScopeID == "" {
		next := a.ov
		next.TemplateIDs = slices.Clone(a.libraryIDs)
		applied, err = a.commitOverrideLibrary(next)
	} else {
		local := a.profileOverrides(a.libraryScopeID)
		local.TemplateIDs = slices.Clone(a.libraryIDs)
		applied, err = a.commitProfileOverrides(a.libraryScopeID, local)
	}
	if err != nil {
		a.libraryError, a.libraryStatus = err.Error(), ""
		return
	}
	a.libraryIDsBase = slices.Clone(a.libraryIDs)
	a.libraryError, a.libraryStatus = "", extensionSavedStatus(applied)
	if !applied && a.libraryScopeID != "" {
		a.libraryStatus = tr("已保存 · 启用此订阅时生效")
	}
}

func (a *App) addTemplateID(id string) {
	if id == "" || slices.Contains(a.libraryIDs, id) {
		return
	}
	a.libraryIDs = append(a.libraryIDs, id)
	a.libraryError, a.libraryStatus = "", ""
}

func (a *App) moveTemplateID(index, direction int) {
	target := index + direction
	if index < 0 || target < 0 || index >= len(a.libraryIDs) || target >= len(a.libraryIDs) {
		return
	}
	a.libraryIDs[index], a.libraryIDs[target] = a.libraryIDs[target], a.libraryIDs[index]
	a.libraryError, a.libraryStatus = "", ""
}

func (a *App) overrideLibraryDialog(c *ui.Context) {
	if !a.showOverrideLibrary {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("override-library")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.closeOverrideLibrary()
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(720), width-64)).MaxHeight(height-64).Padding(20).Gap(12).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("可复用覆写"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("可复用覆写")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				softSegmented(c, &a.libraryTab, tr("模板库"), tr("应用顺序")).AlignSelf(ui.Start).Label(tr("覆写库分类"))
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						if a.libraryTab == 0 {
							a.overrideLibraryFields(c)
						} else {
							a.overrideLibraryAttachments(c)
						}
						if a.libraryError != "" {
							extensionError(c, a.libraryError)
						} else if a.libraryStatus != "" {
							ui.Text(c, a.libraryStatus).FillWidth().FontSize(11.5).TextColor(pal.Success)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					dirty, label, save := a.libraryDirty(), tr("保存模板库"), a.saveOverrideLibraryDraft
					if a.libraryTab == 1 {
						dirty, label, save = a.libraryIDsDirty(), tr("保存应用顺序"), a.saveOverrideTemplateSelection
					}
					status := tr("已与本地保存同步")
					if dirty {
						status = tr("草稿未保存")
					}
					ui.Text(c, status).Grow(1).MinWidth(0).FontSize(11.5).TextColor(pal.Muted)
					softButton(c, tr("取消")).OnClick(a.closeOverrideLibrary)
					softPrimaryButton(c, label).Disabled(!dirty || a.libraryFileBusy).OnClick(save)
				})
			})
		})
	})
}

func (a *App) overrideLibraryFields(c *ui.Context) {
	pal := paletteFor(c)
	ui.Text(c, tr("把常用 YAML 保存为模板，在全局或多个订阅中重复应用。模板按 ID 引用，改名不会断开关联。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if len(a.libraryDraft) > 0 {
			labels := make([]string, len(a.libraryDraft))
			selected := ""
			for i, item := range a.libraryDraft {
				labels[i] = fmt.Sprintf("%d. %s", i+1, item.Name)
				if item.ID == a.librarySelected {
					selected = labels[i]
				}
			}
			ui.Select(c, &selected, labels).Grow(1).MinWidth(0).Label(tr("选择覆写模板")).OnChange(func() {
				for i, label := range labels {
					if label == selected {
						a.librarySelected = a.libraryDraft[i].ID
					}
				}
			})
		} else {
			ui.Text(c, tr("暂无模板")).Grow(1).FontSize(13).TextColor(pal.Muted)
		}
		softButton(c, tr("导入 YAML…")).Disabled(a.libraryFileBusy || len(a.libraryDraft) >= 128).OnClick(a.importOverrideTemplateDialog)
		softButton(c, tr("新建模板")).Disabled(a.libraryFileBusy || len(a.libraryDraft) >= 128).OnClick(a.newOverrideTemplate)
	})
	index := a.selectedLibraryIndex()
	if index < 0 {
		ui.Text(c, tr("新建模板或从本地 YAML 文件导入，保存后可在「应用顺序」中启用。")).FillWidth().FontSize(12).TextColor(pal.Muted).PaddingY(24)
		return
	}
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, tr("模板名称")).FontSize(12).TextColor(pal.Muted)
		ui.TextInput(c, &a.libraryDraft[index].Name).FillWidth().Label(tr("模板名称")).Disabled(a.libraryFileBusy)
	})
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, tr("模板 YAML")).FontSize(12).TextColor(pal.Muted)
		_, height := c.Size()
		editorHeight := float32(180)
		if height < 640 {
			editorHeight = 130
		}
		ui.TextArea(c, &a.libraryDraft[index].YAML).FillWidth().Height(editorHeight).Font("monospace").FontSize(12).Label(tr("模板 YAML")).Disabled(a.libraryFileBusy)
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		softButton(c, tr("导出 YAML…")).Disabled(a.libraryFileBusy).OnClick(a.exportOverrideTemplateDialog)
		softButton(c, tr("删除模板")).Disabled(a.libraryFileBusy).OnClick(a.removeOverrideTemplate)
	})
	ui.Text(c, tr("映射递归合并，数组整体替换。端口、TUN 和控制接口仍由 Gopurr 管理；模板文件可能包含凭据，请妥善保存。")).FillWidth().FontSize(11).TextColor(pal.Muted)
}

func (a *App) overrideLibraryAttachments(c *ui.Context) {
	pal := paletteFor(c)
	ui.Text(c, fmt.Sprintf(tr("作用范围：%s"), a.libraryScopeName)).FillWidth().MaxLines(2).Ellipsis("…").Tooltip(a.libraryScopeName).FontSize(13).FontWeight(550).TextColor(pal.Text)
	ui.Text(c, tr("从上到下依次应用，后面的模板覆盖前面的字段；当前范围内的手写 YAML 最后应用。移除即停用，不删除模板。")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
	var available []patch.OverrideTemplate
	for _, item := range a.ov.Library {
		if !slices.Contains(a.libraryIDs, item.ID) {
			available = append(available, item)
		}
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		labels := make([]string, len(available))
		selected := ""
		for i, item := range available {
			labels[i] = fmt.Sprintf("%d. %s", i+1, item.Name)
			if item.ID == a.libraryPick {
				selected = labels[i]
			}
		}
		if selected == "" && len(available) > 0 {
			a.libraryPick, selected = available[0].ID, labels[0]
		}
		if len(available) > 0 {
			ui.Select(c, &selected, labels).Grow(1).MinWidth(0).Label(tr("添加覆写模板")).OnChange(func() {
				for i, label := range labels {
					if selected == label {
						a.libraryPick = available[i].ID
					}
				}
			})
		} else {
			ui.Text(c, tr("没有可添加的已保存模板")).Grow(1).FontSize(12).TextColor(pal.Muted)
		}
		softButton(c, tr("添加到应用列表")).Disabled(len(available) == 0 || a.libraryDirty()).OnClick(func() { a.addTemplateID(a.libraryPick) })
	})
	if len(a.libraryIDs) == 0 {
		ui.Text(c, tr("此范围尚未应用模板。添加后保存即可启用。")).FillWidth().FontSize(12).TextColor(pal.Muted).PaddingY(24)
	}
	for i, id := range a.libraryIDs {
		name := tr("模板已不存在")
		for _, item := range a.ov.Library {
			if item.ID == id {
				name = item.Name
			}
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%d", i+1)).Width(24).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, name).Grow(1).MinWidth(0).MaxLines(1).Ellipsis("…").Tooltip(name).FontSize(13).TextColor(pal.Text)
			softButton(c, tr("上移")).Label(fmt.Sprintf(tr("上移第 %d 个模板"), i+1)).Disabled(i == 0).OnClick(func() { a.moveTemplateID(i, -1) })
			softButton(c, tr("下移")).Label(fmt.Sprintf(tr("下移第 %d 个模板"), i+1)).Disabled(i == len(a.libraryIDs)-1).OnClick(func() { a.moveTemplateID(i, 1) })
			softButton(c, tr("移除")).Label(fmt.Sprintf(tr("移除第 %d 个模板"), i+1)).OnClick(func() { a.libraryIDs = slices.Delete(a.libraryIDs, i, i+1); a.libraryError, a.libraryStatus = "", "" })
		})
	}
	if a.libraryDirty() {
		ui.Text(c, tr("模板库有未保存的修改，请先返回「模板库」保存。")).FillWidth().FontSize(11).TextColor(pal.Warn)
	}
}

func (a *App) importOverrideTemplateDialog() {
	if a.libraryFileBusy {
		return
	}
	a.libraryFileBusy = true
	generation := a.libraryGeneration
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: tr("导入覆写 YAML"), Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}}})
		var raw []byte
		name := ""
		if err == nil && len(paths) > 0 {
			name = strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0]))
			var file *os.File
			file, err = os.Open(paths[0])
			if err == nil {
				raw, err = io.ReadAll(io.LimitReader(file, patch.MaxTemplateYAML+1))
				_ = file.Close()
			}
		}
		mygo.RunOnMain(func() {
			if generation != a.libraryGeneration || !a.showOverrideLibrary {
				return
			}
			a.libraryFileBusy = false
			if err != nil {
				a.libraryError = err.Error()
			} else if len(paths) > 0 {
				a.addImportedOverrideTemplate(name, string(raw))
			}
			a.invalidate()
		})
	}()
}

func (a *App) addImportedOverrideTemplate(name, raw string) {
	id, err := templateID()
	if err != nil {
		a.libraryError = err.Error()
		return
	}
	item := patch.OverrideTemplate{ID: id, Name: name, YAML: raw}
	if err := patch.ValidateTemplateLibrary([]patch.OverrideTemplate{item}); err != nil {
		a.libraryError = err.Error()
		return
	}
	a.libraryDraft = append(a.libraryDraft, item)
	a.librarySelected, a.libraryError, a.libraryStatus = id, "", tr("已导入到草稿，保存模板库后才会生效")
}

func (a *App) exportOverrideTemplateDialog() {
	index := a.selectedLibraryIndex()
	if index < 0 || a.libraryFileBusy {
		return
	}
	item := a.libraryDraft[index]
	if err := patch.ValidateTemplateLibrary([]patch.OverrideTemplate{item}); err != nil {
		a.libraryError = err.Error()
		return
	}
	a.libraryFileBusy = true
	generation := a.libraryGeneration
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Title: tr("导出覆写 YAML"), DefaultPath: item.ID + ".yaml", Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}}, Message: tr("模板可能包含凭据，请妥善保存。")})
		if err == nil && path != "" {
			err = securefs.WriteFile(path, []byte(item.YAML), 0600)
		}
		mygo.RunOnMain(func() {
			if generation != a.libraryGeneration || !a.showOverrideLibrary {
				return
			}
			a.libraryFileBusy = false
			if err != nil {
				a.libraryError = err.Error()
			} else if path != "" {
				a.libraryError, a.libraryStatus = "", tr("模板 YAML 已导出")
			}
			a.invalidate()
		})
	}()
}
