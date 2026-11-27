// Run from the repository root:
// .cache/nsh/nsh/bin/nsh test native/ui/Preview.tsx --target macos < native/ui/mobile-nodes-upgrade.steps.js
await app.setViewport(390, 844);
await app.setColorScheme("light");

// 1. Upgraded Nodes page (light + dark)
await app.press("id:nav-proxies");
await app.waitFor("id:node-示例节点 / Example proxy");
await app.screenshot("native/ui/screenshots/mobile-nodes-light-390x844.png");
await app.setColorScheme("dark");
await app.screenshot("native/ui/screenshots/mobile-nodes-dark-390x844.png");
await app.setColorScheme("light");

// 2. Add Node BottomSheet
await app.press("id:nodes-add-btn");
await app.waitFor("id:add-node-manual");
await app.screenshot("native/ui/screenshots/mobile-add-node-sheet-light-390x844.png");

// 3. Node Editor Subpage (light + dark)
await app.press("id:add-node-manual");
await app.waitFor("id:node-editor-save");
await app.screenshot("native/ui/screenshots/mobile-node-editor-light-390x844.png");
await app.setColorScheme("dark");
await app.screenshot("native/ui/screenshots/mobile-node-editor-dark-390x844.png");
await app.setColorScheme("light");
await app.press("id:node-editor-back");
await app.waitFor("id:nodes-groups-btn");

// 4. Policy Groups Manager & Group Editor Subpages
await app.press("id:nodes-groups-btn");
await app.waitFor("id:groups-add-new");
await app.screenshot("native/ui/screenshots/mobile-groups-manager-light-390x844.png");
await app.press("id:manage-group-代理选择");
await app.waitFor("id:group-editor-save");
await app.screenshot("native/ui/screenshots/mobile-group-editor-light-390x844.png");
await app.press("id:group-editor-back");
await app.press("id:groups-manager-back");

// 5. Upgraded Subscriptions tab (with traffic bar & settings sheet)
await app.press("id:nav-profiles");
await app.waitFor("id:quota-fixture-profile");
await app.screenshot("native/ui/screenshots/mobile-profiles-quota-light-390x844.png");
await app.setColorScheme("dark");
await app.screenshot("native/ui/screenshots/mobile-profiles-quota-dark-390x844.png");
await app.setColorScheme("light");

// 6. Wide 1060x720 Master-Detail Nodes view
await app.setViewport(1060, 720);
await app.press("id:nav-proxies");
await app.waitFor("id:nodes-detail-pane");
await app.screenshot("native/ui/screenshots/mobile-nodes-master-detail-1060x720.png");
