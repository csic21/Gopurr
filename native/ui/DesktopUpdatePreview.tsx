import { mount } from 'nsh';
import { DesktopApp } from './src/desktop/app';
import { desktopFixtureTransport } from './src/desktop/fixture';
import { page, usePreviewTransport } from './src/desktop/client';
const dispatch=desktopFixtureTransport();
const update:any={enabled:true,phase:'idle',visible:false};let timer=0;
usePreviewTransport(async(method,params)=>{
 if(method==='desktop.snapshot'){const s=await dispatch(method,params);return {...s,status:{...s.status,version:'0.6.24'},update:{...update}};}
 if(method==='desktop.update.check'){
  clearTimeout(timer);Object.assign(update,{phase:'checking',visible:true,error:'',version:'0.6.25',notes:'恢复桌面字号、菜单栏托盘与软件更新进度。'});
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
