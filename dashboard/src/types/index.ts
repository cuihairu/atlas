export interface Server {
  id: string;
  name: string;
  region: string;
  version: string;
  status: 'online' | 'maintenance' | 'suspect' | 'offline' | 'draining';
  player_count: number;
  capacity: number;
  load: number;
  ip?: string;
  port?: number;
  created_at?: string;
  updated_at?: string;
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