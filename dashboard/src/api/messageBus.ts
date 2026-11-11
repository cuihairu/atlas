import type { MessageInstance } from 'antd/es/message/interface';

// antd 静态 message 不感知主题上下文（控制台告警 + 深色档样式错位）。
// 组件内用 App.useApp() 取实例；非组件模块（client.ts）无法用 hook，
// 经此 holder 注入：MessageBridge 在 <AntApp> 挂载时写入，卸载时清空。
let instance: MessageInstance | null = null;

export function setAppMessage(m: MessageInstance | null) {
  instance = m;
}

export function getAppMessage(): MessageInstance | null {
  return instance;
}
