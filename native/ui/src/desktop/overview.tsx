import { Text, View, windowSize, signal, effect, cleanup, reduceMotion } from "nsh";
import { colors } from "./theme";
import { TrafficChart, TrafficRate } from "./traffic-chart";
import { tr } from "./i18n";
import { Action, Heading, Icon, Label, Modal, Row, Section, Select, StatusDot, Tabs, Toggle, fmtBytes, fmtRate, groupTypeLabel } from "./ui";
import { FeatureEditor } from "./settings";
import type { PageProps } from "./types";

export function resolveExit(groups:any[],name:string):string {
  const seen=new Set<string>(); let current=name;
  for(let i=0;i<32;i++){if(seen.has(current))return tr("循环策略","Circular policy");seen.add(current);const g=groups.find(v=>v.name===current);if(!g)return current;if(!g.now||g.type.toLowerCase()==="loadbalance")return g.name;current=g.now;}return current;
}
 const details=signal(false); const service=signal(false); const feature=signal<"dns"|"sniffer"|null>(null);const featureSource=signal("config");
const exitCurrent=signal(""),exitPrevious=signal(""),exitProgress=signal(1);let lastExit="",seenExit=false;
function ExitName(p:{value:()=>string;size?:number}) {
 const current=exitCurrent,previous=exitPrevious,progress=exitProgress;let frameId=0;
 effect(()=>{const next=p.value();if(next===lastExit)return;lastExit=next;cancelAnimationFrame(frameId);if(!next||!seenExit||reduceMotion()){seenExit=!!next;current.set(next);previous.set("");progress.set(1);return;}previous.set(current());current.set(next);progress.set(0);const start=Date.now();const frame=()=>{if(reduceMotion()){progress.set(1);previous.set("");return;}const t=Math.min(1,(Date.now()-start)/280);progress.set(t*t*(3-2*t));if(t<1)frameId=requestAnimationFrame(frame);else previous.set("");};frameId=requestAnimationFrame(frame);});
 cleanup(()=>{cancelAnimationFrame(frameId);progress.set(1);previous.set("");});
 const size=p.size??26,lineHeight=size+6;
 return <View style={{minHeight:lineHeight}}><If when={!!previous()}><Text accessibilityElementsHidden style={{position:"absolute",left:0,right:0,top:0,fontSize:size,lineHeight,fontWeight:"600",letterSpacing:-0.7,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:1-progress(),transform:[{translateY:-4*progress()}]}}>{previous()}</Text></If><Text testID="overview-exit" style={{fontSize:size,lineHeight,fontWeight:"600",letterSpacing:-0.7,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:progress(),transform:[{translateY:4*(1-progress())}]}}>{current()||tr("尚未选择配置","No profile selected")}</Text></View>;
}
/**
 * Connection first: identity on the left of the eye's path, live throughput
 * under it, and the two facts that decide whether traffic is really taken
 * over on the header line. The chart owns the flexible height.
 */
export function OverviewPage(p:PageProps) {
 const status=()=>p.snapshot().status; const groups=()=>p.snapshot().groups??[];
 const group=()=>groups().find(g=>g.name!=="GLOBAL"&&!g.hidden)??groups()[0];
 const exit=()=>status().mode==="direct"?tr("直连","Direct"):resolveExit(groups(),status().mode==="global"?"GLOBAL":group()?.name??"");
 const health=()=>p.snapshot().health??{};
 const proxyHealthy=()=>!!status().systemProxy&&!health().error&&!health().proxyChanged&&!health().proxyError&&!health().runtime?.proxyError;
 const tunHealthy=()=>!!status().tun&&!health().error&&!health().runtime?.tunError;
 const connected=()=>proxyHealthy()||tunHealthy();
 const compact=()=>windowSize().height<660;
 const takeoverText=()=>status().busy?tr("正在处理…","Working…"):health().proxyChanged?tr("系统代理已被其他应用修改","System proxy changed by another app"):health().proxyError?tr("系统代理状态待确认","System proxy state is unknown"):status().kernel?connected()?tunHealthy()?tr("TUN 模式已开启","TUN enabled"):tr("系统代理已开启","System proxy enabled"):tr("内核运行中","Kernel running"):tr("内核未运行","Kernel stopped");
 const policyItems=()=>[{value:"inherit",title:tr("跟随配置","Follow profile")},{value:"true",title:tr("强制开启","Force on")},{value:"false",title:tr("强制关闭","Force off")}];
 const policy=(kind:string)=>p.snapshot().features?.[kind+"Policy"]==null?"inherit":String(p.snapshot().features[kind+"Policy"]);
 const stale=()=>!!status().traffic?.stale;
 const peak=()=>Math.max(0,...(p.snapshot().history??[]).map(h=>Math.max(h.up,h.down)));
 const rowStyle={flexDirection:"row",alignItems:"center",paddingHorizontal:14,minHeight:46,gap:12,backgroundColor:colors().surface} as const;
 const labelWidth=()=>windowSize().width<960?112:120;
 const Control=(c:{title:string;desc:string;kind:"dns"|"sniffer"})=> <View style={rowStyle}><View style={{width:labelWidth()}}><Label text={c.title} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label text={c.desc} tooltip={c.desc} muted size={11.5} lines={1}/></View><Action testID={`feature-settings-${c.kind}`} quiet label={tr("设置","Settings")} onPress={()=>feature.set(c.kind)}/><Select testID={`feature-policy-${c.kind}`} label={c.title} items={policyItems()} value={policy(c.kind)} disabled={!!p.busy()} onChange={v=>void p.run(c.kind,()=>p.call("desktop.features.policy",{kind:c.kind,enabled:v==="inherit"?null:v==="true"}))}/></View>;
 const Rate=(r:{kind:"down"|"up"})=><View style={{gap:3,minWidth:0}}>
   <Row gap={5}><Icon name={r.kind} size={11} color={colors()[r.kind]}/><Text style={{fontSize:11,fontWeight:"500",letterSpacing:0.1,color:colors().muted}}>{r.kind==="down"?tr("下行","Download"):tr("上行","Upload")}</Text></Row>
   <TrafficRate color={colors()[r.kind]} size={compact()?24:28} value={stale()?"—":fmtRate(status().traffic?.[r.kind]??0)}/>
  </View>;
 const errorText=()=>status().error||health().error||"";
 const groupCaption=()=>status().mode==="rule"?`${group()?.name??tr("节点选择","Proxy selection")} · ${groupTypeLabel(group()?.type??"—")}`:status().mode==="direct"?tr("所有请求直接连接","All requests connect directly"):tr("所有请求使用全局策略","All requests use the global policy");
 return <View testID="desktop-overview" style={{flex:1,paddingHorizontal:compact()?16:20,paddingTop:compact()?12:16,paddingBottom:compact()?12:16,gap:compact()?10:12}}>
 <Heading title={tr("概览","Overview")} description={errorText()||tr("连接状态与实时网络活动","Connection status and live network activity")} descriptionTone={errorText()?"danger":"muted"} action={()=><Tabs items={[{id:"rule",label:tr("规则","Rule")},{id:"global",label:tr("全局","Global")},{id:"direct",label:tr("直连","Direct")}]} value={status().mode??"rule"} onChange={mode=>void p.run("mode",()=>p.call("desktop.mode.set",{mode}))}/>}/>
 <View style={{flex:1,minHeight:176,borderRadius:10,backgroundColor:colors().surface,overflow:"hidden"}}>
  <View style={{flex:1,flexDirection:"row"}}>
   <View style={{width:2,backgroundColor:connected()?colors().success:colors().border}}/>
   <View style={{flex:1,minWidth:0,paddingHorizontal:compact()?14:18,paddingTop:compact()?12:15,paddingBottom:compact()?10:12,gap:compact()?6:8}}>
    <Row justify="space-between" gap={18} style={{alignItems:"flex-start"}}>
     <View style={{flexShrink:1,minWidth:0,gap:compact()?4:5}}>
      <Row gap={7} style={{minWidth:0}}><StatusDot on={connected()} size={6} color={connected()?colors().success:colors().faint}/><Text style={{fontSize:12,fontWeight:"500",color:connected()?colors().success:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{takeoverText()}</Text></Row>
      <ExitName value={exit} size={compact()?24:26}/>
      <Text tooltip={groupCaption()} style={{fontSize:11.5,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{groupCaption()}</Text>
     </View>
     <View style={{flexShrink:0,alignItems:"flex-end",gap:compact()?4:6}}>
      <Text style={{fontSize:10.5,color:colors().muted,fontVariant:"tabular-nums",numberOfLines:1}}>{`${tr("峰值","Peak")} ${fmtRate(peak())}`}</Text>
      <Row gap={compact()?22:36} style={{paddingTop:compact()?1:2}}><Rate kind="down"/><Rate kind="up"/></Row>
      <Row gap={5}><Icon name="shield" size={12} color={status().kernel?colors().success:colors().faint}/><Text style={{fontSize:11,color:status().kernel?colors().muted:colors().faint,numberOfLines:1}}>{status().kernel?tr("本地代理就绪","Local proxy ready"):tr("等待启动内核","Waiting for the core")}</Text></Row>
     </View>
    </Row>
    <View style={{flex:1,minHeight:compact()?44:56,paddingTop:2}}><TrafficChart compact={compact()} history={()=>p.snapshot().history??[]}/><If when={!p.snapshot().history?.length}><View style={{position:"absolute",left:0,right:0,top:0,bottom:14,alignItems:"center",justifyContent:"center"}}><Text style={{fontSize:11,color:colors().muted}}>{tr("等待真实流量采样","Waiting for traffic samples")}</Text></View></If></View>
   </View>
  </View>
  <View style={{flexDirection:"row",alignItems:"center",justifyContent:"space-between",paddingHorizontal:14,paddingVertical:8,borderTopWidth:1,borderColor:colors().border,gap:12}}>
   <Row gap={18} wrap style={{flexShrink:1}}><Stat label={tr("活跃连接","Connections")} value={String(status().connections??0)}/><Stat label={tr("生效规则","Rules")} value={String(status().rules??0)}/><Stat label={tr("累计下行","Downloaded")} value={fmtBytes(status().traffic?.downTotal??0)}/><Stat label={tr("累计上行","Uploaded")} value={fmtBytes(status().traffic?.upTotal??0)}/></Row>
   <Action testID="overview-route-action" primary bold label={status().kernel?tr("切换节点","Switch proxy"):tr("启动内核","Start kernel")} disabled={!!p.busy()} onPress={()=>status().kernel?p.navigate("proxies"):void p.run("kernel",()=>p.call("desktop.kernel.toggle"))}/>
  </View>
 </View>
 <Section title={tr("网络控制","Network controls")} subtitle={`${tr("混合端口","Mixed port")} ${p.snapshot().network.mixedPort??7890}`}/>
 <View style={{borderRadius:10,backgroundColor:colors().border,overflow:"hidden",gap:1}}>
  <View style={rowStyle}><View style={{width:labelWidth()}}><Label text={tr("系统代理","System proxy")} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label tooltip={status().systemProxy?proxyHealthy()?tr("已开启 · 本地代理就绪","Enabled · local proxy ready"):tr("已请求开启 · 网络状态待确认","Requested on · network state unconfirmed"):tr("未开启 · 不接管系统流量","Disabled · system traffic unchanged")} text={status().systemProxy?proxyHealthy()?tr("已开启 · 本地代理就绪","Enabled · local proxy ready"):tr("已请求开启 · 网络状态待确认","Requested on · network state unconfirmed"):tr("未开启 · 不接管系统流量","Disabled · system traffic unchanged")} muted size={11.5} lines={1}/></View><Action quiet label={tr("详情","Details")} onPress={()=>details.set(true)}/><Toggle showLabel={false} label={tr("系统代理","System proxy")} checked={!!status().systemProxy} disabled={!!p.busy()||!status().systemProxyAvailable} onChange={enabled=>void p.run("system-proxy",()=>p.call("desktop.systemProxy.set",{enabled}))}/></View>
  <View style={rowStyle}><View style={{width:labelWidth()}}><Label text={tr("TUN 模式","TUN mode")} weight={500} size={13}/></View><View style={{flex:1,minWidth:0}}><Label tooltip={status().tun?tunHealthy()?tr("已开启 · 接管设备流量","Enabled · routing device traffic"):tr("已请求开启 · TUN 状态待确认","Requested on · TUN state unconfirmed"):status().tunInstalled?tr("已安装服务 · 当前未开启","Service installed · disabled"):tr("未安装服务 · 系统代理可独立使用","Service not installed · system proxy works independently")} text={status().tun?tunHealthy()?tr("已开启 · 接管设备流量","Enabled · routing device traffic"):tr("已请求开启 · TUN 状态待确认","Requested on · TUN state unconfirmed"):status().tunInstalled?tr("已安装服务 · 当前未开启","Service installed · disabled"):tr("未安装服务 · 系统代理可独立使用","Service not installed · system proxy works independently")} muted size={11.5} lines={1}/></View><Action quiet label={status().tunInstalled?tr("服务…","Service…"):tr("安装服务…","Install service…")} onPress={()=>service.set(true)}/><Toggle showLabel={false} label="TUN" checked={!!status().tun} disabled={!!p.busy()||!status().tunAvailable} onChange={enabled=>void p.run("tun",()=>p.call("desktop.tun.set",{enabled}))}/></View>
  <Control title={tr("内置 DNS","Built-in DNS")} desc={`${p.snapshot().features?.dns?.enable?tr("已开启","Enabled"):tr("未开启","Disabled")} · ${tr("当前订阅","Current profile")}`} kind="dns"/>
  <Control title={tr("域名嗅探","Domain sniffing")} desc={`${p.snapshot().features?.sniffer?.enable?tr("已开启","Enabled"):tr("未开启","Disabled")} · ${tr("全局","Global")}`} kind="sniffer"/>
 </View>
 <Modal title={feature()==="dns"?tr("内置 DNS 设置","Built-in DNS settings"):tr("域名嗅探设置","Domain sniffing settings")} open={!!feature()} onClose={()=>feature.set(null)} width={660}><If when={!!feature()}><View style={{height:featureSource()==="custom"?Math.min(windowSize().height-210,420):180}}><FeatureEditor page={p} kind={feature()??"dns"} onSourceChange={v=>featureSource.set(v)}/></View></If></Modal>
 <Modal title={tr("系统代理","System proxy")} open={details()} onClose={()=>details.set(false)}><Label text={`${tr("混合端口","Mixed port")}: ${p.snapshot().network.mixedPort??7890}`}/><Label text={tr("系统代理由当前 Gopurr 实例管理。关闭后恢复此前的系统设置。","Gopurr manages the system proxy and restores previous settings when disabled.")} muted/><Action label={tr("打开网络设置","Open network settings")} onPress={()=>{details.set(false);p.navigate("settings");}}/></Modal>
 <Modal title={tr("TUN 系统服务","TUN system service")} open={service()} onClose={()=>service.set(false)} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} onPress={()=>service.set(false)}/><Action primary disabled={!!p.busy()} label={status().tunInstalled?tr("更新服务","Update service"):tr("安装服务","Install service")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.install");service.set(false);})}/></Row>}><Label text={tr("系统会请求管理员授权，以安装用于 TUN 模式的特权服务。","The system will request administrator authorization to install the privileged TUN service.")}/><If when={!!status().tunInstalled}><Action danger label={tr("卸载服务…","Uninstall service…")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.uninstall");service.set(false);})}/></If></Modal>
 </View>;
}
function Stat(p:{label:string;value:string}) {return <Row gap={5}><Text style={{fontSize:11,color:colors().muted,numberOfLines:1}}>{p.label}</Text><Text style={{fontSize:11.5,fontWeight:"600",color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{p.value}</Text></Row>;}
