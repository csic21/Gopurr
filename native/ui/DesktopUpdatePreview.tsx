import { mount } from 'nsh';
import { DesktopApp } from './src/desktop/app';
import { desktopFixtureTransport } from './src/desktop/fixture';
import { page, usePreviewTransport } from './src/desktop/client';
const dispatch=desktopFixtureTransport();
const update:any={enabled:true,phase:'idle',visible:false};let timer=0;
usePreviewTransport(async(method,params)=>{
 if(method==='desktop.snapshot'){const s=await dispatch(method,params);return {...s,status:{...s.status,version:'0.6.24'},update:{...update}};}
 if(method==='desktop.update.check'){
  clearTimeout(timer);Object.assign(update,{phase:'checking',visible:true,error:'',version:'0.6.25'});
  update.notes=`恢复桌面字号、菜单栏托盘与软件更新进度。

- 保留订阅、偏好、组网身份及既有更新公钥。
- 检查、下载、取消和重试均显示实际状态；后台代理保持运行。
- 桌面选择菜单支持键盘操作，实时流量与搜索状态持续更新。

下载：

- macOS 12 及以上 Apple Silicon：gopurr-arm64.dmg。
- Windows 10 1809 及以上／Windows 11 x64：gopurr-setup-amd64.exe。
- Android 9 及以上 ARM64：gopurr-android-arm64-v8a.apk。
- Android x86_64：gopurr-android-x86_64.apk。

也可在应用内检查更新。下载后请核对发布文件的 SHA-256；订阅与设置会保留。

此段较长的说明用于检查最小窗口中的滚动、关闭按钮和底部下载操作。`;
  timer=setTimeout(()=>{update.phase='available';},2500);
 }
 if(method==='desktop.update.install'){
  Object.assign(update,{phase:'downloading',downloaded:40*1024*1024,total:80*1024*1024});
  timer=setTimeout(()=>{update.phase='ready';},1800);
 }
 if(method==='desktop.update.cancel'){clearTimeout(timer);update.phase='idle';}
 if(method==='desktop.update.hide')update.visible=false;
 if(method==='desktop.update.show')update.visible=true;
 return {...update};
});
page.set('settings');mount(DesktopApp);
