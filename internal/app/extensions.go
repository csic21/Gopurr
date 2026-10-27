package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
	"gopurr/internal/patch"
)

// Embed extensionState in App. Drafts live on the UI goroutine, independently
// of the active tab and the last successfully persisted overrides.
type extensionState struct {
	extensionTab   int
	extensionReady bool

	extraRulesDraft  string
	extraRulesSaved  string
	extraRulesError  string
	extraRulesStatus string
	showExtraRules   bool

	mergeYAMLDraft   string
	mergeYAMLSaved   string
	mergeYAMLError   string
	mergeYAMLStatus  string
	showYAMLTemplate bool

	extraRuleKind   string
	extraRuleValue  string
	extraRuleTarget string
	extraRuleError  string
}

var extraRuleKinds = []string{"DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "IP-CIDR", "IP-CIDR6", "PROCESS-NAME"}

const mergeYAMLTemplate = `# 示例：只写需要覆写的字段
profile:
  store-selected: true
dns:
  enable: true
  nameserver:
    - 1.1.1.1
    - 223.5.5.5
`

func (a *App) initExtensionDrafts() {
	if a.extensionReady {
		return
	}
	a.extensionReady = true
	a.extraRulesDraft = strings.Join(a.ov.ExtraRules, "\n")
	a.extraRulesSaved = a.extraRulesDraft
	a.mergeYAMLDraft = a.ov.MergeYAML
	a.mergeYAMLSaved = a.mergeYAMLDraft
	a.extraRuleKind = extraRuleKinds[0]
	a.extraRuleTarget = "DIRECT"
}

// Extra rules are plain text, not a YAML list. Ignore empty lines and full-line
// comments, but leave rule syntax intact for configBytes/patch.Apply to validate.
func extraRuleLines(draft string) []string {
	var rules []string
	for _, line := range strings.Split(draft, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	return rules
}

// Delete by logical rule index without discarding comments, empty lines or
// other edits in the draft. Duplicate rules can be removed independently.
func removeExtraRule(draft string, index int) string {
	if index < 0 {
		return draft
	}
	lines := strings.Split(draft, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if index == 0 {
			return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
		}
		index--
	}
	return draft
}

func ruleTargets(groups []core.Group, current string) []string {
	targets := []string{"DIRECT", "REJECT"}
	for _, group := range groups {
		if group.Name != "" && !containsString(targets, group.Name) {
			targets = append(targets, group.Name)
		}
	}
	// A stopped kernel can temporarily have no groups. Keep the user's choice
	// rather than silently changing the destination of the rule being composed.
	if current != "" && !containsString(targets, current) {
		targets = append(targets, current)
	}
	return targets
}

func buildExtraRule(kind, value, target string) (string, error) {
	if !containsString(extraRuleKinds, kind) {
		return "", errors.New("请选择规则类型")
	}
	value, target = strings.TrimSpace(value), strings.TrimSpace(target)
	if value == "" {
		return "", errors.New("请填写匹配内容，例如 example.com")
	}
	if target == "" {
		return "", errors.New("请选择目标策略")
	}
	if strings.ContainsAny(value+target, ",\r\n") {
		return "", errors.New("匹配内容和目标策略不能包含逗号或换行；完整规则请直接填入下方草稿")
	}
	return kind + "," + value + "," + target, nil
}

func (a *App) appendExtraRule() {
	rule, err := buildExtraRule(a.extraRuleKind, a.extraRuleValue, a.extraRuleTarget)
	if err != nil {
		a.extraRuleError = err.Error()
		return
	}
	if a.extraRulesDraft != "" && !strings.HasSuffix(a.extraRulesDraft, "\n") {
		a.extraRulesDraft += "\n"
	}
	a.extraRulesDraft += rule
	a.extraRuleValue = ""
	a.extraRuleError, a.extraRulesError, a.extraRulesStatus = "", "", ""
}

func (a *App) extraRulesEditor(c *ui.Context) {
	pal := paletteFor(c)
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			card(c).Children(func() {
				ui.Text(c, "优先匹配你的规则").FontSize(13).Bold().TextColor(pal.Text)
				ui.Text(c, "放在最终规则列表最前面，按行顺序匹配；订阅更新后仍会保留。").
					FillWidth().FontSize(11.5).TextColor(pal.Muted)
				ui.Row(c).FillWidth().Gap(8).Children(func() {
					ui.Select(c, &a.extraRuleKind, extraRuleKinds).Width(160).Label("规则类型")
					ui.TextInput(c, &a.extraRuleValue).Grow(1).MinWidth(0).
						Label("匹配内容").Placeholder(extraRulePlaceholder(a.extraRuleKind))
				})
				var groups []core.Group
				if a.k != nil {
					groups = a.k.Groups()
				}
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "目标策略").FontSize(11.5).TextColor(pal.Muted)
					ui.Select(c, &a.extraRuleTarget, ruleTargets(groups, a.extraRuleTarget)).
						Grow(1).MinWidth(0).Label("目标策略")
					ui.Button(c, "添加到草稿").OnClick(a.appendExtraRule)
				})
				if a.extraRuleError != "" {
					extensionError(c, a.extraRuleError)
				}
			})

			ui.Column(c).FillWidth().Gap(8).Children(func() {
				lines := extraRuleLines(a.extraRulesDraft)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					sectionLabel(c, fmt.Sprintf("规则草稿 · %d 条", len(lines)))
					ui.Spacer(c)
					label := "逐条删除"
					if a.showExtraRules {
						label = "收起列表"
					}
					ui.Button(c, label).Disabled(len(lines) == 0).OnClick(func() { a.showExtraRules = !a.showExtraRules })
					ui.Button(c, "清空草稿").Disabled(a.extraRulesDraft == "").OnClick(func() {
						a.extraRulesDraft, a.extraRulesError, a.extraRulesStatus = "", "", ""
					})
				})
				ui.Text(c, "每行一条，无需 YAML 的 “-”；空行和 # 开头的注释会忽略。").
					FillWidth().FontSize(11).TextColor(pal.Muted)
				if ui.TextArea(c, &a.extraRulesDraft).FillWidth().Height(180).Font("monospace").FontSize(11).
					Label("额外规则草稿").Placeholder("DOMAIN-SUFFIX,example.com,DIRECT\nIP-CIDR,192.168.0.0/16,DIRECT,no-resolve").Changed() {
					a.extraRulesError, a.extraRulesStatus = "", ""
				}
				ui.Text(c, "示例：DOMAIN-SUFFIX,example.com,DIRECT\n局域网：IP-CIDR,192.168.0.0/16,DIRECT,no-resolve").
					FillWidth().FontSize(11).TextColor(pal.Muted).Selectable()
				if a.showExtraRules && len(lines) > 0 {
					card(c).Children(func() {
						for i, line := range lines {
							index := i
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, fmt.Sprintf("%d", i+1)).Width(24).FontSize(11).TextColor(pal.Faint)
								ui.Text(c, line).Grow(1).MinWidth(0).FontSize(11.5).TextColor(pal.Text)
								ui.Button(c, "删除").Label(fmt.Sprintf("删除第 %d 条额外规则", i+1)).OnClick(func() {
									a.extraRulesDraft = removeExtraRule(a.extraRulesDraft, index)
									a.extraRulesError, a.extraRulesStatus = "", ""
								})
							})
						}
					})
				}
			})
		})
	})
	extensionFeedback(c, a.extraRulesError, a.extraRulesStatus)
	extensionSaveBar(c, a.extraRulesDraft != a.extraRulesSaved, "保存额外规则", func() {
		a.extraRulesDraft = a.extraRulesSaved
		a.extraRulesError, a.extraRulesStatus = "", ""
	}, func() { _ = a.saveExtraRulesDraft() })
}

func extraRulePlaceholder(kind string) string {
	switch kind {
	case "IP-CIDR":
		return "192.168.0.0/16"
	case "IP-CIDR6":
		return "fc00::/7"
	case "PROCESS-NAME":
		return "应用进程名，例如 curl"
	case "DOMAIN-KEYWORD":
		return "域名中的关键词，例如 example"
	default:
		return "域名，例如 example.com"
	}
}

func (a *App) mergeYAMLEditor(c *ui.Context) {
	pal := paletteFor(c)
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			card(c).Children(func() {
				ui.Text(c, "持久化覆写配置").FontSize(13).Bold().TextColor(pal.Text)
				ui.Text(c, "只写需要修改的字段，保存后会应用到当前及之后使用的订阅。").
					FillWidth().FontSize(11.5).TextColor(pal.Muted)
				ui.Text(c, "映射（map）递归合并，数组整体替换；“额外规则”始终排在最终 rules 的最前面。").
					FillWidth().FontSize(11.5).TextColor(pal.Muted)
				ui.Text(c, "例如 nameserver 数组会替换原列表；覆写 rules 也会替换订阅规则。").
					FillWidth().FontSize(11).TextColor(pal.Faint)
				ui.Text(c, "端口、运行模式、TUN 等由应用管理的选项，以设置页为准。").
					FillWidth().FontSize(11).TextColor(pal.Faint)
			})
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					sectionLabel(c, "YAML 草稿")
					ui.Spacer(c)
					label := "查看模板"
					if a.showYAMLTemplate {
						label = "收起模板"
					}
					ui.Button(c, label).OnClick(func() { a.showYAMLTemplate = !a.showYAMLTemplate })
					ui.Button(c, "清空草稿").Disabled(a.mergeYAMLDraft == "").OnClick(func() {
						a.mergeYAMLDraft, a.mergeYAMLError, a.mergeYAMLStatus = "", "", ""
					})
				})
				if ui.TextArea(c, &a.mergeYAMLDraft).FillWidth().Height(260).Font("monospace").FontSize(11).
					Label("YAML 覆写草稿").Placeholder("在这里填写 YAML 映射，例如：\nprofile:\n  store-selected: true").Changed() {
					a.mergeYAMLError, a.mergeYAMLStatus = "", ""
				}
				ui.Text(c, "留空并保存可移除 YAML 覆写。保存前会验证完整配置，错误时保留草稿。").
					FillWidth().FontSize(11).TextColor(pal.Muted)
			})
			if a.showYAMLTemplate {
				card(c).Children(func() {
					sectionLabel(c, "示例模板 · 仅供参考")
					ui.Text(c, mergeYAMLTemplate).FillWidth().Font("monospace").FontSize(11.5).
						TextColor(pal.Muted).Selectable()
					ui.Text(c, "插入只修改草稿，点击保存后才会应用；有内容时不会覆盖你的草稿。").
						FillWidth().FontSize(11).TextColor(pal.Faint)
					ui.Button(c, "插入模板到空白草稿").Disabled(strings.TrimSpace(a.mergeYAMLDraft) != "").OnClick(func() {
						a.mergeYAMLDraft = mergeYAMLTemplate
						a.mergeYAMLError, a.mergeYAMLStatus = "", ""
					})
				})
			}
		})
	})
	extensionFeedback(c, a.mergeYAMLError, a.mergeYAMLStatus)
	extensionSaveBar(c, a.mergeYAMLDraft != a.mergeYAMLSaved, "保存 YAML 覆写", func() {
		a.mergeYAMLDraft = a.mergeYAMLSaved
		a.mergeYAMLError, a.mergeYAMLStatus = "", ""
	}, func() { _ = a.saveMergeYAMLDraft() })
}

func extensionError(c *ui.Context, message string) {
	pal := paletteFor(c)
	ui.Text(c, message).FillWidth().FontSize(11.5).TextColor(pal.Danger).Selectable()
}

func extensionFeedback(c *ui.Context, errText, status string) {
	pal := paletteFor(c)
	if errText != "" {
		extensionError(c, errText)
	} else if status != "" {
		ui.Text(c, status).FillWidth().FontSize(11.5).TextColor(pal.Success)
	}
}

func extensionSaveBar(c *ui.Context, dirty bool, label string, reset, save func()) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		state, color := "已与本地保存同步", pal.Muted
		if dirty {
			state, color = "草稿未保存", pal.Warn
		}
		ui.Text(c, state).FontSize(11.5).TextColor(color)
		ui.Spacer(c)
		ui.Button(c, "放弃修改").Disabled(!dirty).OnClick(reset)
		ui.PrimaryButton(c, label).Disabled(!dirty).OnClick(save)
	})
}

func (a *App) saveExtraRulesDraft() error {
	next := a.ov
	next.ExtraRules = extraRuleLines(a.extraRulesDraft)
	a.extraRulesStatus = ""
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.extraRulesError = err.Error()
		return err
	}
	a.extraRulesSaved, a.extraRulesError = a.extraRulesDraft, ""
	a.extraRulesStatus = extensionSavedStatus(applied)
	return nil
}

func (a *App) saveMergeYAMLDraft() error {
	next := a.ov
	next.MergeYAML = a.mergeYAMLDraft
	a.mergeYAMLStatus = ""
	applied, err := a.commitExtensionOverrides(next)
	if err != nil {
		a.mergeYAMLError = err.Error()
		return err
	}
	a.mergeYAMLSaved, a.mergeYAMLError = a.mergeYAMLDraft, ""
	a.mergeYAMLStatus = extensionSavedStatus(applied)
	return nil
}

// Validate the actual composed config while the candidate overrides are in
// place. A running kernel must accept them before they can be persisted. If the
// atomic write fails, restore both the in-memory overrides and the old runtime.
// Neither draft nor the previously saved file is changed on any failure.
func (a *App) commitExtensionOverrides(next patch.Overrides) (applied bool, err error) {
	previous := a.ov
	a.ov = next
	if _, err := a.configBytes(); err != nil {
		a.ov = previous
		return false, fmt.Errorf("配置验证失败：%w", err)
	}
	running := a.k != nil && a.k.Running()
	if running && !a.startKernel() {
		a.ov = previous
		return false, fmt.Errorf("内核未接受配置，未保存：%s", a.notice)
	}
	if err := a.saveOverridesAtomic(); err != nil {
		a.ov = previous
		if running && !a.startKernel() {
			return false, fmt.Errorf("保存失败：%w；旧配置重新载入也失败：%s", err, a.notice)
		}
		return false, fmt.Errorf("保存失败，已保留原配置：%w", err)
	}
	return running, nil
}

func extensionSavedStatus(applied bool) string {
	if applied {
		return "已保存并重新载入内核"
	}
	return "已保存 · 下次启动内核时生效"
}

// saveOverridesAtomic is also usable by the other settings pages. Unlike
// saveOverrides it reports failures, and the same-directory rename keeps an
// interrupted or failed write from truncating the last saved settings.
func (a *App) saveOverridesAtomic() error {
	if a.ovPath == "" {
		return errors.New("覆写文件路径不可用")
	}
	body, err := json.MarshalIndent(a.ov, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.ovPath), ".overrides-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	// Overrides can contain credentials. Keep the completed file private too.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), a.ovPath)
}
