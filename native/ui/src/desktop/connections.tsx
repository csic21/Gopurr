import { List, Scroll, Text, View, signal, windowSize } from "nsh";
import { Button } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { nativeCall } from "./client";
import { Action, Empty, Heading, Icon, IconButton, Label, Modal, Panel, Row, SearchField, TYPE, fmtBytes, fmtTime } from "./ui";
import type { PageProps } from "./types";

export function connectionMatches(c:any,query:string):boolean {
  return [c.host,c.process,c.processPath,c.source,c.destination,c.rule,c.rulePayload,c.chain,...(c.chains??[])].join(" ").toLowerCase().includes(query.trim().toLowerCase());
}

const query=signal(""),selected=signal<any|null>(null),confirm=signal(false),paused=signal(false),frozen=signal<any[]>([]);
const RATE_WIDTH=72;
const compact=()=>windowSize().height<660||windowSize().width<960;
const wide=()=>windowSize().width>=1000;
const PAD=()=>compact()?16:24;
const ROUTE_WIDTH=200;
const ruleLine=(c:any)=>(c?.rule||"—")+" "+(c?.rulePayload??"");
const chainLine=(c:any)=>(c?.chains?.length?c.chains:c?.chain?[c.chain]:[]).filter(Boolean).join(" → ")||"—";
const processLine=(c:any)=>[c?.process||c?.source,c?.network&&String(c.network).toUpperCase()].filter(Boolean).join(" · ");

/** Micro labels sit above the rows and carry the column rhythm, never the data. */
function Micro(p:{text:string;align?:"left"|"right"}) {
  return <Text style={{fontSize:TYPE.micro,fontWeight:"500",letterSpacing:0.3,color:colors().faint,textAlign:p.align,numberOfLines:1}}>{p.text}</Text>;
}

/** One page-level card: radius, border and lift all come from the shared tokens. */
function Card(p:{children?:any;style?:any}) {
  return <Panel padding={0} gap={0} style={{overflow:"hidden",...p.style}}>{p.children}</Panel>;
}

function DetailValue(p:{label:string;value?:string;mono?:boolean}) {
  return <View style={{flexDirection:"row",alignItems:"flex-start",gap:10}}>
    <View style={{width:76,paddingTop:1}}><Label text={p.label} faint size={10.5} weight={500} lines={1}/></View>
    <Text tooltip={p.value||"—"} style={{flex:1,minWidth:0,fontSize:11.5,lineHeight:16,color:colors().text,fontFamily:p.mono?"monospace":"system-ui"}}>{p.value||"—"}</Text>
  </View>;
}

function DetailSection(p:{title:string;children?:any}) {
  return <View style={{gap:8,paddingTop:12,borderTopWidth:1,borderColor:colors().border}}>
    <Text style={{fontSize:10.5,fontWeight:"600",letterSpacing:0.4,color:colors().faint,numberOfLines:1}}>{p.title}</Text>
    <View style={{gap:7}}>{p.children}</View>
  </View>;
}

export function ConnectionsPage(p:PageProps) {
  const all=()=>paused()?frozen():p.snapshot().connections??[];
  const filtered=()=>all().filter((c:any)=>connectionMatches(c,query()));
  const details=()=>all().find((c:any)=>c.id===selected()?.id)??selected();
  const sideDetail=()=>!!selected()&&wide();
  const showRouteColumn=()=>!sideDetail()&&windowSize().width>=960;
  const copy=()=>void p.run("copy-connection",()=>nativeCall("desktop.clipboard.write",{text:JSON.stringify(details(),null,2)}));
  const closeConnection=()=>void p.run("close-connection",async()=>{await p.call("desktop.connections.close",{id:details()?.id});selected.set(null);});
  const detailActions=()=><Row justify="space-between"><Action label={tr("复制详情","Copy details")} onPress={copy}/><Action danger label={tr("断开连接","Disconnect")} disabled={!!p.busy()} onPress={closeConnection}/></Row>;
  const detailBody=()=>{const d=details();return <View style={{gap:14}}>
    <View style={{gap:4}}>
      <Text tooltip={d?.host||d?.destination||tr("未知连接","Unknown connection")} style={{fontSize:15,fontWeight:"600",letterSpacing:-0.25,color:colors().text,numberOfLines:2,textOverflow:"ellipsis"}}>{d?.host||d?.destination||tr("未知连接","Unknown connection")}</Text>
      <Text style={{fontSize:11.5,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{processLine(d)}</Text>
    </View>
    <DetailSection title={tr("连接","Connection")}>
      <DetailValue label={tr("来源","Source")} value={d?.source}/>
      <DetailValue label={tr("目标地址","Destination")} value={d?.destination}/>
      <DetailValue label={tr("进程路径","Process path")} value={d?.processPath} mono/>
      <DetailValue label={tr("建立时间","Started")} value={d?.start?fmtTime(d.start):""}/>
    </DetailSection>
    <DetailSection title={tr("路由","Routing")}>
      <DetailValue label={tr("命中规则","Matched rule")} value={ruleLine(d)}/>
      <DetailValue label={tr("代理链","Proxy chain")} value={chainLine(d)}/>
    </DetailSection>
    <DetailSection title={tr("流量","Traffic")}>
      <Row gap={16}>
        <View style={{flex:1,gap:3}}>
          <Row gap={5}><Icon name="down" size={11} color={colors().down}/><Label text={tr("下行","Download")} faint size={10.5}/></Row>
          <Text style={{fontSize:15,fontWeight:"600",letterSpacing:-0.2,color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{fmtBytes(d?.down)}</Text>
        </View>
        <View style={{flex:1,gap:3}}>
          <Row gap={5}><Icon name="up" size={11} color={colors().up}/><Label text={tr("上行","Upload")} faint size={10.5}/></Row>
          <Text style={{fontSize:15,fontWeight:"600",letterSpacing:-0.2,color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{fmtBytes(d?.up)}</Text>
        </View>
      </Row>
    </DetailSection>
  </View>;};
  const connectionRow=(item:any)=>{const active=()=>selected()?.id===item().id;return <Button testID={"connection-"+item().id} selected={active()} onPress={()=>selected.set(item())}
    style={{minHeight:sideDetail()?58:52,paddingHorizontal:16,paddingVertical:6,backgroundColor:active()?colors().selected:colors().surface,transitionProperty:"background-color, border-color",transitionDuration:140,transitionTimingFunction:"ease-out"}}
    hoverStyle={{backgroundColor:active()?colors().selected:colors().hover}}
    focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:-2}}>
    <View style={{position:"absolute",left:0,top:9,bottom:9,width:3,borderRadius:2,backgroundColor:active()?colors().accent:"transparent"}}/>
    <View style={{flexDirection:"row",alignItems:"center",gap:12}}>
      <View style={{flex:1,minWidth:0,gap:2}}>
        <Text tooltip={item().host||item().destination} style={{fontSize:13,fontWeight:"500",letterSpacing:-0.1,color:active()?colors().accent:colors().text,numberOfLines:1,textOverflow:"ellipsis"}}>{item().host||item().destination}</Text>
        <Text tooltip={processLine(item())} style={{fontSize:11,color:colors().faint,numberOfLines:1,textOverflow:"ellipsis"}}>{processLine(item())}</Text>
        <If when={!showRouteColumn()}><Text tooltip={ruleLine(item())+" → "+chainLine(item())} style={{fontSize:10,color:colors().faint,numberOfLines:1,textOverflow:"ellipsis"}}>{ruleLine(item())+" → "+chainLine(item())}</Text></If>
      </View>
      <If when={showRouteColumn()}><View style={{width:ROUTE_WIDTH,gap:2}}>
        <Text tooltip={ruleLine(item())} style={{fontSize:11.5,fontWeight:"500",color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{ruleLine(item())}</Text>
        <Text tooltip={chainLine(item())} style={{fontSize:11,color:colors().faint,numberOfLines:1,textOverflow:"ellipsis"}}>{chainLine(item())}</Text>
      </View></If>
      <View style={{width:RATE_WIDTH,alignItems:"flex-end"}}><Row justify="flex-end" gap={4}><Icon name="down" size={11} color={colors().down}/><Text style={{fontSize:12,fontWeight:"500",color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{fmtBytes(item().down)}</Text></Row></View>
      <View style={{width:RATE_WIDTH,alignItems:"flex-end"}}><Row justify="flex-end" gap={4}><Icon name="up" size={11} color={colors().up}/><Text style={{fontSize:12,fontWeight:"500",color:colors().text,fontVariant:"tabular-nums",numberOfLines:1}}>{fmtBytes(item().up)}</Text></Row></View>
    </View>
  </Button>;};
  return <View testID="desktop-connections" style={{flex:1,paddingHorizontal:PAD(),paddingTop:compact()?10:20,paddingBottom:compact()?12:20,gap:compact()?10:16}}>
    <Heading title={tr("连接","Connections")} description={paused()?tr(`已暂停 · ${all().length} 个连接保持快照`,`Paused · ${all().length} connections frozen`):tr(`${all().length} 个活跃连接 · 自动刷新`,`${all().length} active connections · auto-refresh`)} action={()=><Row gap={8}><Action label={paused()?tr("继续更新","Resume"):tr("暂停更新","Pause")} onPress={()=>{if(!paused())frozen.set([...all()]);paused.set(!paused());}}/><Action quiet danger label={tr("断开全部…","Disconnect all…")} disabled={!all().length||!!p.busy()} onPress={()=>confirm.set(true)}/></Row>}/>
    <Row gap={10}>
      <View style={{width:compact()?260:320,maxWidth:"100%"}}><SearchField testID="connections-search" value={query()} onChange={v=>query.set(v)} placeholder={tr("搜索主机、进程、IP、规则或代理链","Search host, process, IP, rule or chain")}/></View>
      <View style={{flex:1,minWidth:0}}/>
      <Label text={tr(`共 ${filtered().length} 条`,`${filtered().length} total`)} muted size={11.5} tabular/>
    </Row>
    <View style={{flex:1,flexDirection:"row",gap:12,minHeight:0}}>
      <If when={filtered().length>0}>
        <Card style={{flex:1,minWidth:0}}>
          <Row style={{height:32,paddingHorizontal:16,gap:12,alignItems:"center",borderBottomWidth:1,borderColor:colors().border}}>
            <View style={{flex:1,minWidth:0}}><Micro text={tr("主机 / 进程","Host / process")}/></View>
            <If when={showRouteColumn()}><View style={{width:ROUTE_WIDTH}}><Micro text={tr("规则 / 出口","Rule / chain")}/></View></If>
            <View style={{width:RATE_WIDTH,alignItems:"flex-end"}}><Micro text={tr("下行","Down")} align="right"/></View>
            <View style={{width:RATE_WIDTH,alignItems:"flex-end"}}><Micro text={tr("上行","Up")} align="right"/></View>
          </Row>
          <List testID="connections-list" each={filtered()} keyExtractor={(c:any)=>c.id} style={{flex:1}} separator={<View style={{height:1,backgroundColor:colors().border}}/>}>{connectionRow}</List>
        </Card>
      </If>
      <If when={filtered().length===0}>
        <Card style={{flex:1}}><Empty icon={query()?"search":"conns"} title={query()?tr("没有匹配的连接","No matching connections"):tr("暂无活跃连接","No active connections")} description={query()?tr("换个关键词或清空搜索。","Try another keyword or clear your search."):tr("连接建立后会显示在这里。","Connections appear here as they are established.")} action={()=><If when={!!query()}><Action label={tr("清空搜索","Clear search")} onPress={()=>query.set("")}/></If>}/></Card>
      </If>
      <If when={sideDetail()}>
        <Card style={{width:320}}>
          <Row justify="space-between" gap={8} style={{paddingLeft:14,paddingRight:8,paddingVertical:8,borderBottomWidth:1,borderColor:colors().border}}>
            <Row gap={9} style={{flex:1,minWidth:0}}>
              <View style={{width:26,height:26,borderRadius:8,alignItems:"center",justifyContent:"center",backgroundColor:colors().hover,borderWidth:1,borderColor:colors().border}}><Icon name="conns" size={14} color={colors().muted}/></View>
              <Label text={tr("连接详情","Connection details")} weight={600} size={13} lines={1}/>
            </Row>
            <IconButton testID="connection-detail-close" icon="x" label={tr("关闭详情","Close details")} onPress={()=>selected.set(null)}/>
          </Row>
          <Scroll style={{flex:1}} contentStyle={{paddingHorizontal:16,paddingTop:14,paddingBottom:16}}>{detailBody()}</Scroll>
          <View style={{paddingHorizontal:14,paddingVertical:10,borderTopWidth:1,borderColor:colors().border}}>{detailActions()}</View>
        </Card>
      </If>
    </View>
    <Modal title={tr("连接详情","Connection details")} open={!!selected()&&!wide()} onClose={()=>selected.set(null)} width={560} footer={detailActions}>{detailBody()}</Modal>
    <Modal title={tr("断开所有连接？","Disconnect all connections?")} open={confirm()} onClose={()=>confirm.set(false)} footer={()=><Row justify="flex-end" gap={8}><Action label={tr("取消","Cancel")} onPress={()=>confirm.set(false)}/><Action danger label={tr("断开全部","Disconnect all")} onPress={()=>void p.run("close-all",async()=>{await p.call("desktop.connections.closeAll");confirm.set(false);selected.set(null);})}/></Row>}><Label text={tr("现有连接将中断，应用可能自动重新建立连接。","Existing connections will be interrupted. Applications may reconnect automatically.")}/></Modal>
  </View>;
}
