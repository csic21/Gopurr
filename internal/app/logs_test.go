package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

func TestLogSearchAndCopyPreserveFullDiagnostic(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "logs"
	a.logPaused, a.logLevelIdx = true, 2
	diagnostic := "TLS handshake failed: " + strings.Repeat("long-domain.example/", 14) + " certificate has expired"
	a.logFrozen = []core.LogLine{
		{Level: "info", Text: "TLS success"},
		{Level: "warning", Text: "different warning"},
		{Level: "error", Text: diagnostic},
	}
	tr := ui.NewTester(a.View, 840, 560)
	tr.Advance(200 * time.Millisecond)
	if err := tr.Click("搜索日志…"); err != nil {
		t.Fatal(err)
	}
	tr.Type("tls")
	tr.Frame()
	if !tr.HasText(diagnostic) || tr.HasText("TLS success") || tr.HasText("different warning") {
		t.Fatal("search ignored the level filter or truncated the full diagnostic")
	}
	if err := tr.Click("复制日志"); err != nil {
		t.Fatal(err)
	}
	if copied := tr.Clipboard(); !strings.Contains(copied, diagnostic) || strings.Contains(copied, "TLS success") {
		t.Fatalf("copy did not preserve exactly the filtered diagnostic: %q", copied)
	}
}
