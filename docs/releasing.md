# 发布流程

支持 macOS `darwin/arm64` 与 Windows `windows/amd64`。在 `main` 推送和 PR 时，CI 在两个系统运行全部测试、编译程序并保存界面截图。Windows 还在临时 runner 上验证真实 SCM 服务安装、Wintun 网卡创建、HTTP 代理、配置重载、服务升级和卸载，不修改系统代理或默认路由。

## 发布一个版本

1. 更新 `CHANGELOG.md`，添加 `## 版本号` 的更新说明。
2. 在本地通过 `make test`，提交并推送 `main`。
3. 创建版本标签并推送，例如：

   ```sh
   git tag -a v0.2.0 -m 'Gopurr 0.2.0'
   git push origin v0.2.0
   ```

`Release` 工作流在 macOS 和 Windows 分别测试、构建安装器及签名更新包，两个平台全部成功后才公开 GitHub Release。手动运行工作流时填写已经存在的标签。已公开版本的附件不会被工作流覆盖；修复应使用新标签。

版本号由标签传给 `scripts/release`，构建时写入临时版本的 `mygo.json`，主程序嵌入同一配置。因此安装包、界面版本、服务内核版本和更新清单保持一致；构建结束恢复原配置。稳定版设为 latest，带 `-beta` 等后缀的版本作为 prerelease，默认更新通道不推送预发布版本。

每个 Release 包含 DMG、Windows NSIS 安装器、两个平台的 `.tar.gz` 签名更新包、`update-*.json` 和合并的 `SHA256SUMS`。工作流只上传明确收集的发布产物，不上传缓存或密钥。

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
GOCACHE="$PWD/.cache/build" GOTOOLCHAIN=go1.27.3 \
go run ./scripts/release -release -version v0.2.0 -platform darwin/arm64
```

Windows 在原生 Windows runner 上构建，mygo 自动准备 NSIS 安装器工具及框架运行库。mihomo 的 sing-tun 依赖已经内嵌 Wintun，无需用户手动下载 DLL。

## 系统代码签名

更新包签名已启用。Apple Developer ID／公证和 Windows Authenticode 需要对应的开发者证书，目前未配置。未来配置证书时，在私有 CI 凭据中提供签名身份，并调整构建选项；不要将证书或密码写入仓库。

## TUN 服务升级

普通子进程会在应用重启时更新。已安装的管理员服务继续使用旧内核，客户端比较服务状态中的版本与应用版本，在概览页提示重新安装。这个过程保留一次明确的系统授权，Windows 会替换 Program Files 中的服务副本，macOS 重新注册 LaunchDaemon。安装和卸载都不会删除订阅或用户设置。
