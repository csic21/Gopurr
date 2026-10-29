Gopurr 0.3.9 调整侧栏切换按钮的位置。

- 展开侧栏时，按钮向右移到分隔线之外，并留出间距。
- 收起侧栏时，按钮向左靠近品牌，沿用原来的紧凑标题栏和居中导航。
- 按钮跟随已有的切换动画平滑移动，保留完整的图标、悬停底色和点击区域。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-版本-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-版本-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。应用每天自动检查，确认后才下载和安装；更新包使用 Ed25519 签名校验。TUN 服务需在概览页安装，macOS 通过管理员密码、Windows 通过 UAC 授权；软件升级后若提示服务需要更新，点击「重新安装」。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 代码签名证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
