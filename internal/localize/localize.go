// Package localize translates application-owned messages. UI hosts supply the
// effective language; headless helpers keep source messages for their caller.
package localize

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// Chinese source messages are stable IDs; English is the fallback catalog.
//
//go:embed en.json
var catalogJSON []byte

var messages = func() map[string]string {
	var entries map[string]string
	if err := json.Unmarshal(catalogJSON, &entries); err != nil {
		panic(err)
	}
	return entries
}()

var language struct {
	sync.RWMutex
	get func() string
}

// SetLanguageSource connects the native language resolver without making
// profile/configuration packages depend on the desktop window runtime.
func SetLanguageSource(get func() string) {
	language.Lock()
	language.get = get
	language.Unlock()
}

func Text(message string) string {
	language.RLock()
	get := language.get
	language.RUnlock()
	if get == nil {
		return message
	}
	return Translate(get(), message)
}

func Translate(tag, message string) string {
	if tag == "zh-Hans" {
		return message
	}
	if text, ok := messages[message]; ok {
		return text
	}
	return message
}
