//go:build cgo

// Command gopurr-bridge exposes the Go engine to native NSH hosts. Build it as
// a C archive on Apple platforms, or a shared library for JNI/other hosts.
package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef int (*gopurr_protector)(int);
static inline int gopurr_call_protector(void *fn, int fd) {
    return ((gopurr_protector)fn)(fd);
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unsafe"

	"gopurr/sdk/engine"
)

var bridge = struct {
	sync.RWMutex
	next    uint64
	engines map[uint64]*engine.Engine
}{engines: make(map[uint64]*engine.Engine)}

func failure(code, message string) []byte {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message}})
	return body
}

// returnedString contains panics at the ABI boundary. Every returned pointer
// belongs to the caller and must be released with GopurrFree, even on errors.
func returnedString(fn func() []byte) (result *C.char) {
	defer func() {
		if recover() != nil {
			result = C.CString(string(failure("internal", "The native engine call failed")))
		}
	}()
	return C.CString(string(fn()))
}

// GopurrOpen accepts {"home":"/absolute/private/path","mixedPort":17890}.
// The host must choose its own app data directory; desktop profiles are never
// discovered implicitly. A successful response is {"handle":<nonzero integer>}.
//
//export GopurrOpen
func GopurrOpen(optionsJSON *C.char) *C.char {
	return returnedString(func() []byte {
		if optionsJSON == nil {
			return failure("invalid_params", "Engine options are required")
		}
		var options struct {
			Home      string `json:"home"`
			MixedPort int    `json:"mixedPort"`
		}
		if err := json.Unmarshal([]byte(C.GoString(optionsJSON)), &options); err != nil {
			return failure("invalid_params", "Engine options must be valid JSON")
		}
		bridge.Lock()
		defer bridge.Unlock()
		instance, err := engine.Open(engine.Options{Home: options.Home, MixedPort: options.MixedPort})
		if err != nil {
			return failure("open_failed", err.Error())
		}
		bridge.next++
		bridge.engines[bridge.next] = instance
		body, _ := json.Marshal(map[string]uint64{"handle": bridge.next})
		return body
	})
}

// GopurrCall dispatches a JSON request and returns a JSON result/error envelope.
// Calls may originate from native worker threads. Closing waits for active
// calls, and the engine serializes changes to its process-wide mihomo runtime.
//
//export GopurrCall
func GopurrCall(handle C.uint64_t, requestJSON *C.char) *C.char {
	return returnedString(func() []byte {
		if requestJSON == nil {
			return failure("invalid_request", "A JSON request is required")
		}
		bridge.RLock()
		defer bridge.RUnlock()
		instance := bridge.engines[uint64(handle)]
		if instance == nil {
			return failure("closed", "The native engine is closed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return instance.Dispatch(ctx, []byte(C.GoString(requestJSON)))
	})
}

// GopurrStartVPN accepts a system-created Android TUN and a native socket
// protector. The callback remains valid until GopurrStopVPN/Close has returned.
// It may be called concurrently from Go-owned native threads.
//
//export GopurrStartVPN
func GopurrStartVPN(handle C.uint64_t, fd C.int, protector unsafe.Pointer) *C.char {
	return returnedString(func() []byte {
		bridge.RLock()
		defer bridge.RUnlock()
		instance := bridge.engines[uint64(handle)]
		if instance == nil {
			return failure("closed", "The native engine is closed")
		}
		if protector == nil {
			return failure("invalid_params", "A native socket protector is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		status, err := instance.StartVPN(ctx, int(fd), func(socket int) bool {
			return C.gopurr_call_protector(protector, C.int(socket)) != 0
		})
		if err != nil {
			return failure("vpn_failed", err.Error())
		}
		body, _ := json.Marshal(map[string]any{"result": status})
		return body
	})
}

// GopurrStartVPNWithPlan attaches the descriptor created from engine.tunnelPlan.
// Tokens are checked by Go against the current source and routing mode before
// any descriptor is duplicated or the working connection is changed.
//
//export GopurrStartVPNWithPlan
func GopurrStartVPNWithPlan(handle C.uint64_t, fd C.int, protector unsafe.Pointer, planToken *C.char) *C.char {
	return returnedString(func() []byte {
		if planToken == nil || protector == nil {
			return failure("invalid_params", "A tunnel plan and native socket protector are required")
		}
		bridge.RLock()
		defer bridge.RUnlock()
		instance := bridge.engines[uint64(handle)]
		if instance == nil {
			return failure("closed", "The native engine is closed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		status, err := instance.StartVPNWithPlan(ctx, int(fd), func(socket int) bool { return C.gopurr_call_protector(protector, C.int(socket)) != 0 }, C.GoString(planToken))
		if err != nil {
			var api *engine.Error
			if errors.As(err, &api) {
				return failure(api.Code, api.Message)
			}
			return failure("vpn_failed", err.Error())
		}
		body, _ := json.Marshal(map[string]any{"result": status})
		return body
	})
}

//export GopurrStopVPN
func GopurrStopVPN(handle C.uint64_t) *C.char {
	return returnedString(func() []byte {
		bridge.RLock()
		defer bridge.RUnlock()
		instance := bridge.engines[uint64(handle)]
		if instance == nil {
			return failure("closed", "The native engine is closed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status, err := instance.StopVPN(ctx)
		if err != nil {
			return failure("vpn_failed", err.Error())
		}
		body, _ := json.Marshal(map[string]any{"result": status})
		return body
	})
}

// GopurrCancelPending is safe to call from the UI/lifecycle thread before
// enqueueing a disconnect. It interrupts downloads without waiting for core I/O.
//
//export GopurrCancelPending
func GopurrCancelPending(handle C.uint64_t) {
	bridge.RLock()
	defer bridge.RUnlock()
	if instance := bridge.engines[uint64(handle)]; instance != nil {
		instance.CancelPending()
	}
}

// GopurrClose is idempotent. It stops this host's engine and releases ownership.
//
//export GopurrClose
func GopurrClose(handle C.uint64_t) {
	GopurrCancelPending(handle)
	bridge.Lock()
	defer bridge.Unlock()
	if instance := bridge.engines[uint64(handle)]; instance != nil {
		instance.Close()
		delete(bridge.engines, uint64(handle))
	}
}

//export GopurrFree
func GopurrFree(value *C.char) { C.free(unsafe.Pointer(value)) }

func main() {}
