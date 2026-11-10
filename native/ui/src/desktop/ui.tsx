import { computed, Image, Scroll, Text, View, windowSize } from "nsh";
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
// The system font supplies its own line metrics. Medium labels, semibold
// headings, captions and figures are separate roles so pages share one scale.
export const TYPE={title:20,section:12,body:13,secondary:11.5,caption:11,metric:26,mono:12} as const;
export const CONTROL_HEIGHT=28;
export const ROW_HEIGHT=44;
export function Label(p:{text:string;muted?:boolean;size?:number;weight?:number;bold?:boolean;color?:string;mono?:boolean;lines?:number}) {return <Text style={{fontSize:p.size??TYPE.body,fontWeight:p.weight??(p.bold?600:400),fontFamily:p.mono?"monospace":"system-ui",color:p.color??(p.muted?colors().muted:colors().text),numberOfLines:p.lines,textOverflow:p.lines===1?"ellipsis":undefined}}>{p.text}</Text>;}
/**
 * One button geometry for the whole app: 28 high, 6 radius. Primary carries
 * the accent, secondary a hairline, quiet is text-only inside rows. Destructive
 * actions are red text rather than red fills, as on macOS.
 */
export function Action(p:{label:string;onPress:()=>void;primary?:boolean;danger?:boolean;quiet?:boolean;disabled?:boolean;testID?:string;bold?:boolean}) {
 const filled=!!p.primary, plain=!filled&&(p.quiet||p.danger);
 const fg=filled?colors().accentText:p.danger?colors().danger:colors().text;
 return <Button testID={p.testID} onPress={p.onPress} disabled={p.disabled} style={{height:CONTROL_HEIGHT,minHeight:CONTROL_HEIGHT,paddingHorizontal:plain?8:11,borderRadius:6,alignItems:"center",justifyContent:"center",borderWidth:filled||plain?0:1,borderColor:colors().border,backgroundColor:filled?colors().accent:plain?"transparent":colors().surface}}
  hoverStyle={{backgroundColor:filled?colors().accentHover:colors().hover}}
  activeStyle={{backgroundColor:filled?colors().accentPressed:colors().selected,opacity:filled?1:0.9}}
  focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:2}}
  disabledStyle={{opacity:0.4}}><Text style={{fontSize:12.5,fontWeight:p.bold||filled?"600":"500",color:fg,numberOfLines:1}}>{p.label}</Text></Button>;
}
export function IconButton(p:{icon:string;label:string;onPress:()=>void;disabled?:boolean;testID?:string;flat?:boolean;size?:number}) {return <Button testID={p.testID} accessibilityLabel={p.label} disabled={p.disabled} onPress={p.onPress} style={{width:p.size??CONTROL_HEIGHT,height:p.size??CONTROL_HEIGHT,alignItems:"center",justifyContent:"center",borderRadius:6,backgroundColor:"transparent"}} hoverStyle={{backgroundColor:colors().hover}} activeStyle={{backgroundColor:colors().selected}} focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:1}} disabledStyle={{opacity:0.35}}><Icon name={p.icon} size={15}/></Button>;}
export function Panel(p:{children?:JSX.Element;padding?:number;gap?:number;style?:any}) {return <View style={{padding:p.padding??16,gap:p.gap??12,borderRadius:10,backgroundColor:colors().surface,...p.style}}>{p.children}</View>;}
/**
 * Grouped rows: the container paints the hairline, rows sit on the surface and
 * one-point gaps become separators. No border per row, no nested cards.
 */
export function Group(p:{children?:JSX.Element;style?:any}) {return <View style={{borderRadius:10,backgroundColor:colors().border,overflow:"hidden",gap:1,...p.style}}>{p.children}</View>;}
export function GroupRow(p:{children?:JSX.Element;gap?:number;style?:any;hover?:boolean}) {return <View hoverStyle={p.hover?{backgroundColor:colors().hover}:undefined} style={{flexDirection:"row",alignItems:"center",minHeight:ROW_HEIGHT,paddingHorizontal:14,paddingVertical:9,gap:p.gap??12,backgroundColor:colors().surface,...p.style}}>{p.children}</View>;}
export function Row(p:{children?:JSX.Element;gap?:number;justify?:"space-between"|"flex-end"|"flex-start"|"center";wrap?:boolean;style?:any}) {return <View style={{flexDirection:"row",alignItems:"center",gap:p.gap??8,justifyContent:p.justify,flexWrap:p.wrap?"wrap":"nowrap",...p.style}}>{p.children}</View>;}
export function Column(p:{children?:JSX.Element;gap?:number;flex?:number}) {return <View style={{gap:p.gap??8,flex:p.flex}}>{p.children}</View>;}
export function Heading(p:{title:string;description:string;action?:()=>JSX.Element}) {return <View style={{gap:2}}><Row justify="space-between" style={{minHeight:CONTROL_HEIGHT}}><Text role="header" style={{fontSize:TYPE.title,fontWeight:"600",letterSpacing:-0.3,color:colors().text,numberOfLines:1}}>{p.title}</Text>{p.action?.()}</Row><Label text={p.description} muted size={TYPE.section}/></View>;}
/** A quiet group label above a grouped surface; the page title stays the loudest element. */
export function Section(p:{title:string;subtitle?:string}) {return <Row justify="space-between" style={{paddingTop:2,paddingHorizontal:2}}><Label text={p.title} weight={600} size={TYPE.section} muted/><If when={!!p.subtitle}><Label text={p.subtitle??""} muted size={TYPE.caption}/></If></Row>;}
export function Field(p:{accessibilityLabel?:string;label?:string;value:string;onChange:(value:string)=>void;placeholder?:string;multiline?:boolean;height?:number;readonly?:boolean;secret?:boolean;testID?:string}) {return <View style={{gap:5}}><If when={!!p.label}><Label text={p.label??""} size={TYPE.section} weight={500} muted/></If><Input testID={p.testID} accessibilityLabel={p.accessibilityLabel??p.label} value={p.value} onChange={({value})=>p.onChange(value)} placeholder={p.placeholder} multiline={p.multiline} readonly={p.readonly} masked={p.secret} autoCorrect={false} autoCapitalize="none" style={{height:p.height??(p.multiline?140:30),minHeight:30,paddingHorizontal:9,paddingVertical:6,borderRadius:6,borderWidth:1,borderColor:colors().border,backgroundColor:colors().bg,color:colors().text,placeholderColor:colors().faint,fontSize:p.multiline?TYPE.mono:TYPE.body,fontFamily:p.multiline?"monospace":"system-ui"}}/></View>;}
export function SearchField(p:{value:string;onChange:(value:string)=>void;placeholder:string;testID?:string;label?:string}) {return <View style={{height:30,minHeight:30,flexDirection:"row",alignItems:"center",gap:6,paddingLeft:8,paddingRight:3,borderRadius:6,borderWidth:1,borderColor:colors().border,backgroundColor:colors().bg}}><Icon name="search" size={14} color={colors().faint}/><Input testID={p.testID} accessibilityLabel={p.label??p.placeholder} value={p.value} onChange={({value})=>p.onChange(value)} placeholder={p.placeholder} autoCorrect={false} autoCapitalize="none" style={{flex:1,minWidth:0,height:28,paddingHorizontal:0,paddingVertical:0,borderWidth:0,backgroundColor:"transparent",color:colors().text,placeholderColor:colors().faint,fontSize:12.5}}/><If when={!!p.value}><Button testID={p.testID?`${p.testID}-clear`:undefined} accessibilityLabel={tr("清空搜索","Clear search")} onPress={()=>p.onChange("")} style={{width:24,height:24,alignItems:"center",justifyContent:"center",borderRadius:5}} hoverStyle={{backgroundColor:colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><Icon name="x" size={13}/></Button></If></View>;}
export function Toggle(p:{checked:boolean;onChange:(v:boolean)=>void;label?:string;disabled?:boolean;testID?:string;showLabel?:boolean}) {return <Row><If when={!!p.label&&p.showLabel!==false}><Label text={p.label??""}/></If><Switch testID={p.testID} checked={p.checked} disabled={p.disabled} accessibilityLabel={p.label} onChange={({checked})=>p.onChange(checked)} style={{width:36,height:21,opacity:p.disabled?0.4:1}} trackStyle={{width:36,height:21,padding:2,backgroundColor:p.checked?colors().accent:colors().border,borderRadius:11}} thumbStyle={{width:17,height:17,borderRadius:9,backgroundColor:"#ffffff",transform:[{translateX:p.checked?15:0}]}}/></Row>;}
export function Tabs(p:{items:{id:string;label:string}[];value:string;onChange:(v:string)=>void;disabled?:boolean;testIDPrefix?:string}) {return <NativeTabs style={{flexDirection:"row",alignSelf:"flex-start",gap:2,padding:2,borderRadius:7,backgroundColor:colors().surface}} indicatorStyle={{top:2,bottom:2,borderRadius:5,backgroundColor:colors().selected}}><Each of={p.items} key={(item)=>item.id}>{(item)=><Tab testID={`${p.testIDPrefix??"desktop-tab"}-${item().id}`} selected={p.value===item().id} disabled={p.disabled} onPress={()=>p.onChange(item().id)} style={{paddingHorizontal:10,paddingVertical:3,minHeight:24,borderRadius:5,backgroundColor:"transparent"}} hoverStyle={{backgroundColor:p.value===item().id?"transparent":colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent}}><Text style={{fontSize:TYPE.section,fontWeight:p.value===item().id?"600":"400",color:p.value===item().id?colors().accent:colors().muted}}>{item().label}</Text></Tab>}</Each></NativeTabs>;}
export { Select } from "./select";
export function Feedback(p:{text:string;danger?:boolean}) {return <View role="alert" style={{paddingHorizontal:12,paddingVertical:8,borderRadius:8,backgroundColor:p.danger?colors().danger+"1f":colors().hover}}><Label text={p.text} color={p.danger?colors().danger:colors().muted} size={TYPE.section}/></View>;}
export function Empty(p:{title:string;description?:string;action?:()=>JSX.Element;icon?:string}) {return <View style={{flex:1,minHeight:160,padding:24,gap:8,alignItems:"center",justifyContent:"center"}}><If when={!!p.icon}><View style={{width:38,height:38,alignItems:"center",justifyContent:"center",borderRadius:9,backgroundColor:colors().hover}}><Icon name={p.icon??"circle"} size={19} color={colors().faint}/></View></If><Label text={p.title} size={14} bold/><If when={!!p.description}><Text style={{fontSize:TYPE.section,color:colors().muted,maxWidth:380,textAlign:"center",lineHeight:17}}>{p.description}</Text></If><If when={!!p.action}><View style={{paddingTop:4}}>{p.action?.()}</View></If></View>;}
export function Modal(p:{title:string;open:boolean;onClose:()=>void;children?:JSX.Element;footer?:()=>JSX.Element;width?:number;height?:number;error?:string;testID?:string}) {
  // Dialog recreates its native layer whenever its open dependency changes.
  // Deduplicate visibility so fresh snapshots update content without reopening.
  const open=computed(()=>p.open);
  return <Dialog testID={p.testID} open={open()} onOpenChange={({open})=>{if(!open)p.onClose();}} onCancel={p.onClose} animation="fade" style={{marginHorizontal:"auto",marginVertical:"auto",width:Math.min(p.width??560,windowSize().width-48),maxHeight:windowSize().height-48,padding:18,gap:12,backgroundColor:colors().surface,borderRadius:12,borderWidth:1,borderColor:colors().border,boxShadow:[{x:0,y:18,blur:48,color:"#00000059"}]}} backdropStyle={{backgroundColor:"#00000059"}}><Row justify="space-between"><Label text={p.title} size={16} bold/><Action quiet label={tr("关闭","Close")} onPress={p.onClose}/></Row><Scroll style={{maxHeight:p.height??windowSize().height-210}} contentStyle={{gap:12}}>{p.children}</Scroll><If when={!!p.error}><Feedback text={p.error??""} danger/></If>{p.footer?.()}</Dialog>;
}
export function Stat(p:{label:string;value:string}) {return <View style={{flexDirection:"row",alignItems:"baseline",gap:5}}><Label text={p.label} muted size={TYPE.caption}/><Label text={p.value} size={TYPE.caption} weight={500}/></View>;}
export function fmtBytes(value:number):string {let n=Math.max(0,value||0),i=0;const units=["B","KB","MB","GB","TB"];while(n>=1024&&i<4){n/=1024;i++;}return `${n.toFixed(i?1:0)} ${units[i]}`;}
export function fmtRate(value:number):string {return `${fmtBytes(value)}/s`;}
export function fmtTime(value:string|number):string {if(!value)return "—";const d=new Date(value);return Number.isNaN(d.getTime())?String(value):d.toLocaleString();}
export function groupTypeLabel(kind:string):string {switch(String(kind).toLowerCase()){case "selector":case "select":return tr("手动选择","Manual");case "urltest":case "url-test":return tr("自动选择","Auto");case "fallback":return tr("故障转移","Fallback");case "loadbalance":case "load-balance":return tr("负载均衡","Load balance");case "relay":return tr("链式代理","Relay");default:return kind;}}
export function modeLabel(mode:string):string {switch(mode){case "global":return tr("全局","Global");case "direct":return tr("直连","Direct");default:return tr("规则","Rule");}}
