import { expect, open, test } from 'nsh/test';
import { desktopFixtureTransport } from './src/desktop/fixture';

function fixture(ready=true,failLaunch=false) {
  const dispatch=desktopFixtureTransport('en');
  const initialized=dispatch('desktop.snapshot').then(s=>{s.meshAccess.ready=ready;return s;});
  const calls:{method:string;params:any}[]=[];
  return {calls,host:{params:{page:'mesh'},handlers:{
    'desktop.window.dirty':()=>{},'desktop.openURL':()=>({opened:true}),'desktop.clipboard.write':()=>({written:true}),
    'desktop.dispatch':async(data:unknown)=>{
      const r=data as {method:string;params:any};calls.push(r);const s=await initialized;
      if(r.method==='desktop.mesh.shortcut'&&failLaunch)return {error:{message:'Client unavailable; keep your settings.'}};
      if(r.method==='desktop.mesh.probe')return {result:{state:'reachable',message:'Port is reachable; authentication is still required.'}};
      if(r.method==='desktop.mesh.shortcut')return {result:{url:'ssh://100.64.0.2:2222'}};
      return {result:await dispatch(r.method,r.params)};
    },
  }}};
}

test('mesh connection tabs stay in the dialog and reset only when changing protocol',async()=>{
  const f=fixture(),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US',platform:'macos'});
  await app.press('text:Connect');
  for(const id of ['ssh','rdp','vnc'])expect(app.shows(`id:desktop-tab-${id}`)).toBe(true);
  expect(app.value('id:mesh-quick-port')).toBe('22');
  await app.type('id:mesh-quick-user','karl');
  await app.clear('id:mesh-quick-port');await app.type('id:mesh-quick-port','2222');
  await app.press('id:desktop-tab-ssh');expect(app.value('id:mesh-quick-port')).toBe('2222');
  await app.keyboard.press('shift+tab');await app.keyboard.press('right');expect(app.value('id:mesh-quick-port')).toBe('3389');expect(app.shows('id:mesh-quick-user')).toBe(false);
  await app.keyboard.press('end');expect(app.value('id:mesh-quick-port')).toBe('5900');
  await app.keyboard.press('home');expect(app.value('id:mesh-quick-port')).toBe('22');expect(app.value('id:mesh-quick-user')).toBe('karl');
  await app.keyboard.press('escape');await app.waitFor('id:mesh-quick-dialog',{state:'gone'});
  expect(f.calls.some(c=>c.method.startsWith('desktop.mesh.shortcut')||c.method==='desktop.mesh.probe')).toBe(false);
});

test('mesh connection actions use the current draft and keep feedback inside the dialog',async()=>{
  const f=fixture(),app=await open('./Desktop.tsx',{host:f.host,width:1060,height:720,locale:'en-US',platform:'macos'});
  await app.press('text:Connect');await app.clear('id:mesh-quick-port');await app.type('id:mesh-quick-port','2222');await app.type('id:mesh-quick-user','karl');
  await app.press('id:mesh-quick-remember');await app.press('id:mesh-quick-probe');await app.waitFor('text:Port is reachable; authentication is still required.');
  expect(f.calls.find(c=>c.method==='desktop.mesh.probe')?.params).toEqual({id:'peer-1',protocol:'ssh',port:2222});
  await app.press('id:mesh-quick-launch');await app.waitFor('text:Client requested. Complete authentication in the client.');
  expect(f.calls.find(c=>c.method==='desktop.mesh.shortcut')?.params).toEqual({id:'peer-1',kind:'ssh',port:2222,user:'karl',remember:false});
  await app.press('id:mesh-quick-close');expect(app.texts()).not.toContain('Client requested. Complete authentication in the client.');
});

test('mesh preparation can be cancelled without losing the connection draft',async()=>{
  const f=fixture(false),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US',platform:'macos'});
  await app.press('text:Connect');await app.type('id:mesh-quick-user','karl');expect(app.enabled('id:mesh-quick-launch')).toBe(false);
  await app.press('id:mesh-quick-content >> text:Prepare access');await app.waitFor('text:Enable and prepare');await app.press('text:Cancel');
  expect(app.value('id:mesh-quick-user')).toBe('karl');expect(app.enabled('id:mesh-quick-launch')).toBe(false);
  expect(f.calls.some(c=>c.method==='desktop.mesh.action')).toBe(false);
});

test('a failed client launch preserves the draft and shows its error in the dialog',async()=>{
  const f=fixture(true,true),app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,locale:'en-US',platform:'macos'});
  await app.press('text:Connect');await app.type('id:mesh-quick-user','karl');await app.press('id:mesh-quick-launch');await app.waitFor('text:Client unavailable; keep your settings.');
  expect(app.value('id:mesh-quick-user')).toBe('karl');expect(app.enabled('id:mesh-quick-launch')).toBe(true);
  await app.press('id:desktop-tab-vnc');expect(app.texts()).not.toContain('Client unavailable; keep your settings.');
});
