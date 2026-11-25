import type { MessageInstance } from 'antd/es/message/interface';

// antd's static message API is not aware of theme context (console warning +
// wrong styles in dark mode). Inside components, get the instance via
// App.useApp(); non-component modules (client.ts) cannot use hooks, so the
// instance is injected through this holder: MessageBridge sets it when
// <AntApp> mounts and clears it on unmount.
let instance: MessageInstance | null = null;

export function setAppMessage(m: MessageInstance | null) {
  instance = m;
}

export function getAppMessage(): MessageInstance | null {
  return instance;
}
