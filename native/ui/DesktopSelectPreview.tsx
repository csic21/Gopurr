import { mount } from 'nsh';
import { DesktopApp } from './src/desktop/app';
import { usePreviewTransport } from './src/desktop/client';
import { desktopFixtureTransport } from './src/desktop/fixture';
const dispatch=desktopFixtureTransport();
// Match JSON RPC: every poll returns fresh arrays even when choices are unchanged.
usePreviewTransport(async(method,params)=>JSON.parse(JSON.stringify(await dispatch(method,params))));
mount(DesktopApp);
