package mesh

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"time"
)

func (b *Embedded) Probe(ctx context.Context, id, kind string, port uint16) ProbeResult {
	r := ProbeResult{DeviceID: id, Port: port, State: "unknown", Checked: time.Now()}
	b.mu.Lock()
	n := b.node
	var d Device
	if n != nil {
		d = n.devices[id]
	}
	b.mu.Unlock()
	shortcut := Shortcut{Kind: kind, Port: port}
	if n == nil || shortcut.Validate(d) != nil {
		r.Message = "设备或组网连接已变化，请刷新后重试"
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	stop := context.AfterFunc(n.ctx, cancel)
	defer stop()
	conn, err := b.dialPeer(n, ctx, "tcp", net.JoinHostPort(d.IP(), strconv.Itoa(int(port))))
	if err != nil {
		r.State, r.Message = "failed", "未连通，请检查对方的端口授权、服务和网络"
		if ctx.Err() != nil {
			r.Message = "检测超时或已取消；请确认对方在线并开放了相应服务"
		}
		return r
	}
	defer conn.Close()
	stopConn := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopConn()
	_ = conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
	buf := make([]byte, 256)
	size, readErr := io.ReadAtLeast(conn, buf, 4)
	if ctx.Err() != nil {
		r.State, r.Message = "unknown", "检测已取消或组网已断开"
		return r
	}
	if size > 0 && (kind == "ssh" && bytes.HasPrefix(buf[:size], []byte("SSH-")) || kind == "vnc" && bytes.HasPrefix(buf[:size], []byte("RFB "))) {
		r.State, r.Message = "verified", shortcut.Label()+" 服务有响应；登录仍由客户端验证"
		return r
	}
	if readErr != nil {
		if ne, ok := readErr.(net.Error); !ok || !ne.Timeout() {
			r.State, r.Message = "failed", "对端关闭了连接，请检查服务授权与服务是否启动"
			return r
		}
	}
	r.State, r.Message = "connected", "TCP 端口已连接；尚未验证服务类型、授权或登录"
	return r
}
