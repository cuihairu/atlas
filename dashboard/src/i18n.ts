// Minimal i18n for the Atlas dashboard (TODO v0.1.18): Chinese and English
// dictionaries with a module-level current language so non-React code (the
// API client) can translate too. Persistence via localStorage.
import { useEffect, useReducer } from 'react';
import enUS from 'antd/locale/en_US';
import zhCN from 'antd/locale/zh_CN';

export type Lang = 'zh' | 'en';

const zh = {
  overview: '总览',
  servers: '服务器',
  characterSearch: '角色搜索',
  migrations: '迁移管理',
  serverMap: '服务器地图',
  playerTrend: '玩家趋势',
  realTime: '实时',
  updated: '更新于',
  online: '在线',
  players: '玩家',
  load: '负载',
  noData: '暂无数据',
  last24h: '近 24 小时',
  last7d: '近 7 天',
  onlinePlayers: '在线玩家',
  totalServers: '总服务器数',
  onlineServers: '在线服务器',
  totalPlayers: '总在线玩家',
  totalCapacity: '总容量',
  statusDistribution: '服务器状态分布',
  regionDistribution: '区域分布',
  recentServers: '最近服务器',
  id: 'ID',
  name: '名称',
  region: '区域',
  status: '状态',
  version: '版本',
  playersCapacity: '玩家 / 容量',
  actions: '操作',
  loadMore: '加载更多',
  enterMaintenance: '进入维护',
  drain: '排水',
  disable: '禁用',
  enable: '启用',
  confirmAction: '确认{name}？',
  actionOk: '操作成功',
  backToServers: '返回服务器列表',
  basicInfo: '基本信息',
  liveMetrics: '实时指标',
  playerCount: '玩家数',
  capacity: '容量',
  utilization: '使用率',
  characterList: '角色列表',
  characterId: '角色ID',
  serverId: '服务器ID',
  accountId: '账户ID',
  level: '等级',
  class: '职业',
  lastLogin: '最后登录',
  ip: 'IP',
  port: '端口',
  characterName: '角色名',
  server: '服务器',
  selectClass: '选择职业',
  minLevel: '最低等级',
  maxLevel: '最高等级',
  search: '搜索',
  reset: '重置',
  classWarrior: '战士',
  classMage: '法师',
  classPriest: '牧师',
  classRogue: '盗贼',
  classHunter: '猎人',
  classWarlock: '术士',
  classDruid: '德鲁伊',
  classPaladin: '圣骑士',
  createMigration: '创建迁移',
  sourceServerId: '源服务器 ID',
  targetServerId: '目标服务器 ID',
  inputSourceServerId: '请输入源服务器 ID',
  inputTargetServerId: '请输入目标服务器 ID',
  migrationCreated: '迁移已创建',
  rollbackOk: '回滚成功',
  confirmRollback: '确认回滚？',
  rollback: '回滚',
  sourceServer: '源服务器',
  targetServer: '目标服务器',
  startedAt: '开始时间',
  completedAt: '完成时间',
  migPending: '待处理',
  migRunning: '运行中',
  migCompleted: '已完成',
  migFailed: '失败',
  migRolledBack: '已回滚',
  migrationProgress: '迁移进度',
  liveUpdating: '实时刷新中',
  stepCreated: '已创建',
  stepMigrating: '迁移中',
  stepDone: '完成',
  requestFailed: '请求失败',
  language: '语言',
  theme: '主题',
};

const en: Record<keyof typeof zh, string> = {
  overview: 'Overview',
  servers: 'Servers',
  characterSearch: 'Characters',
  migrations: 'Migrations',
  serverMap: 'Server Map',
  playerTrend: 'Player Trend',
  realTime: 'Live',
  updated: 'Updated',
  online: 'Online',
  players: 'Players',
  load: 'Load',
  noData: 'No data yet',
  last24h: 'Last 24h',
  last7d: 'Last 7 days',
  onlinePlayers: 'Online players',
  totalServers: 'Total Servers',
  onlineServers: 'Online Servers',
  totalPlayers: 'Online Players',
  totalCapacity: 'Total Capacity',
  statusDistribution: 'Servers by Status',
  regionDistribution: 'Servers by Region',
  recentServers: 'Recent Servers',
  id: 'ID',
  name: 'Name',
  region: 'Region',
  status: 'Status',
  version: 'Version',
  playersCapacity: 'Players / Capacity',
  actions: 'Actions',
  loadMore: 'Load more',
  enterMaintenance: 'Maintenance',
  drain: 'Drain',
  disable: 'Disable',
  enable: 'Enable',
  confirmAction: 'Confirm {name}?',
  actionOk: 'Success',
  backToServers: 'Back to servers',
  basicInfo: 'Basic Info',
  liveMetrics: 'Live Metrics',
  playerCount: 'Players',
  capacity: 'Capacity',
  utilization: 'Utilization',
  characterList: 'Characters',
  characterId: 'Character ID',
  serverId: 'Server ID',
  accountId: 'Account ID',
  level: 'Level',
  class: 'Class',
  lastLogin: 'Last Login',
  ip: 'IP',
  port: 'Port',
  characterName: 'Character Name',
  server: 'Server',
  selectClass: 'Select class',
  minLevel: 'Min Level',
  maxLevel: 'Max Level',
  search: 'Search',
  reset: 'Reset',
  classWarrior: 'Warrior',
  classMage: 'Mage',
  classPriest: 'Priest',
  classRogue: 'Rogue',
  classHunter: 'Hunter',
  classWarlock: 'Warlock',
  classDruid: 'Druid',
  classPaladin: 'Paladin',
  createMigration: 'Create Migration',
  sourceServerId: 'Source Server ID',
  targetServerId: 'Target Server ID',
  inputSourceServerId: 'Enter source server ID',
  inputTargetServerId: 'Enter target server ID',
  migrationCreated: 'Migration created',
  rollbackOk: 'Rolled back',
  confirmRollback: 'Rollback this migration?',
  rollback: 'Rollback',
  sourceServer: 'Source Server',
  targetServer: 'Target Server',
  startedAt: 'Started',
  completedAt: 'Completed',
  migPending: 'Pending',
  migRunning: 'Running',
  migCompleted: 'Completed',
  migFailed: 'Failed',
  migRolledBack: 'Rolled back',
  migrationProgress: 'Migration Progress',
  liveUpdating: 'Live updating',
  stepCreated: 'Created',
  stepMigrating: 'Migrating',
  stepDone: 'Done',
  requestFailed: 'Request failed',
  language: 'Language',
  theme: 'Theme',
};

const dict: Record<Lang, Record<keyof typeof zh, string>> = { zh, en };

let current: Lang = 'zh';
try {
  const saved = localStorage.getItem('atlas-lang');
  if (saved === 'en' || saved === 'zh') current = saved;
} catch {
  // storage unavailable (private mode) → keep default
}

const listeners = new Set<() => void>();

export function getLang(): Lang {
  return current;
}

export function setLang(lang: Lang): void {
  if (lang === current) return;
  current = lang;
  try {
    localStorage.setItem('atlas-lang', lang);
  } catch {
    // ignore
  }
  listeners.forEach((fn) => fn());
}

/** Translate a key. `{name}` style placeholders are interpolated from params. */
export function t(key: keyof typeof zh, params?: Record<string, string>): string {
  let text = dict[current][key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      text = text.replaceAll(`{${k}}`, v);
    }
  }
  return text;
}

/** antd locale for the current language. */
export function antdLocale() {
  return current === 'zh' ? zhCN : enUS;
}

/** React binding: re-renders the component when the language changes. */
export function useLang(): Lang {
  const [, force] = useReducer((x: number) => x + 1, 0);
  useEffect(() => {
    listeners.add(force);
    return () => {
      listeners.delete(force);
    };
  }, [force]);
  return current;
}
