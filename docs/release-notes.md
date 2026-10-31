Gopurr 0.6.2 让内核启动不再卡住界面，并更快提示不健康的 TUN 服务。

- **异步启动**：启动与重载在后台执行，界面显示「正在启动…」并防止重复点击；端口占用等校验失败仍立即提示。
- **TUN 服务**：已安装但无响应时约 2 秒内失败，并引导在概览页重新安装服务，避免长时间无反馈等待。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.2-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.2-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
