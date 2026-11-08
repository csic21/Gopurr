import { test, open, expect } from 'nsh/test';
import { desktopFixtureTransport } from './src/desktop/fixture';

function fixture(phase='idle',visible=false){
 const dispatch=desktopFixtureTransport('en'),update:any={phase,visible,enabled:true,version:'0.6.25',notes:'Profiles and settings are retained.',downloaded:0,total:0};
 const calls:string[]=[];
 return {update,calls,host:{handlers:{'desktop.window.dirty':()=>{},'desktop.dispatch':async(data:unknown)=>{
  const r=data as {method:string;params:any};calls.push(r.method);
  if(r.method==='desktop.snapshot')return {result:{...await dispatch(r.method,r.params),update:{...update}}};
  if(r.method==='desktop.update.check')Object.assign(update,{phase:'checking',visible:true});
  if(r.method==='desktop.update.install')Object.assign(update,{phase:'downloading',downloaded:75,total:100});
  if(r.method==='desktop.update.cancel')update.phase='failed';
  if(r.method==='desktop.update.hide')update.visible=false;
  if(r.method==='desktop.update.show')update.visible=true;
  return {result:{...update}};
 }}}};
}

test('manual update checks open progress immediately and cancel without installing',async()=>{
 const f=fixture(),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US'});
 await app.press('id:desktop-nav-settings');await app.press('id:desktop-tab-maintenance');await app.press('text:Check updates');
 await app.waitFor('text:Checking for updates');expect(app.shows('id:desktop-update-progress')).toBe(true);
 await app.press('text:Cancel');await app.waitUntil(()=>!app.shows('id:desktop-update-dialog'));
 expect(f.calls.includes('desktop.update.cancel')).toBe(true);expect(f.calls.includes('desktop.update.install')).toBe(false);
});

test('tray-initiated updates appear over any page and downloads have progress',async()=>{
 const f=fixture('available',true),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US',reducedMotion:true});
 await app.waitFor('text:Update available');expect(app.shows('id:desktop-nav-overview')).toBe(true);
 await app.press('text:Download and install');await app.waitFor('text:Downloading update');expect(app.shows('id:desktop-update-progress')).toBe(true);expect(app.texts()).toContain('75 B / 100 B');
 await app.press('text:Cancel');await app.waitUntil(()=>!app.shows('id:desktop-update-dialog'));
});


test('visible inactive desktop windows receive update completion while background windows stop polling',async()=>{
 const f=fixture('checking',true),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US',reducedMotion:true,clock:{time:0}});
 await app.waitFor('text:Checking for updates');await app.setLifecycle('inactive');f.update.phase='current';
 await app.wait(1200);await app.waitFor('text:Up to date');
 await app.setLifecycle('background');const count=f.calls.filter(c=>c==='desktop.snapshot').length;
 await app.wait(2200);expect(f.calls.filter(c=>c==='desktop.snapshot').length).toBe(count);
});
