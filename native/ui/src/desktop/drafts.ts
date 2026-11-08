import { cleanup, effect, signal } from "nsh";

// Only booleans cross the host boundary. Editors and cached credentials remain
// in this engine's memory, including when their page is temporarily unmounted.
const flags=signal<Record<string,boolean>>({});
export function setDesktopDraft(key:string,dirty:boolean):void {
  if(!key||flags()[key]===dirty||!dirty&&!(key in flags()))return;
  const next={...flags()};if(dirty)next[key]=true;else delete next[key];flags.set(next);
}
export function hasUnsavedDesktopDraft():boolean {return Object.values(flags()).some(Boolean);}
export function watchDesktopDraft(key:string|(()=>string),check:()=>boolean):void {
  const update=()=>setDesktopDraft(typeof key==="string"?key:key(),check());
  effect(update);cleanup(update);
}
