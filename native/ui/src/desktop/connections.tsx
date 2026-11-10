import { List, Scroll, Text, View, signal, windowSize } from "nsh";
import { Button } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import { nativeCall } from "./client";
import { Action, Empty, Heading, Icon, IconButton, Label, Modal, Row, SearchField, fmtBytes } from "./ui";
import type { PageProps } from "./types";

export function connectionMatches(c:any,query:string):boolean {
  return [c.host,c.process,c.processPath,c.source,c.destination,c.rule,c.rulePayload,c.chain,...(c.chains??[])].join(" ").toLowerCase().includes(query.trim().toLowerCase());
}

const query=signal(""),selected=signal<any|null>(null),confirm=signal(false),paused=signal(false),frozen=signal<any[]>([]);

function DetailValue(p:{label:string;value?:string;mono?:boolean}) {
  return <View style={{flexDirection:"row",alignItems:"flex-start",gap:8}}><View style={{width:72,paddingTop:1}}><Label text={p.label} muted size={10.5} weight={500}/></View><Text style={{flex:1,minWidth:0,fontSize:11.5,lineHeight:16,color:colors().text,fontFamily:p.mono?"monospace":"system-ui"}}>{p.value||"—"}</Text></View>;
}

function DetailSection(p:{title:string;children?:any}) {
  return <View style={{gap:5,paddingTop:10,borderTopWidth:1,borderColor:colors().border}}><Label text={p.title} muted size={10.5} weight={600}/>{p.children}</View>;
}

export function ConnectionsPage(p:PageProps) {
  const all=()=>paused()?frozen():p.snapshot().connections??[];
  const filtered=()=>all().filter((c:any)=>connectionMatches(c,query()));
  const details=()=>all().find((c:any)=>c.id===selected()?.id)??selected();
  const wide=()=>windowSize().width>=1000;
  const sideDetail=()=>!!selected()&&wide();
  const showRouteColumn=()=>wide()&&!sideDetail();
  const copy=()=>void p.run("copy-connection",()=>nativeCall("desktop.clipboard.write",{text:JSON.stringify(details(),null,2)}));
  const closeConnection=()=>void p.run("close-connection",async()=>{await p.call("desktop.connections.close",{id:details()?.id});selected.set(null);});
  const detailActions=()=><Row wrap><Action label={tr("复制详情","Copy details")} onPress={copy}/><Action danger label={tr("断开连接","Disconnect")} disabled={!!p.busy()} onPress={closeConnection}/></Row>;
  const detailBody=()=> <View style={{gap:12}}>
    <View style={{gap:3}}><Label text={details()?.host||details()?.destination||tr("未知连接","Unknown connection")} bold size={14}/><Label text={[details()?.process,details()?.network].filter(Boolean).join(" · ")} muted size={11}/></View>
    <DetailSection title={tr("连接","Connection")}><DetailValue label={tr("来源","Source")} value={details()?.source}/><DetailValue label={tr("目标地址","Destination")} value={details()?.destination}/><DetailValue label={tr("进程路径","Process path")} value={details()?.processPath} mono/></DetailSection>
    <DetailSection title={tr("路由","Routing")}><DetailValue label={tr("命中规则","Matched rule")} value={`${details()?.rule??""} ${details()?.rulePayload??""}`}/><DetailValue label={tr("代理链","Proxy chain")} value={(details()?.chains??[details()?.chain]).filter(Boolean).join(" → ")}/></DetailSection>
    <DetailSection title={tr("流量","Traffic")}><Row gap={20}><View style={{flex:1}}><Row gap={5}><Icon name="down" size={12} color={colors().down}/><Label text={tr("下行","Download")} muted size={10.5}/></Row><Text style={{fontSize:13,fontWeight:"500",fontVariant:"tabular-nums",color:colors().text}}>{fmtBytes(details()?.down)}</Text></View><View style={{flex:1}}><Row gap={5}><Icon name="up" size={12} color={colors().up}/><Label text={tr("上行","Upload")} muted size={10.5}/></Row><Text style={{fontSize:13,fontWeight:"500",fontVariant:"tabular-nums",color:colors().text}}>{fmtBytes(details()?.up)}</Text></View></Row></DetailSection>
  </View>;
  const detailRow=(item:any)=><Button testID={`connection-${item().id}`} selected={selected()?.id===item().id} onPress={()=>selected.set(item())} style={{minHeight:52,paddingHorizontal:14,paddingVertical:8,borderBottomWidth:1,borderColor:colors().border,backgroundColor:selected()?.id===item().id?colors().selected:colors().surface,gap:7}} hoverStyle={{backgroundColor:selected()?.id===item().id?colors().selected:colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}>
     <Row gap={12}>
       <View style={{flex:1,minWidth:0,gap:2}}>
         <Text style={{fontSize:13,fontWeight:"500",color:colors().text,numberOfLines:1,textOverflow:"ellipsis"}}>{item().host||item().destination}</Text>
         <Text style={{fontSize:11,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{[item().process||item().source,item().network&&String(item().network).toUpperCase()].filter(Boolean).join(" · ")}</Text>
         <If when={!showRouteColumn()}><Text style={{fontSize:10.5,color:colors().faint,numberOfLines:1,textOverflow:"ellipsis"}}>{`${item().rule||"—"} ${item().rulePayload??""} → ${item().chain??""}`}</Text></If>
       </View>
       <If when={showRouteColumn()}><View style={{width:190,gap:2}}><Text style={{fontSize:11.5,fontWeight:"500",color:colors().text,numberOfLines:1,textOverflow:"ellipsis"}}>{`${item().rule||"—"} ${item().rulePayload??""}`}</Text><Text style={{fontSize:11,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{item().chain??""}</Text></View></If>
       <View style={{width:72,alignItems:"flex-end"}}><Row justify="flex-end" gap={4}><Icon name="down" size={11} color={colors().down}/><Text style={{fontSize:11.5,fontWeight:"500",fontVariant:"tabular-nums",color:colors().text}}>{fmtBytes(item().down)}</Text></Row></View>
       <View style={{width:72,alignItems:"flex-end"}}><Row justify="flex-end" gap={4}><Icon name="up" size={11} color={colors().up}/><Text style={{fontSize:11.5,fontWeight:"500",fontVariant:"tabular-nums",color:colors().text}}>{fmtBytes(item().up)}</Text></Row></View>
     </Row>
   </Button>;
  return <View testID="desktop-connections" style={{flex:1,paddingHorizontal:20,paddingVertical:windowSize().height<660?12:16,gap:10}}>
    <Heading title={tr("连接","Connections")} description={paused()?tr(`已暂停 · ${all().length} 个连接保持快照`,`Paused · ${all().length} connections frozen`):tr(`${all().length} 个活跃连接 · 显示 ${filtered().length} 条`,`${all().length} active connections · showing ${filtered().length}`)} action={()=><Row gap={6}><Action quiet label={paused()?tr("继续更新","Resume"):tr("暂停更新","Pause")} onPress={()=>{if(!paused())frozen.set([...all()]);paused.set(!paused());}}/><Action quiet danger label={tr("断开全部…","Disconnect all…")} disabled={!all().length||!!p.busy()} onPress={()=>confirm.set(true)}/></Row>}/>
    <Row gap={10}><View style={{width:320,maxWidth:"100%"}}><SearchField testID="connections-search" value={query()} onChange={v=>query.set(v)} placeholder={tr("搜索主机、进程、IP、规则或代理链","Search host, process, IP, rule or chain")}/></View><View style={{flex:1}}/><Label text={tr(`共 ${filtered().length} 条`,`${filtered().length} total`)} muted size={11.5}/></Row>
    <View style={{flex:1,flexDirection:"row",gap:12,minHeight:0}}>
      <If when={filtered().length>0}>
        <View style={{flex:1,minWidth:0,borderRadius:10,backgroundColor:colors().surface,overflow:"hidden"}}>
          <View style={{height:30,paddingHorizontal:14,paddingLeft:16,flexDirection:"row",alignItems:"center",gap:12}}>
            <View style={{flex:1}}><Label text={tr("主机 / 进程","Host / process")} muted size={10.5} weight={500}/></View>
            <If when={showRouteColumn()}><View style={{width:190}}><Label text={tr("规则 / 出口","Rule / chain")} muted size={10.5} weight={500}/></View></If>
            <View style={{width:72,alignItems:"flex-end"}}><Label text={tr("下行","Down")} muted size={10.5} weight={500}/></View>
            <View style={{width:72,alignItems:"flex-end"}}><Label text={tr("上行","Up")} muted size={10.5} weight={500}/></View>
          </View>
          <View style={{height:1,backgroundColor:colors().border}}/>
          <List testID="connections-list" each={filtered()} keyExtractor={(c:any)=>c.id} style={{flex:1}}>{detailRow}</List>
        </View>
      </If>
      <If when={filtered().length===0}>
        <View style={{flex:1,borderRadius:10,backgroundColor:colors().surface}}><Empty icon={query()?"search":"conns"} title={query()?tr("没有匹配的连接","No matching connections"):tr("暂无活跃连接","No active connections")} description={query()?tr("换个关键词或清空搜索。","Try another keyword or clear your search."):tr("连接建立后会显示在这里。","Connections appear here as they are established.")} action={()=><If when={!!query()}><Action label={tr("清空搜索","Clear search")} onPress={()=>query.set("")}/></If>}/></View>
      </If>
      <If when={sideDetail()}>
        <View style={{width:310,padding:16,gap:10,borderRadius:10,backgroundColor:colors().surface}}>
          <Row justify="space-between"><Label text={tr("连接详情","Connection details")} weight={600} size={13}/><IconButton testID="connection-detail-close" icon="x" label={tr("关闭详情","Close details")} onPress={()=>selected.set(null)}/></Row>
          <Scroll style={{flex:1}}>{detailBody()}</Scroll>
          {detailActions()}
        </View>
      </If>
    </View>
  <Modal title={tr("连接详情","Connection details")} open={!!selected()&&!wide()} onClose={()=>selected.set(null)} width={560} footer={detailActions}>{detailBody()}</Modal>
    <Modal title={tr("断开所有连接？","Disconnect all connections?")} open={confirm()} onClose={()=>confirm.set(false)} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} onPress={()=>confirm.set(false)}/><Action danger label={tr("断开全部","Disconnect all")} onPress={()=>void p.run("close-all",async()=>{await p.call("desktop.connections.closeAll");confirm.set(false);selected.set(null);})}/></Row>}><Label text={tr("现有连接将中断，应用可能自动重新建立连接。","Existing connections will be interrupted. Applications may reconnect automatically.")}/></Modal>
  </View>;
}
