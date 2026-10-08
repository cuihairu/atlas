/** Tag presentation tiers — model.TagTier on the Go side. */
export type TagTier = 'hot' | 'new' | 'warning' | 'info' | 'neutral';

/**
 * One server tag. Presets (hot/full/no_register/maintenance/new/recommended)
 * carry a Chinese label and default tier; custom tags default to internal
 * (public=false, never shown on player-facing surfaces).
 */
export interface ServerTag {
  code: string;
  label: string;
  tier: TagTier;
  public?: boolean;
}

export interface ServerEndpoint {
  host: string;
  port: number;
}

export interface Server {
  id: string;
  name: string;
  type?: string;
  region: string;
  version: string;
  platform?: string;
  realm_id?: string | null;
  shard_id?: string | null;
  status: 'online' | 'maintenance' | 'suspect' | 'offline' | 'draining' | 'starting';
  /** Player-facing responses expose only public tags. */
  tags?: ServerTag[];
  players: number;
  capacity: number;
  load: number;
  endpoint?: ServerEndpoint;
  created_at?: string;
  updated_at?: string;
  /** 开服时间（进程本次启动），详情页在线时长从这里起算。 */
  started_at?: string;
  /** 最近心跳时间；空 = 尚无心跳。 */
  last_seen_at?: string | null;
  /** 注册元数据（平台不解释内容，只透传与过滤）。 */
  metadata?: Record<string, string>;
}

/** 在线时长人性化：3天2小时 / 45分钟（item 9 口径）。 */
export function humanDuration(fromISO?: string | null, toISO?: string | null): string {
  if (!fromISO) return '—';
  const from = new Date(fromISO).getTime();
  if (Number.isNaN(from)) return '—';
  const to = toISO ? new Date(toISO).getTime() : Date.now();
  let sec = Math.max(0, Math.floor((to - from) / 1000));
  const days = Math.floor(sec / 86400);
  sec -= days * 86400;
  const hours = Math.floor(sec / 3600);
  sec -= hours * 3600;
  const mins = Math.floor(sec / 60);
  const zh = getLangSafe();
  if (zh) {
    if (days > 0) return `${days}天${hours}小时`;
    if (hours > 0) return `${hours}小时${mins}分钟`;
    return `${mins}分钟`;
  }
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function getLangSafe(): boolean {
  try {
    return (localStorage.getItem('atlas-lang') ?? 'zh') !== 'en';
  } catch {
    return true;
  }
}

export interface ServerTagListResponse {
  server_id: string;
  tags: ServerTag[];
}

export interface ServerListResponse {
  servers: Server[];
  next_cursor?: string;
}

export interface AdminStats {
  total_servers: number;
  online_servers: number;
  total_players: number;
  total_capacity: number;
  total_characters?: number;
  servers_by_status: Record<string, number>;
  servers_by_region: Record<string, number>;
  servers_by_version?: Record<string, number>;
  /** 筛选条 facets（fleet 聚合，item 12 裁决：所有视图读同一聚合）。 */
  servers_by_type?: Record<string, number>;
  servers_by_realm?: Record<string, number>;
  servers_by_shard?: Record<string, number>;
  servers_by_tag?: Record<string, number>;
  /** 舰队里出现过的 metadata 键（供筛选下拉）。 */
  server_metadata_keys?: string[];
}

export interface Character {
  account_id: string;
  server_id: string;
  character_id: string;
  name: string;
  level: number;
  class_id: string;
  /** Go 侧序列化为 last_login_at（此前前端误写 last_login，恒渲染 -）。 */
  last_login_at?: string | null;
  metadata?: Record<string, string>;
}

// ── 玩家视角排查（/v1/admin/diagnose/routing）───────────────────────
// 与 Go routing.Diagnosis / ServerVerdict 同构：recommend 同一条管线的
// 中间结果摊开（排序 / 逐维判定 / 归因），不另设判据。

export interface DiagnosisServerVerdict {
  /** 内嵌完整服务器记录（与列表同构）。 */
  server: Server | null;
  /** 候选池排序位（1 起）；0 = 未入候选（看 reason）。 */
  rank: number;
  matched_strict: boolean;
  matched_fallback: boolean;
  owned: boolean;
  eligible: boolean;
  /** 未入候选时的拒绝原因（region=na (需要 eu)）或冠军归因。 */
  reason?: string;
}

export interface RoutingDiagnosis {
  request: {
    region?: string;
    version?: string;
    platform?: string;
    status?: string;
    account_id?: number;
  };
  /** strict（过滤器命中）/ fallback（仅状态兜底）/ none（全空）。 */
  stage: 'strict' | 'fallback' | 'none';
  servers: DiagnosisServerVerdict[];
  winner_id?: string;
  winner_reason?: string;
}

// ── 网关/系统配置只读页（/v1/admin/rate-limits）─────────────────────

export interface RateLimitRuleView {
  prefix: string;
  rps: number;
  burst: number;
}

export interface RateLimitStats {
  enabled: boolean;
  default?: RateLimitRuleView;
  rules: RateLimitRuleView[];
  rejected_by_endpoint: Record<string, number>;
  rejected_by_client: Record<string, number>;
}

// ── 负载时间视图 / 消息总线曲线（/v1/admin/load-series · bus-series）──

export type SeriesWindow = '5m' | '10m' | '30m' | '1h' | '10h';

export interface LoadSeriesPoint {
  t: string;
  players: number;
  load: number;
}

export interface LoadSeriesResponse {
  scope: 'fleet' | 'region' | 'server';
  window: string;
  points: LoadSeriesPoint[];
}

export interface BusTopicSeries {
  topic: string;
  published: number;
  consumed: number;
  in_flight: number;
  produce_rate: number;
  consume_rate: number;
  depth: { t: string; v: number }[];
}

export interface BusSeriesResponse {
  adapter: string;
  window: string;
  topics: BusTopicSeries[];
}

export interface CharacterSearchResponse {
  characters: Character[];
  next_cursor?: string;
}

export interface Migration {
  id: string;
  source_server_id: string;
  target_server_id: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'rolled_back';
  started_at?: string;
  completed_at?: string;
  created_at?: string;
  error?: string;
}

export interface MigrationListResponse {
  migrations: Migration[];
  next_cursor?: string;
}
// ── 公告与维护窗口（/v1/admin/announcements · maintenance-windows）────

export type AnnouncementLevel = 'info' | 'warning' | 'critical';

export interface Announcement {
  id: string;
  server_id?: string | null;
  title: string;
  body: string;
  level: AnnouncementLevel;
  starts_at: string;
  ends_at: string;
  created_at?: string;
}

export interface AnnouncementListResponse {
  announcements: Announcement[];
}

export interface MaintenanceWindow {
  id: string;
  server_id: string;
  start_at: string;
  end_at: string;
  previous_status: string;
  announcement_id?: string | null;
  applied?: boolean;
  created_at?: string;
}

export interface MaintenanceWindowListResponse {
  maintenance_windows: MaintenanceWindow[];
}

// ── 跨服配置中心（/v1/admin/crossserver/config）──────────────────────

export interface CrossServerCluster {
  id: string;
  name?: string;
  region?: string;
  status?: string; // active (default) | disabled
  servers: string[];
}

export interface CrossServerGroup {
  id: string;
  name?: string;
  servers: string[];
}

export interface CrossServerMatchDomain {
  id: string;
  name?: string;
  servers: string[];
  params?: Record<string, string>;
}

// 跨服玩法类型表（crossplay_types）一行：与 Go model.CrossPlayType 同构。
// lifecycle 缺省 persistent；id_prefix 是该类型运行时 ID 前缀（唯一）。
export interface CrossPlayType {
  id: string;
  name?: string;
  summary?: string;
  lifecycle?: 'persistent' | 'seasonal' | 'ephemeral';
  matchmaking?: boolean;
  ranking?: boolean;
  id_prefix?: string;
}

export interface CrossServerSpec {
  topology: { clusters: CrossServerCluster[] };
  groups: CrossServerGroup[];
  features: Record<string, boolean>;
  match_domains: CrossServerMatchDomain[];
  crossplay_types: CrossPlayType[];
}

export interface CrossServerConfig {
  version: number;
  hash: string;
  spec: CrossServerSpec;
  updated_at?: string;
}

export interface CrossServerNotifyResult {
  bus: string;
  bus_error?: string;
  targets: string[];
  idempotent: boolean;
  callbacks: {
    targets: number;
    delivered: number;
    failed: number;
    errors?: string[];
  };
}

export interface CrossServerSaveResponse {
  config: CrossServerConfig;
  notify: CrossServerNotifyResult;
}

// ── 指令队列状态（/v1/admin/indexqueue/status）────────────────────────
// 仅内存存储实现 store.QueueStatusProvider；SQL 存储返回 503。
// 字段对应 Go internal/store/store.go 的 QueueStats 结构体。

export interface FlushRecord {
  at: string;
  size: number;
  merged: number;
  duration_ns: number;
}

export interface IndexQueueStatus {
  enabled: boolean;
  watermark: number;
  lanes_control: number;
  lanes_hot: number;
  depth_control: number;
  depth_hot: number;
  enqueued: number;
  merged: number;
  applied: number;
  idempotent_hits: number;
  backpressure_sync: number;
  lane_cap: number;
  batch_cap: number;
  last_flush: string;
  last_flush_batch: number;
  last_flush_duration_ns: number;
  recent_flushes: FlushRecord[];
  applied_by_kind: Record<string, number>;
  capture_len: number;
}
