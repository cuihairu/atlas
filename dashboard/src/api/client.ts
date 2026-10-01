import { message } from 'antd';
import type {
  Server,
  ServerListResponse,
  AdminStats,
  Character,
  CharacterSearchResponse,
  Migration,
  MigrationListResponse,
} from '../types';

const API_BASE = import.meta.env.VITE_API_BASE || 'http://localhost:8080';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${API_BASE}${path}`;
  try {
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
      ...init,
    });
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      throw new Error(`${res.status} ${res.statusText}${text ? `: ${text}` : ''}`);
    }
    return (await res.json()) as T;
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    message.error(`请求失败: ${msg}`);
    throw err;
  }
}

function qs(params: Record<string, string | number | undefined>): string {
  const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== '');
  if (entries.length === 0) return '';
  return '?' + entries.map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`).join('&');
}

// ── Discovery ────────────────────────────────────────────────────────

export function listServers(params?: {
  region?: string;
  status?: string;
  limit?: number;
  cursor?: string;
}): Promise<ServerListResponse> {
  return request(`/v1/discovery/servers${qs(params ?? {})}`);
}

export function getServer(id: string): Promise<Server> {
  return request(`/v1/discovery/servers/${id}`);
}

// ── Directory ────────────────────────────────────────────────────────

export function listCharactersByServer(serverId: string): Promise<Character[]> {
  return request(`/v1/directory/servers/${serverId}/characters`);
}

export function listCharactersByAccount(accountId: string): Promise<Character[]> {
  return request(`/v1/directory/accounts/${accountId}/characters`);
}

export function getCharacter(id: string): Promise<Character> {
  return request(`/v1/directory/characters/${id}`);
}

// ── Admin ────────────────────────────────────────────────────────────

export function getStats(): Promise<AdminStats> {
  return request('/v1/admin/stats');
}

export function serverMaintenance(id: string): Promise<void> {
  return request(`/v1/admin/servers/${id}/maintenance`, { method: 'POST' });
}

export function serverDrain(id: string): Promise<void> {
  return request(`/v1/admin/servers/${id}/drain`, { method: 'POST' });
}

export function serverEnable(id: string): Promise<void> {
  return request(`/v1/admin/servers/${id}/enable`, { method: 'POST' });
}

export function serverDisable(id: string): Promise<void> {
  return request(`/v1/admin/servers/${id}/disable`, { method: 'POST' });
}

export function searchCharacters(params: {
  q?: string;
  server_id?: string;
  class_id?: string;
  min_level?: number;
  max_level?: number;
  limit?: number;
  cursor?: string;
}): Promise<CharacterSearchResponse> {
  return request(`/v1/admin/characters/search${qs(params)}`);
}

export function createMigration(body: {
  source_server_id: string;
  target_server_id: string;
}): Promise<Migration> {
  return request('/v1/admin/migrations', {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export function listMigrations(params?: {
  limit?: number;
  cursor?: string;
}): Promise<MigrationListResponse> {
  return request(`/v1/admin/migrations${qs(params ?? {})}`);
}

export function getMigration(id: string): Promise<Migration> {
  return request(`/v1/admin/migrations/${id}`);
}

export function rollbackMigration(id: string): Promise<void> {
  return request(`/v1/admin/migrations/${id}/rollback`, { method: 'POST' });
}