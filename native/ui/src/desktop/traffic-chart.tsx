import { Canvas, cleanup, createAnimatedValue, effect, lifecycle, reduceMotion, signal, Text } from 'nsh';
import { colors } from './theme';
import { trafficAt, trafficCurve, trafficRest, trafficTransition } from './traffic';
import type { TrafficFrame, TrafficSample } from './traffic';

export function TrafficChart(p:{history:()=>TrafficSample[]}) {
  let previous:TrafficSample[]=[],visible:TrafficFrame=trafficRest([]),signature='',frame=0,latestSample:TrafficSample|undefined;
  const display=signal(visible);
  const show=(value:TrafficFrame)=>{visible=value;display.set(value);};
  effect(()=>{
    const next=p.history().map(v=>({up:Math.max(0,Number.isFinite(v.up)?v.up:0),down:Math.max(0,Number.isFinite(v.down)?v.down:0)}));
    const key=JSON.stringify(next),paused=reduceMotion()||lifecycle()!=='active';
    latestSample=next[next.length-1];
    if(paused){cancelAnimationFrame(frame);previous=next;signature=key;show(trafficRest(next));return;}
    if(key===signature)return;
    signature=key;cancelAnimationFrame(frame);
    const transition=trafficTransition(previous,next,visible);previous=next;
    if(!transition){show(trafficRest(next));return;}
    const start=Date.now();
    const tick=()=>{const t=Math.min(1,(Date.now()-start)/400);show(trafficAt(transition,t));if(t<1)frame=requestAnimationFrame(tick);};
    frame=requestAnimationFrame(tick);
  });
  cleanup(()=>cancelAnimationFrame(frame));
  return <Canvas testID="traffic-chart" style={{flex:1}} draw={(cx,size)=>{
    const current=display(),palette=colors(),bottom=size.height-4,height=Math.max(1,size.height-10),width=Math.max(1,size.width-6);
    cx.lineWidth=1;cx.strokeStyle=palette.grid+'99';
    for(let line=1;line<4;line++){const y=bottom-height*line/4;cx.beginPath();cx.moveTo(3,y);cx.lineTo(size.width-3,y);cx.stroke();}
    const channels=(['down','up'] as const).map(key=>({key,color:palette[key],curve:trafficCurve(current.points.map(v=>({x:3+v.x*width,y:bottom-v[key]/current.scale*height})))}));
    const path=(curve:NonNullable<ReturnType<typeof trafficCurve>>)=>{cx.beginPath();cx.moveTo(curve.start.x,curve.start.y);for(const s of curve.segments)cx.bezierCurveTo(s.c1.x,s.c1.y,s.c2.x,s.c2.y,s.end.x,s.end.y);};
    // Paint the two quiet fills first so neither masks the other channel's line.
    for(const {curve,color} of channels){if(!curve)continue;path(curve);const end=curve.segments[curve.segments.length-1]?.end??curve.start;cx.lineTo(end.x,bottom);cx.lineTo(curve.start.x,bottom);cx.closePath();const fill=cx.createLinearGradient(0,0,0,size.height);fill.addColorStop(0,color+'18');fill.addColorStop(1,color+'00');cx.fillStyle=fill;cx.fill();}
    cx.lineCap='round';cx.lineJoin='round';
    for(const {curve,color,key} of channels){if(!curve)continue;path(curve);cx.strokeStyle=color;cx.lineWidth=1.6;cx.shadowColor=color+'30';cx.shadowBlur=2;cx.stroke();cx.shadowBlur=0;const latest=current.points[current.points.length-1];if(latest?.[key]>0&&(latestSample?.[key]??0)>0){cx.beginPath();cx.arc(size.width-3,bottom-latest[key]/current.scale*height,2,0,Math.PI*2);cx.fillStyle=color;cx.fill();cx.strokeStyle=palette.surface;cx.lineWidth=1;cx.stroke();}}
  }}/>;
}

export function TrafficRate(p:{value:string;color:string}) {
  const opacity=createAnimatedValue(1);let previous=p.value;
  effect(()=>{const value=p.value,paused=reduceMotion()||lifecycle()!=='active';if(paused){previous=value;opacity.set(1);return;}if(value===previous)return;previous=value;opacity.set(.65);void opacity.timing(1,{duration:180,easing:'easeOut'});});
  cleanup(()=>{void opacity.stop();});
  return <Text style={{fontSize:27,fontWeight:'600',color:p.color,fontVariant:'tabular-nums',opacity}}>{p.value}</Text>;
}
