import { expect, open, test } from "nsh/test";
import { desktopFixtureTransport } from "./src/desktop/fixture";

test("Windows desktop settings keep startup controls and omit the macOS Dock preference", async()=>{
 const app=await open("./WindowsDesktopPreview.tsx",{width:840,height:560,platform:"windows",locale:"zh-CN"});
 await app.waitFor("id:desktop-overview");
 await app.press("id:desktop-nav-settings");
 await app.waitFor("text:启动与行为");
 expect(app.texts()).toContain("静默启动");
 expect(app.texts()).not.toContain("显示程序坞图标");
 expect(app.texts()).not.toContain("Dock icon");
});

// Tray/shortcut activation arrives through host events, independently of clicks.
// Keep exercising it after the Rules editor creates and updates its own scope.
test("Windows host activation continues across every page and theme", async()=>{
 const dispatch=desktopFixtureTransport();
 const app=await open("./Desktop.tsx",{width:840,height:560,platform:"windows",locale:"en-US",host:{params:{platform:"windows",nativeTitlebarInset:0},handlers:{"desktop.window.dirty":()=>{},"desktop.dispatch":async(value:unknown)=>{const v=value as {method:string;params:any};return {result:await dispatch(v.method,v.params)};}}}});
 for(const theme of ["light","dark"] as const){
  await app.setColorScheme(theme);
  for(const page of ["overview","subs","proxies","conns","mesh","rules","logs","settings"]){
   await app.host.send("desktop.event",{kind:"show",value:{page}});
   await app.waitFor(({mesh:"id:mesh-search",rules:"id:rules-search",settings:"id:settings-scroll",subs:"id:desktop-profiles",conns:"id:desktop-connections"} as Record<string,string>)[page]??`id:desktop-${page}`);
  }
 }
 expect(app.texts()).toContain("启动与行为");
});
