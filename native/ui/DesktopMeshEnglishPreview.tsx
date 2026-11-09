import { mount } from 'nsh';
import { DesktopApp } from './src/desktop/app';
import { usePreviewTransport } from './src/desktop/client';
import { meshFixtureTransport } from './mesh-fixture';
usePreviewTransport(meshFixtureTransport('en'));
mount(DesktopApp);
