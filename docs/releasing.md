# 发布流程

支持 macOS `darwin/arm64` 与 Windows `windows/amd64`。所有正式构建都启用 `with_gvisor`，以支持默认的 mixed 和可选的 gvisor TUN 协议栈；不要用缺少该标签的裸 `go build` 作为发布包。在 `main` 推送和 PR 时，CI 在两个系统运行全部测试、编译程序并保存界面截图。Windows 还在一次性 runner 上验证真实 SCM 服务安装与崩溃重启、Wintun 网卡、系统代理请求与应用崩溃恢复、TUN 自动路由与 DNS、配置重载、服务升级和卸载。系统代理完整还原；TUN 自动路由仅接管保留的测试目的地址，不改变默认路由。不要在日常使用的 Windows 主机上运行该系统级冒烟命令。

当前控制接口版本为 3：在实际状态接口基础上增加 macOS 系统代理会话与受限的服务重启能力，并验证 TUN 预期路由的完整覆盖。旧接口服务需要在概览「更新服务…」授权更新一次；普通应用版本变化不会触发这个要求。

## 发布一个版本

1. 更新 `CHANGELOG.md`，添加 `## 版本号` 的更新说明。
2. 在本地通过 `make test`，提交并推送 `main`。
3. 创建版本标签并推送，例如：

   ```sh
   git tag -a v0.2.0 -m 'Gopurr 0.2.0'
   git push origin v0.2.0
   ```

`Release` 工作流在 macOS 和 Windows 分别测试、构建安装器及签名更新包，两个平台还会将真实签名更新包安装到一次性旧版应用中，验证从 0.0.0 到发布版本的完整检查、下载、校验与替换；全部成功后才公开 GitHub Release。手动运行工作流时填写已经存在的标签。已公开版本的附件不会被工作流覆盖；修复应使用新标签。

版本号由标签传给 `scripts/release`，构建时写入临时版本的 `mygo.json`，主程序嵌入同一配置。因此安装包、界面版本、服务内核版本和更新清单保持一致；构建结束恢复原配置。稳定版设为 latest，带 `-beta` 等后缀的版本作为 prerelease，默认更新通道不推送预发布版本。

每个 Release 包含 DMG、Windows NSIS 安装器、两个平台的 `.tar.gz` 签名更新包、`update-*.json` 和合并的 `SHA256SUMS`。工作流只上传明确收集的发布产物，不上传缓存或密钥。

更新请求优先使用当前系统代理；系统代理未开启时可使用正在运行的本地内核。这样从 Finder 或开始菜单启动、没有代理环境变量的应用也能检查和下载更新。

## 更新密钥

- 客户端公钥位于 `mygo.json` 的 `updates.publicKey`，仓库为 `updates.github`。
- 私钥保存在仓库 Actions 加密 Secret `MYGO_UPDATER_PRIVATE_KEY`；只有发布构建步骤读取它。
- 本机备份为 `.secrets/mygo-update.key`，目录和 `*.key` 均已被 Git 忽略。请妥善备份，不能直接换一把公钥，否则旧版本无法验证后续更新。
- 全新 checkout 在没有私钥时可以开发、测试和本地打包；正式发布必须提供密钥，缺失时构建会失败。
- 为降低首版发布复杂度，当前使用完整更新包，关闭增量更新。

本地构建 macOS 安装包：

```sh
# 应用包，使用 Makefile 中的项目缓存。
make build bundle

# 完整 DMG 和签名更新产物（本机有 .secrets 私钥时）。
GOPATH="$PWD/.cache/go" GOMODCACHE="$PWD/.cache/mod" \
GOCACHE="$PWD/.cache/build" GOTOOLCHAIN=go1.27.4 \
go run ./scripts/release -release -version v0.2.0 -platform darwin/arm64
```

Windows 在原生 Windows runner 上构建，mygo 自动准备 NSIS 安装器工具及框架运行库。mihomo 的 sing-tun 依赖已经内嵌 Wintun，无需用户手动下载 DLL。

## 系统代码签名

更新包签名已启用。Apple Developer ID／公证和 Windows Authenticode 需要对应的开发者证书，目前未配置。未来配置证书时，在私有 CI 凭据中提供签名身份，并调整构建选项；不要将证书或密码写入仓库。

## TUN 服务升级

普通子进程会在应用重启时更新。已安装的管理员服务继续使用独立的旧内核副本；应用升级不会自动覆盖它。要取得新版内核或后台服务的修复，可在网络详情更新服务一次，无需卸载应用或重新导入订阅。

强制「重新安装」仅在 `SecurityVersion` 或 `ControllerVersion` 低于当前应用常量时触发（安全边界或控制器 API 不兼容）。仅营销／应用 `Version` 不同且上述常量仍匹配时，服务视为 API 兼容：不强制重装，概览可软提示可选升级。不要在无 UAC／管理员授权的情况下静默写入 Program Files 或 LaunchDaemons。

Windows 更新服务先在受保护目录完成复制、同步、哈希核对和可执行性检查，再停止旧服务并替换副本。安装程序等待新服务 PID 对应的认证控制接口就绪；替换、启动或就绪检查失败时恢复旧程序及服务配置，并尝试恢复原运行状态。回滚失败会保留错误与可用备份供诊断。macOS 替换受保护副本并重新注册 LaunchDaemon。安装和卸载都不会删除订阅或用户设置。兼容服务的版本差异只在网络详情提供可选更新，不遮盖实际接管状态，也不把暂时无响应等同于需要重装。

macOS 的控制套接字位于私有临时目录，只通过数据目录中的软链发布。内核每 5 秒校验后端与已发布套接字，连续两次失效即正常退出，由 launchd 的 KeepAlive（或应用看门狗）以全新套接字重启；否则系统清理 `/tmp` 后进程会一直「活着但不可达」，同时继续占用混合端口与 TUN 路由。控制套接字已经失效时，网络详情的「重启服务」会用 `launchctl kickstart -k` 经一次管理员授权替换守护进程；套接字仍可用时继续使用无需授权的原地重启。

macOS 系统代理使用已有认证控制网关的 CONNECT 会话，只接受固定的代理操作，不接受 shell 命令、可执行文件或任意路径。连接断开时恢复设置，受保护的恢复记录覆盖服务崩溃；退出先关闭代理会话，再结束内核。`POST /gopurr/restart` 只能让服务请求自身退出，由 launchd 启动原来的受保护副本，不能替换程序。安装、替换特权程序和卸载继续使用显式管理员授权；未配置系统代码签名时，不增加绕过授权的自更新路径。

## 独立内核冒烟与诊断日志

`scripts/smoke.sh`（或构建后 `go run ./scripts/smoke -binary <应用路径>`）使用临时配置、随机认证密钥与空闲端口启动真实内核。测试通过该内核下载 4 MiB、上传 64 KiB，核对完整内容和内核流量计数，最后关闭测试进程并清理临时目录；不读取真实订阅、不安装服务、不修改系统代理。CI 在两个平台运行它，Windows 系统级冒烟仍仅限一次性 runner。

Windows 服务及应用启动的普通内核使用轮转日志：单文件最多 10 MiB，活动文件和两个归档合计最多 30 MiB。连续相同日志按 5 秒窗口汇总次数；诊断读取使用有界尾部。macOS LaunchDaemon 的系统重定向日志仍沿用原有机制。
