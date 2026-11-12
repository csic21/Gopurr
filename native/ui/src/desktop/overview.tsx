import { Text, View, windowSize, signal, effect, cleanup, reduceMotion, colorScheme, createAnimatedValue } from "nsh";
import { colors } from "./theme";
import { TrafficChart, TrafficRate } from "./traffic-chart";
import { tr } from "./i18n";
import { Action, elevation, Heading, Icon, Label, Modal, RADIUS, Row, Section, Select, StatusDot, Tabs, Toggle, fmtBytes, fmtRate, groupTypeLabel, tint } from "./ui";
import { FeatureEditor } from "./settings";
import type { PageProps } from "./types";

export function resolveExit(groups:any[],name:string):string {
  const seen=new Set<string>(); let current=name;
  for(let i=0;i<32;i++){if(seen.has(current))return tr("循环策略","Circular policy");seen.add(current);const g=groups.find(v=>v.name===current);if(!g)return current;if(!g.now||g.type.toLowerCase()==="loadbalance")return g.name;current=g.now;}return current;
}
 const details=signal(false); const service=signal(false); const feature=signal<"dns"|"sniffer"|null>(null);const featureSource=signal("config");
const exitCurrent=signal(""),exitPrevious=signal(""),exitProgress=createAnimatedValue(1);let lastExit="",seenExit=false;
function ExitName(p:{value:()=>string;size?:number}) {
 const current=exitCurrent,previous=exitPrevious,progress=exitProgress;
 effect(()=>{const next=p.value();if(next===lastExit)return;lastExit=next;void progress.stop();if(!next||!seenExit||reduceMotion()){progress.set(1);seenExit=!!next;current.set(next);previous.set("");return;}previous.set(current());current.set(next);progress.set(0);void progress.timing(1,{duration:280,easing:"easeInOut"}).then(({finished})=>{if(finished)previous.set("");});});
 cleanup(()=>{void progress.stop();progress.set(1);previous.set("");});
 const size=p.size??32,lineHeight=size+8;
 return <View style={{minHeight:lineHeight}}><If when={!!previous()}><Text accessibilityElementsHidden style={{position:"absolute",left:0,right:0,top:0,fontSize:size,lineHeight,fontWeight:"600",letterSpacing:-1,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:progress.interpolate({inputRange:[0,1],outputRange:[1,0]}),transform:[{translateY:progress.interpolate({inputRange:[0,1],outputRange:[-5,0]})}]}}>{previous()}</Text></If><Text testID="overview-exit" style={{fontSize:size,lineHeight,fontWeight:"600",letterSpacing:-1,color:colors().text,numberOfLines:2,textOverflow:"ellipsis",opacity:progress,transform:[{translateY:progress.interpolate({inputRange:[0,1],outputRange:[5,0]})}]}}>{current()||tr("尚未选择配置","No profile selected")}</Text></View>;
}
/**
 * Connection first, then the live instrument: a wide console card carries the
 * takeover state, the final exit and the traffic surface while a narrow column
 * keeps the live rates and the session counters beside it. Short windows fall
 * back to a single stacked card so every control stays on the first screen.
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
 // The two-column console needs a desktop-sized canvas; below it the pages
 // stack so the minimum window keeps the same information.
 const bento=()=>windowSize().width>=1024&&windowSize().height>=700;
 const takeoverText=()=>status().busy?tr("正在处理…","Working…"):health().proxyChanged?tr("系统代理已被其他应用修改","System proxy changed by another app"):health().proxyError?tr("系统代理状态待确认","System proxy state is unknown"):status().kernel?connected()?tunHealthy()?tr("TUN 模式已开启","TUN enabled"):tr("系统代理已开启","System proxy enabled"):tr("内核运行中","Kernel running"):tr("内核未运行","Kernel stopped");
 const policyItems=()=>[{value:"inherit",title:tr("跟随配置","Follow profile")},{value:"true",title:tr("强制开启","Force on")},{value:"false",title:tr("强制关闭","Force off")}];
 const policy=(kind:string)=>p.snapshot().features?.[kind+"Policy"]==null?"inherit":String(p.snapshot().features[kind+"Policy"]);
 const stale=()=>!!status().traffic?.stale;
 const peak=()=>Math.max(0,...(p.snapshot().history??[]).map(h=>Math.max(h.up,h.down)));
 const labelWidth=()=>windowSize().width<960?112:124;
 const rowHeight=()=>compact()?44:50;
 const dark=()=>colorScheme()==="dark";
 const card=()=>({borderRadius:RADIUS.surface,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface"),overflow:"hidden"} as const);
 const railColor=()=>connected()?colors().success:colors().border;
 const rail=()=><View style={{width:3,backgroundImage:"linear-gradient(180deg, "+railColor()+" 0%, "+tint(railColor(),"00")+" 100%)"}}/>;
 const Control=(c:{title:string;desc:string;kind:"dns"|"sniffer";icon:string})=> <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:16,minHeight:rowHeight(),gap:14,backgroundColor:colors().surface}}>
   <View style={{width:28,height:28,borderRadius:9,alignItems:"center",justifyContent:"center",backgroundColor:colors().hover,borderWidth:1,borderColor:colors().border}}><Icon name={c.icon} size={14} color={colors().muted}/></View>
   <View style={{width:labelWidth()}}><Label text={c.title} weight={500} size={13}/></View>
   <View style={{flex:1,minWidth:0}}><Label text={c.desc} tooltip={c.desc} faint size={12} lines={1}/></View>
   <Action testID={`feature-settings-${c.kind}`} quiet label={tr("设置","Settings")} onPress={()=>feature.set(c.kind)}/>
   <Select testID={`feature-policy-${c.kind}`} label={c.title} items={policyItems()} value={policy(c.kind)} disabled={!!p.busy()} onChange={v=>void p.run(c.kind,()=>p.call("desktop.features.policy",{kind:c.kind,enabled:v==="inherit"?null:v==="true"}))}/>
 </View>;
 const Rate=(r:{kind:"down"|"up";size?:number})=><View style={{gap:4,minWidth:0}}>
   <Row gap={5}><Icon name={r.kind} size={12} color={colors()[r.kind]}/><Text style={{fontSize:11,fontWeight:"500",letterSpacing:0.3,color:colors().muted}}>{r.kind==="down"?tr("下行","Download"):tr("上行","Upload")}</Text></Row>
   <TrafficRate color={colors()[r.kind]} size={r.size??(compact()?23:30)} value={stale()?"—":fmtRate(status().traffic?.[r.kind]??0)}/>
  </View>;
 const Metric=(m:{label:string;value:string;size?:number})=><View style={{gap:3,minWidth:0}}>
   <Text style={{fontSize:10,fontWeight:"500",letterSpacing:0.4,color:colors().faint,numberOfLines:1}}>{m.label}</Text>
   <Text style={{fontSize:m.size??13,fontWeight:"600",letterSpacing:-0.1,color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{m.value}</Text>
  </View>;
 const Divider=()=><View style={{width:1,height:24,backgroundColor:colors().border}}/>;
 const Hairline=()=><View style={{height:1,backgroundColor:colors().border}}/>;
 const errorText=()=>status().error||health().error||"";
 const groupCaption=()=>status().mode==="rule"?(group()?.name??tr("节点选择","Proxy selection"))+" · "+groupTypeLabel(group()?.type??"—"):status().mode==="direct"?tr("所有请求直接连接","All requests connect directly"):tr("所有请求使用全局策略","All requests use the global policy");
 const route=()=>status().kernel?p.navigate("proxies"):void p.run("kernel",()=>p.call("desktop.kernel.toggle"));
 const Identity=(o:{size:number;compact:boolean})=><View style={{flexShrink:1,minWidth:0,gap:o.compact?5:7}}>
   <Row gap={8} style={{minWidth:0}}>
    <StatusDot on={connected()} size={7} color={connected()?colors().success:colors().faint} glow={connected()}/>
    <Text style={{fontSize:12,fontWeight:"600",letterSpacing:0.1,color:connected()?colors().success:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{takeoverText()}</Text>
    <If when={o.compact}><Row gap={6} style={{minWidth:0,flexShrink:1}}><Text style={{fontSize:11.5,color:colors().faint,numberOfLines:1}}>·</Text><Text tooltip={groupCaption()} style={{fontSize:11.5,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis",flexShrink:1,minWidth:0}}>{groupCaption()}</Text></Row></If>
   </Row>
   <ExitName value={exit} size={o.size}/>
   <If when={!o.compact}><Text tooltip={groupCaption()} style={{fontSize:12,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{groupCaption()}</Text></If>
  </View>;
 const Health=(o:{compact:boolean})=><Row gap={6}><Icon name="shield" size={12} color={status().kernel?colors().success:colors().faint}/><Text style={{fontSize:11,color:status().kernel?colors().muted:colors().faint,numberOfLines:1}}>{status().kernel?tr("本地代理就绪","Local proxy ready"):tr("等待启动内核","Waiting for the core")}</Text></Row>;
 const Chart=()=><View style={{flex:1,minHeight:compact()?50:60,paddingTop:2}}><TrafficChart compact={compact()} history={()=>p.snapshot().history??[]}/><If when={!p.snapshot().history?.length}><View style={{position:"absolute",left:0,right:0,top:0,bottom:16,alignItems:"center",justifyContent:"center"}}><Text style={{fontSize:11.5,color:colors().faint}}>{tr("等待真实流量采样","Waiting for traffic samples")}</Text></View></If></View>;
 const RouteAction=()=><Action testID="overview-route-action" primary bold icon={status().kernel?"swap":"power"} label={status().kernel?tr("切换节点","Switch proxy"):tr("启动内核","Start kernel")} disabled={!!p.busy()} onPress={route}/>;
 /**
  * The desktop console: identity and traffic on the left, live rates and
  * session counters in the narrow column, all above the network controls.
  */
 const ConsoleLayout=()=><View style={{flex:1,minHeight:0,flexDirection:"row",gap:16}}>
  <View style={{flex:1,minWidth:0,...card(),backgroundImage:"linear-gradient(180deg, "+tint(colors().surface,"00")+" 60%, "+tint(colors().accent,dark()?"0f":"0b")+" 100%)"}}>
   <View style={{flex:1,flexDirection:"row"}}>
    {rail()}
    <View style={{flex:1,minWidth:0,paddingHorizontal:24,paddingTop:22,paddingBottom:14,gap:12}}>
     <Row justify="space-between" gap={24} style={{alignItems:"flex-start"}}>
      <Identity size={36} compact={false}/>
      <View style={{flexShrink:0,alignItems:"flex-end",gap:12}}>
       <RouteAction/>
       <Health compact={false}/>
      </View>
     </Row>
     <Chart/>
    </View>
   </View>
  </View>
  <View style={{width:272,gap:16}}>
   <View style={{flex:1,minHeight:0,...card(),padding:16,gap:10,justifyContent:"space-between"}}>
    <Text style={{fontSize:11,fontWeight:"600",letterSpacing:0.3,color:colors().muted,numberOfLines:1}}>{tr("实时速率","Live throughput")}</Text>
    <View style={{gap:8}}>
     <Row justify="space-between" gap={10} style={{minWidth:0}}>
      <Row gap={5}><Icon name="down" size={12} color={colors().down}/><Text style={{fontSize:11,fontWeight:"500",letterSpacing:0.3,color:colors().muted}}>{tr("下行","Download")}</Text></Row>
      <TrafficRate color={colors().down} size={22} value={stale()?"—":fmtRate(status().traffic?.down??0)}/>
     </Row>
     <Hairline/>
     <Row justify="space-between" gap={10} style={{minWidth:0}}>
      <Row gap={5}><Icon name="up" size={12} color={colors().up}/><Text style={{fontSize:11,fontWeight:"500",letterSpacing:0.3,color:colors().muted}}>{tr("上行","Upload")}</Text></Row>
      <TrafficRate color={colors().up} size={22} value={stale()?"—":fmtRate(status().traffic?.up??0)}/>
     </Row>
    </View>
    <Row gap={6}><Icon name="activity" size={11} color={colors().faint}/><Text style={{fontSize:11,color:colors().faint,fontVariant:"tabular-nums",numberOfLines:1}}>{tr("峰值","Peak")+" "+fmtRate(peak())}</Text></Row>
   </View>
   <View style={{flex:1,minHeight:0,...card(),padding:16,gap:10,justifyContent:"space-between"}}>
    <Text style={{fontSize:11,fontWeight:"600",letterSpacing:0.3,color:colors().muted,numberOfLines:1}}>{tr("会话统计","Session")}</Text>
    <View style={{gap:10}}>
     <Row justify="space-between" gap={12}><Metric label={tr("活跃连接","Connections")} value={String(status().connections??0)} size={16}/><Metric label={tr("生效规则","Rules")} value={String(status().rules??0)} size={16}/></Row>
     <Hairline/>
     <Row justify="space-between" gap={12}><Metric label={tr("累计下行","Downloaded")} value={fmtBytes(status().traffic?.downTotal??0)} size={16}/><Metric label={tr("累计上行","Uploaded")} value={fmtBytes(status().traffic?.upTotal??0)} size={16}/></Row>
    </View>
   </View>
  </View>
 </View>;
 /**
  * The stacked console for the minimum window: one card holds the identity,
  * the rates, the traffic surface and the counters.
  */
 const StackedLayout=()=><View style={{flex:1,minHeight:compact()?206:236,...card(),backgroundImage:"linear-gradient(180deg, "+tint(colors().surface,"00")+" 55%, "+tint(colors().accent,dark()?"0d":"0a")+" 100%)"}}>
  <View style={{flex:1,flexDirection:"row"}}>
   {rail()}
   <View style={{flex:1,minWidth:0,paddingHorizontal:compact()?16:22,paddingTop:compact()?12:18,paddingBottom:compact()?10:12,gap:compact()?8:10}}>
    <Row justify="space-between" gap={20} style={{alignItems:"flex-start"}}>
     <Identity size={compact()?24:34} compact={compact()}/>
     <View style={{flexShrink:0,alignItems:"flex-end",gap:compact()?3:8}}>
      <Row gap={6}>
       <Icon name="activity" size={compact()?11:12} color={colors().faint}/>
       <Text style={{fontSize:compact()?10.5:11,color:colors().faint,fontVariant:"tabular-nums",numberOfLines:1}}>{tr("峰值","Peak")+" "+fmtRate(peak())}</Text>
      </Row>
      <Row gap={compact()?22:40} style={{paddingTop:compact()?0:3}}><Rate kind="down"/><Rate kind="up"/></Row>
      <If when={!compact()}><Health compact={false}/></If>
     </View>
    </Row>
    <Chart/>
   </View>
  </View>
  <View style={{flexDirection:"row",alignItems:"center",justifyContent:"space-between",paddingLeft:compact()?16:22,paddingRight:compact()?12:14,minHeight:compact()?44:52,borderTopWidth:1,borderColor:colors().border,gap:12,backgroundColor:dark()?tint(colors().surface,"00"):tint(colors().bg,"66")}}>
   <Row gap={compact()?12:18} style={{flexShrink:1,minWidth:0}}>
    <Metric label={tr("活跃连接","Connections")} value={String(status().connections??0)}/>
    <Divider/>
    <Metric label={tr("生效规则","Rules")} value={String(status().rules??0)}/>
    <Divider/>
    <Metric label={tr("累计下行","Downloaded")} value={fmtBytes(status().traffic?.downTotal??0)}/>
    <Divider/>
    <Metric label={tr("累计上行","Uploaded")} value={fmtBytes(status().traffic?.upTotal??0)}/>
   </Row>
   <RouteAction/>
  </View>
 </View>;
 return <View testID="desktop-overview" style={{flex:1,paddingHorizontal:compact()?16:24,paddingTop:compact()?10:20,paddingBottom:compact()?10:20,gap:compact()?8:16}}>
 <Heading title={tr("概览","Overview")} description={errorText()||tr("连接状态与实时网络活动","Connection status and live network activity")} descriptionTone={errorText()?"danger":"muted"} action={()=><Tabs items={[{id:"rule",label:tr("规则","Rule")},{id:"global",label:tr("全局","Global")},{id:"direct",label:tr("直连","Direct")}]} value={status().mode??"rule"} onChange={mode=>void p.run("mode",()=>p.call("desktop.mode.set",{mode}))}/>}/>
 <If when={bento()}>{ConsoleLayout()}</If>
 <If when={!bento()}>{StackedLayout()}</If>
 <Section title={tr("网络控制","Network controls")} subtitle={tr("混合端口","Mixed port")+" "+String(p.snapshot().network.mixedPort??7890)}/>
 <View style={{borderRadius:RADIUS.surface,backgroundColor:colors().border,overflow:"hidden",gap:1,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface")}}>
  <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:16,minHeight:rowHeight(),gap:14,backgroundColor:colors().surface}}>
   <View style={{width:28,height:28,borderRadius:9,alignItems:"center",justifyContent:"center",backgroundColor:colors().hover,borderWidth:1,borderColor:colors().border}}><Icon name="shield" size={14} color={colors().muted}/></View>
   <View style={{width:labelWidth()}}><Label text={tr("系统代理","System proxy")} weight={500} size={13}/></View>
   <View style={{flex:1,minWidth:0}}><Label tooltip={status().systemProxy?proxyHealthy()?tr("已开启 · 本地代理就绪","Enabled · local proxy ready"):tr("已请求开启 · 网络状态待确认","Requested on · network state unconfirmed"):tr("未开启 · 不接管系统流量","Disabled · system traffic unchanged")} text={status().systemProxy?proxyHealthy()?tr("已开启 · 本地代理就绪","Enabled · local proxy ready"):tr("已请求开启 · 网络状态待确认","Requested on · network state unconfirmed"):tr("未开启 · 不接管系统流量","Disabled · system traffic unchanged")} faint size={12} lines={1}/></View>
   <Action quiet label={tr("详情","Details")} onPress={()=>details.set(true)}/><Toggle showLabel={false} label={tr("系统代理","System proxy")} checked={!!status().systemProxy} disabled={!!p.busy()||!status().systemProxyAvailable} onChange={enabled=>void p.run("system-proxy",()=>p.call("desktop.systemProxy.set",{enabled}))}/>
  </View>
  <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:16,minHeight:rowHeight(),gap:14,backgroundColor:colors().surface}}>
   <View style={{width:28,height:28,borderRadius:9,alignItems:"center",justifyContent:"center",backgroundColor:colors().hover,borderWidth:1,borderColor:colors().border}}><Icon name="route" size={14} color={colors().muted}/></View>
   <View style={{width:labelWidth()}}><Label text={tr("TUN 模式","TUN mode")} weight={500} size={13}/></View>
   <View style={{flex:1,minWidth:0}}><Label tooltip={status().tun?tunHealthy()?tr("已开启 · 接管设备流量","Enabled · routing device traffic"):tr("已请求开启 · TUN 状态待确认","Requested on · TUN state unconfirmed"):status().tunInstalled?tr("已安装服务 · 当前未开启","Service installed · disabled"):tr("未安装服务 · 系统代理可独立使用","Service not installed · system proxy works independently")} text={status().tun?tunHealthy()?tr("已开启 · 接管设备流量","Enabled · routing device traffic"):tr("已请求开启 · TUN 状态待确认","Requested on · TUN state unconfirmed"):status().tunInstalled?tr("已安装服务 · 当前未开启","Service installed · disabled"):tr("未安装服务 · 系统代理可独立使用","Service not installed · system proxy works independently")} faint size={12} lines={1}/></View>
   <Action quiet label={status().tunInstalled?tr("服务…","Service…"):tr("安装服务…","Install service…")} onPress={()=>service.set(true)}/><Toggle showLabel={false} label="TUN" checked={!!status().tun} disabled={!!p.busy()||!status().tunAvailable} onChange={enabled=>void p.run("tun",()=>p.call("desktop.tun.set",{enabled}))}/>
  </View>
  <Control title={tr("内置 DNS","Built-in DNS")} icon="server" desc={(p.snapshot().features?.dns?.enable?tr("已开启","Enabled"):tr("未开启","Disabled"))+" · "+tr("当前订阅","Current profile")} kind="dns"/>
  <Control title={tr("域名嗅探","Domain sniffing")} icon="radar" desc={(p.snapshot().features?.sniffer?.enable?tr("已开启","Enabled"):tr("未开启","Disabled"))+" · "+tr("全局","Global")} kind="sniffer"/>
 </View>
 <Modal title={feature()==="dns"?tr("内置 DNS 设置","Built-in DNS settings"):tr("域名嗅探设置","Domain sniffing settings")} open={!!feature()} onClose={()=>feature.set(null)} width={660}><If when={!!feature()}><View style={{height:featureSource()==="custom"?Math.min(windowSize().height-210,420):180}}><FeatureEditor page={p} kind={feature()??"dns"} onSourceChange={v=>featureSource.set(v)}/></View></If></Modal>
 <Modal title={tr("系统代理","System proxy")} open={details()} onClose={()=>details.set(false)}><Label text={tr("混合端口","Mixed port")+": "+String(p.snapshot().network.mixedPort??7890)}/><Label text={tr("系统代理由当前 Gopurr 实例管理。关闭后恢复此前的系统设置。","Gopurr manages the system proxy and restores previous settings when disabled.")} muted/><Action label={tr("打开网络设置","Open network settings")} onPress={()=>{details.set(false);p.navigate("settings");}}/></Modal>
 <Modal title={tr("TUN 系统服务","TUN system service")} open={service()} onClose={()=>service.set(false)} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} onPress={()=>service.set(false)}/><Action primary disabled={!!p.busy()} label={status().tunInstalled?tr("更新服务","Update service"):tr("安装服务","Install service")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.install");service.set(false);})}/></Row>}><Label text={tr("系统会请求管理员授权，以安装用于 TUN 模式的特权服务。","The system will request administrator authorization to install the privileged TUN service.")}/><If when={!!status().tunInstalled}><Action danger label={tr("卸载服务…","Uninstall service…")} onPress={()=>void p.run("service",async()=>{await p.call("desktop.service.uninstall");service.set(false);})}/></If></Modal>
 </View>;
}
