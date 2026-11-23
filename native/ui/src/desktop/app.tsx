import { batch, Canvas, cleanup, createAnimatedValue, effect, host, Image, keymap, lifecycle, locale, Text, View, signal, windowSize, reduceMotion, colorScheme } from "nsh";
import { Button, Tabs as NativeTabs, Tab } from "nsh/base";
import { colors } from "./theme";
import { language, tr } from "./i18n";
import { busy, call, chromeHeight, chromeInset, closeRequested, error, listenDesktopEvents, navigate, nativeCall, page, refresh, run, state } from "./client";
import { Action, Feedback, Icon, Label, Modal, Row, StatusDot, tint } from "./ui";
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
import { NavigationFlight, brandLayout } from "./nav-flight";
import logo from "../../assets/sidebar.png";
const nav:{id:Page;zh:string;en:string}[]=[{id:"overview",zh:"概览",en:"Overview"},{id:"subs",zh:"订阅",en:"Profiles"},{id:"proxies",zh:"代理",en:"Proxies"},{id:"conns",zh:"连接",en:"Connections"},{id:"mesh",zh:"组网",en:"Mesh"},{id:"rules",zh:"规则",en:"Rules"},{id:"logs",zh:"日志",en:"Logs"},{id:"settings",zh:"设置",en:"Settings"}];
// NSH does not rebuild a Button's content when only the appearance changes;
// remount the page stack per scheme so labels always use the current palette.
function DesktopPageStack(q:{page:Page;p:PageProps}) {const p=q.p;return <View style={{flex:1,minWidth:0}}><If when={q.page==="overview"}><OverviewPage {...p}/></If><If when={q.page==="subs"}><ProfilesPage {...p}/></If><If when={q.page==="proxies"}><ProxiesPage {...p}/></If><If when={q.page==="conns"}><ConnectionsPage {...p}/></If><If when={q.page==="mesh"}><MeshPage {...p}/></If><If when={q.page==="rules"}><RulesPage {...p}/></If><If when={q.page==="logs"}><LogsPage {...p}/></If><If when={q.page==="settings"}><SettingsPage {...p}/></If></View>;}
export function DesktopApp(preview:{motionProgress?:number;motionCollapsed?:boolean}={}) {
 // `settled` is the resting navigation. It changes only once the flight has
 // reached that pose, so the real labels are not shown while the overlay still
 // has them short of the end.
 const previewInitial=preview.motionProgress!==undefined||preview.motionCollapsed!==undefined;
 const collapsed=signal(preview.motionCollapsed??(preview.motionProgress!==undefined));
 const settled=signal(preview.motionCollapsed===true);
 const initial=signal(previewInitial);
 const navMoving=signal(preview.motionProgress!==undefined);
 const navProgress=createAnimatedValue(preview.motionProgress??(preview.motionCollapsed===true?1:0));
 const closeBusy=signal(false),closeError=signal("");
 const continueInBackground=async()=>{if(closeBusy())return;closeBusy.set(true);closeError.set("");try{await call("desktop.prefs.save",{prefs:{closeToTrayAcknowledged:true}});await nativeCall("desktop.window.close");closeRequested.set(false);}catch(e){closeError.set(e instanceof Error?e.message:String(e));}finally{closeBusy.set(false);}};
 watchDesktopDraft("operations",()=>!!busy()||closeBusy());
 let postedDirty:boolean|undefined;
 effect(()=>{const dirty=hasUnsavedDesktopDraft();if(dirty===postedDirty)return;postedDirty=dirty;if(host.available())host.post("desktop.window.dirty",{dirty});});
 const moving=()=>navMoving();
 let navRun=0;
 let sidebarSave=Promise.resolve();
 const toggle=()=>{const value=!collapsed();const target=value?1:0;const run=++navRun;if(reduceMotion()){batch(()=>{collapsed.set(value);settled.set(value);navProgress.set(target);navMoving.set(false);});}else{
  // AppKit does not reflow the descendants of a resized container during a
  // LayoutAnimation frame. Interpolating the content tree leaves cards and
  // group rows at their old width, exposing the Group separator as a gray
  // corner while the window is cropped. Commit the content width once and let
  // the input-free navigation flight plus GPU-composited content glide carry
  // the visible transition instead.
  batch(()=>{collapsed.set(value);navMoving.set(true);});
  void navProgress.timing(target,{duration:280,easing:[0.32,0.72,0,1]}).then(({finished})=>{if(finished&&run===navRun)batch(()=>{navProgress.set(target);settled.set(value);navMoving.set(false);});});
 }sidebarSave=sidebarSave.then(()=>call("desktop.prefs.save",{prefs:{sidebarCollapsed:value}})).catch(e=>error.set(e instanceof Error?e.message:String(e)));};
 const goto=(value:Page)=>{navigate(value);void call("desktop.page.set",{page:value}).catch(e=>error.set(String(e)));};
 const p:PageProps={snapshot:state,call,refresh,busy,run,navigate:goto};
 const reload=()=>void refresh().then(()=>{if(!initial()){collapsed.set(!!state().prefs.sidebarCollapsed);settled.set(collapsed());navProgress.set(collapsed()?1:0);initial.set(true);}}).catch(e=>error.set(e instanceof Error?e.message:String(e)));
 reload();
 listenDesktopEvents();
 const timer=setInterval(()=>{if(lifecycle()!=="background"&&!busy())reload();},1000);cleanup(()=>clearInterval(timer));
 nav.forEach((n,i)=>keymap.bind(`mod+${i+1}`,()=>goto(n.id)));
 keymap.bind("mod+shift+l",toggle);keymap.bind("mod+\\",toggle);keymap.bind("mod+r",reload);
 const titlebarInset=chromeInset;
 const titlebarHeight=chromeHeight;
 const measurements=signal<{labels:number[];brand:number;status:number}>({labels:[],brand:48,status:76});
 let measuredKey=`${language()}:${locale()}`;
 effect(()=>{const key=`${language()}:${locale()}`;if(key!==measuredKey){measuredKey=key;measurements.set({labels:[],brand:48,status:76});}});
 const statusLabel=()=>!state().status.kernel&&state().status.busy?tr("正在启动…","Starting…"):state().status.kernel?tr("内核运行中","Kernel running"):tr("内核已停止","Kernel stopped");
 const layout=()=>navigationLayout(windowSize().width,titlebarInset(),nav.map((n,i)=>measurements().labels[i]??tr(n.zh,n.en).length*(tr(n.zh,n.en)===n.zh?12.5:6.5)),measurements().brand,measurements().status);
 const topWidth=(index:number)=>layout().widths[index]??46;
 const topX=(index:number)=>layout().left+layout().widths.slice(0,index).reduce((sum,width)=>sum+width+2,0);
 const toggleAnchor=()=>Math.max(192,layout().leading)+4;
 const brand=()=>brandLayout(titlebarInset(),titlebarHeight(),settled());
 const selectedPillBorder=()=>colorScheme()==="dark"?tint(colors().accent,"2e"):tint(colors().accent,"26");
 const contentTransform=()=>!moving()?undefined:[{translateX:navProgress.interpolate({inputRange:[0,1],outputRange:collapsed()?[184,0]:[0,-184]})}];
 const contentOpacity=()=>!moving()?1:navProgress.interpolate({inputRange:[0,0.35,0.65,1],outputRange:collapsed()?[0.92,0.98,1,1]:[1,1,0.98,0.92],extrapolate:"clamp"});
 cleanup(()=>void navProgress.stop());
 return <View style={{flex:1,height:"100%",overflow:"hidden",backgroundColor:colors().bg,backgroundImage:"radial-gradient(ellipse 980px 520px at 90% 0%, "+colors().accent+(colorScheme()==="dark"?"0c":"09")+", transparent 64%)"}}>
 <View style={{pointerEvents:"none",position:"absolute",left:0,top:0,bottom:0,width:184,backgroundColor:colors().side,backgroundImage:"linear-gradient(180deg, "+colors().side+" 0%, "+colors().bg+" 100%)",borderRightWidth:1,borderColor:colors().border,opacity:!settled()&&!moving()?1:0}}/>
 <View style={{flex:1,minWidth:0,pointerEvents:moving()?"none":"auto"}}><View style={{height:titlebarHeight()}}/>
 <View testID="desktop-content" style={{flex:1,minWidth:0,marginLeft:collapsed()?0:184,opacity:contentOpacity(),transform:contentTransform()}}><If when={!!error()}><View style={{paddingHorizontal:windowSize().width<960?16:24,paddingBottom:10,gap:8}}><Feedback text={error()} danger/><Row><Action label={tr("重试","Retry")} onPress={reload}/><Action label={tr("关闭提示","Dismiss")} onPress={()=>error.set("")}/></Row></View></If><If when={colorScheme()==="light"}><DesktopPageStack page={page()} p={p}/></If><If when={colorScheme()==="dark"}><DesktopPageStack page={page()} p={p}/></If></View>
 </View>
 <View style={{pointerEvents:"none",position:"absolute",right:14,top:(titlebarHeight()-26)/2,height:26,flexDirection:"row",alignItems:"center",gap:10}}>
 <If when={!!busy()&&!settled()}><Label text={tr("处理中…","Working…")} muted size={11}/></If>
 <View testID="desktop-top-kernel-status" style={{flexDirection:"row",alignItems:"center",gap:7,flexShrink:0,height:26,paddingHorizontal:10,borderRadius:999,borderWidth:1,borderColor:colorScheme()==="dark"?"#ffffff14":colors().border,backgroundColor:colorScheme()==="dark"?"#ffffff09":"#ffffffcc"}}><StatusDot on={!!state().status.kernel} size={6} glow={!!state().status.kernel}/><Text style={{fontSize:11.5,fontWeight:"500",letterSpacing:0.1,color:state().status.kernel?colors().text:colors().muted,numberOfLines:1}}>{statusLabel()}</Text></View>
 </View>
 <View role="navigation" style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%"}}>

 <View testID="desktop-brand" style={{pointerEvents:"none",position:"absolute",left:brand().left,top:brand().top,opacity:moving()?0:1,flexDirection:"row",alignItems:"center",gap:brand().gap}}><Image src={logo} style={{width:brand().icon,height:brand().icon,objectFit:"contain"}}/><Label text="Gopurr" size={brand().text} weight={600}/></View>
 <If when={moving()}><NavigationFlight progress={navProgress} page={page()} titlebarInset={titlebarInset()} titlebarHeight={titlebarHeight()} layout={layout()} status={statusLabel()} proxies={state().status.proxies??0} rules={state().status.rules??0} kernel={!!state().status.kernel}/></If>
 <NativeTabs style={{pointerEvents:"box-none",position:"absolute",left:0,top:0,width:"100%",height:"100%",flexDirection:settled()?"row":"column",opacity:moving()?0:1}} indicatorStyle={{...(settled()?{top:(titlebarHeight()-30)/2,height:30,borderRadius:8,backgroundColor:colors().selected,borderWidth:1,borderColor:selectedPillBorder()}:{left:8,width:167,borderRadius:8,backgroundColor:colors().selected,borderWidth:1,borderColor:selectedPillBorder()})}}><Each of={nav}>{(n,index)=><Tab testID={`desktop-nav-${n().id}`} disabled={moving()} selected={page()===n().id} onPress={()=>goto(n().id)} style={{cursor:moving()?"default":"pointer",pointerEvents:moving()?"none":"auto",position:"absolute",left:settled()?topX(index()):8,top:settled()?(titlebarHeight()-30)/2:titlebarHeight()+62+index()*38+(index()===7?12:0),width:settled()?topWidth(index()):167,height:settled()?30:36,flexDirection:"row",alignItems:"center",justifyContent:settled()?"center":"flex-start",gap:settled()?0:10,paddingHorizontal:settled()?layout().padding:13,borderRadius:8,backgroundColor:"transparent",transitionProperty:"color",transitionDuration:140}} hoverStyle={{backgroundColor:page()===n().id?"transparent":colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><If when={!settled()}><View style={{width:17,overflow:"hidden"}}><Icon name={n().id} size={17} color={page()===n().id?colors().accent:colors().muted}/></View></If><Text style={{fontSize:settled()?12.5:13,numberOfLines:1,fontWeight:page()===n().id?"600":"500",letterSpacing:-0.1,color:page()===n().id?colors().accent:colors().muted}}>{tr(n().zh,n().en)}</Text></Tab>}</Each></NativeTabs>
 <Button testID="desktop-toggle-sidebar" accessibilityLabel={tr("切换侧栏","Toggle sidebar")} onPress={toggle} style={{cursor:"pointer",position:"absolute",left:toggleAnchor(),top:(titlebarHeight()-30)/2,width:30,height:30,alignItems:"center",justifyContent:"center",borderRadius:6,transform:[{translateX:navProgress.interpolate({inputRange:[0,1],outputRange:[0,layout().toggle-toggleAnchor()]})}],transitionProperty:"background-color",transitionDuration:120}} hoverStyle={{backgroundColor:colors().hover}} activeStyle={{backgroundColor:colors().selected}} focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:1}}><Icon name="panel" size={15}/></Button>
 <If when={!settled()&&!moving()}><View testID="desktop-sidebar-summary" style={{pointerEvents:"none",position:"absolute",left:14,width:156,bottom:14,paddingTop:12,gap:5,borderTopWidth:1,borderColor:colors().border,backgroundColor:"transparent"}}><Row gap={6}><StatusDot on={!!state().status.kernel} size={5.5}/><Label text={statusLabel()} muted size={11} weight={500}/><If when={!!busy()}><Label text="·" size={11} faint/><Label text={tr("处理中","Working")} size={11} faint/></If></Row><Text style={{fontSize:10.5,color:colors().faint,numberOfLines:1,fontVariant:"tabular-nums"}}>{`${state().status.proxies??0} ${tr("代理","proxies")} · ${state().status.rules??0} ${tr("规则","rules")}`}</Text></View></If>
 </View>
 <UpdateDialog {...p}/>
 <Modal title={tr("关闭窗口后继续代理","Keep running after closing")} open={closeRequested()} onClose={()=>{if(!closeBusy())closeRequested.set(false);}} error={closeError()} width={500} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} disabled={closeBusy()} onPress={()=>closeRequested.set(false)}/><Action testID="desktop-close-background" primary label={tr("继续后台运行","Continue in background")} disabled={closeBusy()} onPress={()=>void continueInBackground()}/></Row>}><Label text={tr("关闭窗口后，Gopurr 会继续在菜单栏或托盘运行，代理保持当前状态。要停止并退出，请使用菜单中的“退出 Gopurr”。","Closing the window keeps Gopurr and your proxy running in the menu bar or tray. To stop and exit, choose Quit Gopurr from the menu.")}/></Modal>
 <Each of={[`${language()}:${locale()}`]} key={(v)=>v}>{()=><Canvas accessibilityElementsHidden style={{position:"absolute",left:-2,top:-2,width:1,height:1,pointerEvents:"none"}} draw={(cx)=>{cx.font="600 12.5px system-ui";const labels=nav.map(n=>Math.ceil(cx.measureText(tr(n.zh,n.en)).width)+1);cx.font="600 13px system-ui";const brand=Math.ceil(cx.measureText("Gopurr").width)+1;cx.font="500 11.5px system-ui";const status=Math.ceil(Math.max(...[tr("内核运行中","Kernel running"),tr("内核已停止","Kernel stopped"),tr("正在启动…","Starting…")].map(v=>cx.measureText(v).width)))+36;const next={labels,brand,status};if(brand>0&&status>0&&labels.every(v=>v>0)&&JSON.stringify(next)!==JSON.stringify(measurements()))measurements.set(next);}}/>}</Each>
 </View>;
}
