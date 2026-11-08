import { Canvas, cleanup, effect, host, Image, keymap, lifecycle, Text, View, signal, windowSize, reduceMotion } from "nsh";
import { Button, Tabs as NativeTabs, Tab } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { busy, call, chromeHeight, chromeInset, closeRequested, error, listenDesktopEvents, navigate, nativeCall, page, refresh, run, state } from "./client";
import { Action, Feedback, Icon, Label, Modal, Row } from "./ui";
import { OverviewPage } from "./overview";
import { ProfilesPage } from "./profiles";
import { ProxiesPage } from "./proxies";
import { ConnectionsPage } from "./connections";
import { LogsPage } from "./logs";
import { RulesPage } from "./rules";
import { MeshPage } from "./mesh";
import { SettingsPage } from "./settings";
import { UpdateDialog } from "./updates";
import type { Page, PageProps } from "./types";
import { hasUnsavedDesktopDraft, watchDesktopDraft } from "./drafts";
export { hasUnsavedDesktopDraft } from "./drafts";
import { navigationLayout } from "./navigation";
import logo from "../../assets/sidebar.png";
const nav:{id:Page;zh:string;en:string}[]=[{id:"overview",zh:"概览",en:"Overview"},{id:"subs",zh:"订阅",en:"Profiles"},{id:"proxies",zh:"代理",en:"Proxies"},{id:"conns",zh:"连接",en:"Connections"},{id:"mesh",zh:"组网",en:"Mesh"},{id:"rules",zh:"规则",en:"Rules"},{id:"logs",zh:"日志",en:"Logs"},{id:"settings",zh:"设置",en:"Settings"}];
export function DesktopApp(preview:{motionProgress?:number}={}) {
 const collapsed=signal(false), initial=signal(false),navProgress=signal(preview.motionProgress??0); let navFrame=0;
 const ease=(x:number)=>{let lo=0,hi=1;for(let i=0;i<18;i++){const t=(lo+hi)/2,c=3*(1-t)*(1-t)*t*.42+3*(1-t)*t*t*.58+t*t*t;if(c<x)lo=t;else hi=t;}const t=(lo+hi)/2;return 3*(1-t)*t*t+t*t*t;};
 const animateNav=(target:number)=>{cancelAnimationFrame(navFrame);if(reduceMotion()){navProgress.set(target);return;}const from=navProgress(),start=Date.now();const frame=()=>{if(reduceMotion()){navProgress.set(target);return;}const t=Math.min(1,(Date.now()-start)/320);if(t===1){navProgress.set(target);return;}navProgress.set(from+(target-from)*ease(t));navFrame=requestAnimationFrame(frame);};navFrame=requestAnimationFrame(frame);};
 cleanup(()=>cancelAnimationFrame(navFrame));
 const closeBusy=signal(false),closeError=signal("");
 const continueInBackground=async()=>{if(closeBusy())return;closeBusy.set(true);closeError.set("");try{await call("desktop.prefs.save",{prefs:{closeToTrayAcknowledged:true}});await nativeCall("desktop.window.close");closeRequested.set(false);}catch(e){closeError.set(e instanceof Error?e.message:String(e));}finally{closeBusy.set(false);}};
 watchDesktopDraft("operations",()=>!!busy()||closeBusy());
 let postedDirty:boolean|undefined;
 effect(()=>{const dirty=hasUnsavedDesktopDraft();if(dirty===postedDirty)return;postedDirty=dirty;if(host.available())host.post("desktop.window.dirty",{dirty});});
 const moving=()=>navProgress()!==(collapsed()?1:0);
 const lerp=(from:number,to:number)=>from+(to-from)*navProgress();
 let sidebarSave=Promise.resolve();
 const toggle=()=>{const value=!collapsed();collapsed.set(value);animateNav(value?1:0);sidebarSave=sidebarSave.then(()=>call("desktop.prefs.save",{prefs:{sidebarCollapsed:value}})).then(()=>refresh()).catch(e=>error.set(e instanceof Error?e.message:String(e)));};
 const goto=(value:Page)=>{navigate(value);void call("desktop.page.set",{page:value}).catch(e=>error.set(String(e)));};
 const p:PageProps={snapshot:state,call,refresh,busy,run,navigate:goto};
 const reload=()=>void refresh().then(()=>{if(!initial()){collapsed.set(!!state().prefs.sidebarCollapsed);if(preview.motionProgress===undefined)navProgress.set(collapsed()?1:0);initial.set(true);}}).catch(e=>error.set(e instanceof Error?e.message:String(e)));
 reload();
 listenDesktopEvents();
 const timer=setInterval(()=>{if(lifecycle()!=="background"&&!busy())reload();},1000);cleanup(()=>clearInterval(timer));
 nav.forEach((n,i)=>keymap.bind(`mod+${i+1}`,()=>goto(n.id)));
 keymap.bind("mod+shift+l",toggle);keymap.bind("mod+\\",toggle);keymap.bind("mod+r",reload);
 const titlebarInset=chromeInset;
 const titlebarHeight=chromeHeight;
 const measurements=signal<{labels:number[];brand:number;status:number}>({labels:[],brand:48,status:76});
 const statusLabel=()=>!state().status.kernel&&state().status.busy?tr("正在启动…","Starting…"):state().status.kernel?tr("内核运行中","Kernel running"):tr("内核已停止","Kernel stopped");
 const layout=()=>navigationLayout(windowSize().width,titlebarInset(),nav.map((n,i)=>measurements().labels[i]??tr(n.zh,n.en).length*(tr(n.zh,n.en)===n.zh?12.5:6.5)),measurements().brand,measurements().status);
 const topWidth=(index:number)=>layout().widths[index]??46;
 const topX=(index:number)=>layout().left+layout().widths.slice(0,index).reduce((sum,width)=>sum+width+2,0);
 return <View style={{flex:1,flexDirection:"row",backgroundColor:colors().bg}}>
 <View style={{width:lerp(184,0),backgroundColor:colors().side,borderRightWidth:navProgress()<1?1:0,borderColor:colors().border}}/>
 <View style={{flex:1,minWidth:0,pointerEvents:moving()?"none":"auto"}}><View style={{height:titlebarHeight(),paddingLeft:lerp(Math.max(192,layout().leading)+4,layout().toggle)+34-lerp(184,0),paddingRight:16,flexDirection:"row",alignItems:"center",gap:10}}><View style={{opacity:1-navProgress(),flexDirection:"row",gap:10}}><Label text={tr("工作区","Workspace")} muted size={11}/><Label text="›" muted/><Label text={tr(nav.find(n=>n.id===page())?.zh??"概览",nav.find(n=>n.id===page())?.en??"Overview")} size={11}/></View><View style={{flex:1}}/><If when={!!busy()&&navProgress()===0}><Label text={tr("处理中…","Working…")} muted size={11}/></If><View testID="desktop-top-kernel-status" accessibilityElementsHidden={navProgress()<1} style={{opacity:navProgress(),flexDirection:"row",alignItems:"center",gap:6,flexShrink:0}}><View style={{width:5,height:5,borderRadius:3,backgroundColor:state().status.kernel?colors().success:colors().faint}}/><Text style={{fontSize:11,color:colors().muted,numberOfLines:1}}>{statusLabel()}</Text></View></View>
 <If when={!!error()}><View style={{paddingHorizontal:windowSize().width<960?16:20,paddingBottom:8,gap:6}}><Feedback text={error()} danger/><Row><Action label={tr("重试","Retry")} onPress={reload}/><Action label={tr("关闭提示","Dismiss")} onPress={()=>error.set("")}/></Row></View></If>
 <If when={page()==="overview"}><OverviewPage {...p}/></If><If when={page()==="subs"}><ProfilesPage {...p}/></If><If when={page()==="proxies"}><ProxiesPage {...p}/></If><If when={page()==="conns"}><ConnectionsPage {...p}/></If><If when={page()==="mesh"}><MeshPage {...p}/></If><If when={page()==="rules"}><RulesPage {...p}/></If><If when={page()==="logs"}><LogsPage {...p}/></If><If when={page()==="settings"}><SettingsPage {...p}/></If>
 </View>
 <View role="navigation" style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%"}}>

 <View style={{pointerEvents:"none",position:"absolute",left:lerp(20,titlebarInset()+8),top:titlebarHeight()+16+((titlebarHeight()-24)/2-(titlebarHeight()+16))*navProgress(),flexDirection:"row",alignItems:"center",gap:lerp(8,6)}}><Image src={logo} style={{width:lerp(32,24),height:lerp(32,24),objectFit:"contain"}}/><Label text="Gopurr" size={lerp(16,13)} bold/></View>
 <NativeTabs style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%",flexDirection:collapsed()?"row":"column"}} indicatorStyle={{...(collapsed()?{top:(titlebarHeight()-30)/2,height:30}:{left:10,width:163}),borderRadius:8,backgroundColor:colors().selected,opacity:moving()?0:1}}><Each of={nav}>{(n,index)=><Tab testID={`desktop-nav-${n().id}`} disabled={moving()} selected={page()===n().id} onPress={()=>goto(n().id)} style={{pointerEvents:moving()?"none":"auto",position:"absolute",left:lerp(10,topX(index())),top:lerp(titlebarHeight()+64+index()*38+(index()===7?12:0),(titlebarHeight()-30)/2),width:lerp(163,topWidth(index())),height:lerp(36,30),flexDirection:"row",alignItems:"center",gap:lerp(10,0),paddingHorizontal:lerp(12,layout().padding),borderRadius:8,backgroundColor:moving()&&page()===n().id?colors().selected:"transparent"}} hoverStyle={{backgroundColor:page()===n().id?"transparent":colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><View style={{width:lerp(17,0),opacity:Math.max(0,1-navProgress()*2),overflow:"hidden"}}><Icon name={n().id} color={page()===n().id?colors().accent:colors().muted}/></View><Text style={{fontSize:lerp(13,12.5),numberOfLines:1,fontWeight:page()===n().id?"600":"400",color:page()===n().id?colors().accent:colors().muted}}>{tr(n().zh,n().en)}</Text></Tab>}</Each></NativeTabs>
 <Button testID="desktop-toggle-sidebar" accessibilityLabel={tr("切换侧栏","Toggle sidebar")} onPress={toggle} style={{position:"absolute",left:lerp(Math.max(192,layout().leading)+4,layout().toggle),top:(titlebarHeight()-30)/2,width:30,height:30,alignItems:"center",justifyContent:"center",borderRadius:4}} hoverStyle={{backgroundColor:colors().hover}}><Icon name="panel" size={15}/></Button>
 <View style={{pointerEvents:"none",position:"absolute",left:16,bottom:16,gap:5,opacity:1-navProgress(),transform:[{translateX:lerp(0,-184)}]}}><Row gap={6}><View style={{width:5,height:5,borderRadius:3,backgroundColor:state().status.kernel?colors().success:colors().faint}}/><Label text={statusLabel()} size={11} muted/></Row><Label text={`${state().status.proxies??0} ${tr("代理","proxies")} · ${state().status.rules??0} ${tr("规则","rules")}`} size={10} muted/></View>
 </View>
 <UpdateDialog {...p}/>
 <Modal title={tr("关闭窗口后继续代理","Keep running after closing")} open={closeRequested()} onClose={()=>{if(!closeBusy())closeRequested.set(false);}} error={closeError()} width={500} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} disabled={closeBusy()} onPress={()=>closeRequested.set(false)}/><Action testID="desktop-close-background" primary label={tr("继续后台运行","Continue in background")} disabled={closeBusy()} onPress={()=>void continueInBackground()}/></Row>}><Label text={tr("关闭窗口后，Gopurr 会继续在菜单栏或托盘运行，代理保持当前状态。要停止并退出，请使用菜单中的“退出 Gopurr”。","Closing the window keeps Gopurr and your proxy running in the menu bar or tray. To stop and exit, choose Quit Gopurr from the menu.")}/></Modal>
 <Canvas accessibilityElementsHidden style={{position:"absolute",left:-2,top:-2,width:1,height:1,pointerEvents:"none"}} draw={(cx)=>{cx.font="600 12.5px system-ui";const labels=nav.map(n=>cx.measureText(tr(n.zh,n.en)).width);cx.font="600 13px system-ui";const brand=cx.measureText("Gopurr").width;cx.font="400 11px system-ui";const status=Math.max(...[tr("内核运行中","Kernel running"),tr("内核已停止","Kernel stopped"),tr("正在启动…","Starting…")].map(v=>cx.measureText(v).width));const next={labels,brand,status};if(brand>0&&status>0&&labels.every(v=>v>0)&&JSON.stringify(next)!==JSON.stringify(measurements()))measurements.set(next);}}/>
 </View>;
}
