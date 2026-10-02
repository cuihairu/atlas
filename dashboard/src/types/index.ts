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