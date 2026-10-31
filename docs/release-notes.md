Gopurr 0.6.7 记住上次的系统代理会话，并减少不必要的 TUN 服务重装与更新后端口占用。

- **会话恢复**：在概览或托盘开关系统代理时，同步「启动时自动开启系统代理」；退出仍先恢复他人的系统代理，下次启动再按偏好重新启用。
- **更聪明的服务更新**：仅当 `SecurityVersion`／`ControllerVersion` 不兼容时强制重新安装 TUN 服务；仅应用版本变化且 API 仍兼容时不再强制重装，概览可软提示可选升级。
- **更新后端口占用**：启动与更新重启前回收 `core.json` 残留内核并等待混合端口释放；无自动恢复偏好时清理指向已失效 Gopurr 端口的残留系统代理。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.7-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.7-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。仅在安全／控制器版本不兼容时需要在概览页「重新安装…」并完成管理员授权；仅应用版本更新时一般不必重装服务。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
