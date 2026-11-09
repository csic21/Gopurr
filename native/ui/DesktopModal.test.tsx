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
