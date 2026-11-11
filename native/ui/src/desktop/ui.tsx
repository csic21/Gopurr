import { colorScheme, computed, Image, Scroll, Text, View, windowSize } from "nsh";
import type { JSX, ImageAsset } from "nsh";
import { Button, Dialog, Input, Switch, Tabs as NativeTabs, Tab } from "nsh/base";
import { colors } from "./theme";
import { tr } from "./i18n";
import cable from "../../assets/icons/cable.svg";
import layers from "../../assets/icons/layers-2.svg";
import filter from "../../assets/icons/list-filter.svg";
import network from "../../assets/icons/network.svg";
import panel from "../../assets/icons/panel-left.svg";
import terminal from "../../assets/icons/square-terminal.svg";
import down from "../../assets/icons/arrow-down-left.svg";
import up from "../../assets/icons/arrow-up-right.svg";
import overview from "../../assets/icons/layout-dashboard.svg";
import profiles from "../../assets/icons/file-text.svg";
import proxies from "../../assets/icons/globe.svg";
import settings from "../../assets/icons/settings-2.svg";
import check from "../../assets/icons/circle-check.svg";
import circle from "../../assets/icons/circle.svg";
import shield from "../../assets/icons/shield-check.svg";
import search from "../../assets/icons/search.svg";
import x from "../../assets/icons/x.svg";
import chevronDown from "../../assets/icons/chevron-down.svg";
import chevronRight from "../../assets/icons/chevron-right.svg";
import refresh from "../../assets/icons/refresh-cw.svg";
const icons:Record<string,ImageAsset>={overview,subs:layers,profiles,proxies,conns:cable,mesh:network,rules:filter,logs:terminal,panel,down,up,settings,check,circle,search,x,chevronDown,chevronRight,refresh,shield};
export function Icon(p:{name:string;size?:number;color?:string}) {return <Image src={icons[p.name]??circle} style={{width:p.size??16,height:p.size??16,objectFit:"contain",tintColor:p.color??colors().muted}}/>;}
// One scale for the whole app. The system font supplies its own line metrics;
// headings are tight, figures are tabular, and the quiet roles stay legible.
export const TYPE={title:20,hero:26,section:12,body:13,secondary:11.5,caption:10.5,metric:28,mono:12} as const;
export const CONTROL_HEIGHT=28;
export const ROW_HEIGHT=46;
export const RADIUS={control:6,surface:10,dialog:12};
const MOTION={transitionProperty:"background-color, border-color, color, opacity",transitionDuration:120,transitionTimingFunction:"ease-out"} as const;
export function Label(p:{text:string;muted?:boolean;size?:number;weight?:number;bold?:boolean;color?:string;mono?:boolean;lines?:number;tabular?:boolean;tooltip?:string}) {return <Text tooltip={p.tooltip} style={{fontSize:p.size??TYPE.body,fontWeight:p.weight??(p.bold?600:400),fontFamily:p.mono?"monospace":"system-ui",color:p.color??(p.muted?colors().muted:colors().text),numberOfLines:p.lines,textOverflow:p.lines===1?"ellipsis":undefined,fontVariant:p.tabular?"tabular-nums":undefined}}>{p.text}</Text>;}
/**
 * One button geometry for the whole app: 28 high, 6 radius. Primary carries
 * the accent, secondary a hairline, quiet is text-only inside rows and page
 * heads. Destructive actions are red text rather than red fills, as on macOS.
 */
export function Action(p:{label:string;onPress:()=>void;primary?:boolean;danger?:boolean;quiet?:boolean;disabled?:boolean;testID?:string;bold?:boolean}) {
 const filled=!!p.primary, plain=!filled&&(p.quiet||p.danger);
 const fg=filled?colors().accentText:p.danger?colors().danger:colors().text;
 return <Button testID={p.testID} onPress={p.onPress} disabled={p.disabled} style={{height:CONTROL_HEIGHT,minHeight:CONTROL_HEIGHT,paddingHorizontal:plain?8:12,borderRadius:RADIUS.control,alignItems:"center",justifyContent:"center",borderWidth:filled||plain?0:1,borderColor:colors().border,backgroundColor:filled?colors().accent:plain?"transparent":colors().surface,...MOTION}}
  hoverStyle={{backgroundColor:filled?colors().accentHover:colors().hover}}
  activeStyle={{backgroundColor:filled?colors().accentPressed:colors().selected,opacity:filled?1:0.9}}
  focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:2}}
  disabledStyle={{opacity:0.4}}><Text style={{fontSize:12.5,fontWeight:p.bold||filled?"600":"500",color:fg,numberOfLines:1}}>{p.label}</Text></Button>;
}
export function IconButton(p:{icon:string;label:string;onPress:()=>void;disabled?:boolean;testID?:string;flat?:boolean;size?:number}) {return <Button testID={p.testID} accessibilityLabel={p.label} disabled={p.disabled} onPress={p.onPress} style={{width:p.size??CONTROL_HEIGHT,height:p.size??CONTROL_HEIGHT,alignItems:"center",justifyContent:"center",borderRadius:RADIUS.control,backgroundColor:"transparent",...MOTION}} hoverStyle={{backgroundColor:colors().hover}} activeStyle={{backgroundColor:colors().selected}} focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:1}} disabledStyle={{opacity:0.35}}><Icon name={p.icon} size={15}/></Button>;}
export function StatusDot(p:{on:boolean;size?:number;color?:string}) {return <View style={{width:p.size??6,height:p.size??6,borderRadius:(p.size??6)/2,backgroundColor:p.color??(p.on?colors().success:colors().faint)}}/>;}
/** A small state label: tinted surface, semantic text, never color alone. */
export function Tag(p:{text:string;tone?:"accent"|"success"|"danger"|"muted";testID?:string}) {
 const color=p.tone==="danger"?colors().danger:p.tone==="success"?colors().success:p.tone==="muted"?colors().muted:colors().accent;
 return <View testID={p.testID} style={{height:19,paddingHorizontal:6,justifyContent:"center",borderRadius:4,backgroundColor:p.tone==="muted"?colors().hover:color+"16"}}><Text style={{fontSize:10.5,fontWeight:"600",color,numberOfLines:1}}>{p.text}</Text></View>;
}
export function Panel(p:{children?:JSX.Element;padding?:number;gap?:number;style?:any}) {return <View style={{padding:p.padding??16,gap:p.gap??12,borderRadius:RADIUS.surface,backgroundColor:colors().surface,...p.style}}>{p.children}</View>;}
/**
 * Grouped rows: the container paints the hairline, rows sit on the surface and
 * one-point gaps become separators. No border per row, no nested cards.
 */
export function Group(p:{children?:JSX.Element;style?:any}) {return <View style={{borderRadius:RADIUS.surface,backgroundColor:colors().border,overflow:"hidden",gap:1,...p.style}}>{p.children}</View>;}
export function GroupRow(p:{children?:JSX.Element;gap?:number;style?:any;hover?:boolean}) {return <View hoverStyle={p.hover?{backgroundColor:colors().hover}:undefined} style={{flexDirection:"row",alignItems:"center",minHeight:ROW_HEIGHT,paddingHorizontal:14,paddingVertical:9,gap:p.gap??12,backgroundColor:colors().surface,...MOTION,...p.style}}>{p.children}</View>;}
export function Row(p:{children?:JSX.Element;gap?:number;justify?:"space-between"|"flex-end"|"flex-start"|"center";wrap?:boolean;style?:any}) {return <View style={{flexDirection:"row",alignItems:"center",gap:p.gap??8,justifyContent:p.justify,flexWrap:p.wrap?"wrap":"nowrap",...p.style}}>{p.children}</View>;}
export function Column(p:{children?:JSX.Element;gap?:number;flex?:number}) {return <View style={{gap:p.gap??8,flex:p.flex}}>{p.children}</View>;}
export function Heading(p:{title:string;description:string;action?:()=>JSX.Element;descriptionTone?:"muted"|"danger"}) {return <View style={{gap:3}}><Row justify="space-between" style={{minHeight:CONTROL_HEIGHT,gap:12}}><Text role="header" style={{fontSize:TYPE.title,fontWeight:"600",letterSpacing:-0.45,color:colors().text,numberOfLines:1,flexShrink:1,minWidth:0}}>{p.title}</Text><View style={{flexShrink:0}}>{p.action?.()}</View></Row><Text style={{fontSize:12,lineHeight:16,color:p.descriptionTone==="danger"?colors().danger:colors().muted,numberOfLines:1,textOverflow:"ellipsis"}}>{p.description}</Text></View>;}
/** A quiet group label above a grouped surface; the page title stays the loudest element. */
export function Section(p:{title:string;subtitle?:string}) {return <Row justify="space-between" style={{paddingTop:8,paddingHorizontal:2,gap:8}}><Text style={{fontSize:TYPE.section,fontWeight:"600",letterSpacing:0.1,color:colors().muted,numberOfLines:1}}>{p.title}</Text><If when={!!p.subtitle}><Label text={p.subtitle??""} muted size={TYPE.caption} lines={1}/></If></Row>;}
export function Field(p:{accessibilityLabel?:string;label?:string;value:string;onChange:(value:string)=>void;placeholder?:string;multiline?:boolean;height?:number;readonly?:boolean;secret?:boolean;testID?:string}) {return <View style={{gap:5}}><If when={!!p.label}><Text style={{fontSize:TYPE.section,fontWeight:"500",color:colors().muted}}>{p.label??""}</Text></If><Input testID={p.testID} accessibilityLabel={p.accessibilityLabel??p.label} value={p.value} onChange={({value})=>p.onChange(value)} placeholder={p.placeholder} multiline={p.multiline} readonly={p.readonly} masked={p.secret} autoCorrect={false} autoCapitalize="none" style={{height:p.height??(p.multiline?140:30),minHeight:30,paddingHorizontal:9,paddingVertical:6,borderRadius:RADIUS.control,borderWidth:1,borderColor:colors().border,backgroundColor:colors().bg,color:colors().text,placeholderColor:colors().faint,fontSize:p.multiline?TYPE.mono:TYPE.body,fontFamily:p.multiline?"monospace":"system-ui",...MOTION}} focusStyle={{borderColor:colors().accent,backgroundColor:colors().surface}}/></View>;}
export function SearchField(p:{value:string;onChange:(value:string)=>void;placeholder:string;testID?:string;label?:string}) {return <View style={{height:30,minHeight:30,flexDirection:"row",alignItems:"center",gap:6,paddingLeft:9,paddingRight:3,borderRadius:7,borderWidth:1,borderColor:colors().border,backgroundColor:colors().surface,...MOTION}} focusWithinStyle={{borderColor:colors().accent}}><Icon name="search" size={14} color={colors().faint}/><Input testID={p.testID} accessibilityLabel={p.label??p.placeholder} value={p.value} onChange={({value})=>p.onChange(value)} placeholder={p.placeholder} autoCorrect={false} autoCapitalize="none" style={{flex:1,minWidth:0,height:28,paddingHorizontal:0,paddingVertical:0,borderWidth:0,backgroundColor:"transparent",color:colors().text,placeholderColor:colors().faint,fontSize:12.5}}/><If when={!!p.value}><Button testID={p.testID?`${p.testID}-clear`:undefined} accessibilityLabel={tr("清空搜索","Clear search")} onPress={()=>p.onChange("")} style={{width:24,height:24,alignItems:"center",justifyContent:"center",borderRadius:5,...MOTION}} hoverStyle={{backgroundColor:colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><Icon name="x" size={13}/></Button></If></View>;}
export function Toggle(p:{checked:boolean;onChange:(v:boolean)=>void;label?:string;disabled?:boolean;testID?:string;showLabel?:boolean}) {return <Row><If when={!!p.label&&p.showLabel!==false}><Label text={p.label??""}/></If><Switch testID={p.testID} checked={p.checked} disabled={p.disabled} accessibilityLabel={p.label} onChange={({checked})=>p.onChange(checked)} style={{width:36,height:21,opacity:p.disabled?0.4:1}} trackStyle={{width:36,height:21,padding:2,backgroundColor:p.checked?colors().accent:colors().border,borderRadius:11}} thumbStyle={{width:17,height:17,borderRadius:9,backgroundColor:"#ffffff",boxShadow:[{x:0,y:1,blur:2,color:"#00000033"}]}}/></Row>;}
/**
 * Segmented control: a recessed track with one raised selected pill. The
 * indicator moves natively; text and press targets stay fixed.
 */
export function Tabs(p:{items:{id:string;label:string}[];value:string;onChange:(v:string)=>void;disabled?:boolean;testIDPrefix?:string}) {return <NativeTabs style={{flexDirection:"row",alignSelf:"flex-start",gap:2,padding:2,borderRadius:7,backgroundColor:colorScheme()==="dark"?colors().surface:colors().hover}} indicatorStyle={{top:2,bottom:2,borderRadius:5,backgroundColor:colorScheme()==="dark"?colors().hover:colors().surface,boxShadow:[{x:0,y:1,blur:2,color:"#0000001f"}]}}><Each of={p.items} key={(item)=>item.id}>{(item)=><Tab testID={`${p.testIDPrefix??"desktop-tab"}-${item().id}`} selected={p.value===item().id} disabled={p.disabled} onPress={()=>p.onChange(item().id)} style={{paddingHorizontal:10,paddingVertical:3,minHeight:24,borderRadius:5,backgroundColor:"transparent"}} hoverStyle={{backgroundColor:p.value===item().id?"transparent":colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><Text style={{fontSize:12,fontWeight:p.value===item().id?"600":"500",color:p.value===item().id?colors().accent:colors().muted}}>{item().label}</Text></Tab>}</Each></NativeTabs>;}
export { Select } from "./select";
export function Feedback(p:{text:string;danger?:boolean}) {return <View role="alert" style={{paddingHorizontal:12,paddingVertical:9,borderRadius:8,backgroundColor:p.danger?colors().danger+"12":colors().hover,borderLeftWidth:2,borderLeftColor:p.danger?colors().danger:colors().accent+"80"}}><Text style={{fontSize:12,lineHeight:16,color:p.danger?colors().danger:colors().text}}>{p.text}</Text></View>;}
export function Empty(p:{title:string;description?:string;action?:()=>JSX.Element;icon?:string}) {return <View style={{flex:1,minHeight:170,padding:24,gap:9,alignItems:"center",justifyContent:"center"}}><If when={!!p.icon}><View style={{width:42,height:42,alignItems:"center",justifyContent:"center",borderRadius:RADIUS.surface,backgroundColor:colors().hover}}><Icon name={p.icon??"circle"} size={20} color={colors().faint}/></View></If><Text style={{fontSize:13.5,fontWeight:"600",color:colors().text,textAlign:"center"}}>{p.title}</Text><If when={!!p.description}><Text style={{fontSize:12,lineHeight:18,color:colors().muted,maxWidth:360,textAlign:"center"}}>{p.description}</Text></If><If when={!!p.action}><View style={{paddingTop:5}}>{p.action?.()}</View></If></View>;}
/**
 * Dialog: one surface, a title row, a scrolling body and a footer that stays
 * put. The close action keeps its label so keyboard and screen-reader users
 * always have a named way out.
 */
export function Modal(p:{title:string;open:boolean;onClose:()=>void;children?:JSX.Element;footer?:()=>JSX.Element;width?:number;height?:number;error?:string;testID?:string}) {
  // Dialog recreates its native layer whenever its open dependency changes.
  // Deduplicate visibility so fresh snapshots update content without reopening.
  const open=computed(()=>p.open);
  return <Dialog testID={p.testID} open={open()} onOpenChange={({open})=>{if(!open)p.onClose();}} onCancel={p.onClose} animation="fade" style={{marginHorizontal:"auto",marginVertical:"auto",width:Math.min(p.width??560,windowSize().width-48),maxHeight:windowSize().height-48,padding:0,gap:0,backgroundColor:colors().surface,borderRadius:RADIUS.dialog,borderWidth:1,borderColor:colors().border,boxShadow:[{x:0,y:24,blur:60,color:"#0000005c"}]}} backdropStyle={{backgroundColor:"#0000004d"}}>
    <Row justify="space-between" gap={12} style={{paddingHorizontal:18,paddingTop:14,paddingBottom:12}}><Text style={{fontSize:15,fontWeight:"600",letterSpacing:-0.15,color:colors().text,numberOfLines:1,textOverflow:"ellipsis"}}>{p.title}</Text><Action quiet label={tr("关闭","Close")} onPress={p.onClose}/></Row>
    <Scroll style={{maxHeight:p.height??windowSize().height-210}} contentStyle={{paddingHorizontal:18,paddingBottom:16,gap:12}}>{p.children}</Scroll>
    <If when={!!p.error}><View style={{paddingHorizontal:18,paddingBottom:12}}><Feedback text={p.error??""} danger/></View></If>
    <If when={!!p.footer}><View style={{paddingHorizontal:18,paddingVertical:12,borderTopWidth:1,borderColor:colors().border}}>{p.footer?.()}</View></If>
  </Dialog>;
}
export function Stat(p:{label:string;value:string}) {return <View style={{flexDirection:"row",alignItems:"baseline",gap:5}}><Label text={p.label} muted size={TYPE.caption}/><Label text={p.value} size={TYPE.caption} weight={600} tabular/></View>;}
export function fmtBytes(value:number):string {let n=Math.max(0,value||0),i=0;const units=["B","KB","MB","GB","TB"];while(n>=1024&&i<4){n/=1024;i++;}return `${n.toFixed(i?1:0)} ${units[i]}`;}
export function fmtRate(value:number):string {return `${fmtBytes(value)}/s`;}
export function fmtTime(value:string|number):string {if(!value)return "—";const d=new Date(value);return Number.isNaN(d.getTime())?String(value):d.toLocaleString();}
export function groupTypeLabel(kind:string):string {switch(String(kind).toLowerCase()){case "selector":case "select":return tr("手动选择","Manual");case "urltest":case "url-test":return tr("自动选择","Auto");case "fallback":return tr("故障转移","Fallback");case "loadbalance":case "load-balance":return tr("负载均衡","Load balance");case "relay":return tr("链式代理","Relay");default:return kind;}}
export function modeLabel(mode:string):string {switch(mode){case "global":return tr("全局","Global");case "direct":return tr("直连","Direct");default:return tr("规则","Rule");}}
