Gopurr 0.6.73 全面重构 PC 桌面端与手机端的节点和订阅体验，并新增自建 VPS 分享链接解析、零订阅本地档案与订阅刷新节点保护。

- 订阅叠加自建 VPS 节点与刷新保护：在远程订阅上新增的自建节点通过 `AppendMembers` 非破坏性叠加，刷新订阅时完整保留手动节点与原订阅规则并自动去重同名成员；删除本地节点同事务清理组引用并校验悬空引用。
- 自建 VPS 分享链接解析与零订阅本地档案：内置基于 mihomo 的多协议 URI（`vless`/`vmess`/`trojan`/`ss`/`ssr`/`hysteria2`/`tuic`/`wireguard`/`socks5`/`http`）、Base64 与 Clash YAML 容错解析；支持 0 订阅自动创建「本地节点」纯本地档案，直写基础 YAML 兼容聚合与导出。
- PC 桌面端代理与订阅重构：三段响应式 Master-Detail 代理页（`1060×720` 展开侧栏自动收折组轨为原生 `Select`，`840×560` 降级单层居中 `Modal`）、结构化多协议节点表单与快速粘贴新增、策略组「跟随订阅新增 / 自定义完整列表」双模式与零嵌套选择器、客户端并发测速池与停核离线预览；订阅页新增「全部更新」、智能统一导入（首导支持 UA/代理与 `http://` 歧义切换）、4px 流量进度条与叠加摘要、清晰自动更新选择器和轻量实时聚合预览。
- 手机端节点管理与订阅升级：节点页支持模式切换、`全部节点` 视图、筛选排序、并发测速池、多通道快速新增（粘贴链接容错预览 / 5 段渐进手动表单 / 文件导入）、策略组双模式编辑子页与 Android VPN `reconnectRequired` 串行安全重连；订阅页补齐流量进度条、到期倒计时、可选名称、全参数编辑及宽屏 Master-Detail。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.73-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.73-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.73-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.73-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
