Gopurr 0.6.61 修复 macOS 桌面侧栏收展时右下角出现灰色方块。

- 侧栏切换不再对整棵内容树做布局插值，内容宽度一次提交到目标值；侧栏滑出、品牌迁移和导航移动继续由 240ms 导航叠层完成。
- macOS AppKit 的中间帧不再保留旧宽度的卡片与分组行，右侧裁切处不会露出分组分隔线底色。Windows 行为不变。
- 展开／收起、明暗主题、中英文导航在 840×560 与 1060×720 下均已重新截图核对。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.61-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.61-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.61-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.61-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
