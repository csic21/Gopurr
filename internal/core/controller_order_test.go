package core

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReloadSanitizerPreservesDNSOrder(t *testing.T) {
	payload := "external-controller: 0.0.0.0:9090\ndns:\n  nameserver-policy:\n    z.example: 1.1.1.1\n    a.example: 8.8.8.8\n"
	body, _ := json.Marshal(map[string]string{"payload": payload})
	r := httptest.NewRequest("PUT", "/configs", strings.NewReader(string(body)))
	if err := sanitizeReload(r); err != nil {
		t.Fatal(err)
	}
	encoded, _ := io.ReadAll(r.Body)
	var result struct{ Payload string }
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Payload, "external-controller") ||
		strings.Index(result.Payload, "z.example:") > strings.Index(result.Payload, "a.example:") {
		t.Fatalf("reload lost ordering or controller isolation:\n%s", result.Payload)
	}
}
