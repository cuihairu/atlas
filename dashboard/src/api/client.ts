import { message } from 'antd';
import { t } from '../i18n';
import type {
  Server,
  ServerListResponse,
  AdminStats,
  Character,
  CharacterSearchResponse,
  Migration,
  MigrationListResponse,
  ServerTagListResponse,
  Announcement,
  AnnouncementListResponse,
  MaintenanceWindow,
  MaintenanceWindowListResponse,
  CrossServerSpec,
  CrossServerConfig,
  CrossServerSaveResponse,
} from '../types';

// Same-origin by default: the vite dev/preview proxy splits the API by path
// (/v1/admin → :8082, everything else → :8080) because the admin listener
// deliberately carries no CORS. Deployments behind one origin keep this;
// anything else sets VITE_API_BASE / VITE_ADMIN_API_BASE.
const API_BASE = (import.meta.env.VITE_API_BASE || '').replace(/\/$/, '');
const ADMIN_BASE = (import.meta.env.VITE_ADMIN_API_BASE || '').replace(/\/$/, '');

async function request<T>(path: string, init?: RequestInit, base = API_BASE): Promise<T> {
  const url = `${base}${path}`;
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
    message.error(`${t('requestFailed')}: ${msg}`);
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

// Admin calls go to the management listener (:8082) — either through the
// /v1/admin proxy or via VITE_ADMIN_API_BASE.

export function getStats(): Promise<AdminStats> {
  return request('/v1/admin/stats', undefined, ADMIN_BASE);
}

export function getServerTags(id: string): Promise<ServerTagListResponse> {
  return request(`/v1/admin/servers/${id}/tags`, undefined, ADMIN_BASE);
}

export function addServerTag(
  id: string,
  body: { code: string; label?: string; tier?: string; public?: boolean },
): Promise<ServerTagListResponse> {
  return request(`/v1/admin/servers/${id}/tags`, {
    method: 'POST',
    body: JSON.stringify(body),
  }, ADMIN_BASE);
}

export function removeServerTag(
  id: string,
  code: string,
): Promise<{ server_id: string; removed: string }> {
  return request(`/v1/admin/servers/${id}/tags/${code}`, { method: 'DELETE' }, ADMIN_BASE);
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
// ── 公告与维护窗口 ────────────────────────────────────────────────────

export function listAnnouncements(params?: {
  server_id?: string;
  active?: 'true';
}): Promise<AnnouncementListResponse> {
  return request(`/v1/admin/announcements${qs(params ?? {})}`, undefined, ADMIN_BASE);
}

export function createAnnouncement(body: {
  title: string;
  body: string;
  level: string;
  server_id?: string;
  starts_at: string;
  ends_at: string;
}): Promise<Announcement> {
  return request('/v1/admin/announcements', {
    method: 'POST',
    body: JSON.stringify(body),
  }, ADMIN_BASE);
}

export function deleteAnnouncement(id: string): Promise<void> {
  return request(`/v1/admin/announcements/${id}`, { method: 'DELETE' }, ADMIN_BASE);
}

export function listMaintenanceWindows(params?: {
  server_id?: string;
}): Promise<MaintenanceWindowListResponse> {
  return request(`/v1/admin/maintenance-windows${qs(params ?? {})}`, undefined, ADMIN_BASE);
}

export function createMaintenanceWindow(
  serverId: string,
  body: { start_at: string; end_at: string; announce?: boolean },
): Promise<MaintenanceWindow> {
  return request(`/v1/admin/servers/${serverId}/maintenance-window`, {
    method: 'POST',
    body: JSON.stringify(body),
  }, ADMIN_BASE);
}

export function deleteMaintenanceWindow(id: string): Promise<void> {
  return request(`/v1/admin/maintenance-windows/${id}`, { method: 'DELETE' }, ADMIN_BASE);
}

// ── 跨服配置中心 ─────────────────────────────────────────────────────

export function getCrossServerConfig(): Promise<CrossServerConfig> {
  return request('/v1/admin/crossserver/config', undefined, ADMIN_BASE);
}

export function updateCrossServerConfig(spec: CrossServerSpec): Promise<CrossServerSaveResponse> {
  return request('/v1/admin/crossserver/config', {
    method: 'PUT',
    body: JSON.stringify(spec),
  }, ADMIN_BASE);
}
