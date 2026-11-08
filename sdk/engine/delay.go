package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/metacubex/mihomo/tunnel"
)

func (e *Engine) delayCall(ctx context.Context, params json.RawMessage) (any, error) {
	var request struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if request.URL == "" {
		request.URL = "https://www.gstatic.com/generate_204"
	}
	if _, err := httpURL(request.URL); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, failure("closed", "内核已关闭")
	}
	if e.core == nil || !e.core.Running() {
		e.mu.Unlock()
		return nil, failure("not_running", "请先连接代理")
	}
	proxy, exists := tunnel.Proxies()[request.Name]
	epoch := e.runtimeEpoch
	e.mu.Unlock()
	if !exists {
		return nil, failure("invalid_params", "节点不存在")
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	delay, err := proxy.URLTest(ctx, request.URL, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failure("delay_failed", "节点延迟测试失败")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.closed || e.runtimeEpoch != epoch || !e.core.Running() {
		return nil, context.Canceled
	}
	if e.delays == nil {
		e.delays = map[string]uint16{}
	}
	e.delays[request.Name] = delay
	return map[string]any{"name": request.Name, "delay": delay}, nil
}
