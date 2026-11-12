import { expect, open, test } from 'nsh/test';

for(const platform of ['ios','macos','windows'] as const) {
  test(`desktop dialog stays mounted through identical visibility snapshots on ${platform}`,async()=>{
    const app=await open('./DesktopModalPreview.tsx',{platform,width:840,height:560,clock:{time:0}});
    await app.waitFor('text:Snapshot 0');
    await app.clock!.runFor(3200);
    expect(app.texts()).toContain('Snapshot 3');
    expect(app.texts()).toContain('Mounted 1, unmounted 0');
    await app.press('text:Close');
    await app.waitFor('text:Polling dialog',{state:'gone'});
    expect(app.texts()).toContain('Mounted 1, unmounted 1');
    await app.clock!.runFor(1200);
    expect(app.shows('text:Polling dialog')).toBe(false);
    await app.press('text:Open');
    await app.waitFor('text:Polling dialog');
    expect(app.texts()).toContain('Mounted 2, unmounted 1');
    await app.keyboard.press('escape');
    await app.waitFor('text:Polling dialog',{state:'gone'});
  });
}

// NSH 0.4.0's Win32 backend stops drawing after a dialog fades out; the shared
// Modal asks the Windows host for one more whole-window frame to clear it.
test('windows dialog close asks the host for a whole-window repaint',async()=>{
  let repaints=0;
  const app=await open('./DesktopModalPreview.tsx',{platform:'windows',width:840,height:560,clock:{time:0},host:{handlers:{'desktop.window.repaint':()=>{repaints++;return {ok:true};}}}});
  await app.waitFor('text:Polling dialog');
  await app.press('text:Close');
  await app.waitFor('text:Polling dialog',{state:'gone'});
  await app.waitUntil(()=>repaints>0);
  expect(repaints).toBeGreaterThan(0);
});

test('macOS dialog close keeps the native material and asks for no repaint',async()=>{
  let repaints=0;
  const app=await open('./DesktopModalPreview.tsx',{platform:'macos',width:840,height:560,clock:{time:0},host:{handlers:{'desktop.window.repaint':()=>{repaints++;return {ok:true};}}}});
  await app.waitFor('text:Polling dialog');
  await app.press('text:Close');
  await app.waitFor('text:Polling dialog',{state:'gone'});
  await app.clock!.runFor(600);
  expect(repaints).toBe(0);
});
