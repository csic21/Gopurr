import { test, open, expect } from "nsh/test";
import { desktopFixtureTransport } from "./src/desktop/fixture";
import { mergeDiff } from "./src/desktop/proxies";
import { navigationLayout } from "./src/desktop/navigation";
import { switchThumbTransform } from "./src/desktop/ui";
function host(reject=false) { const dispatch=desktopFixtureTransport();return {handlers:{"desktop.window.dirty":()=>{},"desktop.dispatch":async(value:unknown)=>{const v=value as {method:string;params:any};if(reject&&v.method==="desktop.profiles.import")return {error:{message:"Download failed; draft retained."}};return {result:await dispatch(v.method,v.params)};}}}; }
test("desktop proxy latency testing never switches selection",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-proxies");await app.waitFor("id:proxy-香港 01");await app.press("id:test-proxy-香港 01");expect(app.host.calls().some(c=>(c.data as any)?.method==="desktop.group.select")).toBe(false);await app.press("id:proxy-新加坡 03");await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==="desktop.group.select"));
});
test("desktop proxy mode and bulk testing stay explicit",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-proxies");await app.waitFor("id:group-节点选择");await app.press("id:proxy-mode-global");await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==="desktop.mode.set"&&(c.data as any)?.params?.mode==="global"));await app.waitFor("id:group-GLOBAL");await app.press("text:测试全部");await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==="desktop.group.delay"&&(c.data as any)?.params?.names?.includes("香港 01")));await app.press("id:proxy-mode-direct");await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==="desktop.mode.set"&&(c.data as any)?.params?.mode==="direct"));await app.waitFor("text:当前为直连模式");
});
test("failed desktop subscription imports preserve editable draft",async()=>{
 const app=await open("./Desktop.tsx",{host:host(true),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-subs");await app.press("id:add-profile");await app.type("id:profile-url","https://example.invalid/bad.yaml");await app.press("text:添加");await app.waitFor("text:Download failed; draft retained.");expect(app.value("id:profile-url")).toBe("https://example.invalid/bad.yaml");
});
test("connection search and closing a connection use its stable id",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-conns");await app.type("id:connections-search","youtube");await app.press("id:connection-conn-1");await app.press("text:断开连接");await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==="desktop.connections.close"&&(c.data as any)?.params?.id==="conn-1"));
});
test("desktop search clear restores the full connection list",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-conns");await app.type("id:connections-search","youtube");expect(app.texts()).not.toContain("download.example.com");await app.press("id:connections-search-clear");await app.waitFor("text:download.example.com");expect(app.value("id:connections-search")).toBe("");
});
test("proxy diff does not freeze credentials and marks explicit removals",()=>{expect(mergeDiff({password:"old",tls:{enabled:true,sni:"before"}},{password:"old",tls:{enabled:true}})).toEqual({tls:{sni:null}});});
test("desktop navigation retains searches and an unfinished import draft",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-proxies");await app.type("id:proxy-search","Tokyo");await app.press("id:desktop-nav-logs");await app.press("id:desktop-nav-proxies");expect(app.value("id:proxy-search")).toBe("Tokyo");await app.press("id:desktop-nav-subs");await app.press("id:add-profile");await app.type("id:profile-url","https://example.invalid/unfinished.yaml");await app.keyboard.press("mod+1");await app.keyboard.press("mod+2");expect(app.value("id:profile-url")).toBe("https://example.invalid/unfinished.yaml");
});
test("rapid navigation reversals persist the final layout and preserve the page",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-proxies");await app.type("id:proxy-search","Tokyo");await Promise.all([app.keyboard.press("mod+\\"),app.keyboard.press("mod+\\"),app.keyboard.press("mod+\\")]);await app.waitUntil(()=>app.host.calls().filter(c=>(c.data as any)?.method==="desktop.prefs.save").length===3);const writes=app.host.calls().filter(c=>(c.data as any)?.method==="desktop.prefs.save");expect((writes[2].data as any).params.prefs.sidebarCollapsed).toBe(true);expect(app.value("id:proxy-search")).toBe("Tokyo");
});
test("reduced motion keeps desktop layout controls usable",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN",reducedMotion:true});await app.press("id:desktop-toggle-sidebar");await app.press("id:desktop-nav-logs");await app.type("id:logs-search","DNS");expect(app.value("id:logs-search")).toBe("DNS");
});
test("native titlebar controls, brand, navigation and status never overlap",()=>{
 for(const inset of [0,80,100])for(const width of [840,1060])for(const labels of [[25,25,25,25,25,25,25,25],[56,44,43,76,32,31,28,47]]) {
  const l=navigationLayout(width,inset,labels,48,76);
  expect(l.leading).toBeGreaterThan(inset+24+6);
  expect(l.toggle+20).toBeLessThan(l.left);
  expect(l.left+l.width+8).toBeLessThan(l.statusLeft);
  expect(l.statusLeft+11+76).toBeLessThan(width);
 }
});
test("switch thumbs travel across their tracks",()=>{
 expect(switchThumbTransform(false)).toBe("none");
 expect(switchThumbTransform(true)).toBe("translateX(16px)");
});
test("first close cancels without hiding and persists acknowledgment before backgrounding",async()=>{
 const h=host();(h.handlers as Record<string,any>)["desktop.window.close"]=()=>({ok:true});
 const app=await open("./Desktop.tsx",{host:h,width:840,height:560,locale:"zh-CN"});
 await app.host.send("desktop.event",{kind:"closeRequested"});await app.waitFor("id:desktop-close-background");await app.press("text:取消");expect(app.host.calls().some(c=>c.name==="desktop.window.close")).toBe(false);
 await app.host.send("desktop.event",{kind:"closeRequested"});await app.press("id:desktop-close-background");await app.waitUntil(()=>app.host.calls().some(c=>c.name==="desktop.window.close"));
 const calls=app.host.calls(),ack=calls.findIndex(c=>(c.data as any)?.method==="desktop.prefs.save"&&(c.data as any)?.params?.prefs?.closeToTrayAcknowledged===true),close=calls.findIndex(c=>c.name==="desktop.window.close");expect(ack).toBeGreaterThan(-1);expect(close).toBeGreaterThan(ack);
});
test("exit changes do not animate again on identical snapshots",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.waitFor("text:香港 01");await app.press("text:直连");await app.waitFor("text:所有请求直接连接");await app.wait(1200);expect(app.texts()).not.toContain("香港 01");expect(app.texts()).toContain("直连");
});
test("a running local configuration can switch proxies without a saved profile",async()=>{
 const h=host(),dispatch=h.handlers["desktop.dispatch"];h.handlers["desktop.dispatch"]=async(value:unknown)=>{const result=await dispatch(value);if((value as any).method==="desktop.snapshot")(result.result as any).activeProfile="";return result;};
 const app=await open("./Desktop.tsx",{host:h,width:840,height:560,locale:"zh-CN"});await app.waitFor("id:overview-route-action");expect(app.enabled("id:overview-route-action")).toBe(true);await app.press("id:overview-route-action");await app.waitFor("id:proxy-search");
});
test("empty traffic history explains that it is waiting for real samples",async()=>{
 const h=host(),dispatch=h.handlers["desktop.dispatch"];h.handlers["desktop.dispatch"]=async(value:unknown)=>{const result=await dispatch(value);if((value as any).method==="desktop.snapshot"){(result.result as any).history=[];(result.result as any).status.traffic.stale=true;}return result;};
 const app=await open("./Desktop.tsx",{host:h,width:840,height:560,locale:"zh-CN"});await app.waitFor("text:等待真实流量采样");expect(app.texts()).toContain("—");
});
test("a failed first-close preference save keeps the window open",async()=>{
 const h=host(),dispatch=h.handlers["desktop.dispatch"];h.handlers["desktop.dispatch"]=async(value:unknown)=>{if((value as any).params?.prefs?.closeToTrayAcknowledged)return {error:{message:"Could not save preferences."}} as any;return dispatch(value);};(h.handlers as Record<string,any>)["desktop.window.close"]=()=>({ok:true});
 const app=await open("./Desktop.tsx",{host:h,width:840,height:560,locale:"zh-CN"});await app.host.send("desktop.event",{kind:"closeRequested"});await app.press("id:desktop-close-background");await app.waitFor("text:Could not save preferences.");expect(app.host.calls().some(c=>c.name==="desktop.window.close")).toBe(false);expect(app.enabled("id:desktop-close-background")).toBe(true);
});
test("the host receives only boolean dirty transitions and retains a hidden-page draft",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,locale:"zh-CN"});await app.press("id:desktop-nav-subs");await app.press("id:add-profile");await app.type("id:profile-url","https://example.invalid/secret-profile-token");await app.keyboard.press("mod+8");
 const dirtyPosts=()=>app.host.posts().filter(v=>v.name==="desktop.window.dirty");expect(dirtyPosts().map(v=>v.data)).toEqual([{dirty:false},{dirty:true}]);expect(JSON.stringify(dirtyPosts())).not.toContain("secret-profile-token");
 await app.keyboard.press("mod+2");expect(app.value("id:profile-url")).toBe("https://example.invalid/secret-profile-token");await app.press("text:取消");expect(dirtyPosts().map(v=>v.data)).toEqual([{dirty:false},{dirty:true},{dirty:false}]);
});
test("native tab lists support focused arrows and Home and End",async()=>{
 const app=await open("./Desktop.tsx",{host:host(),width:840,height:560,platform:"macos",locale:"zh-CN"});
 await app.keyboard.press("tab");await app.keyboard.press("right");await app.waitFor("text:所有请求使用全局策略");await app.keyboard.press("end");await app.waitFor("text:所有请求直接连接");await app.keyboard.press("home");await app.waitFor("text:节点选择 · 手动选择");
 await app.keyboard.press("shift+tab");await app.keyboard.press("shift+tab");await app.keyboard.press("down");await app.waitFor("id:desktop-profiles");await app.keyboard.press("end");await app.waitFor("text:偏好自动保存；网络与测试设置确认后应用");await app.keyboard.press("home");await app.waitFor("id:desktop-overview");
});
