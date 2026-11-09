import { expect, test } from 'nsh/test';
import { trafficAt, trafficCurve, trafficRest, trafficScale, trafficTransition } from './src/desktop/traffic';
const samples=(...values:number[])=>values.map(down=>({down,up:down/4}));

test('rolling traffic moves retained samples left without bending their values',()=>{
  const old=samples(100,300,200),next=samples(300,200,400);
  const transition=trafficTransition(old,next,trafficRest(old))!;
  const halfway=trafficAt(transition,.5);
  expect(halfway.points.find(p=>p.index===0)).toEqual({index:0,x:.25,down:300,up:75});
  expect(halfway.points[halfway.points.length-1]).toEqual({index:2,x:1,down:300,up:75});
  expect(trafficAt(transition,1)).toEqual(trafficRest(next,transition.to.scale));
});

test('traffic handles growing histories and skipped snapshots',()=>{
  const old=samples(100,300,200);
  for(const next of [samples(100,300,200,400),samples(200,400,500)]){
    const transition=trafficTransition(old,next,trafficRest(old))!;
    expect(!!transition).toBe(true);
    expect(trafficAt(transition,1).points.map(p=>({down:p.down,up:p.up}))).toEqual(next);
  }
});

test('a reset or disconnected history snaps instead of morphing unrelated samples',()=>{
  expect(trafficTransition(samples(100,300,200),samples(700,800,900),trafficRest(samples(100,300,200)))).toBe(null);
  expect(trafficTransition([],samples(100),trafficRest([]))).toBe(null);
  expect(trafficTransition(samples(100),[],trafficRest(samples(100)))).toBe(null);
});

test('an interrupted transition continues from the visible frame and reaches actual samples',()=>{
  const first=samples(100,300,200),second=samples(300,200,400),third=samples(200,400,500);
  const midway=trafficAt(trafficTransition(first,second,trafficRest(first))!,.4);
  const resumed=trafficTransition(second,third,midway)!;
  expect(trafficAt(resumed,0).points.map(p=>[p.x,p.up,p.down])).toEqual(midway.points.map(p=>[p.x,p.up,p.down]).concat([[1,midway.points[midway.points.length-1].up,midway.points[midway.points.length-1].down]]));
  expect(trafficAt(resumed,1)).toEqual(trafficRest(third,resumed.to.scale));
});

test('traffic scale has headroom and stays steady across small changes',()=>{
  const scale=trafficScale(samples(1300));
  expect(scale>1300).toBe(true);
  expect(trafficScale(samples(1100),scale)).toBe(scale);
  expect(trafficScale(samples(900),scale)).toBe(scale);
  expect(trafficScale(samples(2500),scale)>scale).toBe(true);
  expect(trafficScale(samples(0),16384)).toBe(1024);
});

test('the smoothed curve never overshoots the recorded samples',()=>{
  const points=[{x:0,y:10},{x:1,y:90},{x:2,y:30},{x:6,y:40},{x:7,y:40},{x:9,y:0}];
  const curve=trafficCurve(points)!;
  for(let i=0;i<curve.segments.length;i++){
    const segment=curve.segments[i],a=points[i].y,b=segment.end.y;
    for(let step=0;step<=20;step++){
      const t=step/20,u=1-t,y=u*u*u*a+3*u*u*t*segment.c1.y+3*u*t*t*segment.c2.y+t*t*t*b;
      expect(y>=Math.min(a,b)-1e-6&&y<=Math.max(a,b)+1e-6).toBe(true);
    }
  }
});

test('zero, single samples and duplicate endpoints stay finite',()=>{
  expect(trafficRest(samples(0)).points).toEqual([{down:0,up:0,index:0,x:1}]);
  expect(trafficCurve([])).toBe(null);
  expect(trafficCurve([{x:1,y:0},{x:1,y:0}])?.segments).toEqual([]);
  const a=samples(10,0),b=samples(0,0),transition=trafficTransition(a,b,trafficRest(a))!;
  expect(trafficAt(transition,1).points.every(p=>p.up===0&&p.down===0)).toBe(true);
});
