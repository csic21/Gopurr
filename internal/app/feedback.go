package app

import (
	"github.com/egoist/mygo/ui"
	"time"
)

type noticeKind uint8

const (
	noticeInfo noticeKind = iota
	noticeSuccess
	noticeError
)

type feedbackState struct {
	text        string
	kind        noticeKind
	expires     time.Time
	showDetails bool
}

func (a *App) successNotice(message string) {
	a.notice = message
	a.feedback = feedbackState{text: message, kind: noticeSuccess}
}
func (a *App) errorNotice(message string) {
	a.notice = message
	a.feedback = feedbackState{text: message, kind: noticeError}
}

// Feedback reuses the page header's existing subtitle line. It cannot move
// content or cover an action. Longer messages are available in a native dialog.
func (a *App) updateFeedback(c *ui.Context) {
	if a.notice == "" {
		a.feedback = feedbackState{}
		return
	}
	if a.feedback.text != a.notice {
		a.feedback = feedbackState{text: a.notice}
	}
	if a.feedback.kind == noticeSuccess {
		if a.feedback.expires.IsZero() {
			a.feedback.expires = c.Now().Add(4 * time.Second)
		}
		if !c.Now().Before(a.feedback.expires) {
			a.notice = ""
			a.feedback = feedbackState{}
			return
		}
		c.After(a.feedback.expires.Sub(c.Now()))
	}
}

func (a *App) feedbackDialog(c *ui.Context) {
	softModal(c, "action-feedback", &a.feedback.showDetails, func() {
		width, height := c.Size()
		ui.Column(c).Width(min(float32(520), width-64)).MaxHeight(height - 64).Padding(20).Gap(12).Children(func() {
			ui.Text(c, tr("操作反馈")).FontSize(18).FontWeight(600)
			ui.Scroll(c).FillWidth().MaxHeight(height - 220).Children(func() {
				ui.Text(c, a.notice).FillWidth().FontSize(12).Selectable()
			})
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				softPrimaryButton(c, tr("关闭提示")).OnClick(func() { a.notice = ""; a.feedback.showDetails = false })
			})
		})
	})
}
