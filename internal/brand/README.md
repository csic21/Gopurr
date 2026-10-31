# Gopurr 品牌素材

当前图标为纯白圆角底板与纯黑蜷睡猫剪影。身体贴地形成柔和椭圆，头靠在前爪旁，尾巴沿身体外缘收回并逐渐变细。字母 G 的联想让位于自然的猫姿态。面部采用完整的黑色剪影，不绘制眼睛、鼻子、嘴或其他五官；不添加浮雕或光泽。

- `icon-master.png`：应用与侧栏使用的 1024 × 1024 渲染源，底板外保持透明。
- `logo.svg`：同一设计的矢量版本，仅包含纯白 `#ffffff` 底板与纯黑 `#000000` 剪影，抗锯齿像素为中性灰。
- `tray.svg`：相同剪影的菜单栏版本，只有黑色与透明像素，缩紧视口以适配 16/18 像素。
- `logo.png`、`icon.png`、`sidebar.png`：从渲染源派生的 1024、512、128 像素版本。
- `tray.png`：从菜单栏矢量派生的 32 像素模板，由 macOS 适配明暗。
- `tray-windows-light.png` / `tray-windows-dark.png`：Windows 托盘专用 32 像素实色图（Graphite + 白边 / 浅色填充 + 深边），非模板；`main` 在 Windows 上按任务栏明暗切换。

编辑 `logo.svg` 与 `tray.svg` 时保持剪影路径一致，然后运行：

```sh
make brand-master
make icons preview-icons
make test
make build bundle
```

`make brand-master` 用项目的原生 SVG 渲染器重绘 `icon-master.png` 并更新派生尺寸。仅替换渲染源 PNG 时直接运行 `make icons`，不会重绘源文件。`docs/logo-preview.png` 包含明暗背景、应用图标与菜单栏的实际使用尺寸检查图。

当前设计使用内置 ImageGen 生成蜷睡猫，再将轮廓整理成矢量，以保证纯黑、纯白和干净的透明边缘。原始概念稿为 `logo-v5-concept.png`，提示与处理方式记录在 `logo-v5-prompt.txt`；运行尺寸以经过整理的 `icon-master.png` 为准。

当前纯剪影版本移除了眼部子路径，保留头部、身体、前爪与尾巴之间的结构留白，应用与菜单栏使用一致的轮廓。早期眼部方案的对比保存在 `docs/logo-eye-comparison.png`，当前使用尺寸以 `docs/logo-preview.png` 为准。

`logo-prompt.txt`、`logo-v2-*.txt` 与 `logo-v4-*` 保留早期图标的生成记录，不适用于当前设计。
