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
  region: string;
  version: string;
  status: 'online' | 'maintenance' | 'suspect' | 'offline' | 'draining' | 'starting';
  /** Player-facing responses expose only public tags. */
  tags?: ServerTag[];
  players: number;
  capacity: number;
  load: number;
  endpoint?: ServerEndpoint;
  created_at?: string;
  updated_at?: string;
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
  servers_by_status: Record<string, number>;
  servers_by_region: Record<string, number>;
}

export interface Character {
  account_id: string;
  server_id: string;
  character_id: string;
  name: string;
  level: number;
  class_id: string;
  last_login?: string;
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
