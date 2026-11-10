import { Scroll, Text, View, windowSize, signal, effect, cleanup, reduceMotion } from "nsh";
import { colors } from "./theme";
import { TrafficChart, TrafficRate } from "./traffic-chart";
import { tr } from "./i18n";
import { Action, Feedback, Icon, Heading, Label, Modal, Row, Select, Tabs, Toggle, fmtBytes, fmtRate, groupTypeLabel, modeLabel } from "./ui";
import { FeatureEditor } from "./settings";
import type { PageProps } from "./types";

export function resolveExit(groups:any[],name:string):string {
 const seen=new Set<string>(); let current=name;
 for(let i=0;i<32;i++){if(seen.has(current))return tr("循环策略","Circular policy");seen.add(current);const g=groups.find(v=>v.name===current);if(!g)return current;if(!g.now||g.type.toLowerCase()==="loadbalance")return g.name;current=g.now;}return current;
}
 const details=signal(false); const service=signal(false); const feature=signal<"dns"|"sniffer"|null>(null);const featureSource=signal("config");
const exitCurrent=signal(""),exitPrevious=signal(""),exitProgress=signal(1);let lastExit="",seenExit=false;
function ExitName(p:{value:()=>string}) {
 const current=exitCurrent,previous=exitPrevious,progress=exitProgress;let frameId=0;
 effect(()=>{const next=p.value();if(next===lastExit)return;lastExit=next;cancelAnimationFrame(frameId);if(!next||!seenExit||reduceMotion()){seenExit=!!next;current.set(next);previous.set("");progress.set(1);return;}previous.set(current());current.set(next);progress.set(0);const start=Date.now();const frame=()=>{if(reduceMotion()){progress.set(1);previous.set("");return;}const t=Math.min(1,(Date.now()-start)/280);progress.set(t*t*(3-2*t));if(t<1)frameId=requestAnimationFrame(frame);else previous.set("");};frameId=requestAnimationFrame(frame);});
 cleanup(()=>{cancelAnimationFrame(frameId);progress.set(1);previous.set("");});
 return <View style={{minHeight:34}}><If when={!!previous()}><Text accessibilityElementsHidden style={{position:"absolute",left:0,right:0,top:0,fontSize:26,lineHeight:32,fontWeight:"600",letterSpacing:-0.4,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:1-progress(),transform:[{translateY:-4*progress()}]}}>{previous()}</Text></If><Text testID="overview-exit" style={{fontSize:26,lineHeight:32,fontWeight:"600",letterSpacing:-0.4,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:progress(),transform:[{translateY:4*(1-progress())}]}}>{current()||tr("尚未选择配置","No profile selected")}</Text></View>;
}
export function OverviewPage(p:PageProps) {
 const status=()=>p.snapshot().status; const groups=()=>p.snapshot().groups??[];
 const group=()=>groups().find(g=>g.name!=="GLOBAL"&&!g.hidden)??groups()[0];
 const exit=()=>status().mode==="direct"?tr("直连","Direct"):resolveExit(groups(),status().mode==="global"?"GLOBAL":group()?.name??"");
 const health=()=>p.snapshot().health??{};
 const proxyHealthy=()=>!!status().systemProxy&&!health().error&&!health().proxyChanged&&!health().proxyError&&!health().runtime?.proxyError;
 const tunHealthy=()=>!!status().tun&&!health().error&&!health().runtime?.tunError;
 const connected=()=>proxyHealthy()||tunHealthy();
 const compact=()=>windowSize().height<620;
 const takeoverText=()=>status().busy?tr("正在处理…","Working…"):health().proxyChanged?tr("系统代理已被其他应用修改","System proxy changed by another app"):health().proxyError?tr("系统代理状态待确认","System proxy state is unknown"):status().kernel?connected()?tunHealthy()?tr("TUN 模式已开启","TUN enabled"):tr("系统代理已开启","System proxy enabled"):tr("内核运行中","Kernel running"):tr("内核未运行","Kernel stopped");
 const policyItems=()=>[{value:"inherit",title:tr("跟随配置","Follow profile")},{value:"true",title:tr("强制开启","Force on")},{value:"false",title:tr("强制关闭","Force off")}];
 const policy=(kind:string)=>p.snapshot().features?.[kind+"Policy"]==null?"inherit":String(p.snapshot().features[kind+"Policy"]);
 const Control=(c:{title:string;desc:string;kind:"dns"|"sniffer"})=> <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:14,minHeight:44,gap:12,backgroundColor:colors().surface}}><View style={{width:windowSize().width<960?112:120}}><Label text={c.title} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label text={c.desc} muted size={11.5}/></View><Action testID={`feature-settings-${c.kind}`} quiet label={tr("设置","Settings")} onPress={()=>feature.set(c.kind)}/><Select testID={`feature-policy-${c.kind}`} label={c.title} items={policyItems()} value={policy(c.kind)} disabled={!!p.busy()} onChange={v=>void p.run(c.kind,()=>p.call("desktop.features.policy",{kind:c.kind,enabled:v==="inherit"?null:v==="true"}))}/></View>;
 const stale=()=>!!status().traffic?.stale;
 return <Scroll testID="desktop-overview" style={{flex:1}} contentStyle={{paddingHorizontal:20,paddingVertical:compact()?12:16,gap:compact()?10:12}}>
 <Heading title={tr("概览","Overview")} description={tr("连接状态与实时网络活动","Connection status and live network activity")} action={()=><Tabs items={[{id:"rule",label:tr("规则","Rule")},{id:"global",label:tr("全局","Global")},{id:"direct",label:tr("直连","Direct")}]} value={status().mode??"rule"} onChange={mode=>void p.run("mode",()=>p.call("desktop.mode.set",{mode}))}/>}/>
 <View style={{borderRadius:10,backgroundColor:colors().surface,overflow:"hidden"}}>
  <View style={{flexDirection:"row",minHeight:compact()?150:176}}>
   <View style={{width:windowSize().width<960?232:280,padding:16,paddingBottom:14,gap:6}}>
    <Row gap={6}><View style={{width:6,height:6,borderRadius:3,backgroundColor:connected()?colors().success:colors().faint}}/><Label text={takeoverText()} color={connected()?colors().success:colors().muted} size={12}/></Row>
    <View style={{paddingTop:4}}><ExitName value={exit}/></View>
    <Label text={status().mode==="rule"?`${group()?.name??tr("节点选择","Proxy selection")} · ${groupTypeLabel(group()?.type??"—")}`:status().mode==="direct"?tr("所有请求直接连接","All requests connect directly"):tr("所有请求使用全局策略","All requests use the global policy")} muted size={11.5} lines={2}/>
    <View style={{flex:1}}/>
    <Row gap={8}><Icon name="shield" size={13} color={status().kernel?colors().success:colors().faint}/><Label text={status().kernel?tr("本地代理就绪","Local proxy ready"):tr("等待启动内核","Waiting for the core")} muted size={11}/></Row>
   </View>
   <View style={{flex:1,minWidth:0,padding:16,paddingBottom:12,gap:6,borderLeftWidth:1,borderColor:colors().border}}>
    <Row justify="space-between"><Label text={tr("实时流量","Live traffic")} weight={600} size={12.5}/><Label text={`${tr("峰值","Peak")} ${fmtRate(Math.max(0,...(p.snapshot().history??[]).map(h=>Math.max(h.up,h.down))))}`} muted size={10.5}/></Row>
    <Row gap={28} style={{paddingTop:2}}><View style={{flex:1,gap:1}}><Row gap={4}><Icon name="down" size={12} color={colors().down}/><Label text={tr("下行","Download")} muted size={11}/></Row><TrafficRate color={colors().down} value={stale()?"—":fmtRate(status().traffic?.down??0)}/></View><View style={{flex:1,gap:1}}><Row gap={4}><Icon name="up" size={12} color={colors().up}/><Label text={tr("上行","Upload")} muted size={11}/></Row><TrafficRate color={colors().up} value={stale()?"—":fmtRate(status().traffic?.up??0)}/></View></Row>
    <View style={{flex:1,minHeight:compact()?32:54}}><TrafficChart history={()=>p.snapshot().history??[]}/><If when={!p.snapshot().history?.length}><View style={{position:"absolute",left:0,right:0,top:0,bottom:0,alignItems:"center",justifyContent:"center"}}><Label text={tr("等待真实流量采样","Waiting for traffic samples")} muted size={11}/></View></If></View>
    <Row justify="space-between"><Label text={tr(`${Math.max(0,(p.snapshot().history?.length??1)-1)} 秒前`,`${Math.max(0,(p.snapshot().history?.length??1)-1)} seconds ago`)} muted size={10}/><Label text={tr("现在","Now")} muted size={10}/></Row>
   </View>
  </View>
  <View style={{flexDirection:"row",alignItems:"center",justifyContent:"space-between",paddingHorizontal:16,paddingVertical:9,borderTopWidth:1,borderColor:colors().border,gap:12}}>
   <Row gap={16} wrap><Row gap={5}><Label text={tr("活跃连接","Connections")} muted size={11}/><Label text={String(status().connections??0)} size={11} weight={500}/></Row><Row gap={5}><Label text={tr("生效规则","Rules")} muted size={11}/><Label text={String(status().rules??0)} size={11} weight={500}/></Row><Row gap={5}><Label text={tr("累计下行","Downloaded")} muted size={11}/><Label text={fmtBytes(status().traffic?.downTotal??0)} size={11} weight={500}/></Row><Row gap={5}><Label text={tr("累计上行","Uploaded")} muted size={11}/><Label text={fmtBytes(status().traffic?.upTotal??0)} size={11} weight={500}/></Row></Row>
   <Action testID="overview-route-action" primary label={status().kernel?tr("切换节点","Switch proxy"):tr("启动内核","Start kernel")} disabled={!!p.busy()} onPress={()=>status().kernel?p.navigate("proxies"):void p.run("kernel",()=>p.call("desktop.kernel.toggle"))}/>
  </View>
 </View>
 <View style={{paddingTop:4}}><Row justify="space-between"><Label text={tr("网络控制","Network controls")} weight={600} size={12} muted/><Label text={stale()?tr("等待采样","Waiting for samples"):modeLabel(status().mode??"rule")} muted size={11}/></Row></View>
 <View style={{borderRadius:10,backgroundColor:colors().border,overflow:"hidden",gap:1}}>
  <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:14,minHeight:44,gap:12,backgroundColor:colors().surface}}><View style={{width:windowSize().width<960?112:120}}><Label text={tr("系统代理","System proxy")} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label text={status().systemProxy?proxyHealthy()?tr("已开启 · 本地代理就绪","Enabled · local proxy ready"):tr("已请求开启 · 网络状态待确认","Requested on · network state unconfirmed"):tr("未开启 · 不接管系统流量","Disabled · system traffic unchanged")} muted size={11.5}/></View><Action quiet label={tr("详情","Details")} onPress={()=>details.set(true)}/><Toggle showLabel={false} label={tr("系统代理","System proxy")} checked={!!status().systemProxy} disabled={!!p.busy()||!status().systemProxyAvailable} onChange={enabled=>void p.run("system-proxy",()=>p.call("desktop.systemProxy.set",{enabled}))}/></View>
  <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:14,minHeight:44,gap:12,backgroundColor:colors().surface}}><View style={{width:windowSize().width<960?112:120}}><Label text={tr("TUN 模式","TUN mode")} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label text={status().tun?tunHealthy()?tr("已开启 · 接管设备流量","Enabled · routing device traffic"):tr("已请求开启 · TUN 状态待确认","Requested on · TUN state unconfirmed"):status().tunInstalled?tr("已安装服务 · 当前未开启","Service installed · disabled"):tr("未安装服务 · 系统代理可独立使用","Service not installed · system proxy works independently")} muted size={11.5}/></View><Action quiet label={status().tunInstalled?tr("服务…","Service…"):tr("安装服务…","Install service…")} onPress={()=>service.set(true)}/><Toggle showLabel={false} label="TUN" checked={!!status().tun} disabled={!!p.busy()||!status().tunAvailable} onChange={enabled=>void p.run("tun",()=>p.call("desktop.tun.set",{enabled}))}/></View>
  <Control title={tr("内置 DNS","Built-in DNS")} desc={`${p.snapshot().features?.dns?.enable?tr("已开启","Enabled"):tr("未开启","Disabled")} · ${tr("当前订阅","Current profile")}`} kind="dns"/><Control title={tr("域名嗅探","Domain sniffing")} desc={`${p.snapshot().features?.sniffer?.enable?tr("已开启","Enabled"):tr("未开启","Disabled")} · ${tr("全局","Global")}`} kind="sniffer"/>
 </View>
 <If when={!!status().error||!!health().error}><Feedback danger text={status().error||health().error}/></If>
 <Modal title={feature()==="dns"?tr("内置 DNS 设置","Built-in DNS settings"):tr("域名嗅探设置","Domain sniffing settings")} open={!!feature()} onClose={()=>feature.set(null)} width={660}><If when={!!feature()}><View style={{height:featureSource()==="custom"?Math.min(windowSize().height-210,420):180}}><FeatureEditor page={p} kind={feature()??"dns"} onSourceChange={v=>featureSource.set(v)}/></View></If></Modal>
 <Modal title={tr("系统代理","System proxy")} open={details()} onClose={()=>details.set(false)}><Label text={`${tr("混合端口","Mixed port")}: ${p.snapshot().network.mixedPort??7890}`}/><Label text={tr("系统代理由当前 Gopurr 实例管理。关闭后恢复此前的系统设置。","Gopurr manages the system proxy and restores previous settings when disabled.")} muted/><Action label={tr("打开网络设置","Open network settings")} onPress={()=>{details.set(false);p.navigate("settings");}}/></Modal>
 <Modal title={tr("TUN 系统服务","TUN system service")} open={service()} onClose={()=>service.set(false)} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} onPress={()=>service.set(false)}/><Action primary disabled={!!p.busy()} label={status().tunInstalled?tr("更新服务","Update service"):tr("安装服务","Install service")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.install");service.set(false);})}/></Row>}><Label text={tr("系统会请求管理员授权，以安装用于 TUN 模式的特权服务。","The system will request administrator authorization to install the privileged TUN service.")}/><If when={!!status().tunInstalled}><Action danger label={tr("卸载服务…","Uninstall service…")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.uninstall");service.set(false);})}/></If></Modal>
 </Scroll>;
}
