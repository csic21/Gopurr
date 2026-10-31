Gopurr 0.6.5 修复 Windows 发布流水线中内核就绪判定的竞态，并保留 0.6.4 的圆形反色徽章托盘。

- **内核就绪**：守护进程仍在混合端口可连接后再写入 `core.json`；测试改为同时预留 TCP+UDP 端口并放宽就绪等待，避免 Windows 上 UDP 占用导致入站关闭、发布 Test 误失败。
- **Windows 托盘（变体 C）**：沿用 Graphite／象牙圆形反色徽章；macOS `tray.png` 不变。
- 本版基于已公开的 v0.6.4（圆形托盘）与 v0.6.3（混合端口就绪判定与发布冒烟加固）。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.5-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.5-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
