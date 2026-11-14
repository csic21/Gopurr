import { host, signal, setColorScheme, cleanup } from "nsh";
import { language } from "./i18n";
import { mergeDesktopSnapshot } from "./snapshot";
import type { DesktopSnapshot, Page } from "./types";
export const desktopPlatform = signal(String(host.params.platform??"darwin"));
export const page = signal<Page>((["overview","subs","proxies","conns","mesh","rules","logs","settings"].includes(String(host.params.page)) ? host.params.page : "overview") as Page);
export const state = signal<DesktopSnapshot>({apiVersion:2,status:{traffic:{}},prefs:{},network:{},features:{},profiles:[],groups:[],history:[],service:{},capabilities:{}});
export const snapshot = state;
export const busy = signal("");
export const error = signal("");
export const loaded = signal(false);
export const pendingImport = signal<{url:string;name?:string}|null>(null);
export const closeRequested = signal(false);
export const chromeInset = signal(Math.max(0,Number(host.params.nativeTitlebarInset??host.params.nativeTitlebar??host.params.desktopTitlebarInset??80)));
export const chromeHeight = signal(Math.max(38,Number(host.params.nativeTitlebarHeight??38)));
export let scopeId = "";
type Transport = (method:string,params:any)=>Promise<any>;
let previewTransport:Transport|undefined;
let refreshing=false;
export function usePreviewTransport(transport:Transport) { previewTransport=transport; }
export async function nativeCall<T=any>(method:string,params:any={}):Promise<T> {
  if(previewTransport) return previewTransport(method,params);
  return await host.call(method,params,{timeout:0}) as T;
}
export async function call<T=any>(method:string,params:any={}):Promise<T> {
  if(previewTransport) return previewTransport(method,params);
  if(!host.available()) throw new Error("Launch Gopurr from its native desktop app.");
  const reply=await host.call("desktop.dispatch",{method,params},{timeout:0}) as {result?:T;error?:{message:string}|string};
  if(reply.error) throw new Error(typeof reply.error==="string"?reply.error:reply.error.message);
  if(!("result" in reply)) throw new Error("The desktop service returned an invalid reply.");
  return reply.result as T;
}

export async function refresh():Promise<void> {
  if(refreshing) return;
  refreshing=true;
  try {
    const requestedPage=page();
    const next=await call<DesktopSnapshot>("desktop.snapshot",{page:requestedPage,scopeId});
    if(next.apiVersion!==2) throw new Error("The desktop service and UI versions do not match.");
    // Object identities survive a JSON round trip only through this merge;
    // unchanged rows and controls then remain the same NSH nodes.
    const merged=mergeDesktopSnapshot(state(),next,requestedPage);
    if(merged!==state())state.set(merged);
    loaded.set(true);
    const appearance=next.prefs?.appearance;
    if(["system","light","dark"].includes(appearance)) setColorScheme(appearance);
    const lang=next.prefs?.language;
    if(["system","zh-Hans","en"].includes(lang)) language.set(lang);
  } finally {refreshing=false;}
}
export async function run(key:string,fn:()=>Promise<any>):Promise<void> {
  if(busy()) return;
  busy.set(key); error.set("");
  try { await fn(); await refresh(); } catch(e) {error.set(e instanceof Error?e.message:String(e));} finally {busy.set("");}
}
export function invoke(method:string,params:any={}) {return run(method,()=>call(method,params));}
export function navigate(value:Page) { page.set(value); void refresh().catch(e=>error.set(String(e))); }
export function listenDesktopEvents() {
 cleanup(host.on("desktop.event",(payload)=>{const e=payload as {kind:string;value:any};if(e.kind==="show"&&e.value?.page)navigate(e.value.page);if(e.kind==="import"){pendingImport.set(e.value);navigate("subs");}if(e.kind==="closeRequested")closeRequested.set(true);if(e.kind==="chrome"){if(typeof e.value?.left==="number"&&Number.isFinite(e.value.left))chromeInset.set(Math.max(0,e.value.left));if(typeof e.value?.height==="number"&&Number.isFinite(e.value.height))chromeHeight.set(Math.max(38,e.value.height));}if(e.kind==="error")error.set(String(e.value));if(e.kind==="prefs")void refresh().catch(v=>error.set(String(v)));}));
}
