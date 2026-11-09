await app.setViewport(1060,720);
await app.setColorScheme('light');
for(let frame=0;frame<16;frame++) {
 await app.screenshot(`.cache/traffic-frames/frame-${String(frame).padStart(2,'0')}.png`);
 await app.wait(80);
}
await app.wait(7500);
await app.screenshot('native/ui/screenshots/desktop-traffic-zero-light-1060x720.png');
