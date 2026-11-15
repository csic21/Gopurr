Gopurr 0.6.52 修复日志页的空值崩溃并发布侧栏选中项样式修复。

- 日志页在收到不完整条目时不再读取缺失的等级字段；筛选、计数和复制导出统一使用安全数据。
- 桌面侧栏选中项恢复为干净的浅蓝圆角块，不再叠加整圈描边、多余竖线和重投影。
- 动画叠层终点与静止导航共用同一选中样式。
- 保留 0.6.50 的品牌终点对齐与 0.6.49 的原轨迹；快速反向与系统「减少动态效果」不变。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.52-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.52-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.52-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.52-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
