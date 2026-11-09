// Explicit visual fixture. Polling replays a finite sample sequence, then settles.
import { mount } from 'nsh';
import { DesktopApp } from './src/desktop/app';
import { usePreviewTransport } from './src/desktop/client';
import { desktopFixtureTransport } from './src/desktop/fixture';
const dispatch=desktopFixtureTransport();
const samples=[{down:460800,up:19456},{down:921600,up:28672},{down:1515520,up:40960},{down:1126400,up:32768},{down:573440,up:16384},{down:204800,up:8192},{down:0,up:0}];
let history:{up:number;down:number}[]|undefined,step=0;
usePreviewTransport(async(method,params)=>{
 const result=JSON.parse(JSON.stringify(await dispatch(method,params)));
 if(method==='desktop.snapshot'){
  if(!history){history=result.history;result.status.traffic={...result.status.traffic,...history![history!.length-1]};}
  else if(step<samples.length){history=[...history.slice(1),samples[step++]];}
  result.history=history;
  result.status.traffic={...result.status.traffic,...history![history!.length-1]};
 }
 return result;
});
mount(DesktopApp);
