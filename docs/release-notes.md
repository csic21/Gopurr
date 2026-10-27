修复 macOS 退出应用时反复要求管理员授权的问题。每次启动后首次开启系统代理时授权，同一应用会话内的开关切换、端口调整和退出恢复不再重复弹窗。后台代理助手会在应用异常退出时尝试自动恢复接管前的代理设置；恢复失败时保留运行中的内核与恢复记录，并在概览页显示错误，方便重试。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-版本-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-版本-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。应用每天自动检查，确认后才下载和安装；更新包使用 Ed25519 签名校验。TUN 服务需在概览页安装，macOS 通过管理员密码、Windows 通过 UAC 授权；软件升级后若提示服务需要更新，点击「重新安装」。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
