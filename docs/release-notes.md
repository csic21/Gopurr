Gopurr 0.6.55 恢复桌面侧栏收展动画，并修复代理列表无法滚到后续节点。

- 概览内容现在随侧栏收展平滑移动并改变宽度，不再在点击瞬间直接跳到目标尺寸。
- 切换时页面不再整页闪隐；侧栏宽度、内容区与导航叠层共用 240ms 原生节奏。
- 当前页的选中底色与左缘蓝色竖线继续常驻显示，不依赖悬停。
- 快速反向切换与系统「减少动态效果」继续可用。
- 代理列表向下滚动超过约 20 项后，不再重复显示前面的节点；后续节点可以继续出现并被选中。虚拟列表回收行后改为读取当前项。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.55-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.55-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.55-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.55-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
