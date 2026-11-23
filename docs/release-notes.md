Gopurr 0.6.65 平衡了桌面侧栏收展的往返动画速度，并恢复了全部桌面可交互控件的手型指针光标。

- 侧栏切换改用时间对称的 240ms `easeInOut` 曲线，并收窄侧栏行交接区间与延长飞行导航标签可见区段，使菜单从顶部回落到左侧栏时与上飞阶段同样清晰、顺滑。
- 为桌面端全部按钮、开关、分段控制、下拉选择器与选项、侧栏/顶栏导航项、代理组与节点行、连接列表行以及组网复选框补齐 `cursor: pointer`，禁用状态保持默认光标。
- 已重新校验中英文过渡帧截图并完成应用构建。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.65-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.65-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.65-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.65-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
