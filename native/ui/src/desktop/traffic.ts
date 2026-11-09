export type TrafficSample = { up:number; down:number };
export type TrafficPoint = TrafficSample & { index:number; x:number };
export type TrafficFrame = { points:TrafficPoint[]; scale:number };
export type TrafficTransition = { from:TrafficFrame; to:TrafficFrame; history:TrafficSample[] };

const peak = (history:TrafficSample[]) => Math.max(0,...history.map(v=>Math.max(v.up,v.down)));

// A little headroom and hysteresis keep a quiet chart from breathing every second.
export function trafficScale(history:TrafficSample[], current=1024):number {
  const desired=Math.max(1024,2**Math.ceil(Math.log2(Math.max(1,peak(history)*1.18))));
  return desired>current||desired*2<current?desired:current;
}

export function trafficRest(history:TrafficSample[], scale=trafficScale(history)):TrafficFrame {
  return {scale,points:history.map((v,index)=>({...v,index,x:history.length===1?1:index/(history.length-1)}))};
}

// Match retained samples, rather than interpolating unrelated array positions.
// This supports a growing history, a full sliding window, and skipped snapshots.
export function trafficTransition(previous:TrafficSample[], next:TrafficSample[], visible:TrafficFrame):TrafficTransition|null {
  let overlap=Math.min(previous.length,next.length);
  for(;overlap>0;overlap--){
    if(next.slice(0,overlap).every((v,i)=>v.up===previous[previous.length-overlap+i].up&&v.down===previous[previous.length-overlap+i].down))break;
  }
  if(!overlap||next.length===overlap)return null;
  const drop=previous.length-overlap,span=Math.max(1,next.length-1);
  const from:TrafficPoint[]=[],to:TrafficPoint[]=[];
  for(const point of visible.points){
    const index=point.index-drop;
    if(index>=next.length)continue;
    from.push({...point,index});to.push({...point,...(next[index]??{}),index,x:index/span});
  }
  const tail=visible.points[visible.points.length-1];
  if(!tail)return null;
  for(let index=overlap;index<next.length;index++){
    from.push({...tail,index,x:1});to.push({...next[index],index,x:index/span});
  }
  return {history:next,from:{points:from,scale:visible.scale},to:{points:to,scale:trafficScale(next,visible.scale)}};
}

export function trafficAt(transition:TrafficTransition, progress:number):TrafficFrame {
  if(progress>=1)return trafficRest(transition.history,transition.to.scale);
  const t=Math.max(0,progress),e=t*t*(3-2*t);
  return {scale:transition.from.scale+(transition.to.scale-transition.from.scale)*e,
    points:transition.to.points.map((v,i)=>{const a=transition.from.points[i];return {index:v.index,x:a.x+(v.x-a.x)*e,up:a.up+(v.up-a.up)*e,down:a.down+(v.down-a.down)*e};})};
}

type Point = {x:number;y:number};
export type CurveSegment = { c1:Point; c2:Point; end:Point };

// Monotone cubic interpolation softens corners without inventing traffic peaks.
export function trafficCurve(input:Point[]):{start:Point;segments:CurveSegment[]}|null {
  const points=input.filter((p,i)=>i===0||p.x>input[i-1].x+1e-6);
  if(!points.length)return null;
  const slopes=points.slice(1).map((p,i)=>(p.y-points[i].y)/(p.x-points[i].x));
  const tangents=points.map((_,i)=>{
    if(i===0)return slopes[0]??0;
    if(i===points.length-1)return slopes[i-1]??0;
    const a=slopes[i-1],b=slopes[i];return a*b>0?2*a*b/(a+b):0;
  });
  return {start:points[0],segments:points.slice(1).map((p,i)=>{const a=points[i],dx=(p.x-a.x)/3;return {c1:{x:a.x+dx,y:a.y+dx*tangents[i]},c2:{x:p.x-dx,y:p.y-dx*tangents[i+1]},end:p};})};
}
