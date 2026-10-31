Gopurr 0.6.3 修复 Windows TUN 服务「已就绪」与混合端口实际可连之间的竞态，并加固发布冒烟。

- **服务就绪**：写入状态前确认混合端口已接受连接；控制器 Probe 成功不再单独代表流量路径可用。
- **发布冒烟**：预留端口同时检查 TCP/UDP，HTTP 经混合端口探测带截止前重试，降低 Windows runner 偶发失败。
- **Windows 托盘**：Graphite 明暗图标剪影更大、边距约 1 像素；macOS 模板托盘图不变。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.3-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.3-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
