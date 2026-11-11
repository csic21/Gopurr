import { effect, Image, List, Scroll, signal, Text, View, windowSize } from 'nsh';
import { Checkbox } from 'nsh/base';
import type { PageProps } from './types';
import { Action, Empty, Feedback, Field, Group, GroupRow, Heading, Icon, Label, Modal, Row, SearchField, StatusDot, Tabs, Toggle, elevation, fmtTime, tint, RADIUS } from './ui';
import { colors } from './theme';
import { watchDesktopDraft } from './drafts';
import { nativeCall } from './client';
import { tr } from './i18n';
import checkMark from '../../assets/icons/check.svg';

const errorText=(e:unknown)=>e instanceof Error?e.message:String(e);
const deviceIP=(d:any)=>(d?.ips??[]).find((ip:string)=>!ip.includes(':'))??d?.ips?.[0]??'';
export function filterDevices(devices:any[],query:string){const q=query.trim().toLowerCase();return devices.filter(d=>!q||[d.name,d.dnsName,d.owner,d.login,...(d.ips??[])].join(' ').toLowerCase().includes(q));}
export function serviceProtocol(services:{protocol:string;port:number}[]){return services.map(s=>`${s.port}/${s.protocol}`).join(',');}

const tab=signal('devices'),query=signal(''),error=signal(''),notice=signal(''),working=signal(false),guide=signal(false),prepare=signal(false),detail=signal<any>(null),portsOpen=signal(false),portDraft=signal(''),allowAfterSave=signal(false),clear=signal(false),quick=signal<any>(null),quickKind=signal('ssh'),quickPort=signal('22'),quickUser=signal(''),remember=signal(true),probe=signal<any>(null),quickError=signal(''),loginPending=signal(false);

const quickNotice=signal('');

// Shared motion: colour only, matching the rest of the desktop shell.
const MOTION={transitionProperty:"background-color, border-color, color, opacity",transitionDuration:140,transitionTimingFunction:"ease-out"} as const;
const compact=()=>windowSize().width<960||windowSize().height<660;
const pagePad=()=>compact()?16:24;
const pageBottom=()=>compact()?12:20;
const ipWidth=()=>compact()?112:128;
const stateWidth=()=>60;
const actionsWidth=()=>compact()?192:200;
const labelWidth=()=>compact()?148:156;

function Chip(p:{name:string}) {
  return <View style={{width:28,height:28,flexShrink:0,borderRadius:9,alignItems:"center",justifyContent:"center",backgroundColor:colors().hover,borderWidth:1,borderColor:colors().border,...MOTION}}><Icon name={p.name} size={14} color={colors().muted}/></View>;
}

/** A grouped setting line: title and explanation on the left, controls on the right. */
function InfoRow(p:{title:string;description?:string;icon?:string;children?:any}) {
  return <GroupRow gap={14}>
    <If when={!!p.icon}><Chip name={p.icon??"circle"}/></If>
    <View style={{flex:1,minWidth:0,gap:2}}>
      <Label text={p.title} weight={500} size={13} lines={2} tooltip={p.title}/>
      <If when={!!p.description}><Label text={p.description??''} muted size={11.5} lines={2} tooltip={p.description}/></If>
    </View>
    <Row gap={8} style={{flexShrink:0}}>{p.children}</Row>
  </GroupRow>;
}

function FieldLabel(p:{title:string;description?:string}) {
  return <View style={{flex:1,minWidth:0,gap:2}}><Label text={p.title} weight={500} size={13} lines={2} tooltip={p.title}/><If when={!!p.description}><Label text={p.description??''} muted size={11.5} lines={2} tooltip={p.description}/></If></View>;
}

export function MeshPage(p:PageProps){
  watchDesktopDraft('mesh',()=>portsOpen()||!!quick()||working());
  const m=()=>p.snapshot().mesh??{};const s=()=>m().status??{};
  const busy=()=>working()||!!m().busy;
  const devices=()=>[...(s().self?.id?[s().self]:[]),...(s().peers??[])];
  const filtered=()=>filterDevices(devices(),query());
  const allowed=()=>s().shieldsKnown&&!s().shieldsUp;
  const services=()=>s().services??[];const blocked=()=>s().blocked??[];
  async function work(fn:()=>Promise<void>,quickContext=false){if(working())return;working.set(true);if(quickContext){quickError.set('');quickNotice.set('');}else error.set('');notice.set('');try{await fn();await p.refresh();}catch(e){(quickContext?quickError:error).set(errorText(e));}finally{working.set(false);}}
  const action=(action:string)=>{if(action==='disconnect')loginPending.set(false);void work(async()=>{await p.call('desktop.mesh.action',{action});});};
  async function beginConnect(){loginPending.set(true);try{await p.call('desktop.mesh.action',{action:'connect'});}catch(e){loginPending.set(false);throw e;}}
  effect(()=>{if(loginPending()&&s().authURL&&m().managed&&s().shieldsKnown&&s().shieldsUp&&!m().busy&&!working()){loginPending.set(false);void work(login);}});
  async function login(){const r=await p.call<any>('desktop.mesh.action',{action:'login'});if(r.url)await nativeCall('desktop.openURL',{url:r.url});}
  const openURL=(url:string)=>void work(async()=>{await nativeCall('desktop.openURL',{url});});
  const copy=(text:string)=>void work(async()=>{await nativeCall('desktop.clipboard.write',{text});notice.set(tr('已复制','Copied'));});
  function openPorts(enable:boolean){portDraft.set(serviceProtocol(services()));allowAfterSave.set(enable);portsOpen.set(true);error.set('');}
  function openQuick(d:any){const saved=p.snapshot().meshShortcuts?.[d.id];const kind=saved?.kind??(d.os?.toLowerCase().includes('windows')?'rdp':/mac|darwin/i.test(d.os??'')?'vnc':'ssh');quick.set(d);quickKind.set(kind);quickPort.set(String(saved?.port??({ssh:22,rdp:3389,vnc:5900} as any)[kind]));quickUser.set(saved?.user??'');remember.set(true);probe.set(null);quickError.set('');quickNotice.set('');}
  const quickParams=()=>({id:quick()?.id,kind:quickKind(),port:Number(quickPort()),user:quickUser().trim(),remember:remember()});
  async function connectQuick(){const r=await p.call<any>('desktop.mesh.shortcut',quickParams());if(r.url)await nativeCall('desktop.openURL',{url:r.url});quickNotice.set(tr('已请求打开客户端，请在客户端完成登录','Client requested. Complete authentication in the client.'));}
  const accessReady=()=>!!p.snapshot().meshAccess?.ready;

  // Connection state, kept above the list so the page always says where it stands.
  const statusText=()=>s().running?tr('已连接 Tailscale','Connected to Tailscale'):m().busy?tr('正在连接…','Connecting…'):tr('尚未连接组网','Mesh disconnected');
  const statusColor=()=>s().running?colors().success:m().busy?colors().accent:colors().muted;
  const railColor=()=>s().running?colors().success:m().busy?colors().accent:colors().border;
  const identity=()=>s().running?(s().self?.name??''):'';
  const identityMeta=()=>[deviceIP(s().self),s().tailnet].filter(Boolean).join(' · ')||tr('内置组网内核，使用官方账号授权','Built-in mesh engine with official account authorization');
  const inboundDescription=()=>!s().shieldsKnown?tr('防护状态待确认，请刷新或断开组网','Protection state is unknown; refresh or disconnect'):allowed()?(serviceProtocol(services())?tr('已开放 · 仅限授权端口','Open · authorized ports only')+': '+serviceProtocol(services()):tr('已开放 · 仅限授权端口','Open · authorized ports only')):tr('入站访问已关闭；仍可主动访问其他设备','Inbound access is closed; outbound access remains available');

  const deviceEmpty=()=>{
    if(!s().running)return <Empty icon="mesh" title={tr('连接后显示设备','Devices appear once connected')} description={tr('连接组网后，同一网络中的设备会显示在这里。','Devices in the same tailnet appear here once the mesh is connected.')} action={()=><Action label={tr('连接设置','Connection setup')} onPress={()=>guide.set(true)}/>}/>;
    if(!devices().length)return <Empty icon="mesh" title={tr('没有可显示的设备','No devices to display')} description={tr('同一网络中有权限查看的设备会显示在这里。','Devices you can access in the same tailnet appear here.')} action={()=><Action label={tr('连接设置','Connection setup')} onPress={()=>guide.set(true)}/>}/>;
    return <Empty icon="search" title={tr('没有匹配的设备','No matching devices')} description={tr('没有设备匹配当前搜索。','No devices match the current search.')} action={()=><Action label={tr('清空搜索','Clear search')} onPress={()=>query.set('')}/>}/>;
  };

  return <View style={{flex:1}}>
    <View style={{paddingHorizontal:pagePad(),paddingTop:compact()?10:20,paddingBottom:compact()?8:12,gap:compact()?10:16}}>
      <Heading title={tr('组网','Mesh')} description={tr('通过 Tailscale 连接自己的电脑、手机和服务器','Connect your computers, phones and servers through Tailscale')} action={()=><Row gap={6}><Action label={tr('邀请成员','Invite members')} onPress={()=>openURL('https://console.tailscale.com/admin/users')}/><Action label={tr('连接设置','Connection setup')} onPress={()=>guide.set(true)}/><Action label={tr('刷新组网','Refresh')} disabled={busy()} onPress={()=>action('refresh')}/></Row>}/>

      {/* Connection card: identity, one primary action, and the inbound access switch. */}
      <View style={{borderRadius:RADIUS.surface,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface"),overflow:"hidden"}}>
        <View style={{flexDirection:"row"}}>
          <View style={{width:3,backgroundImage:"linear-gradient(180deg, "+railColor()+" 0%, "+tint(railColor(),"00")+" 100%)"}}/>
          <View style={{flex:1,minWidth:0,flexDirection:"row",alignItems:"center",paddingHorizontal:16,paddingVertical:compact()?11:14,gap:14}}>
            <View style={{flex:1,minWidth:0,gap:3}}>
              <Row gap={7} style={{minWidth:0}}>
                <StatusDot on={!!s().running} size={7} color={statusColor()} glow={!!s().running}/>
                <Text style={{fontSize:12,fontWeight:"600",letterSpacing:0.1,color:statusColor(),numberOfLines:1,textOverflow:"ellipsis"}}>{statusText()}</Text>
              </Row>
              <Row gap={8} style={{minWidth:0,alignItems:"baseline"}}>
                <If when={!!identity()}><Text tooltip={identity()} style={{fontSize:compact()?14:15,fontWeight:"600",letterSpacing:-0.2,color:colors().text,numberOfLines:1,textOverflow:"ellipsis",flexShrink:1,minWidth:0}}>{identity()}</Text></If>
                <Text tooltip={identityMeta()} style={{fontSize:11.5,color:colors().muted,numberOfLines:1,textOverflow:"ellipsis",flexShrink:1,minWidth:0,fontVariant:"tabular-nums"}}>{identityMeta()}</Text>
              </Row>
            </View>
            <Row gap={6} style={{flexShrink:0}}>
              <If when={s().running&&!accessReady()}><Action label={tr('准备访问','Prepare access')} disabled={busy()} onPress={()=>prepare.set(true)}/></If>
              <Action label={s().running||s().authURL||m().busy?tr('断开组网','Disconnect'):tr('连接组网','Connect')} primary={!s().running} disabled={working()} onPress={()=>s().running||s().authURL||m().busy?action('disconnect'):guide.set(true)}/>
            </Row>
          </View>
        </View>
        <View style={{height:1,backgroundColor:colors().border}}/>
        <View style={{flexDirection:"row",alignItems:"center",paddingHorizontal:16,minHeight:compact()?46:50,gap:14}}>
          <Chip name="shield"/>
          <View style={{width:labelWidth(),flexShrink:0}}><Label text={tr('允许组网设备访问本机','Allow mesh devices to access this machine')} weight={500} size={13} lines={2} tooltip={tr('允许组网设备访问本机','Allow mesh devices to access this machine')}/></View>
          <View style={{flex:1,minWidth:0}}><Label text={inboundDescription()} faint size={12} lines={2} tooltip={inboundDescription()}/></View>
          <Toggle checked={!!allowed()} disabled={busy()||!s().shieldsKnown||(!s().running&&!allowed())} label={tr('允许组网设备访问本机','Allow mesh devices to access this machine')} showLabel={false} onChange={v=>v?openPorts(true):action('shieldsUp')}/>
        </View>
      </View>

      <Tabs value={tab()} onChange={v=>tab.set(v)} items={[{id:'devices',label:tr('设备','Devices')},{id:'visits',label:tr('本机访问','Local access')},{id:'security',label:tr('安全设置','Security')}]}/>
    </View>

    <If when={tab()==='devices'}>
      <View style={{flex:1,paddingHorizontal:pagePad(),paddingBottom:pageBottom()}}>
        <View style={{flex:1,borderRadius:RADIUS.surface,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface"),overflow:"hidden"}}>
          <View style={{minHeight:compact()?42:44,paddingHorizontal:14,flexDirection:"row",alignItems:"center",gap:10,borderBottomWidth:1,borderColor:colors().border,backgroundColor:colors().bg}}>
            <View style={{width:compact()?260:300,maxWidth:"72%"}}><SearchField testID="mesh-search" value={query()} onChange={v=>query.set(v)} placeholder={tr('搜索设备、账号或虚拟 IP','Search devices, accounts or mesh IP')}/></View>
            <View style={{flex:1}}/>
            <Label text={String(filtered().length)+" "+tr('台设备','devices')} muted size={11.5} tabular lines={1}/>
          </View>
          <If when={filtered().length>0}>
            <View style={{minHeight:30,paddingHorizontal:14,flexDirection:"row",alignItems:"center",gap:12,borderBottomWidth:1,borderColor:colors().border}}>
              <View style={{flex:1,minWidth:0}}><Text style={{fontSize:10.5,fontWeight:"500",letterSpacing:0.3,color:colors().faint,numberOfLines:1}}>{tr('设备','Device')}</Text></View>
              <View style={{width:ipWidth()}}><Text style={{fontSize:10.5,fontWeight:"500",letterSpacing:0.3,color:colors().faint,numberOfLines:1}}>{tr('虚拟 IP','Mesh IP')}</Text></View>
              <View style={{width:stateWidth()}}><Text style={{fontSize:10.5,fontWeight:"500",letterSpacing:0.3,color:colors().faint,numberOfLines:1}}>{tr('状态','State')}</Text></View>
              <View style={{width:actionsWidth()}}/>
            </View>
          </If>
          <List each={filtered()} keyExtractor={(d:any)=>d.id} style={{flex:1}} separator={<View style={{height:1,backgroundColor:colors().border}}/>} empty={<View style={{flex:1,justifyContent:"center"}}>{deviceEmpty()}</View>}>
            {(device)=><View testID={"mesh-device-"+device().id} style={{flexDirection:"row",alignItems:"center",minHeight:52,paddingHorizontal:14,gap:12,backgroundColor:colors().surface,...MOTION}} hoverStyle={{backgroundColor:colors().hover}}>
              <Row gap={9} style={{flex:1,minWidth:0}}>
                <StatusDot on={!!device().online} size={7} color={blocked().includes(device().id)?colors().danger:device().online?colors().success:colors().faint}/>
                <View style={{flex:1,minWidth:0,gap:2}}>
                  <Label text={device().name+(device().id===s().self?.id?tr(' · 本机',' · This device'):'')} weight={500} size={13} lines={1} tooltip={device().name+(device().id===s().self?.id?tr(' · 本机',' · This device'):'')}/>
                  <Label text={device().owner||device().login||''} muted size={11.5} lines={1} tooltip={device().owner||device().login||''}/>
                </View>
              </Row>
              <View style={{width:ipWidth()}}><Label text={deviceIP(device())||'—'} muted size={12} tabular lines={1} tooltip={deviceIP(device())}/></View>
              <View style={{width:stateWidth()}}><Label text={blocked().includes(device().id)?tr('已封禁','Blocked'):device().online?tr('在线','Online'):tr('离线','Offline')} color={blocked().includes(device().id)?colors().danger:device().online?colors().success:colors().muted} size={12} lines={1}/></View>
              <Row gap={2} style={{width:actionsWidth(),flexShrink:0,justifyContent:"flex-end"}}>
                <If when={device().id!==s().self?.id}><Action testID={"mesh-connect-"+device().id} quiet label={tr('连接设备','Connect')} disabled={!device().online||blocked().includes(device().id)} onPress={()=>openQuick(device())}/></If>
                <Action quiet label={tr('复制 IP','Copy IP')} disabled={!deviceIP(device())} onPress={()=>copy(deviceIP(device()))}/>
                <Action quiet label={tr('详情','Details')} onPress={()=>detail.set(device())}/>
              </Row>
            </View>}
          </List>
        </View>
      </View>
    </If>

    <If when={tab()==='visits'}>
      <View style={{flex:1,paddingHorizontal:pagePad(),paddingBottom:pageBottom(),gap:12}}>
        <Group>
          <InfoRow icon="conns" title={tr('本机授权端口','Authorized local ports')} description={serviceProtocol(services())||tr('尚未授权任何端口','No ports authorized')}>
            <Action label={tr('编辑端口','Edit ports')} disabled={busy()} onPress={()=>openPorts(allowed())}/>
          </InfoRow>
        </Group>
        <Label text={tr('授权只放行组网流量，不会启动 SSH、远程桌面或屏幕共享服务。','Authorization permits mesh traffic; it does not start SSH, remote desktop or screen sharing.')} muted size={11.5} lines={2}/>
        <If when={!!m().monitorError}><Feedback text={m().monitorError} danger/></If>
        <If when={!!m().visits?.length}>
          <Row justify="space-between" style={{paddingHorizontal:2}}>
            <Label text={tr('访问记录','Access history')} weight={600} size={12} muted lines={1}/>
            <Action quiet label={tr('清空记录','Clear history')} disabled={busy()} onPress={()=>clear.set(true)}/>
          </Row>
          <Scroll style={{flex:1}} contentStyle={{gap:12,paddingBottom:2}}>
            <Group>
              <Each of={m().visits??[]} key={(visit:any)=>[visit.deviceID,visit.remote,visit.local,visit.protocol].filter(Boolean).join('|')}>
                {(visit:any)=><GroupRow gap={12}>
                  <View style={{flex:1,minWidth:0,gap:2}}>
                    <Label text={visit().device||visit().remote||tr('未知设备','Unknown device')} weight={500} size={13} lines={1} tooltip={visit().device||visit().remote||tr('未知设备','Unknown device')}/>
                    <Label text={[visit().owner||visit().login,visit().protocol,(visit().remote??'')+' → '+(visit().local??'')].filter(Boolean).join(' · ')} muted size={11.5} lines={1} tooltip={[visit().owner||visit().login,visit().protocol,(visit().remote??'')+' → '+(visit().local??'')].filter(Boolean).join(' · ')}/>
                  </View>
                  <If when={!!visit().active}><Label text={tr('活跃','Active')} color={colors().success} size={11} weight={600} lines={1}/></If>
                  <If when={!visit().active}><Label text={fmtTime(visit().lastSeen)} faint size={11.5} tabular lines={1}/></If>
                  <If when={!!visit().deviceID}><Action quiet danger={!blocked().includes(visit().deviceID)} label={blocked().includes(visit().deviceID)?tr('解除封禁','Unblock'):tr('封禁设备','Block device')} disabled={busy()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.block',{id:visit().deviceID,blocked:!blocked().includes(visit().deviceID)});})}/></If>
                </GroupRow>}
              </Each>
            </Group>
          </Scroll>
        </If>
        <If when={!m().visits?.length}>
          <View style={{flex:1,borderRadius:RADIUS.surface,backgroundColor:colors().surface,borderWidth:1,borderColor:colors().border,boxShadow:elevation("surface")}}>
            <Empty icon="activity" title={tr('暂无访问记录','No access history')} description={tr('记录显示到本机的组网连接，不代表远程登录成功。','Records show inbound mesh connections, not successful remote authentication.')}/>
          </View>
        </If>
      </View>
    </If>

    <If when={tab()==='security'}>
      <Scroll style={{flex:1}} contentStyle={{paddingHorizontal:pagePad(),paddingBottom:pageBottom(),gap:12}}>
        <Group>
          <InfoRow title={tr('启动时自动连接组网','Connect mesh on startup')} description={tr('使用本机保存的官方账号授权，不自动开放新端口','Use the saved authorization; no new ports are opened automatically')}>
            <Toggle checked={!!m().autoConnect} disabled={busy()} onChange={enabled=>void work(async()=>{await p.call('desktop.mesh.autoConnect',{enabled});})}/>
          </InfoRow>
          <InfoRow title={tr('入站访问保护','Inbound protection')} description={allowed()?tr('仅放行授权端口；可随时关闭全部入站访问','Only authorized ports are allowed; close all inbound access at any time'):tr('当前阻止其他设备访问本机','Other devices cannot currently access this machine')}>
            <Action label={tr('关闭入站访问','Close inbound access')} disabled={!allowed()||busy()} onPress={()=>action('shieldsUp')}/>
          </InfoRow>
          <InfoRow title={tr('封禁设备','Blocked devices')} description={tr('封禁在本机生效，不会把设备移出官方网络','Blocks apply on this machine; devices remain in the tailnet')}>
            <Label text={String(blocked().length)} muted tabular size={12}/>
          </InfoRow>
          <Each of={blocked()}>
            {(id)=><GroupRow gap={12}>
              <FieldLabel title={devices().find(d=>d.id===id())?.name??id()}/>
              <Action label={tr('解除封禁','Unblock')} disabled={busy()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.block',{id:id(),blocked:false});})}/>
            </GroupRow>}
          </Each>
        </Group>
        <Group>
          <InfoRow title={tr('官方设备管理','Official device management')}>
            <Action label={tr('打开管理台','Open admin')} onPress={()=>openURL('https://login.tailscale.com/admin/machines')}/>
          </InfoRow>
          <InfoRow title={tr('网络访问策略','Tailnet access policy')} description={tr('用官方 ACL 控制哪些成员可访问哪些设备','Use official ACLs to control access between members and devices')}>
            <Action label={tr('访问策略','Access policy')} onPress={()=>openURL('https://login.tailscale.com/admin/acls')}/>
          </InfoRow>
          <InfoRow title={tr('设备审批','Device approval')}>
            <Action label={tr('审批设置','Approval settings')} onPress={()=>openURL('https://login.tailscale.com/admin/settings/device-management')}/>
          </InfoRow>
        </Group>
        <If when={!!s().health?.length}><Feedback text={s().health.join('\n')}/></If>
      </Scroll>
    </If>

    <Modal title={tr('组网连接设置','Mesh connection setup')} open={guide()} onClose={()=>guide.set(false)} error={error()||m().error||p.snapshot().meshAccess?.prepareError} width={620} footer={()=><Row justify="flex-end"><Action label={tr('完成','Done')} onPress={()=>guide.set(false)}/></Row>}>
      <Group>
        <InfoRow title={tr('1 · 加入同一个网络','1 · Join the same tailnet')} description={tr('两台设备分别用官方账号登录，首次授权在浏览器完成。','Sign in on both devices with your official account; authorize in your browser.')}>
          <Action primary={!s().running} label={s().running?tr('已连接','Connected'):s().authURL?tr('浏览器登录','Browser login'):tr('连接并登录','Connect and sign in')} disabled={s().running||busy()} onPress={()=>void work(async()=>{if(s().authURL)await login();else await beginConnect();})}/>
        </InfoRow>
      </Group>
      <If when={s().state==='NeedsMachineAuth'}><Feedback text={tr('正在等待网络管理员批准此设备','Waiting for the network administrator to approve this device')}/></If>
      <If when={m().busy}><Row justify="flex-end"><Action label={tr('取消连接','Cancel connection')} onPress={()=>action('disconnect')}/></Row></If>
      <Group>
        <InfoRow title={tr('2 · 准备本机网络','2 · Prepare this machine')} description={tr('启用 TUN 自动路由和规则模式，使普通应用可访问组网 IP。','Enable TUN automatic routes and rule mode so apps can reach mesh IPs.')}>
          <Action label={accessReady()?tr('已准备','Prepared'):tr('准备访问','Prepare access')} disabled={accessReady()||busy()} onPress={()=>prepare.set(true)}/>
        </InfoRow>
      </Group>
      <Group>
        <GroupRow gap={14}>
          <View style={{flex:1,minWidth:0,gap:2}}>
            <Label text={tr('3 · 选择设备并打开连接客户端','3 · Choose a device and open a client')} weight={500} size={13} lines={2}/>
            <Label text={tr('在线不代表端口可访问；端口检测通过也不代表远程登录成功。首次连接默认关闭本机入站访问。','Online status does not prove port reachability; a successful port check does not prove remote authentication. Local inbound access is closed on first connection.')} muted size={11.5} lines={3}/>
          </View>
        </GroupRow>
      </Group>
    </Modal>

    <Modal title={tr('准备访问其他设备','Prepare access to other devices')} open={prepare()} onClose={()=>prepare.set(false)} error={error()||m().error||p.snapshot().meshAccess?.prepareError} footer={()=><Row justify="flex-end"><Action label={tr('取消','Cancel')} disabled={working()} onPress={()=>prepare.set(false)}/><Action primary label={tr('启用并准备','Enable and prepare')} disabled={working()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.action',{action:'prepare'});prepare.set(false);})}/></Row>}>
      <Label text={tr('将启用 Gopurr TUN 自动路由并切换到规则模式；需要时安装自带的系统服务，系统会请求管理员授权。本机服务仍由访问开关单独控制。','Enable Gopurr TUN automatic routes and rule mode. Install the bundled system service if needed; macOS may ask for administrator authorization. Local services remain controlled by the inbound access switch.')} lines={4}/>
    </Modal>

    <Modal title={tr('授权本机服务端口','Authorize local service ports')} open={portsOpen()} onClose={()=>portsOpen.set(false)} error={error()||m().error||p.snapshot().meshAccess?.prepareError} footer={()=><Row justify="flex-end"><Action label={tr('取消','Cancel')} disabled={working()} onPress={()=>portsOpen.set(false)}/><Action primary label={allowAfterSave()?tr('保存并允许访问','Save and allow access'):tr('保存端口','Save ports')} disabled={working()||(allowAfterSave()&&!portDraft().trim())} onPress={()=>void work(async()=>{await p.call('desktop.mesh.services',{protocol:portDraft(),enabled:allowAfterSave()});portsOpen.set(false);})}/></Row>}>
      <Label text={tr('仅添加你确实需要开放的本机端口。授权不会启动系统服务；网络成员仍受官方 ACL 和本机封禁限制。','Authorize only the local ports you need. This does not start system services. Official ACLs and local device blocks still apply.')} muted lines={3}/>
      <Row wrap gap={6}>
        <Action label="SSH · TCP 22" onPress={()=>portDraft.set([...new Set([...portDraft().split(',').filter(Boolean),'22/tcp'])].join(','))}/>
        <Action label={tr('远程桌面 · TCP 3389','RDP · TCP 3389')} onPress={()=>portDraft.set([...new Set([...portDraft().split(',').filter(Boolean),'3389/tcp'])].join(','))}/>
        <Action label={tr('屏幕共享 · TCP 5900','VNC · TCP 5900')} onPress={()=>portDraft.set([...new Set([...portDraft().split(',').filter(Boolean),'5900/tcp'])].join(','))}/>
      </Row>
      <Field label={tr('协议与端口','Protocols and ports')} value={portDraft()} onChange={v=>portDraft.set(v)} placeholder="22/tcp,3389/tcp,5353/udp"/>
      <Toggle testID="mesh-allow-after-save" label={tr('保存后允许入站访问','Allow inbound access after saving')} checked={allowAfterSave()} onChange={v=>allowAfterSave.set(v)}/>
    </Modal>

    <Modal title={detail()?.name??tr('设备详情','Device details')} open={!!detail()} onClose={()=>detail.set(null)} footer={detail()?.id!==s().self?.id?()=><Row justify="flex-end"><Action label={blocked().includes(detail()?.id)?tr('解除封禁','Unblock'):tr('封禁设备','Block device')} danger={!blocked().includes(detail()?.id)} disabled={busy()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.block',{id:detail()?.id,blocked:!blocked().includes(detail()?.id)});})}/><Action label={tr('连接设置','Connection settings')} onPress={()=>{openQuick(detail());detail.set(null);}}/></Row>:undefined}>
      <Group>
        <Each of={[[tr('账号','Account'),detail()?.owner||detail()?.login],[tr('系统','OS'),detail()?.os],['DNS',detail()?.dnsName],['IP',(detail()?.ips??[]).join('\n')],[tr('连接路径','Connection path'),detail()?.path],[tr('最后在线','Last seen'),fmtTime(detail()?.lastSeen)]]} key={(row:any)=>row[0]}>
          {(row)=><GroupRow gap={14} style={{alignItems:"flex-start"}}>
            <View style={{width:96,flexShrink:0}}><Label text={row()[0]??''} muted size={12} lines={1}/></View>
            <View style={{flex:1,minWidth:0}}><Label text={row()[1]||'—'} size={12.5} lines={4}/></View>
          </GroupRow>}
        </Each>
      </Group>
    </Modal>

    <Modal testID="mesh-quick-dialog" closeTestID="mesh-quick-close" title={tr('连接','Connect')+" "+(quick()?.name??'')} open={!!quick()} onClose={()=>quick.set(null)} width={560} error={quickError()} footer={()=><Row justify="flex-end" gap={6}>
        <Action label={tr('复制地址','Copy address')} disabled={working()} onPress={()=>void work(async()=>{await nativeCall('desktop.clipboard.write',{text:deviceIP(quick())+':'+quickPort()});quickNotice.set(tr('连接地址已复制','Connection address copied'));},true)}/>
        <Action testID="mesh-quick-probe" label={tr('检测连接','Check connection')} disabled={working()||!s().running||!quick()?.online} onPress={()=>void work(async()=>{probe.set(await p.call('desktop.mesh.probe',{id:quick()?.id,protocol:quickKind(),port:Number(quickPort())}));},true)}/>
        <Action testID="mesh-quick-launch" primary label={tr('打开客户端','Open client')} disabled={working()||!s().running||!quick()?.online||!accessReady()} onPress={()=>void work(connectQuick,true)}/>
    </Row>}>
      <View testID="mesh-quick-content" style={{gap:12}}>
        <Row gap={8} style={{minWidth:0}}>
          <StatusDot on={!!quick()?.online} size={6}/>
          <View style={{flex:1,minWidth:0}}><Label text={[quick()?.owner||quick()?.login,deviceIP(quick()),quick()?.online?tr('设备在线','Device online'):tr('设备离线','Device offline')].filter(Boolean).join(' · ')} muted size={11.5} lines={1} tooltip={[quick()?.owner||quick()?.login,deviceIP(quick()),quick()?.online?tr('设备在线','Device online'):tr('设备离线','Device offline')].filter(Boolean).join(' · ')}/></View>
        </Row>
        <Tabs value={quickKind()} onChange={v=>{if(v===quickKind())return;quickKind.set(v);quickPort.set(String(({ssh:22,rdp:3389,vnc:5900} as any)[v]));probe.set(null);quickError.set('');quickNotice.set('');}} items={[{id:'ssh',label:'SSH'},{id:'rdp',label:tr('远程桌面','Remote desktop')},{id:'vnc',label:tr('屏幕共享','Screen sharing')}]}/>
        <Row gap={12} style={{alignItems:"flex-end"}}>
          <View style={{width:112}}><Field label={tr('服务端口','Service port')} testID="mesh-quick-port" accessibilityLabel={tr('连接服务端口','Connection service port')} value={quickPort()} onChange={v=>{quickPort.set(v);probe.set(null);}}/></View>
          <If when={quickKind()==='ssh'}><View style={{flex:1,minWidth:0}}><Field label={tr('用户名','Username')} testID="mesh-quick-user" accessibilityLabel={tr('SSH 用户名','SSH username')} value={quickUser()} onChange={v=>quickUser.set(v)} placeholder={tr('用户名（留空用客户端默认）','Username (client default if empty)')}/></View></If>
        </Row>
        <Checkbox testID="mesh-quick-remember" checked={remember()} onChange={({checked})=>remember.set(checked)} accessibilityLabel={tr('记住连接方式（不保存密码）','Remember connection settings (no passwords)')}
          style={{flexDirection:"row",alignItems:"center",gap:8,minHeight:24}}
          focusStyle={{outlineWidth:2,outlineColor:colors().accent,borderRadius:4}}
          indicatorStyle={{width:16,height:16,borderRadius:4,borderWidth:1,borderColor:remember()?colors().accent:colors().border,backgroundColor:remember()?colors().accent:colors().surface,alignItems:"center",justifyContent:"center",...MOTION}}
          indicator={<If when={remember()}><Image src={checkMark} style={{width:13,height:13,tintColor:colors().accentText}}/></If>}>
          <Label text={tr('记住连接方式（不保存密码）','Remember connection settings (no passwords)')} size={12}/>
        </Checkbox>
        <If when={!accessReady()}>
          <Feedback text={tr('请先准备本机网络，再打开客户端。连接设置草稿会保留。','Prepare this machine before opening a client. Your draft will be retained.')}/>
          <Row><Action label={tr('准备访问','Prepare access')} onPress={()=>prepare.set(true)}/></Row>
        </If>
        <If when={!!probe()}><Feedback text={probe()?.message??probe()?.state??''}/></If>
        <If when={!!quickNotice()}><Feedback text={quickNotice()}/></If>
        <Label text={tr('对方需开放相应端口并启动服务；客户端会独立验证账号和主机身份。远程桌面需本机已安装兼容客户端。','The peer must open the port and start its service. The client verifies the account and host identity. Remote desktop requires a compatible client.')} muted size={11} lines={3}/>
        <Row wrap gap={6}>
          <Action label={tr('仅保存设置','Save settings only')} disabled={working()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.shortcut.save',quickParams());quickNotice.set(tr('连接设置已保存','Connection settings saved'));},true)}/>
          <Action label={tr('忘记设置','Forget settings')} disabled={working()||!p.snapshot().meshShortcuts?.[quick()?.id]} onPress={()=>void work(async()=>{await p.call('desktop.mesh.shortcut.forget',{id:quick()?.id});quickNotice.set(tr('已忘记连接设置','Connection settings forgotten'));},true)}/>
        </Row>
      </View>
    </Modal>

    <Modal title={tr('清空本机访问记录？','Clear local access history?')} open={clear()} onClose={()=>clear.set(false)} footer={()=><Row justify="flex-end"><Action label={tr('取消','Cancel')} disabled={working()} onPress={()=>clear.set(false)}/><Action danger label={tr('清空记录','Clear history')} disabled={working()} onPress={()=>void work(async()=>{await p.call('desktop.mesh.action',{action:'clearHistory'});clear.set(false);})}/></Row>}>
      <Label text={tr('清空后无法恢复；已授权端口与封禁设置保留。','Cleared history cannot be recovered. Authorized ports and blocked devices are retained.')}/>
    </Modal>
  </View>;
}
export default MeshPage;
