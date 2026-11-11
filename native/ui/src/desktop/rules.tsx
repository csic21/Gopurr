import { effect, List, Scroll, Text, View, signal, windowSize } from 'nsh';
import type { PageProps } from './types';
import { Action, Column, Empty, Feedback, Field, Heading, Label, Modal, Panel, Row, SearchField, Select, Tabs, Toggle, TYPE } from './ui';
import { colors } from './theme';
import { watchDesktopDraft } from './drafts';
import { nativeCall } from './client';
import { tr } from './i18n';
import { FeatureEditor, SaveBar, splitLines } from './settings';

const copy=<T,>(v:T):T=>JSON.parse(JSON.stringify(v));
const message=(e:unknown)=>e instanceof Error?e.message:String(e);
export function filterRules(rules:any[],query:string,target:string){const q=query.trim().toLowerCase();return rules.map((rule,index)=>({rule,index})).filter(({rule})=>(!target||String(rule.adapter??rule.Adapter)===target)&&(!q||[rule.type??rule.Type,rule.payload??rule.Payload,rule.adapter??rule.Adapter].join(' ').toLowerCase().includes(q)));}
export function composeRule(kind:string,value:string,target:string){if(!value.trim()||!target.trim()||[kind,value,target].some(v=>/[,\r\n]/.test(v)))throw Error(tr('匹配值与出口不能为空，也不能包含逗号或换行','Value and target are required and cannot contain commas or newlines'));return `${kind},${value.trim()},${target.trim()}`;}

const compact=()=>windowSize().height<660||windowSize().width<960;
const PAD=()=>compact()?16:24;
const padBottom=()=>compact()?12:20;
const ORDER_COL=40,TYPE_COL=120,TARGET_COL=150,ROW=48;
/** Column micro labels: the table's rhythm, never its data. */
function Micro(p:{text:string;align?:"left"|"right"}) { return <Text style={{fontSize:TYPE.micro,fontWeight:"500",letterSpacing:0.3,color:colors().faint,textAlign:p.align,numberOfLines:1}}>{p.text}</Text>; }
/** A page-level list surface: one card, radius and lift from the shared tokens. */
function Card(p:{children?:any;style?:any}) { return <Panel padding={0} gap={0} style={{overflow:"hidden",...p.style}}>{p.children}</Panel>; }

  const tab=signal('effective'),query=signal(''),adapter=signal(''),scope=signal(''),scopeData=signal<any>(null),loading=signal(false),err=signal(''),notice=signal(''),saving=signal(false);
  const extra=signal(''),yaml=signal(''),extraBase=signal(''),yamlBase=signal(''),kind=signal('DOMAIN-SUFFIX'),value=signal(''),target=signal('DIRECT');
  const apps=signal<any[]>([]),appsBase=signal('[]'),appsReady=signal(false),appQuery=signal(''),picker=signal(false),catalog=signal<any[]>([]),scanning=signal(false),pickerQuery=signal(''),appEdit=signal<any>(null),appIndex=signal(-1);
  const libraryOpen=signal(false),libraryTab=signal('library'),library=signal<any[]>([]),libraryBase=signal('[]'),ids=signal<string[]>([]),idsBase=signal('[]'),templateID=signal(''),templateName=signal(''),templateYAML=signal(''),deleteTemplate=signal(''),libraryClose=signal(false);
  const drafts=new Map<string,{extra:string;yaml:string;extraBase:string;yamlBase:string}>();let loadedScope='\u0000',seq=0;

export function RulesPage(p:PageProps){
  watchDesktopDraft('rules',()=>extra()!==extraBase()||yaml()!==yamlBase()||JSON.stringify(apps())!==appsBase()||!!appEdit()||!!value().trim()||libraryOpen()&& (JSON.stringify(library())!==libraryBase()||JSON.stringify(ids())!==idsBase()||templateName()!==(library().find(v=>v.id===templateID())?.name??'')||templateYAML()!==(library().find(v=>v.id===templateID())?.yaml??''))||[...drafts.entries()].some(([id,v])=>id!==loadedScope&&(v.extra!==v.extraBase||v.yaml!==v.yamlBase)));
  const profiles=()=>p.snapshot().profiles??[];const rules=()=>p.snapshot().effectiveRules??[];
  const groups=()=>[...new Set<string>(['DIRECT','REJECT',...(p.snapshot().groups??[]).map((v:any)=>v.name),...rules().map((v:any)=>v.adapter??v.Adapter).filter(Boolean)])];
  const visible=()=>filterRules(rules(),query(),adapter());
  async function loadScope(id:string){const current=++seq;loading.set(true);err.set('');notice.set('');try{const s=await p.call<any>('desktop.snapshot',{page:'rules',scopeId:id});if(current!==seq)return;scopeData.set(s);const cached=drafts.get(id);extra.set(cached?.extra??(s.scope?.extraRules??[]).join('\n'));yaml.set(cached?.yaml??s.scope?.mergeYaml??'');extraBase.set(cached?.extraBase??extra());yamlBase.set(cached?.yamlBase??yaml());}catch(e){if(current===seq)err.set(message(e));}finally{if(current===seq)loading.set(false);}}
  effect(()=>{const id=scope();if(id===loadedScope)return;if(loadedScope!=='\u0000')drafts.set(loadedScope,{extra:extra(),yaml:yaml(),extraBase:extraBase(),yamlBase:yamlBase()});loadedScope=id;void loadScope(id);});
  effect(()=>{if(appsReady()||!p.snapshot().appRules)return;apps.set(copy(p.snapshot().appRules??[]));appsBase.set(JSON.stringify(apps()));appsReady.set(true);});
  async function work(fn:()=>Promise<void>){if(saving())return;saving.set(true);err.set('');notice.set('');try{await fn();await p.refresh();}catch(e){err.set(message(e));}finally{saving.set(false);}}
  async function scan(){scanning.set(true);err.set('');try{const list=await p.call<any[]>('desktop.applications.scan');catalog.set(Array.isArray(list)?list:[]);}catch(e){err.set(message(e));}finally{scanning.set(false);}}
  function editApp(application:any,index=-1){appEdit.set(index<0?{application,target:'DIRECT',includeHelpers:application.path?.endsWith('.app')??false}:copy(apps()[index]));appIndex.set(index);picker.set(false);err.set('');}
  async function chooseApp(){const r=await nativeCall<any>('desktop.file.open',{kind:'application'});if(r.path){const application=await p.call<any>('desktop.applications.inspect',{path:r.path});editApp(application);}}
  function commitApp(){if(!appEdit()?.application?.path||!appEdit()?.target)return;const list=copy(apps());const index=appIndex()<0?list.findIndex(v=>v.application.path===appEdit().application.path):appIndex();if(index<0)list.push(copy(appEdit()));else list[index]=copy(appEdit());apps.set(list);appEdit.set(null);}
  async function openLibrary(){err.set('');const s=await p.call<any>('desktop.snapshot',{page:'rules',scopeId:scope()});library.set(copy(s.library??[]));libraryBase.set(JSON.stringify(library()));templateName.set('');templateYAML.set('');ids.set([...(s.scope?.templateIds??[])]);idsBase.set(JSON.stringify(ids()));templateID.set('');libraryOpen.set(true);}
  function pickTemplate(id:string){const v=library().find(v=>v.id===id);templateID.set(id);templateName.set(v?.name??'');templateYAML.set(v?.yaml??'');}
  function updateTemplate(){if(!templateName().trim()){err.set(tr('请输入模板名称','Enter a template name'));return;}const id=templateID()||`template-${Date.now().toString(16)}-${Math.random().toString(16).slice(2,10)}`;const next={id,name:templateName().trim(),yaml:templateYAML()};library.set([...library().filter(v=>v.id!==id),next]);templateID.set(id);err.set('');}
  function moveTemplate(index:number,direction:number){const next=[...ids()];if(index+direction<0||index+direction>=next.length)return;[next[index],next[index+direction]]=[next[index+direction],next[index]];ids.set(next);}
  const templateDirty=()=>{const current=library().find(v=>v.id===templateID());return templateName()!==(current?.name??'')||templateYAML()!==(current?.yaml??'');};
  const currentLibrary=()=>{if(!templateDirty())return library();if(!templateName().trim())throw Error(tr('请输入模板名称','Enter a template name'));const id=templateID()||`template-${Date.now().toString(16)}-${Math.random().toString(16).slice(2,10)}`;return [...library().filter(v=>v.id!==id),{id,name:templateName().trim(),yaml:templateYAML()}];};
  const closeLibrary=()=>{if(JSON.stringify(library())!==libraryBase()||JSON.stringify(ids())!==idsBase()||templateDirty())libraryClose.set(true);else libraryOpen.set(false);};
  const scopeName=()=>scope()?profiles().find((v:any)=>v.id===scope())?.name??scope():tr('全局','Global');
  const selectedCatalog=()=>catalog().filter(v=>[v.name,v.path,v.executable].join(' ').toLowerCase().includes(pickerQuery().toLowerCase()));
  const filteredApps=()=>apps().map((rule,index)=>({rule,index})).filter(({rule})=>[rule.application?.name,rule.application?.path,rule.target].join(' ').toLowerCase().includes(appQuery().toLowerCase()));
  return <View style={{flex:1}}>
    <View style={{paddingHorizontal:PAD(),paddingTop:compact()?10:20,paddingBottom:12,gap:compact()?10:14}}>
      <Heading title={tr('规则','Rules')} description={rules().length+' '+tr('条生效 · 按顺序匹配，命中即停','active rules · first matching rule wins')}/>
      <Tabs value={tab()} onChange={v=>{tab.set(v);err.set('');notice.set('');}} items={[{id:'effective',label:tr('生效规则','Effective rules')},{id:'apps',label:tr('应用分流','Applications')},{id:'extra',label:tr('额外规则','Extra rules')},{id:'yaml',label:tr('YAML 覆写','YAML overrides')},{id:'dns',label:'DNS'}]}/>
      <If when={['extra','yaml','dns'].includes(tab())}><Row justify="space-between" gap={12}><Row gap={8}><Micro text={tr('作用范围','Scope')}/><Select label={tr('作用范围','Scope')} disabled={saving()||loading()} value={scope()} onChange={v=>scope.set(v)} items={[{value:'',title:tr('全局','Global')},...profiles().map((v:any)=>({value:v.id,title:v.name}))]}/></Row><Label text={tr('订阅设置优先于全局；草稿按范围保留','Profile overrides take priority; drafts are retained by scope')} muted size={11}/></Row></If>
      <If when={!!err()&&!picker()&&!appEdit()&&!libraryOpen()}><Feedback text={err()} danger/></If>
      <If when={!!notice()}><Feedback text={notice()}/></If>
    </View>
    <If when={tab()==='effective'}>
      <View style={{paddingHorizontal:PAD(),gap:compact()?10:14,flex:1,paddingBottom:padBottom()}}>
        <Row gap={10}>
          <View style={{width:compact()?260:320,maxWidth:"100%"}}><SearchField testID="rules-search" value={query()} onChange={v=>query.set(v)} placeholder={tr('搜索域名、IP 或规则类型…','Search domain, IP or rule type…')}/></View>
          <View style={{flex:1,minWidth:0}}/>
          <Select value={adapter()} onChange={v=>adapter.set(v)} items={[{value:'',title:tr('全部出口','All targets')},...groups().map(value=>({value,title:value}))]}/>
        </Row>
        <Label text={tr('显示','Showing')+' '+visible().length+' / '+rules().length+' · '+tr('出口是规则命中后交给的策略','Target is the strategy used after a match')} muted size={11}/>
        <Card style={{flex:1,minHeight:0}}>
          <Row style={{height:32,paddingHorizontal:16,gap:12,alignItems:"center",borderBottomWidth:1,borderColor:colors().border}}>
            <View style={{width:ORDER_COL,alignItems:"flex-end"}}><Micro text={tr('顺序','Order')} align="right"/></View>
            <View style={{flex:1,minWidth:0}}><Micro text={tr('匹配条件','Match')}/></View>
            <View style={{width:TYPE_COL}}><Micro text={tr('类型','Type')}/></View>
            <View style={{width:TARGET_COL}}><Micro text={tr('出口','Target')}/></View>
          </Row>
          <If when={visible().length>0}>
            <List each={visible()} keyExtractor={(item:any)=>String(item.index)} style={{flex:1}} separator={<View style={{height:1,backgroundColor:colors().border}}/>}>{(item)=>
              <View style={{flexDirection:'row',alignItems:'center',minHeight:ROW,paddingVertical:10,paddingHorizontal:16,gap:12,transitionProperty:"background-color",transitionDuration:140}} hoverStyle={{backgroundColor:colors().hover}}>
                <View style={{width:ORDER_COL,alignItems:'flex-end'}}><Label text={String(item().index+1)} faint size={10.5} mono/></View>
                <View style={{flex:1,minWidth:0}}><Label text={item().rule.payload||item().rule.Payload||tr('全部流量','All traffic')} tooltip={item().rule.payload||item().rule.Payload||tr('全部流量','All traffic')} lines={1}/></View>
                <View style={{width:TYPE_COL}}><Label text={item().rule.type??item().rule.Type??''} muted size={11} mono lines={1}/></View>
                <View style={{width:TARGET_COL}}><Label text={item().rule.adapter??item().rule.Adapter??''} color={colors().accent} size={12} lines={1} tooltip={item().rule.adapter??item().rule.Adapter??''}/></View>
              </View>}
            </List>
          </If>
          <If when={!visible().length}><Empty icon={rules().length?"search":"rules"} title={rules().length?tr('没有匹配的规则','No matching rules'):tr('暂无生效规则','No active rules')} description={rules().length?tr('尝试其他关键词或出口','Try another query or target'):tr('启动内核后查看当前配置的匹配顺序','Start the core to view the matching order')} action={()=>rules().length?<Action label={tr('重置筛选','Reset filters')} onPress={()=>{query.set('');adapter.set('');}}/>:null}/></If>
        </Card>
      </View>
    </If>
    <If when={tab()==='apps'}>
      <View style={{flex:1}}>
        <Scroll style={{flex:1}} contentStyle={{paddingHorizontal:PAD(),paddingBottom:12,gap:12}}>
          <Row justify="space-between" gap={12}>
            <Label text={tr('按应用完整路径分流，优先于额外规则','Route by full application path, before extra rules')} muted size={12}/>
            <Action primary label={tr('添加应用','Add application')} onPress={()=>{picker.set(true);pickerQuery.set('');void scan();}}/>
          </Row>
          <Row gap={10}>
            <View style={{width:compact()?260:320,maxWidth:"100%"}}><SearchField value={appQuery()} onChange={v=>appQuery.set(v)} placeholder={tr('搜索应用、路径或出口','Search app, path or target')}/></View>
            <View style={{flex:1,minWidth:0}}/>
          </Row>
          <Card>
            <If when={filteredApps().length>0}>
              <Each of={filteredApps()} key={(entry:any)=>entry.rule?.application?.path??String(entry.index)}>{(entry,index)=>
                <View style={{minHeight:52,paddingHorizontal:16,paddingVertical:10,gap:6,backgroundColor:colors().surface,borderTopWidth:index()>0?1:0,borderColor:colors().border,transitionProperty:"background-color",transitionDuration:140}} hoverStyle={{backgroundColor:colors().hover}}>
                  <Row justify="space-between" gap={12}>
                    <View style={{flex:1,minWidth:0,gap:3}}>
                      <Label text={entry().rule.application?.name??''} weight={500} size={13} lines={1}/>
                      <Label text={entry().rule.application?.path??''} faint mono size={11} lines={1} tooltip={entry().rule.application?.path??''}/>
                    </View>
                    <Label text={entry().rule.target} color={colors().accent} size={12} lines={1}/>
                    <Action quiet label={tr('编辑','Edit')} onPress={()=>editApp(entry().rule.application,entry().index)}/>
                    <Action quiet danger label={tr('移除','Remove')} onPress={()=>apps.set(apps().filter((_,i)=>i!==entry().index))}/>
                  </Row>
                  <If when={entry().rule.includeHelpers}><Label text={tr('包括辅助进程','Includes helper processes')} muted size={11}/></If>
                </View>}
              </Each>
            </If>
            <If when={!filteredApps().length}><Empty icon="search" title={apps().length?tr('没有匹配的应用','No matching applications'):tr('尚未添加应用','No application rules')} description={tr('选择已安装应用，或手动选择应用与可执行文件。','Choose an installed app, or select an app or executable manually.')}/></If>
          </Card>
        </Scroll>
        <View style={{paddingHorizontal:PAD(),paddingTop:compact()?8:10,paddingBottom:compact()?8:16}}><SaveBar floating dirty={JSON.stringify(apps())!==appsBase()} busy={saving()} onDiscard={()=>apps.set(JSON.parse(appsBase()))} onSave={()=>void work(async()=>{await p.call('desktop.rules.save',{appRules:apps()});appsBase.set(JSON.stringify(apps()));notice.set(tr('应用分流已应用','Application rules applied'));})}/></View>
      </View>
    </If>
    <If when={tab()==='extra'||tab()==='yaml'}>
      <View style={{flex:1}}>
        <Scroll style={{flex:1}} contentStyle={{paddingHorizontal:PAD(),paddingBottom:12,gap:12}}>
          <If when={loading()}><Label text={tr('正在读取…','Loading…')} muted/></If>
          <If when={!loading()&&scopeData()}>
            <If when={tab()==='extra'}>
              <Panel style={{gap:12}}>
                <Row wrap gap={10} style={{alignItems:'flex-end'}}>
                  <View style={{width:compact()?150:158,gap:5}}>
                    <Micro text={tr('类型','Type')}/>
                    <Select value={kind()} onChange={v=>kind.set(v)} items={['DOMAIN-SUFFIX','DOMAIN','DOMAIN-KEYWORD','IP-CIDR','IP-CIDR6','PROCESS-NAME'].map(value=>({value,title:value}))}/>
                  </View>
                  <View style={{flex:1,minWidth:150,gap:5}}>
                    <Micro text={tr('匹配值','Value')}/>
                    <Field accessibilityLabel={tr('匹配值','Value')} value={value()} onChange={v=>value.set(v)} placeholder={tr('例如 example.com','For example example.com')}/>
                  </View>
                  <View style={{width:compact()?150:158,gap:5}}>
                    <Micro text={tr('出口','Target')}/>
                    <Select label={tr('出口','Target')} value={target()} onChange={v=>target.set(v)} items={groups().map(value=>({value,title:value}))}/>
                  </View>
                  <Action label={tr('添加规则','Add rule')} disabled={!value().trim()} onPress={()=>{try{const next=composeRule(kind(),value(),target());extra.set(extra()+(extra().trim()?'\n':'')+next);value.set('');err.set('');}catch(e){err.set(message(e));}}}/>
                </Row>
                <Field label={tr('额外规则 · 每行一条','Extra rules · one per line')} testID="extra-rules-editor" multiline height={compact()?170:220} value={extra()} onChange={v=>extra.set(v)} placeholder={'DOMAIN-SUFFIX,example.com,DIRECT'}/>
                <Label text={tr('空行和 # 注释会忽略。应用分流优先，额外规则位于订阅规则之前。','Blank lines and # comments are ignored. Application rules run first, followed by these rules and profile rules.')} muted size={12}/>
              </Panel>
            </If>
            <If when={tab()==='yaml'}>
              <Panel style={{gap:12}}>
                <Row justify="space-between" gap={12}><Label text={scopeName()+' · '+tr('只写需要覆写的字段','Only include fields to override')} muted size={12}/><Action label={tr('模板库与应用顺序','Templates and order')} onPress={()=>void work(openLibrary)}/></Row>
                <Field testID="yaml-override-editor" multiline height={compact()?190:260} value={yaml()} onChange={v=>yaml.set(v)} placeholder={'profile:\n  store-selected: true\ndns:\n  nameserver:\n    - 1.1.1.1'}/>
                <Label text={tr('订阅原文件保持完整；监听端口与 TUN 等应用网络设置最后应用。','The original profile stays intact. App-owned port and TUN settings apply last.')} muted size={12}/>
              </Panel>
            </If>
          </If>
        </Scroll>
        <View style={{paddingHorizontal:PAD(),paddingTop:compact()?8:10,paddingBottom:compact()?8:16}}>
          <If when={tab()==='extra'}><SaveBar floating dirty={extra()!==extraBase()} busy={saving()} onDiscard={()=>extra.set(extraBase())} onSave={()=>void work(async()=>{await p.call('desktop.rules.save',{id:scope(),extraRules:splitLines(extra())});extraBase.set(extra());notice.set(tr('额外规则已应用','Extra rules applied'));})}/></If>
          <If when={tab()==='yaml'}><SaveBar floating dirty={yaml()!==yamlBase()} busy={saving()} onDiscard={()=>yaml.set(yamlBase())} onSave={()=>void work(async()=>{await p.call('desktop.rules.save',{id:scope(),mergeYaml:yaml()});yamlBase.set(yaml());notice.set(tr('YAML 覆写已应用','YAML overrides applied'));})}/></If>
        </View>
      </View>
    </If>
    <If when={tab()==='dns'}><View style={{flex:1,paddingHorizontal:PAD(),paddingBottom:padBottom()}}><FeatureEditor page={p} kind="dns" scopeId={scope}/></View></If>
    <Modal title={picker()?tr('选择应用','Choose application'):tr('编辑应用分流','Edit application rule')} open={picker()||!!appEdit()} onClose={()=>{if(picker())picker.set(false);else appEdit.set(null);}} error={err()} width={620} footer={()=>picker()?<Row justify="space-between"><Action label={tr('手动选择…','Choose manually…')} onPress={()=>void work(chooseApp)}/><Action label={tr('重新扫描','Scan again')} disabled={scanning()} onPress={()=>void scan()}/></Row>:<Row justify="flex-end"><Action label={tr('加入草稿','Add to draft')} primary onPress={commitApp}/></Row>}>
      <If when={picker()}><View style={{gap:12}}>
        <SearchField value={pickerQuery()} onChange={v=>pickerQuery.set(v)} placeholder={tr('搜索应用或路径','Search app or path')}/>
        <If when={scanning()}><Label text={tr('正在扫描已安装应用…','Scanning installed applications…')} muted/></If>
        <If when={!scanning()}><Card>
          <If when={selectedCatalog().length>0}>
            <Each of={selectedCatalog()} key={(app:any)=>app.path||app.name}>{(app,index)=>
              <View style={{minHeight:52,paddingHorizontal:16,paddingVertical:10,borderTopWidth:index()>0?1:0,borderColor:colors().border,backgroundColor:colors().surface,transitionProperty:"background-color",transitionDuration:140}} hoverStyle={{backgroundColor:colors().hover}}>
                <Row justify="space-between" gap={12}>
                  <View style={{flex:1,minWidth:0,gap:3}}>
                    <Label text={app().name} weight={500} size={13} lines={1}/>
                    <Label text={app().path} faint size={11} mono lines={1} tooltip={app().path}/>
                  </View>
                  <Action quiet label={tr('选择','Choose')} onPress={()=>editApp(app())}/>
                </Row>
              </View>}
            </Each>
          </If>
          <If when={!selectedCatalog().length}><Empty icon="search" title={tr('没有找到应用','No applications found')} description={tr('可以手动选择应用或可执行文件','Select an application or executable manually')}/></If>
        </Card></If>
      </View></If>
      <If when={!picker()&&!!appEdit()}><View style={{gap:14}}>
        <View style={{gap:3}}>
          <Label text={appEdit()?.application?.name??''} weight={600} size={15} lines={1}/>
          <Label text={appEdit()?.application?.path??''} faint mono size={11} lines={1} tooltip={appEdit()?.application?.path??''}/>
        </View>
        <View style={{gap:6}}>
          <Text style={{fontSize:12,fontWeight:'600',color:colors().muted,numberOfLines:1}}>{tr('出口策略','Target strategy')}</Text>
          <Select label={tr('出口策略','Target strategy')} value={appEdit()?.target??'DIRECT'} items={[...new Set([...groups(),appEdit()?.target].filter(Boolean))].map(value=>({value,title:value}))} onChange={v=>appEdit.set({...appEdit(),target:v})}/>
        </View>
        <If when={!!appEdit()?.target&&!groups().includes(appEdit()?.target)}><Feedback text={tr('此出口不在当前策略组中，请选择可用出口后保存。','This target is not in the current proxy groups. Choose an available target before saving.')} danger/></If>
        <If when={appEdit()?.application?.path?.endsWith('.app')}><Row justify="space-between"><Label text={tr('包括应用辅助进程','Include application helpers')} weight={500}/><Toggle showLabel={false} label={tr('包括应用辅助进程','Include application helpers')} checked={!!appEdit()?.includeHelpers} onChange={v=>appEdit.set({...appEdit(),includeHelpers:v})}/></Row></If>
        <Label text={tr('确认后加入页面草稿，再点击保存并应用。','Confirm adds the rule to the page draft. Save and apply when ready.')} muted/>
      </View></If>
    </Modal>
    <Modal title={`${tr('YAML 模板','YAML templates')} · ${scopeName()}`} open={libraryOpen()} onClose={closeLibrary} width={740} height={400} error={err()} footer={()=><Row justify="space-between"><Action label={tr('放弃更改','Discard')} disabled={saving()} onPress={()=>{library.set(JSON.parse(libraryBase()));ids.set(JSON.parse(idsBase()));libraryOpen.set(false);}}/><Action primary label={libraryTab()==='library'?tr('保存模板库','Save library'):tr('保存应用顺序','Save order')} disabled={saving()||(libraryTab()==='library'?JSON.stringify(library())===libraryBase()&&!templateDirty():JSON.stringify(ids())===idsBase())} onPress={()=>void work(async()=>{if(libraryTab()==='library'){const next=currentLibrary();await p.call('desktop.library.save',{library:next});library.set(next);libraryBase.set(JSON.stringify(next));if(templateName().trim()){const v=next.find(v=>v.name===templateName().trim());if(v)templateID.set(v.id);}}else{await p.call('desktop.rules.save',{id:scope(),templateIds:ids()});idsBase.set(JSON.stringify(ids()));}notice.set(tr('模板设置已保存','Template settings saved'));})}/></Row>}><Tabs value={libraryTab()} onChange={v=>libraryTab.set(v)} items={[{id:'library',label:tr('模板库','Library')},{id:'order',label:tr('应用顺序','Apply order')}]}/><If when={libraryTab()==='library'}>
        <Row wrap gap={8} style={{alignItems:'center'}}>
          <Select value={templateID()} onChange={pickTemplate} items={[{value:'',title:tr('新建模板','New template')},...library().map(v=>({value:v.id,title:v.name}))]}/>
          <Action label={tr('新建','New')} onPress={()=>pickTemplate('')}/>
          <If when={!!templateID()}><Action danger label={tr('删除','Delete')} onPress={()=>deleteTemplate.set(templateID())}/></If>
          <View style={{flex:1,minWidth:0}}/>
          <Action label={tr('更新模板草稿','Update template draft')} onPress={updateTemplate}/>
        </Row>
        <Field testID="template-name" label={tr('名称','Name')} value={templateName()} onChange={v=>templateName.set(v)}/>
        <Field testID="template-yaml" label="YAML" multiline height={compact()?120:160} value={templateYAML()} onChange={v=>templateYAML.set(v)}/>
        <Label text={tr('先更新模板草稿，再保存模板库；被任一范围引用的模板不可删除。','Update the template draft, then save the library. Referenced templates cannot be deleted.')} muted size={12}/>
      </If>
      <If when={libraryTab()==='order'}>
        <Label text={tr('从上到下依次合并，后面的模板覆盖前面的同名字段。','Merge from top to bottom; later templates override earlier fields.')} muted/>
        <If when={ids().length>0}>
          <Card>
            <Each of={ids()}>{(id,index)=>
              <View style={{minHeight:44,paddingHorizontal:16,paddingVertical:8,backgroundColor:colors().surface,borderTopWidth:index()>0?1:0,borderColor:colors().border}} hoverStyle={{backgroundColor:colors().hover}}>
                <Row justify="space-between" gap={10}>
                  <View style={{flex:1,minWidth:0}}><Label text={index()+1+'. '+(library().find(v=>v.id===id())?.name??id())} lines={1}/></View>
                  <Action quiet label={tr('上移','Up')} disabled={index()===0} onPress={()=>moveTemplate(index(),-1)}/>
                  <Action quiet label={tr('下移','Down')} disabled={index()===ids().length-1} onPress={()=>moveTemplate(index(),1)}/>
                  <Action quiet danger label={tr('移除','Remove')} onPress={()=>ids.set(ids().filter(v=>v!==id()))}/>
                </Row>
              </View>}
            </Each>
          </Card>
        </If>
        <If when={library().filter(v=>!ids().includes(v.id)).length>0}>
          <Card>
            <Each of={library().filter(v=>!ids().includes(v.id))} key={(v:any)=>v.id}>{(v,index)=>
              <View style={{minHeight:44,paddingHorizontal:16,paddingVertical:8,backgroundColor:colors().surface,borderTopWidth:index()>0?1:0,borderColor:colors().border}} hoverStyle={{backgroundColor:colors().hover}}>
                <Row justify="space-between" gap={10}><Label text={v().name} lines={1}/><Action quiet label={tr('添加到此范围','Attach to scope')} onPress={()=>ids.set([...ids(),v().id])}/></Row>
              </View>}
            </Each>
          </Card>
        </If>
        <If when={!library().length}><Empty title={tr('模板库为空','No templates')} description={tr('先在模板库创建并保存一个模板','Create and save a template in the library first')}/></If>
      </If></Modal>
    <Modal title={tr('删除模板？','Delete template?')} open={!!deleteTemplate()} onClose={()=>deleteTemplate.set('')} footer={()=><Action danger label={tr('从草稿中删除','Delete from draft')} onPress={()=>{library.set(library().filter(v=>v.id!==deleteTemplate()));if(templateID()===deleteTemplate())pickTemplate('');deleteTemplate.set('');}}/>}><Label text={tr('保存模板库时会验证所有全局与订阅引用；引用未解除时会保留现有库与草稿并提示错误。','Saving validates references from all scopes. If still referenced, the library is kept and an error is shown.')}/></Modal>
  <Modal title={tr('放弃模板更改？','Discard template changes?')} open={libraryClose()} onClose={()=>libraryClose.set(false)} footer={()=><Row justify="flex-end"><Action label={tr('继续编辑','Keep editing')} onPress={()=>libraryClose.set(false)}/><Action danger label={tr('放弃并关闭','Discard and close')} onPress={()=>{library.set(JSON.parse(libraryBase()));ids.set(JSON.parse(idsBase()));templateID.set('');templateName.set('');templateYAML.set('');libraryClose.set(false);libraryOpen.set(false);}}/></Row>}><Label text={tr('模板库、应用顺序和当前编辑内容中有未保存的更改。','There are unsaved changes in the library, apply order or current template.')}/></Modal>
  </View>;
}
export default RulesPage;
