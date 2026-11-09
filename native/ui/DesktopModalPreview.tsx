import { cleanup, mount, signal, View } from 'nsh';
import { Action, Label, Modal } from './src/desktop/ui';
import { language } from './src/desktop/i18n';

language.set('en');

// Fresh snapshots reproduce the desktop poll without touching the real host.
function ModalPreview() {
  const snapshot=signal({visible:true,revision:0});
  const counts=signal({mounts:0,unmounts:0});
  let mounts=0,unmounts=0;
  const timer=setInterval(()=>snapshot.set({...snapshot(),revision:snapshot().revision+1}),1000);
  cleanup(()=>clearInterval(timer));
  function Body() {
    counts.set({mounts:++mounts,unmounts});
    cleanup(()=>counts.set({mounts,unmounts:++unmounts}));
    return <Label text={`Snapshot ${snapshot().revision}`}/>;
  }
  return <View><Label text={`Mounted ${counts().mounts}, unmounted ${counts().unmounts}`}/><Action label="Open" onPress={()=>snapshot.set({...snapshot(),visible:true})}/><Modal title="Polling dialog" open={snapshot().visible} onClose={()=>snapshot.set({...snapshot(),visible:false})}><Body/></Modal></View>;
}

mount(ModalPreview);
