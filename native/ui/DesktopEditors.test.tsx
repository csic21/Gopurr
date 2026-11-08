import { expect, open, test } from 'nsh/test';
import { desktopFixtureTransport } from './src/desktop/fixture';

function fixture(rejectRules=false){
  const dispatch=desktopFixtureTransport();const calls:{method:string;params:any}[]=[];
  const initialized=dispatch('desktop.snapshot').then((s:any)=>{s.prefs.language='en';s.prefs.shortcuts=['','','','','',''];s.network.unified=true;s.network.dns=null;s.scope={id:'',extraRules:[],mergeYaml:'',dns:null,templateIds:[]};s.features.dns={enable:true,enhancedMode:'fake-ip',ipv6:false,nameserver:['1.1.1.1'],defaultNameserver:['223.5.5.5'],fallback:[],fakeIPFilter:[]};s.mesh={status:{running:true,official:true,shieldsKnown:true,shieldsUp:true,self:{id:'self',name:'My Mac',ips:['100.70.0.1'],online:true},peers:[{id:'nas',name:'NAS',owner:'Karl',ips:['100.70.0.5'],online:true,os:'linux'}],services:[],blocked:[]},visits:[],autoConnect:false};s.meshAccess={ready:false};s.meshShortcuts={};return s;});
  return {calls,host:{params:{page:'rules'},handlers:{'desktop.window.dirty':()=>{},'desktop.dispatch':async(data:unknown)=>{const r=data as {method:string;params:any};calls.push(r);const s=await initialized;if(r.method==='desktop.snapshot')return {result:{...s,scope:{...s.scope,id:r.params?.scopeId??''}}};if((r.method==='desktop.rules.save'||r.method==='desktop.library.save')&&rejectRules)return {error:{message:'Invalid YAML: keep your draft'}};if(r.method==='desktop.rules.save'){Object.assign(s.scope,r.params);return {result:{}};}if(r.method==='desktop.mesh.services'){s.mesh.status.services=[{protocol:'tcp',port:22}];s.mesh.status.shieldsUp=!r.params.enabled;return {result:{}};}return {result:await dispatch(r.method,r.params)};},'desktop.file.open':()=>({path:null}),'desktop.file.save':()=>({path:null}),'desktop.openURL':()=>({opened:true}),'desktop.clipboard.write':()=>({written:true})}}};
}

test('desktop rule search preserves order and an unsuccessful save preserves the draft',async()=>{
 const f=fixture(true);const app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,platform:'macos',locale:'en-US'});
 await app.waitFor('id:rules-search');await app.type('id:rules-search','github');expect(app.texts()).toContain('github.com');expect(app.texts()).not.toContain('example.com');
 await app.press('text:YAML overrides');await app.waitFor('id:yaml-override-editor');await app.type('id:yaml-override-editor','dns:\n  invalid: [');await app.press('text:Save and apply');await app.waitFor('text:Invalid YAML: keep your draft');expect(app.value('id:yaml-override-editor')).toBe('dns:\n  invalid: [');expect(app.enabled('text:Save and apply')).toBe(true);await app.press('id:desktop-nav-overview');await app.press('id:desktop-nav-rules');await app.waitFor('id:yaml-override-editor');expect(app.value('id:yaml-override-editor')).toBe('dns:\n  invalid: [');
});

test('desktop local access stays closed until specific service ports are confirmed',async()=>{
 const f=fixture();const app=await open('./Desktop.tsx',{host:f.host,width:1060,height:720,platform:'macos',locale:'en-US'});await app.press('id:desktop-nav-mesh');await app.waitFor('text:Connected to Tailscale');await app.press('text:Local access');await app.press('text:Edit ports');
 expect(f.calls.some(c=>c.method==='desktop.mesh.services')).toBe(false);await app.press('text:SSH · TCP 22');await app.press('id:mesh-allow-after-save');await app.press('text:Save and allow access');await app.waitUntil(()=>f.calls.some(c=>c.method==='desktop.mesh.services'));
 const request=f.calls.find(c=>c.method==='desktop.mesh.services')!;expect(request.params.protocol).toBe('22/tcp');expect(request.params.enabled).toBe(true);
});

test('desktop settings saves partial preferences and keeps network changes explicit',async()=>{
 const f=fixture();const app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,platform:'macos',locale:'en-US'});await app.press('id:desktop-nav-settings');await app.waitFor('text:Appearance');await app.press('text:Network');await app.waitFor('text:Mixed proxy port');
 expect(f.calls.some(c=>c.method==='desktop.network.save')).toBe(false);await app.press('id:settings-allow-lan');expect(f.calls.some(c=>c.method==='desktop.network.save')).toBe(false);await app.press('text:Save and apply');await app.waitUntil(()=>f.calls.some(c=>c.method==='desktop.network.save'));expect(f.calls.find(c=>c.method==='desktop.network.save')!.params.allowLan).toBe(true);
});


test('desktop YAML library saves typed fields directly and guards unsaved close',async()=>{
 const f=fixture(true);const app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,platform:'macos',locale:'en-US'});await app.waitFor('id:rules-search');await app.press('text:YAML overrides');await app.press('text:Templates and order');await app.type('id:template-name','Keep template');await app.type('id:template-yaml','dns: [');expect(app.enabled('text:Save library')).toBe(true);await app.press('text:Save library');await app.waitFor('text:Invalid YAML: keep your draft');expect(app.value('id:template-name')).toBe('Keep template');expect(app.value('id:template-yaml')).toBe('dns: [');await app.press('text:Close');await app.waitFor('text:Discard template changes?');await app.press('text:Keep editing');expect(app.value('id:template-yaml')).toBe('dns: [');
});

test('desktop DNS retains unsaved custom fields after navigating away',async()=>{
 const f=fixture();const app=await open('./Desktop.tsx',{host:f.host,width:840,height:560,platform:'macos',locale:'en-US'});await app.press('id:desktop-nav-settings');await app.press('text:DNS');await app.waitFor('text:Follow profile and YAML');await app.press('text:Follow profile and YAML');await app.press('text:Custom');await app.waitFor('id:dns-nameserver');await app.clear('id:dns-nameserver');await app.type('id:dns-nameserver','https://dns.example.com/dns-query');await app.press('id:desktop-nav-overview');await app.press('id:desktop-nav-settings');await app.waitFor('id:dns-nameserver');expect(app.value('id:dns-nameserver')).toBe('https://dns.example.com/dns-query');expect(f.calls.some(c=>c.method==='desktop.features.save')).toBe(false);const flags=()=>app.host.posts().filter(p=>p.name==='desktop.window.dirty');expect(flags().at(-1)?.data).toEqual({dirty:true});expect(JSON.stringify(flags())).not.toContain('dns.example.com');await app.press('text:Discard');expect(flags().at(-1)?.data).toEqual({dirty:false});
});
