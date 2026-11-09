import { cleanup, effect, Image, keymap, platform, signal, Text, View, windowSize } from 'nsh';
import type { ViewRef } from 'nsh';
import { Command, Popover } from 'nsh/base';
import type { CommandItem, CommandRef } from 'nsh/base';
import { colors } from './theme';
import chevron from '../../assets/icons/chevron-down.svg';
import check from '../../assets/icons/check.svg';

// Catalyst is an iOS platform to NSH: Select opens a phone sheet there.
// A desktop selector always anchors its native list box beside the trigger.
export function Select(p:{items:{value:string;title:string}[];value:string;onChange:(v:string)=>void;label?:string;disabled?:boolean;testID?:string}) {
  const open=signal(false),width=signal(116),items=signal<CommandItem[]>([]);
  let signature='',trigger:ViewRef|undefined,alive=true;
  cleanup(()=>{alive=false;});
  effect(()=>{
    const next=p.items.map(item=>({id:item.value,label:item.title,checked:item.value===p.value}));
    const key=JSON.stringify(next);
    // Snapshots arrive each second. Identical options must not rebuild the
    // native rows, reset their highlight or replay an opening animation.
    if(key!==signature){signature=key;items.set(next);}
  });
  effect(()=>{if(p.disabled)open.set(false);});
  const close=()=>open.set(false);
  return <Popover open={open()} onOpenChange={({open:next})=>open.set(next&&!p.disabled)} placement="bottom" align="end" offset={4} margin={12}
    popupStyle={{minWidth:Math.max(148,width()),maxWidth:Math.min(340,windowSize().width-32),padding:4,borderWidth:1,borderColor:colors().border,borderRadius:8,backgroundColor:colors().surface}}
    onClose={()=>{if(alive&&!p.disabled&&(platform.os==='macos'||platform.os==='web'))trigger?.focus();}}
    trigger={<View testID={p.testID} ref={node=>{trigger=node;}} role="combobox" accessibilityLabel={p.label} accessibilityValue={{text:p.items.find(i=>i.value===p.value)?.title??p.value}} accessibilityState={{expanded:open(),disabled:!!p.disabled}} disabled={p.disabled}
      onLayout={({bounds})=>{if(bounds.size.width>0&&Math.abs(width()-bounds.size.width)>.5)width.set(bounds.size.width);}}
      style={{minHeight:30,minWidth:116,maxWidth:Math.min(320,windowSize().width-64),paddingHorizontal:10,paddingVertical:5,borderRadius:8,borderWidth:1,borderColor:open()?colors().selectedBorder:colors().border,backgroundColor:colors().surface,flexDirection:'row',alignItems:'center',justifyContent:'space-between',gap:12,opacity:p.disabled ? .45 : 1}}
      hoverStyle={{backgroundColor:colors().hover}} focusStyle={{outlineWidth:2,outlineColor:colors().accent,outlineOffset:2}}>
      <Text style={{fontSize:12,color:colors().text,numberOfLines:1,flexShrink:1}}>{p.items.find(i=>i.value===p.value)?.title??p.value}</Text>
      <Image src={chevron} style={{width:14,height:14,tintColor:colors().muted}}/>
    </View>}>
    <If when={open()}><SelectOptions items={items} value={p.value} label={p.label} testID={p.testID} onClose={close} onChoose={id=>{close();if(id!==p.value)p.onChange(id);}}/></If>
  </Popover>;
}


function SelectOptions(p:{items:()=>CommandItem[];value:string;label?:string;testID?:string;onChoose:(id:string)=>void;onClose:()=>void}) {
  let list:CommandRef|undefined,current=p.value,focusFrame=0,prefix='',typedAt=0;
  cleanup(()=>cancelAnimationFrame(focusFrame));
  const highlight=(id:string)=>{current=id;list?.highlight(id);};
  const move=(delta:number)=>{const rows=p.items();if(!rows.length)return;const at=Math.max(0,rows.findIndex(v=>v.id===current));highlight(rows[(at+delta+rows.length)%rows.length].id);};
  // Native focus on a generic view is unavailable on Catalyst. Bind only for
  // the lifetime of the popup, keeping keyboard behavior identical on desktop hosts.
  keymap.bind('down',()=>move(1));keymap.bind('up',()=>move(-1));
  keymap.bind('home',()=>{const first=p.items()[0];if(first)highlight(first.id);});
  keymap.bind('end',()=>{const rows=p.items();if(rows.length)highlight(rows[rows.length-1].id);});
  keymap.bind('enter',()=>p.onChoose(current));keymap.bind('space',()=>p.onChoose(current));
  keymap.bind('escape',p.onClose);
  for(const key of 'abcdefghijklmnopqrstuvwxyz0123456789')keymap.bind(key,()=>{const now=Date.now();prefix=(now-typedAt<600?prefix:'')+key;typedAt=now;const row=p.items().find(v=>v.label.toLowerCase().startsWith(prefix));if(row)highlight(row.id);});
  return (
<Command testID={p.testID?`${p.testID}-options`:undefined} ref={node=>{list=node;focusFrame=requestAnimationFrame(()=>{list?.highlight(p.value);if(platform.os==='macos'||platform.os==='web')list?.focus();});}} items={p.items()} searchable={false} accessibilityLabel={p.label} onHighlightChange={({highlighted})=>{if(highlighted)current=highlighted;}}
      listStyle={{maxHeight:Math.min(264,windowSize().height-80)}}
      itemClass={`focus:bg-[${colors().hover}] checked:bg-[${colors().selected}]`}
      itemStyle={{color:colors().text,fontSize:12.5,paddingHorizontal:8,paddingVertical:7,borderRadius:5}}
      renderItem={item=><View style={{flexDirection:'row',alignItems:'center',gap:12}}><Text tooltip={item.label} style={{flex:1,minWidth:0,fontSize:12.5,color:item.checked?colors().accent:colors().text,numberOfLines:1}}>{item.label}</Text><View style={{width:14,height:14}}><If when={item.checked}><Image src={check} style={{width:14,height:14,tintColor:colors().accent}}/></If></View></View>}
      onSelect={({id})=>p.onChoose(id)} onCancel={p.onClose}/>
  );
}
