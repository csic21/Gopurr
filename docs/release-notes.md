Gopurr 0.6.6 改进 Windows 托盘图标的小尺寸辨识度，采用圆底与宽脸双耳剪影。

- **Windows 托盘**：去掉拥挤的眼鼻细节，使用清楚的双耳和饱满轮廓。浅色任务栏显示纯黑圆底与纯白猫头，深色任务栏反转；圆外保持透明。
- **小尺寸表现**：调整猫头与圆底的比例和留白，保留耳尖，更新 16／20／24／32px 明暗预览。
- **资源生成**：`make icons` 同步生成 Windows 托盘资源，确保后续构建能复现同一设计。
- macOS 继续使用原有的蜷睡猫菜单栏图标。

macOS Apple Silicon（M1 及后续 M 系列）下载 `gopurr-0.6.6-arm64.dmg`，打开后将 Gopurr 拖入 Applications。Windows 10/11 x64 下载 `gopurr-setup-0.6.6-amd64.exe`，按向导安装。

在「设置 → 维护 → 软件更新」检查新版本。更新包通过 Ed25519 签名校验。若提示 TUN 服务需要更新，在概览页点击「重新安装…」并完成管理员授权。

本版本尚未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。确认下载来自本仓库后，macOS 可在「系统设置 → 隐私与安全性」允许打开，Windows 可在 SmartScreen 中查看发布信息。

`SHA256SUMS` 用于校验安装包与更新文件。`update-*.json` 和 `.tar.gz` 由应用内更新器使用，无需手动安装。
