import { cleanup, effect, lifecycle, reduceMotion, signal, View } from 'nsh';
import { Progress } from 'nsh/base';
import type { PageProps } from './types';
import { Action, Label, Modal, Row, fmtBytes } from './ui';
import { colors } from './theme';
import { tr } from './i18n';

// Animate only while the backend reports work with an unknown extent.
function UpdateProgress(p:{value:number;indeterminate:boolean}) {
  const width=signal(0),position=signal(0);let frame=0;
  effect(()=>{
    cancelAnimationFrame(frame);
    if(!p.indeterminate||reduceMotion()||lifecycle()==='background'){position.set(.5);return;}
    const start=Date.now();
    const tick=()=>{position.set(((Date.now()-start)%1400)/1400);frame=requestAnimationFrame(tick);};
    frame=requestAnimationFrame(tick);
  });
  cleanup(()=>cancelAnimationFrame(frame));
  return <View onLayout={({bounds})=>width.set(bounds.size.width)}>
    <Progress testID="desktop-update-progress" value={p.value} indeterminate={p.indeterminate} accessibilityLabel={tr('更新进度','Update progress')}
      trackStyle={{height:5,borderRadius:3,backgroundColor:colors().border,overflow:'hidden'}}
      indicatorStyle={{height:5,borderRadius:3,backgroundColor:colors().accent,...(p.indeterminate?{width:'30%',transform:[{translateX:(position()*1.3-.3)*width()}]}:{})}}/>
  </View>;
}

export function UpdateDialog(p:PageProps) {
  const working=signal(false),error=signal('');
  async function run(fn:()=>Promise<unknown>){if(working())return;working.set(true);error.set('');try{await fn();await p.refresh();}catch(e){error.set(e instanceof Error?e.message:String(e));}finally{working.set(false);}}
  const update=()=>p.snapshot().update??{};
  const phase=()=>update().phase;
  const checking=()=>phase()==='checking',downloading=()=>phase()==='downloading';
  const active=()=>checking()||downloading();
  const title=()=>({checking:tr('正在检查更新','Checking for updates'),available:tr('发现新版本','Update available'),current:tr('已是最新版本','Up to date'),downloading:tr('正在下载更新','Downloading update'),ready:tr('更新已准备好','Update ready'),failed:tr('暂时无法更新','Update unavailable')} as Record<string,string>)[phase()]??tr('软件更新','Software update');
  const description=()=>checking()?tr('正在连接 GitHub Releases…','Connecting to GitHub Releases…'):downloading()?tr('下载完成并通过签名校验后准备安装，代理会继续运行。','The update will be prepared after download and signature verification. The proxy keeps running.'):phase()==='current'?`${tr('你正在使用 Gopurr','You are using Gopurr')} ${p.snapshot().status.version}`:phase()==='ready'?tr('重启后使用新版本，订阅与设置会保留。','Restart to use the new version. Profiles and settings will be retained.'):update().version?`Gopurr ${update().version}`:tr('请检查网络连接后重试。','Check your connection and retry.');
  const command=(method:string)=>void run(()=>p.call(method));
  const hide=()=>command('desktop.update.hide');
  const cancel=()=>void run(async()=>{await p.call('desktop.update.cancel');await p.call('desktop.update.hide');});
  return <Modal testID="desktop-update-dialog" title={title()} open={!!update().visible} onClose={hide} error={error()||update().error} footer={()=><Row justify="flex-end">
    <If when={active()}><Action label={tr('取消','Cancel')} disabled={working()} onPress={cancel}/></If>
    <If when={!active()}><Action label={phase()==='available'?tr('稍后','Later'):phase()==='ready'?tr('稍后重启','Restart later'):tr('关闭','Close')} disabled={working()} onPress={hide}/></If>
    <If when={phase()==='available'}><Action primary label={tr('下载并安装','Download and install')} disabled={working()} onPress={()=>command('desktop.update.install')}/></If>
    <If when={phase()==='ready'}><Action primary label={tr('立即重启','Restart now')} disabled={working()} onPress={()=>command('desktop.update.restart')}/></If>
    <If when={['failed','current'].includes(phase())&&update().enabled!==false}><Action primary label={tr('重新检查','Check again')} disabled={working()} onPress={()=>command('desktop.update.check')}/></If>
  </Row>}>
    <Label text={description()} muted size={12}/>
    <If when={phase()==='available'&&!!update().notes}><Label text={update().notes} size={12}/></If>
    <If when={active()}><UpdateProgress value={update().total>0?Math.min(100,(update().downloaded??0)/update().total*100):0} indeterminate={!!update().visible&&(checking()||!(update().total>0))}/></If>
    <If when={downloading()}><Label text={`${fmtBytes(update().downloaded??0)} / ${update().total>0?fmtBytes(update().total):tr('大小待确认','Size unknown')}`} muted size={11}/></If>
  </Modal>;
}
