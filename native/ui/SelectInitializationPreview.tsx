// Isolated regression fixture: defer display frames so input can precede setup.
import { mount, signal, Text, View } from 'nsh';
import { Select } from './src/desktop/select';
const frames=new Map<number,ReturnType<typeof setTimeout>>();let serial=0;
globalThis.requestAnimationFrame=callback=>{const id=++serial;frames.set(id,setTimeout(()=>{frames.delete(id);callback(Date.now());},100));return id;};
globalThis.cancelAnimationFrame=id=>{if(id===undefined)return;const timer=frames.get(id);if(timer!==undefined){clearTimeout(timer);frames.delete(id);}};
mount(()=>{
 const value=signal('inherit');
 return <View style={{padding:20,gap:20}}><Select testID="delayed-select" label="Policy" items={[{value:'inherit',title:'Follow profile'},{value:'true',title:'Force on'},{value:'false',title:'Force off'}]} value={value()} onChange={v=>value.set(v)}/><Text>{`choice:${value()}`}</Text></View>;
});
