Gopurr 0.6.1 修复 Windows 托盘图标，并改善内核启动的可靠性。

- **Windows 托盘**：浅色／深色任务栏改用 Graphite 与描边图标，不再显示为黑色剪影；macOS 仍使用黑色模板剪影。
- **内核启动**：启动前回收上一会话残留的守护进程与 `core.sock`，避免间歇性端口占用或套接字无法替换；重置控制器空闲连接。
- **超时**：大型订阅首次解析与配置重载的等待时间更宽裕，减少误报「启动超时」；启动日志按次截断，便于定位本次失败原因。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.1-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.1-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
