import { expect, open, test } from 'nsh/test';
import { desktopFixtureTransport } from './src/desktop/fixture';

function fixture() {
  const dispatch=desktopFixtureTransport('en');
  return {handlers:{'desktop.window.dirty':()=>{},'desktop.dispatch':async(data:unknown)=>{
    const r=data as {method:string;params:any};
    return {result:JSON.parse(JSON.stringify(await dispatch(r.method,r.params)))};
  }}};
}

for(const platform of ['ios','macos','windows'] as const) {
  test(`desktop policy selector stays open through snapshots on ${platform}`,async()=>{
    const app=await open('./Desktop.tsx',{host:fixture(),platform,width:840,height:560,locale:'en-US'});
    await app.press('id:feature-policy-dns');
    await app.waitFor('id:feature-policy-dns-options');
    await app.keyboard.press('down');
    await app.wait(2200);
    expect(app.shows('id:feature-policy-dns-options')).toBe(true);
    await app.keyboard.press('enter');
    await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==='desktop.features.policy'));
    const r=app.host.calls().find(c=>(c.data as any)?.method==='desktop.features.policy');
    expect((r?.data as any)?.params).toEqual({kind:'dns',enabled:true});
    await app.waitFor('id:feature-policy-dns-options',{state:'gone'});
  });
}

test('desktop policy cancellation does not change configuration',async()=>{
  const app=await open('./Desktop.tsx',{host:fixture(),platform:'ios',width:1060,height:720,locale:'en-US'});
  await app.press('id:feature-policy-sniffer');await app.keyboard.press('down');await app.keyboard.press('escape');
  await app.waitFor('id:feature-policy-sniffer-options',{state:'gone'});
  expect(app.host.calls().some(c=>(c.data as any)?.method==='desktop.features.policy')).toBe(false);
  await app.press('id:feature-policy-dns');await app.press('text:Force off');
  await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==='desktop.features.policy'));
  expect((app.host.calls().find(c=>(c.data as any)?.method==='desktop.features.policy')?.data as any)?.params).toEqual({kind:'dns',enabled:false});
});


test('closing and reopening menus releases the previous keyboard bindings',async()=>{
  const app=await open('./Desktop.tsx',{host:fixture(),platform:'ios',width:840,height:560,locale:'en-US'});
  await app.press('id:feature-policy-dns');await app.keyboard.press('down');await app.keyboard.press('escape');
  await app.press('id:feature-policy-sniffer');await app.keyboard.press('end');await app.keyboard.press('enter');
  await app.waitUntil(()=>app.host.calls().some(c=>(c.data as any)?.method==='desktop.features.policy'));
  await app.press('id:feature-policy-dns');await app.keyboard.press('down');await app.keyboard.press('enter');
  await app.waitUntil(()=>app.host.calls().filter(c=>(c.data as any)?.method==='desktop.features.policy').length===2);
  expect(app.host.calls().filter(c=>(c.data as any)?.method==='desktop.features.policy').map(c=>(c.data as any).params)).toEqual([{kind:'sniffer',enabled:false},{kind:'dns',enabled:true}]);
});

test('Escape closes a selector inside an editor while retaining the editor',async()=>{
  const app=await open('./Desktop.tsx',{host:fixture(),platform:'ios',width:840,height:560,locale:'en-US'});
  await app.press('id:feature-settings-dns');await app.press('id:dns-source');
  await app.wait(2200);expect(app.shows('id:dns-source-options')).toBe(true);
  await app.keyboard.press('escape');
  await app.waitFor('id:dns-source-options',{state:'gone'});expect(app.shows('id:dns-source')).toBe(true);
});
