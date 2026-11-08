import { expect, open, test } from "nsh/test";

test("Windows desktop settings keep startup controls and omit the macOS Dock preference", async()=>{
 const app=await open("./WindowsDesktopPreview.tsx",{width:840,height:560,platform:"windows",locale:"zh-CN"});
 await app.waitFor("id:desktop-overview");
 await app.press("id:desktop-nav-settings");
 await app.waitFor("text:启动与行为");
 expect(app.texts()).toContain("静默启动");
 expect(app.texts()).not.toContain("显示程序坞图标");
 expect(app.texts()).not.toContain("Dock icon");
});
