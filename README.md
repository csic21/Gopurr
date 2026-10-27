<p align="center">
  <img src="internal/brand/logo.png" alt="Gopurr 蜷睡猫标记" width="160">
</p>

# Gopurr

让连接简单一点。

Gopurr 是基于 [mihomo](https://github.com/MetaCubeX/mihomo) 的原生桌面代理客户端，支持 macOS Apple Silicon 和 Windows x64。提供订阅管理、策略组与节点切换、延迟测试、系统代理、TUN、规则编辑和 YAML 覆写，界面支持跟随系统、浅色与深色外观。

![概览](docs/render-overview-live.png)

## 安装

从 [GitHub Releases](https://github.com/csic21/Gopurr/releases/latest) 下载当前版本：

| 平台 | 安装包 | 安装方式 |
| --- | --- | --- |
| macOS Apple Silicon（M1 及后续 M 系列） | `gopurr-版本-arm64.dmg` | 打开后将 Gopurr 拖入 Applications |
| Windows 10/11 x64 | `gopurr-setup-版本-amd64.exe` | 按安装向导安装到当前用户目录 |

安装与系统验证提示见 [发布说明](docs/release-notes.md)。

## 开始使用

1. 打开 Gopurr，应用会自动启动内核。
2. 在「订阅」导入订阅链接或本地 YAML 配置，并启用所需配置。
3. 在「代理」选择策略组和节点，可先运行延迟测试。
4. 在「概览」开启系统代理。需要 TUN 时，先点击「安装服务…」并完成管理员授权，再开启 TUN。

TUN 在 macOS 使用 LaunchDaemon，在 Windows 使用系统服务与内嵌 Wintun。服务安装、升级和卸载由用户主动发起并授权。

macOS 每次启动后首次开启系统代理时需要管理员授权。同一应用会话内的代理开关、端口调整及退出恢复会复用权限；退出时恢复接管前的配置。若恢复失败，应用保留内核并显示错误，修复后可再次退出。

## 功能

| 页面 | 能力 |
| --- | --- |
| 概览 | 实时速率、历史流量、连接与规则数量、规则/全局/直连模式、系统代理与 TUN 开关 |
| 订阅 | 链接与本地文件导入、流量与到期信息、自动更新、启用与删除 |
| 代理 | 策略组与节点切换、全部/本组/单节点延迟测试、测试进度与取消、排序与筛选 |
| 连接 | 主机、命中规则、代理链路、上下行流量，点击复制主机名 |
| 规则 | 生效规则搜索、出口筛选、额外规则构建与编辑、YAML 覆写 |
| 日志 | 级别筛选、暂停与继续、回到底部、清空 |
| 设置 | 外观、窗口行为、订阅更新、端口与局域网、IPv6、TUN 协议栈、测试参数及软件更新 |

手动策略组支持选择节点，自动策略组展示内核调度的当前出口。延迟测试支持去重、并发、取消、延迟排序、名称排序和仅显示可用节点；结果反映响应延迟与连通性。

![代理与节点](docs/render-proxies.png)

额外规则按顺序放在最终规则前部；YAML 覆写递归合并映射、整体替换数组，应用管理的网络参数最后写入。订阅更新保留自定义内容，配置校验、重载或保存失败时保留原配置与编辑草稿。

## 外观与窗口

在「设置 → 常规 → 外观」选择跟随系统、浅色或深色。应用、侧栏和菜单栏使用统一的纯黑白蜷睡猫标记。

![浅色外观](docs/render-light-settings.png)

macOS 可选择程序坞图标的显示方式，修改后立即生效并保存：

| 选项 | 行为 |
| --- | --- |
| 隐藏 | 始终隐藏程序坞图标，从菜单栏「显示窗口」打开界面 |
| 跟随窗口 | 打开窗口时显示，关闭窗口时隐藏 |
| 显示（默认） | 关闭窗口后保留图标，点击即可重新打开窗口 |

关闭窗口后代理继续运行。轻量模式会在设定时间后释放窗口，下次打开时重新创建；程序坞图标仍按所选偏好显示。最小化的窗口可从程序坞恢复。

macOS 菜单栏还提供系统代理、TUN、路由模式和节点选择。需要退出应用时，使用菜单栏/托盘的「退出 Gopurr」；macOS 也支持 ⌘Q。

## 软件更新

在「设置 → 维护 → 软件更新」或托盘菜单检查更新。应用默认每天检查新版本，展示更新说明，经确认后下载和安装。更新包使用固定公钥校验 Ed25519 签名，软件更新会保留订阅、规则和设置。

若更新后提示 TUN 服务版本落后，在概览页点击「重新安装…」并再次授权，即可升级服务内核。

## 数据目录

| 平台 | 默认目录 |
| --- | --- |
| macOS | `~/Library/Application Support/Gopurr/` |
| Windows | `%APPDATA%\Gopurr\` |

可通过 `GOPURR_HOME` 指定其他数据目录。目录包含界面偏好、订阅索引与内容、本地配置、规则覆写、运行配置及 mihomo 缓存；这些数据保存在应用包之外。

## 开发与构建

使用 Go 1.27.3。界面由 [mygo](https://github.com/egoist/mygo) 原生渲染，mihomo 内核在独立进程中运行，应用通过本地控制器 socket 与内核通信。

```sh
git clone https://github.com/csic21/Gopurr.git
cd Gopurr
make run
```

Makefile 使用仓库内的 `.cache` 缓存，并启用 TUN 所需的 `with_gvisor` 构建标签。

| 命令 | 用途 |
| --- | --- |
| `make run` | 编译并运行 |
| `make build` | 生成 `bin/Gopurr` |
| `make test` | 运行测试与界面渲染检查 |
| `make test-native` | 在 macOS 验证原生窗口与程序坞行为 |
| `make bundle` | 生成 `bin/darwin-arm64/Gopurr.app` |
| `make icons preview-icons` | 更新运行图标与明暗背景预览 |

测试使用临时数据目录、本地测试源和控制器，不加载真实订阅或修改系统代理。发布工作流在 macOS 与 Windows 上测试、构建安装器，并验证签名更新的完整安装过程。

- [发布流程](docs/releasing.md)
- [UI 规范](DESIGN.md)
- [品牌素材](internal/brand/README.md)
- [更新记录](CHANGELOG.md)

项目许可见 [LICENSE](LICENSE)，依赖许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 与 [licenses](licenses)。
