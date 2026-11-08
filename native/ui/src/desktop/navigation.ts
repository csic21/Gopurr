export interface NavigationLayout { leading:number; toggle:number; left:number; width:number; padding:number; widths:number[]; statusLeft:number }

// Match the desktop window chrome: center against the whole window, then clamp
// between the native controls/brand and the persistent kernel status.
export function navigationLayout(viewport:number,inset:number,labelWidths:number[],brandWidth:number,statusWidth:number):NavigationLayout {
  const leading=inset+8+24+6+brandWidth;
  const left=leading+50, right=43+statusWidth;
  const labels=labelWidths.reduce((sum,v)=>sum+v,0), gaps=Math.max(0,labelWidths.length-1)*2;
  const fit=(viewport-left-right-labels-gaps)/(2*labelWidths.length);
  const padding=Math.max(4,Math.min(viewport<900?10:12,fit));
  const widths=labelWidths.map(v=>v+2*padding),width=widths.reduce((sum,v)=>sum+v,0)+gaps;
  return {leading,toggle:leading+4,left:Math.max(left,Math.min((viewport-width)/2,viewport-right-width)),width,padding,widths,statusLeft:viewport-16-11-statusWidth};
}
