package profile

import (
	"errors"
	"net/http"
	"testing"
)

func TestImportBytesKeepsPortableSourceAndChecksBeforeCommit(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("proxies: []\nrules: ['MATCH,DIRECT']\n")
	p, err := s.AddBytesChecked("手机配置", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "" || !p.IsFile() {
		t.Fatalf("import retained host-specific source: %+v", p)
	}
	if err := s.Refresh(p.ID); err != nil {
		t.Fatalf("portable source cannot refresh from cache: %v", err)
	}
	if _, err := s.AddBytesChecked("rejected", append(body, []byte("# reject")...), func(Profile, []byte) error { return errors.New("reject") }); err == nil {
		t.Fatal("accepted rejected bytes")
	}
	if len(s.List()) != 1 {
		t.Fatal("rejected import changed index")
	}
	headers := http.Header{"Profile-Title": []string{"来源标题"}, "Subscription-Userinfo": []string{"upload=1; download=2; total=100"}}
	downloaded, err := s.AddDownloadedChecked("https://example.invalid/sub?token=secret", "", body, headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.Name != "来源标题" || downloaded.Info.Used() != 3 || downloaded.Kind != KindURL {
		t.Fatalf("download metadata lost: %+v", downloaded)
	}
}
