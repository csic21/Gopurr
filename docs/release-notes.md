Gopurr 0.6.4 将 Windows 托盘更新为圆形反色徽章（变体 C），让蜷睡猫在通知区更清晰。

- **Windows 托盘（变体 C）**：浅色任务栏为 Graphite 圆底 + 象牙白猫；深色任务栏为象牙圆底 + Graphite 猫。圆约占满 32×32，猫在圆内等比放大（约 1–2px 边距）。macOS 模板托盘图不变。
- 本版基于已公开的 v0.6.3（混合端口就绪判定与发布冒烟加固）与 v0.6.2（异步内核启动／重载、更快的 TUN 服务失败反馈）。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.4-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.4-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
