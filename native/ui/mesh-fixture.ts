// Visual fixtures only: no system settings, real peers or native clients.
import { desktopFixtureTransport } from './src/desktop/fixture';

export function meshFixtureTransport(language:'en'|'zh-Hans'='zh-Hans') {
  const dispatch=desktopFixtureTransport(language);
  const initialized=dispatch('desktop.snapshot').then(s=>{
    s.mesh.status.peers=[
      {id:'peer-1',name:'KarldeMacBook-Air.local-gopurr · 用于远程工作的 MacBook',os:'macOS',owner:'Karl · karl@example.com',ips:['100.113.197.83'],online:true},
      {id:'peer-2',name:'Home Server',os:'linux',owner:'Karl',ips:['100.70.0.5'],online:true},
      {id:'peer-3',name:'DESKTOP-IH8OK35-gopurr',os:'windows',owner:'Karl',ips:['100.126.33.41'],online:false},
    ];
    s.meshAccess.ready=false;
    return s;
  });
  return async(method:string,params:any={})=>{
    const s=await initialized;
    if(method==='desktop.snapshot')return JSON.parse(JSON.stringify(s));
    if(method==='desktop.mesh.action'&&params.action==='prepare'){s.meshAccess.ready=true;return {};}
    if(method==='desktop.mesh.probe')return {state:'reachable',message:language==='en'?'The port is reachable. Complete account authentication in the client.':'端口可达，请在客户端完成账号验证；端口可达不代表登录成功。'};
    if(method==='desktop.mesh.shortcut')throw new Error(language==='en'?'Unable to open the client. Install a compatible client, or copy the address to connect manually.':'未能打开客户端；请确认已安装对应客户端，也可复制地址手动连接。');
    if(method==='desktop.mesh.shortcut.save'){s.meshShortcuts[params.id]=params;return {};}
    if(method==='desktop.mesh.shortcut.forget'){delete s.meshShortcuts[params.id];return {};}
    return dispatch(method,params);
  };
}
