import { List, Text, View, signal, windowSize } from "nsh";
import { colors } from "./theme";
import { tr } from "./i18n";
import { nativeCall } from "./client";
import { Action, Empty, Heading, Label, Modal, RADIUS, Row, SearchField, Select, elevation, fmtTime, tint } from "./ui";
import type { PageProps } from "./types";

const query=signal(""),level=signal("all"),paused=signal(false),frozen=signal<any[]>([]),confirm=signal(false);

const levelTone=(value:string)=>value==="error"?colors().danger:value==="warning"?colors().warn:value==="info"?colors().muted:colors().faint;
const levelRail=(value:string)=>value==="error"?tint(colors().danger,"a6"):value==="warning"?tint(colors().warn,"a6"):"transparent";

/** A console rather than a table: one surface, a quiet filter bar and hairline rows. */
export function LogsPage(p:PageProps) {
  const compact=()=>windowSize().width<960||windowSize().height<660;
  const pad=()=>compact()?16:24;
  const all=()=>paused()?frozen():p.snapshot().logs??[];
  const filtered=()=>all().filter((v:any)=>(level()==="all"||v.level.toLowerCase()===level())&&(v.level+" "+v.text).toLowerCase().includes(query().toLowerCase()));
  const body=()=>filtered().map((v:any)=>fmtTime(v.at)+" ["+v.level+"] "+v.text).join("\n");
  const filtering=()=>!!query().trim()||level()!=="all";
  const reset=()=>{query.set("");level.set("all");};
  const pause=()=>{if(!paused())frozen.set([...all()]);paused.set(!paused());};
  const time=(at:string)=>new Date(at).toLocaleTimeString();
  return <View testID="desktop-logs" style={{flex:1,minWidth:0,paddingHorizontal:pad(),paddingTop:compact()?10:20,paddingBottom:compact()?10:16,gap:compact()?8:14}}>
    <Heading title={tr("日志","Logs")} description={tr("内核运行记录与网络事件","Kernel activity and network events")}/>
    <View style={{flex:1,minHeight:0,borderRadius:RADIUS.surface,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface"),overflow:"hidden"}}>
      <View style={{flexDirection:"row",alignItems:"center",gap:8,paddingHorizontal:compact()?12:14,minHeight:compact()?46:50,borderBottomWidth:1,borderColor:colors().border}}>
        <Row gap={8} style={{flex:1,minWidth:0}}><View style={{flex:1,minWidth:96,maxWidth:340}}><SearchField testID="logs-search" value={query()} onChange={v=>query.set(v)} placeholder={tr("搜索日志内容","Search logs")}/></View><Select label={tr("日志等级","Log level")} value={level()} onChange={v=>level.set(v)} items={[{value:"all",title:tr("全部等级","All levels")},...['debug','info','warning','error','silent'].map(v=>({value:v,title:v}))]}/></Row>
        <Row gap={6} style={{flexShrink:0}}>
          <Action label={paused()?tr("继续","Resume"):tr("暂停","Pause")} onPress={pause}/>
          <Action label={tr("复制","Copy")} disabled={!filtered().length} onPress={()=>void p.run("copy-logs",()=>nativeCall("desktop.clipboard.write",{text:body()}))}/>
          <Action label={tr("导出…","Export…")} disabled={!filtered().length} onPress={()=>void p.run("export-logs",async()=>{const file=await nativeCall("desktop.file.save",{kind:"logs",name:"gopurr-logs.txt"});if(file.path)await p.call("desktop.logs.export",{path:file.path,text:body()});})}/>
          <Action quiet danger label={tr("清空…","Clear…")} onPress={()=>confirm.set(true)}/>
        </Row>
      </View>
      <List testID="logs-list" each={filtered()} keyExtractor={(v:any,i:number)=>v.at+"-"+i} style={{flex:1,minHeight:0}} empty={<View style={{flex:1,justifyContent:"center"}}><Empty icon={filtering()?"search":"logs"} title={tr("暂无日志","No logs")} description={filtering()?tr("没有日志匹配当前条件。","No logs match the current filters."):tr("内核事件会显示在这里。","Kernel events appear here.")} action={filtering()?()=><Action label={tr("重置筛选","Reset filters")} onPress={reset}/>:undefined}/></View>}>
        {item=><View hoverStyle={{backgroundColor:colors().hover}} style={{flexDirection:"row",alignItems:"flex-start",gap:10,paddingLeft:12,paddingRight:16,paddingVertical:8,minHeight:36,borderBottomWidth:1,borderColor:colors().border,backgroundColor:colors().surface,transitionProperty:"background-color, border-color",transitionDuration:120,transitionTimingFunction:"ease-out"}}>
          <View style={{width:2,alignSelf:"stretch",borderRadius:1,backgroundColor:levelRail(item().level)}}/>
          <View style={{width:compact()?62:68,paddingTop:2}}><Label text={time(item().at)} muted size={10.5} mono tabular lines={1}/></View>
          <Row gap={5} style={{width:compact()?58:64,paddingTop:2}}><View style={{width:5,height:5,borderRadius:2.5,backgroundColor:levelTone(item().level)}}/><Label text={item().level} size={10.5} weight={600} color={levelTone(item().level)} lines={1}/></Row>
          <Text style={{flex:1,minWidth:0,fontSize:12,lineHeight:17,fontFamily:"monospace",color:colors().text}}>{item().text}</Text>
        </View>}
      </List>
      <View style={{height:30,minHeight:30,flexDirection:"row",alignItems:"center",justifyContent:"space-between",gap:12,paddingHorizontal:14,borderTopWidth:1,borderColor:colors().border}}>
        <Row gap={6} style={{flexShrink:1,minWidth:0}}><If when={paused()}><View style={{width:6,height:6,borderRadius:3,backgroundColor:colors().warn}}/><Label text={tr("已暂停显示，内核仍在记录","Display paused; the kernel continues logging")} color={colors().warn} size={11.5} lines={1}/></If></Row>
        <Label text={String(filtered().length)+" "+tr("条记录","entries")} muted size={11.5} tabular lines={1}/>
      </View>
    </View>
    <Modal title={tr("清空日志？","Clear logs?")} open={confirm()} onClose={()=>confirm.set(false)} footer={()=><Row justify="flex-end"><Action label={tr("取消","Cancel")} onPress={()=>confirm.set(false)}/><Action danger label={tr("清空","Clear")} onPress={()=>void p.run("clear-logs",async()=>{await p.call("desktop.logs.clear");frozen.set([]);confirm.set(false);})}/></Row>}><Label text={tr("当前日志缓冲区将被清空。需要保留的内容请先导出。","The current log buffer will be cleared. Export any entries you need first.")}/></Modal>
  </View>;
}
