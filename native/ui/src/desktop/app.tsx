import { batch, Canvas, cleanup, effect, host, Image, keymap, LayoutAnimation, lifecycle, Text, View, signal, windowSize, reduceMotion, colorScheme } from "nsh";
import { Button, Tabs as NativeTabs, Tab } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { busy, call, chromeHeight, chromeInset, closeRequested, error, listenDesktopEvents, navigate, nativeCall, page, refresh, run, state } from "./client";
import { Action, elevation, Feedback, Icon, Label, Modal, Row, StatusDot, tint } from "./ui";
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
// NSH does not rebuild a Button's content when only the appearance changes;
// remount the page stack per scheme so labels always use the current palette.
function DesktopPageStack(q:{page:Page;p:PageProps}) {const p=q.p;return <View style={{flex:1,minWidth:0}}><If when={q.page==="overview"}><OverviewPage {...p}/></If><If when={q.page==="subs"}><ProfilesPage {...p}/></If><If when={q.page==="proxies"}><ProxiesPage {...p}/></If><If when={q.page==="conns"}><ConnectionsPage {...p}/></If><If when={q.page==="mesh"}><MeshPage {...p}/></If><If when={q.page==="rules"}><RulesPage {...p}/></If><If when={q.page==="logs"}><LogsPage {...p}/></If><If when={q.page==="settings"}><SettingsPage {...p}/></If></View>;}
export function DesktopApp(preview:{motionProgress?:number}={}) {
 const collapsed=signal(false), initial=signal(false),navProgress=signal(preview.motionProgress??0);
 const closeBusy=signal(false),closeError=signal("");
 const continueInBackground=async()=>{if(closeBusy())return;closeBusy.set(true);closeError.set("");try{await call("desktop.prefs.save",{prefs:{closeToTrayAcknowledged:true}});await nativeCall("desktop.window.close");closeRequested.set(false);}catch(e){closeError.set(e instanceof Error?e.message:String(e));}finally{closeBusy.set(false);}};
 watchDesktopDraft("operations",()=>!!busy()||closeBusy());
 let postedDirty:boolean|undefined;
 effect(()=>{const dirty=hasUnsavedDesktopDraft();if(dirty===postedDirty)return;postedDirty=dirty;if(host.available())host.post("desktop.window.dirty",{dirty});});
 const moving=()=>navProgress()!==(collapsed()?1:0);
 const lerp=(from:number,to:number)=>from+(to-from)*navProgress();
 let sidebarSave=Promise.resolve();
 const toggle=()=>{const value=!collapsed();if(!reduceMotion())LayoutAnimation.configureNext({duration:320,update:{type:"easeInEaseOut",duration:320}});batch(()=>{collapsed.set(value);navProgress.set(value?1:0);});sidebarSave=sidebarSave.then(()=>call("desktop.prefs.save",{prefs:{sidebarCollapsed:value}})).catch(e=>error.set(e instanceof Error?e.message:String(e)));};
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
 <View style={{width:lerp(184,0),backgroundImage:"linear-gradient(180deg, "+colors().side+" 0%, "+colors().bg+" 100%)",borderRightWidth:navProgress()<1?1:0,borderColor:colors().border}}/>
 <View style={{flex:1,minWidth:0,pointerEvents:moving()?"none":"auto",backgroundImage:"radial-gradient(ellipse 980px 520px at 90% 0%, "+colors().accent+(colorScheme()==="dark"?"16":"12")+", transparent 64%)"}}><View style={{height:titlebarHeight(),paddingLeft:16,paddingRight:14,flexDirection:"row",alignItems:"center",gap:10}}><View style={{flex:1}}/><If when={!!busy()&&navProgress()===0}><Label text={tr("处理中…","Working…")} muted size={11}/></If><View testID="desktop-top-kernel-status" style={{flexDirection:"row",alignItems:"center",gap:8,flexShrink:0,height:28,paddingHorizontal:12,borderRadius:14,borderWidth:1,borderColor:state().status.kernel?tint(colors().success,"38"):colors().border,backgroundColor:state().status.kernel?tint(colors().success,"12"):colors().surface,boxShadow:state().status.kernel?[{x:0,y:1,blur:2,color:tint(colors().success,"1a")}]:[]}}><StatusDot on={!!state().status.kernel} size={6} glow={!!state().status.kernel}/><Text style={{fontSize:11.5,fontWeight:"600",letterSpacing:0.1,color:state().status.kernel?colors().success:colors().muted,numberOfLines:1}}>{statusLabel()}</Text></View></View>
 <If when={!!error()}><View style={{paddingHorizontal:windowSize().width<960?16:24,paddingBottom:10,gap:8}}><Feedback text={error()} danger/><Row><Action label={tr("重试","Retry")} onPress={reload}/><Action label={tr("关闭提示","Dismiss")} onPress={()=>error.set("")}/></Row></View></If>
 <If when={colorScheme()==="light"}><DesktopPageStack page={page()} p={p}/></If><If when={colorScheme()==="dark"}><DesktopPageStack page={page()} p={p}/></If>
 </View>
 <View role="navigation" style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%"}}>

 <View style={{pointerEvents:"none",position:"absolute",left:lerp(20,titlebarInset()+8),top:titlebarHeight()+15+((titlebarHeight()-24)/2-(titlebarHeight()+15))*navProgress(),flexDirection:"row",alignItems:"center",gap:lerp(10,6)}}><Image src={logo} style={{width:lerp(30,24),height:lerp(30,24),objectFit:"contain"}}/><Label text="Gopurr" size={lerp(15,13)} weight={600}/></View>
 <NativeTabs style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%",flexDirection:collapsed()?"row":"column"}} indicatorStyle={{...(collapsed()?{top:(titlebarHeight()-30)/2,height:30}:{left:8,width:167}),borderRadius:9,backgroundColor:colors().selected,borderWidth:1,borderColor:tint(colors().accent,"26"),boxShadow:collapsed()?elevation("popover"):[{x:0,y:1,blur:2,color:tint(colors().accent,"14")}],opacity:moving()?0:1,borderLeftWidth:collapsed()?1:2.5,borderLeftColor:collapsed()?tint(colors().accent,"26"):colors().accent}}><Each of={nav}>{(n,index)=><Tab testID={`desktop-nav-${n().id}`} disabled={moving()} selected={page()===n().id} onPress={()=>goto(n().id)} style={{pointerEvents:moving()?"none":"auto",position:"absolute",left:lerp(8,topX(index())),top:lerp(titlebarHeight()+62+index()*38+(index()===7?12:0),(titlebarHeight()-30)/2),width:lerp(167,topWidth(index())),height:lerp(36,30),flexDirection:"row",alignItems:"center",gap:lerp(10,0),paddingHorizontal:lerp(13,layout().padding),borderRadius:9,backgroundColor:moving()&&page()===n().id?colors().selected:"transparent",transitionProperty:"background-color, color",transitionDuration:140}} hoverStyle={{backgroundColor:page()===n().id?"transparent":colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><View style={{width:lerp(17,0),opacity:Math.max(0,1-navProgress()*2),overflow:"hidden"}}><Icon name={n().id} size={17} color={page()===n().id?colors().accent:colors().muted}/></View><Text style={{fontSize:lerp(13,12.5),numberOfLines:1,fontWeight:page()===n().id?"600":"500",letterSpacing:-0.1,color:page()===n().id?colors().accent:colors().muted}}>{tr(n().zh,n().en)}</Text></Tab>}</Each></NativeTabs>
 <Button testID="desktop-toggle-sidebar" accessibilityLabel={tr("切换侧栏","Toggle sidebar")} onPress={toggle} style={{position:"absolute",left:lerp(Math.max(192,layout().leading)+4,layout().toggle),top:(titlebarHeight()-30)/2,width:30,height:30,alignItems:"center",justifyContent:"center",borderRadius:6,transitionProperty:"background-color",transitionDuration:120}} hoverStyle={{backgroundColor:colors().hover}} activeStyle={{backgroundColor:colors().selected}} focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:1}}><Icon name="panel" size={15}/></Button>
 <View style={{pointerEvents:"none",position:"absolute",left:14,bottom:14,width:156,padding:12,gap:7,borderRadius:13,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface"),opacity:1-navProgress(),transform:[{translateX:lerp(0,-184)}]}}><Row gap={7}><StatusDot on={!!state().status.kernel} size={6} glow={!!state().status.kernel}/><Label text={statusLabel()} size={11.5} weight={600}/><If when={!!busy()}><Label text="·" size={11.5} muted/><Label text={tr("处理中","Working")} size={11.5} muted/></If></Row><Text style={{fontSize:10.5,color:colors().faint,numberOfLines:1,fontVariant:"tabular-nums"}}>{`${state().status.proxies??0} ${tr("代理","proxies")} · ${state().status.rules??0} ${tr("规则","rules")}`}</Text></View>
 </View>
 <UpdateDialog {...p}/>
 <Modal title={tr("关闭窗口后继续代理","Keep running after closing")} open={closeRequested()} onClose={()=>{if(!closeBusy())closeRequested.set(false);}} error={closeError()} width={500} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} disabled={closeBusy()} onPress={()=>closeRequested.set(false)}/><Action testID="desktop-close-background" primary label={tr("继续后台运行","Continue in background")} disabled={closeBusy()} onPress={()=>void continueInBackground()}/></Row>}><Label text={tr("关闭窗口后，Gopurr 会继续在菜单栏或托盘运行，代理保持当前状态。要停止并退出，请使用菜单中的“退出 Gopurr”。","Closing the window keeps Gopurr and your proxy running in the menu bar or tray. To stop and exit, choose Quit Gopurr from the menu.")}/></Modal>
 <Canvas accessibilityElementsHidden style={{position:"absolute",left:-2,top:-2,width:1,height:1,pointerEvents:"none"}} draw={(cx)=>{cx.font="600 12.5px system-ui";const labels=nav.map(n=>cx.measureText(tr(n.zh,n.en)).width);cx.font="600 13px system-ui";const brand=cx.measureText("Gopurr").width;cx.font="500 11.5px system-ui";const status=Math.max(...[tr("内核运行中","Kernel running"),tr("内核已停止","Kernel stopped"),tr("正在启动…","Starting…")].map(v=>cx.measureText(v).width))+36;const next={labels,brand,status};if(brand>0&&status>0&&labels.every(v=>v>0)&&JSON.stringify(next)!==JSON.stringify(measurements()))measurements.set(next);}}/>
 </View>;
}
