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
  status: 'online' | 'maintenance' | 'suspect' | 'offline' | 'draining' | 'starting' | 'disabled';
  /** Player-facing responses expose only public tags. */
  tags?: ServerTag[];
  /** Operational routing weight (canary rollout knob): 100 = neutral, >100 attracts traffic, <100 suppresses it. */
  weight?: number;
  players: number;
  capacity: number;
  load: number;
  endpoint?: ServerEndpoint;
  created_at?: string;
  updated_at?: string;
  /** Server start time (this process run); the detail page counts uptime from here. */
  started_at?: string;
  /** Last heartbeat time; empty = no heartbeat yet. */
  last_seen_at?: string | null;
  /** Registration metadata (the platform does not interpret contents, only passes them through and filters on them). */
  metadata?: Record<string, string>;
}

/** Human-readable uptime: 3 days 2 hours / 45 minutes (item 9 semantics). */
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
  /** Filter-bar facets (fleet aggregation, item 12 decision: all views read the same aggregate). */
  servers_by_type?: Record<string, number>;
  servers_by_realm?: Record<string, number>;
  servers_by_shard?: Record<string, number>;
  servers_by_tag?: Record<string, number>;
  /** Metadata keys seen across the fleet (for the filter dropdown). */
  server_metadata_keys?: string[];
}

export interface Character {
  account_id: string;
  server_id: string;
  character_id: string;
  name: string;
  level: number;
  class_id: string;
  /** The Go side serializes this as last_login_at (the frontend previously wrote last_login by mistake, which always rendered "-"). */
  last_login_at?: string | null;
  metadata?: Record<string, string>;
}

// ── Player-perspective diagnosis (/v1/admin/diagnose/routing) ───────
// Isomorphic to Go routing.Diagnosis / ServerVerdict: lays out the
// intermediate results of the same recommend pipeline (ranking /
// per-dimension verdict / attribution) instead of defining new criteria.

export interface DiagnosisServerVerdict {
  /** Embedded full server record (same shape as the list). */
  server: Server | null;
  /** Position in the candidate pool ranking (1-based); 0 = not in candidates (see reason). */
  rank: number;
  matched_strict: boolean;
  matched_fallback: boolean;
  owned: boolean;
  /** A maintenance window that is active or starts within the Lead (window exclusion verdict). */
  maintenance_window?: MaintenanceWindow;
  /** Operational routing weight (0 = unset, same as neutral). */
  weight?: number;
  /** Effective load = load × 100 / weight (only meaningful when weight > 0 and ≠ 100). */
  effective_load?: number;
  eligible: boolean;
  /** Rejection reason when not in candidates (region=na (needs eu)) or attribution for the winner. */
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
  /** strict (filter hit) / fallback (status-only fallback) / none (all empty). */
  stage: 'strict' | 'fallback' | 'none';
  servers: DiagnosisServerVerdict[];
  winner_id?: string;
  winner_reason?: string;
}

// ── Gateway/system config read-only page (/v1/admin/rate-limits) ────

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

// ── Load time view / message bus series (/v1/admin/load-series · bus-series) ──

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

// ── Server heatmap (/v1/admin/load-heatmap · GetLoadHeatmap) ──

export type HeatmapWindow = '1h' | '24h';

// One time bucket of a server's load history. samples === 0 means no data
// in that span (restart, mid-window registration) — rendered as its own
// "no data" state, never as a zero-load color.
export interface HeatBucket {
  t: string;
  load: number;
  players: number;
  samples: number;
}

export interface ServerHeatmap {
  server_id: string;
  name: string;
  region: string;
  type: string;
  status: string;
  players: number;
  capacity: number;
  load: number;
  buckets: HeatBucket[];
}

export interface LoadHeatmapResponse {
  window: string;
  bucket: string;
  generated_at: string;
  servers: ServerHeatmap[];
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

// ── Admin operation audit (GET /v1/admin/audit) ──────────────────────
// Server-side ring buffer of the latest N entries (ATLAS_AUDIT_MAX, default
// 1000); REST and gRPC write operations share one ring (both feed into
// httpapi.AuditLog.Record).
export interface AuditEntry {
  time: string;
  /** "role:certificate fingerprint" or "role:api-key" (httpapi.Actor.String). */
  actor: string;
  method: string;
  path: string;
  status: number;
  /** Change record: only mutating methods have one, truncated to ATLAS_AUDIT_MAX_BODY. */
  body?: unknown;
  query?: string;
}

export interface AuditListResponse {
  entries: AuditEntry[];
}

// Mirrors model.Migration: the source side is an array (multi-source merge)
// and the target is a single server id.
export interface Migration {
  id: string;
  source_servers: string[];
  target_server: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'rolled_back';
  started_at?: string;
  completed_at?: string;
}

export interface MigrationListResponse {
  migrations: Migration[];
  next_cursor?: string;
}
// ── Announcements and maintenance windows (/v1/admin/announcements · maintenance-windows) ──

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

/** Fleet-level batch window template: one time span spread over server_ids, with staggered offsets / dry_run preview. */
export interface MaintenanceWindowBatchRequest {
  server_ids: string[];
  /** Defaults to the current time. */
  start_at?: string;
  end_at: string;
  /** The i-th server's window shifts back by i × stagger_seconds (rolling maintenance); 0 = simultaneous. */
  stagger_seconds?: number;
  /** Defaults to true: each server automatically gets one associated warning announcement created. */
  announce?: boolean;
  /** Only validate and return the planned windows (ids are preview values); nothing is persisted. */
  dry_run?: boolean;
}

export interface MaintenanceWindowBatchFailure {
  server_id: string;
  error: string;
}

/** The batch is partially-successful semantics: successful ones go into windows, unknown/invalid servers each go into failed. */
export interface MaintenanceWindowBatchResponse {
  windows: MaintenanceWindow[];
  failed: MaintenanceWindowBatchFailure[];
}

// ── Cross-server config center (/v1/admin/crossserver/config) ────────

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

// One row of the crossplay types table (crossplay_types): isomorphic to Go
// model.CrossPlayType. lifecycle defaults to persistent; id_prefix is the
// runtime ID prefix for that type (unique).
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

// ── Index queue status (/v1/admin/indexqueue/status) ─────────────────
// Only the in-memory store implements store.QueueStatusProvider; SQL stores
// return 503. Fields mirror the QueueStats struct in Go internal/store/store.go.

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
