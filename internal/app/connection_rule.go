package app

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

type connectionRuleState struct {
	showConnRule                                bool
	connRuleConnection                          core.Conn
	connRuleKind, connRuleValue, connRuleTarget string
	connRuleProfileID, connRuleProfileName      string
	connRuleScope                               int
	connRuleDisconnect                          bool
	connRuleError                               string
}

type connectionRuleChoice struct{ kind, value string }

func connectionRuleChoices(cn core.Conn) []connectionRuleChoice {
	var choices []connectionRuleChoice
	host := strings.TrimSuffix(strings.TrimSpace(cn.Host), ".")
	if host != "" {
		if _, err := netip.ParseAddr(host); err != nil {
			choices = append(choices, connectionRuleChoice{"DOMAIN", host}, connectionRuleChoice{"DOMAIN-SUFFIX", host})
		}
	}
	if cn.Process != "" {
		choices = append(choices, connectionRuleChoice{"PROCESS-NAME", cn.Process})
	}
	ip := cn.DestinationIP
	if ip == "" {
		ip = host
	}
	if addr, err := netip.ParseAddr(ip); err == nil && addr.Zone() == "" {
		kind := "IP-CIDR"
		if addr.Is6() {
			kind = "IP-CIDR6"
		}
		choices = append(choices, connectionRuleChoice{kind, netip.PrefixFrom(addr, addr.BitLen()).String()})
	}
	return choices
}

func (a *App) openConnectionRule(cn core.Conn) {
	choices := connectionRuleChoices(cn)
	if len(choices) == 0 {
		return
	}
	a.connRuleConnection = cn
	a.connRuleKind, a.connRuleValue, a.connRuleTarget = choices[0].kind, choices[0].value, "DIRECT"
	a.connRuleScope, a.connRuleProfileID, a.connRuleProfileName = 0, "", ""
	if a.store != nil {
		if p, err := a.store.Get(a.store.ActiveID()); err == nil {
			a.connRuleScope, a.connRuleProfileID, a.connRuleProfileName = 1, p.ID, p.Name
		}
	}
	a.connRuleDisconnect, a.connRuleError = false, ""
	a.showConn, a.showConnRule = false, true
}

func (a *App) saveConnectionRule() {
	rule, err := buildExtraRule(a.connRuleKind, a.connRuleValue, a.connRuleTarget)
	if err != nil {
		a.connRuleError = err.Error()
		return
	}
	if a.connRuleKind == "IP-CIDR" || a.connRuleKind == "IP-CIDR6" {
		rule += ",no-resolve"
	}
	if a.connRuleScope == 1 {
		if a.store == nil || a.store.ActiveID() != a.connRuleProfileID {
			a.connRuleError = tr("当前订阅已切换，请关闭对话框后重新创建规则。")
			return
		}
		local := a.profileOverrides(a.connRuleProfileID)
		local.ExtraRules = prependUniqueRule(local.ExtraRules, rule)
		_, err = a.commitProfileOverrides(a.connRuleProfileID, local)
	} else {
		next := a.ov
		next.ExtraRules = prependUniqueRule(next.ExtraRules, rule)
		_, err = a.commitExtensionOverrides(next)
	}
	if err != nil {
		a.connRuleError = err.Error()
		return
	}
	// Reload editors from the newly persisted list without losing other drafts.
	key := ""
	if a.connRuleScope == 1 {
		key = a.connRuleProfileID
	}
	if a.extensionReady && a.extensionDraftKey == key {
		a.extraRulesDraft = prependRuleDraft(a.extraRulesDraft, rule)
		a.extraRulesSaved = prependRuleDraft(a.extraRulesSaved, rule)
	} else if draft, ok := a.extensionDrafts[key]; ok {
		draft.extraRulesDraft = prependRuleDraft(draft.extraRulesDraft, rule)
		draft.extraRulesSaved = prependRuleDraft(draft.extraRulesSaved, rule)
		a.extensionDrafts[key] = draft
	}
	a.showConnRule = false
	a.notice = tr("规则已保存，新连接将使用新规则。")
	if a.connRuleDisconnect && a.connRuleConnection.ID != "" && a.k != nil {
		if err := a.k.CloseConnection(a.connRuleConnection.ID); err != nil {
			a.connError = tr("规则已保存，断开原连接失败，请手动重试。")
		} else {
			a.notice = tr("规则已保存，原连接已断开；应用重新连接后可查看命中结果。")
			a.refreshConnections()
		}
	}
}

func prependRuleDraft(draft, rule string) string {
	lines := []string{rule}
	if draft != "" {
		for _, line := range strings.Split(draft, "\n") {
			if strings.TrimSpace(line) != rule {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func prependUniqueRule(rules []string, rule string) []string {
	next := []string{rule}
	for _, existing := range rules {
		if existing != rule {
			next = append(next, existing)
		}
	}
	return next
}

func (a *App) connectionRuleDialog(c *ui.Context) {
	pal := paletteFor(c)
	softModal(c, "connection_rule-a-showConnRule", &a.showConnRule, func() {
		ui.Column(c).Width(520).Padding(20).Gap(12).Children(func() {
			ui.Text(c, tr("从连接创建规则")).FontSize(16).Bold().TextColor(pal.Text)
			ui.Scroll(c).Height(280).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					choices := connectionRuleChoices(a.connRuleConnection)
					kinds := make([]string, len(choices))
					for i, choice := range choices {
						kinds[i] = choice.kind
					}
					ui.Text(c, tr("匹配方式")).FontSize(11).TextColor(pal.Muted)
					ui.Select(c, &a.connRuleKind, kinds).FillWidth().Label(tr("连接规则类型")).OnChange(func() {
						for _, choice := range choices {
							if choice.kind == a.connRuleKind {
								a.connRuleValue = choice.value
							}
						}
					})
					ui.Text(c, tr("匹配内容")).FontSize(11).TextColor(pal.Muted)
					ui.TextInput(c, &a.connRuleValue).FillWidth().Label(tr("连接规则匹配内容"))
					ui.Text(c, tr("目标策略")).FontSize(11).TextColor(pal.Muted)
					var groups []core.Group
					if a.k != nil {
						groups = a.k.Groups()
					}
					ui.Select(c, &a.connRuleTarget, ruleTargets(groups, a.connRuleTarget)).FillWidth().Label(tr("连接规则目标策略"))
					ui.Text(c, tr("作用范围")).FontSize(11).TextColor(pal.Muted)
					softSegmented(c, &a.connRuleScope, tr("全局"), tr("当前订阅")).Disabled(a.connRuleProfileID == "").Label(tr("连接规则作用范围"))
					if a.connRuleScope == 1 {
						ui.Text(c, a.connRuleProfileName).FillWidth().SingleLine().Ellipsis("…").FontSize(11).TextColor(pal.Muted)
					}
					preview := fmt.Sprintf("%s,%s,%s", a.connRuleKind, a.connRuleValue, a.connRuleTarget)
					if strings.HasPrefix(a.connRuleKind, "IP-CIDR") {
						preview += ",no-resolve"
					}
					ui.Text(c, tr("规则预览 · 保存到所选范围的最前面")).FontSize(11).TextColor(pal.Faint)
					ui.Text(c, preview).FillWidth().Font("monospace").FontSize(11).TextColor(pal.Text).Selectable()
					settingsRow(c, tr("保存后断开原连接"), tr("应用重新连接后使用新规则"), func() {
						softSwitch(c, &a.connRuleDisconnect).Disabled(a.connRuleConnection.ID == "").Label(tr("保存后断开原连接"))
					})
				})
			})
			if a.connRuleError != "" {
				extensionError(c, a.connRuleError)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				busy, _ := a.subState()
				softButton(c, tr("取消")).OnClick(func() { a.showConnRule = false })
				softPrimaryButton(c, tr("保存分流规则")).Disabled(busy).OnClick(a.saveConnectionRule)
			})
		})
	})
}
