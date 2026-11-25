import { message as staticMessage } from 'antd';
import { getAppMessage } from './messageBus';
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
  MaintenanceWindowBatchRequest,
  MaintenanceWindowBatchResponse,
  AuditListResponse,
  CrossServerSpec,
  CrossServerConfig,
  CrossServerSaveResponse,
  RoutingDiagnosis,
  RateLimitStats,
  LoadSeriesResponse,
  LoadHeatmapResponse,
  HeatmapWindow,
  BusSeriesResponse,
  SeriesWindow,
  IndexQueueStatus,
} from '../types';

// Same-origin by default: the vite dev/preview proxy splits the API by path
// (/v1/admin → :8082, everything else → :8080) because the admin listener
// deliberately carries no CORS. Deployments behind one origin keep this;
// anything else sets VITE_API_BASE / VITE_ADMIN_API_BASE.
const API_BASE = (import.meta.env.VITE_API_BASE || '').replace(/\/$/, '');
const ADMIN_BASE = (import.meta.env.VITE_ADMIN_API_BASE || '').replace(/\/$/, '');

// ── Admin console session (API Key login) ────────────────────────────
// The backend admin endpoint has no user system; auth is a Bearer API Key
// (role-scoped admin/operator/viewer, viewer read-only). The "password" in
// account/password is the key issued to that account: the key for the demo
// account "demo" is in the README's "demo site" section. Sessions live in
// localStorage, i.e. this browser only.
const KEY_STORAGE = 'atlas.adminKey';
const ACCOUNT_STORAGE = 'atlas.adminAccount';

export function getAdminKey(): string {
  try {
    return localStorage.getItem(KEY_STORAGE) || '';
  } catch {
    return '';
  }
}

export function getAdminAccount(): string {
  try {
    return localStorage.getItem(ACCOUNT_STORAGE) || '';
  } catch {
    return '';
  }
}

export function setAdminSession(account: string, key: string): void {
  try {
    localStorage.setItem(KEY_STORAGE, key);
    localStorage.setItem(ACCOUNT_STORAGE, account);
  } catch {
    // storage unavailable — session just won't persist
  }
}

export function clearAdminSession(): void {
  try {
    localStorage.removeItem(KEY_STORAGE);
    localStorage.removeItem(ACCOUNT_STORAGE);
  } catch {
    // ignore
  }
}

// Probe whether the session can reach the admin endpoint: 200 = pass
// (including deployments where the server has auth disabled),
// 401/403 = not logged in or the key is stale; network and other errors are
// treated as pass to avoid wrongly locking the user on the login page.
export async function probeAdmin(key = getAdminKey()): Promise<'ok' | 'unauthorized'> {
  try {
    const headers: Record<string, string> = {};
    if (key) headers['Authorization'] = `Bearer ${key}`;
    const res = await fetch(`${ADMIN_BASE}/v1/admin/stats`, { headers });
    return res.status === 401 || res.status === 403 ? 'unauthorized' : 'ok';
  } catch {
    return 'ok';
  }
}

async function request<T>(path: string, init?: RequestInit, base = API_BASE): Promise<T> {
  const url = `${base}${path}`;
  try {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    // Admin calls carry the session key (decided by path: all three bases
    // may carry /v1/admin, and the reverse proxy routes by path to the admin listener)
    const key = getAdminKey();
    if (key && url.includes('/v1/admin')) headers['Authorization'] = `Bearer ${key}`;
    const res = await fetch(url, {
      headers,
      ...init,
    });
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      throw new Error(`${res.status} ${res.statusText}${text ? `: ${text}` : ''}`);
    }
    return (await res.json()) as T;
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    // Prefer the App-context instance (theme-aware); before mount (MessageBridge
    // not ready) fall back to the static one
    const apiMessage = getAppMessage();
    if (apiMessage) apiMessage.error(`${t('requestFailed')}: ${msg}`);
    else staticMessage.error(`${t('requestFailed')}: ${msg}`);
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

// Admin console fleet list (/v1/admin/servers): full filter bar (status/region/
// type/realm/shard/version/platform/tag + ID substring + metadata key-value
// pairs), reading the fleet aggregate index — same source as the overview
// numbers (item 12 decision). Includes admin-plane fields such as heartbeat
// and metadata.
export function adminListServers(params?: {
  id?: string;
  status?: string;
  region?: string;
  realm?: string;
  shard?: string;
  version?: string;
  type?: string;
  platform?: string;
  tag?: string;
  metadata_key?: string;
  metadata_value?: string;
  limit?: number;
  cursor?: string;
}): Promise<ServerListResponse> {
  return request(`/v1/admin/servers${qs(params ?? {})}`, undefined, ADMIN_BASE);
}

// Player-perspective diagnosis (/v1/admin/diagnose/routing): intermediate results of the same recommend pipeline.
export function diagnoseRouting(params: {
  account_id?: string;
  region?: string;
  version?: string;
  platform?: string;
}): Promise<{ diagnosis: RoutingDiagnosis; characters?: Character[] }> {
  return request(`/v1/admin/diagnose/routing${qs(params)}`, undefined, ADMIN_BASE);
}

// Gateway/system config read-only page: parsed rate-limit rules + 429 counts.
export function getRateLimits(): Promise<RateLimitStats> {
  return request('/v1/admin/rate-limits', undefined, ADMIN_BASE);
}

// Load time view (overview): players / load dual lines, window tiers 5m-10h.
export function getLoadSeries(params: {
  window?: SeriesWindow;
  server_id?: string;
  region?: string;
}): Promise<LoadSeriesResponse> {
  return request(`/v1/admin/load-series${qs(params)}`, undefined, ADMIN_BASE);
}

// Server heatmap: one row per live server, bucketed mean load (last 1h/24h).
export function getLoadHeatmap(params: { window?: HeatmapWindow }): Promise<LoadHeatmapResponse> {
  return request(`/v1/admin/load-heatmap${qs(params)}`, undefined, ADMIN_BASE);
}

// Message bus series: per-topic backlog depth + produce/consume rates.
export function getBusSeries(params: { window?: SeriesWindow }): Promise<BusSeriesResponse> {
  return request(`/v1/admin/bus-series${qs(params)}`, undefined, ADMIN_BASE);
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

/** Set the routing weight (0..1000, 0 clears = neutral). */
export function setServerWeight(
  id: string,
  weight: number,
): Promise<{ server_id: string; weight: number }> {
  return request(`/v1/admin/servers/${id}/weight`, {
    method: 'POST',
    body: JSON.stringify({ weight }),
  }, ADMIN_BASE);
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
  /** Player ID search: opaque account reference (the platform does not interpret it). */
  account_id?: string;
  /** Metadata key-value filtering (business concepts such as class go here; not built into the platform). */
  metadata_key?: string;
  metadata_value?: string;
  min_level?: number;
  max_level?: number;
  limit?: number;
  cursor?: string;
}): Promise<CharacterSearchResponse> {
  return request(`/v1/admin/characters/search${qs(params)}`, undefined, ADMIN_BASE);
}

// Body keys are the server's wire contract (migration.CreateMigrationRequest):
// source_servers is an array, target_server a single id. The handler
// rejects an empty source_servers as INVALID_ARGUMENT.
export function createMigration(body: {
  source_servers: string[];
  target_server: string;
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
// ── Announcements and maintenance windows ────────────────────────────

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

/** Fleet-level batch windows: one template spread across multiple servers, partial-success semantics (successes go into windows, unknown/invalid into failed). */
export function createMaintenanceWindowsBatch(
  body: MaintenanceWindowBatchRequest,
): Promise<MaintenanceWindowBatchResponse> {
  return request('/v1/admin/maintenance-windows/batch', {
    method: 'POST',
    body: JSON.stringify(body),
  }, ADMIN_BASE);
}

// ── Admin operation audit ────────────────────────────────────────────

/** Latest N admin operations (server-side ring buffer, 50 by default). */
export function listAuditEntries(params?: { limit?: number }): Promise<AuditListResponse> {
  return request(`/v1/admin/audit${qs(params ?? {})}`, undefined, ADMIN_BASE);
}

// ── Cross-server config center ───────────────────────────────────────

export function getCrossServerConfig(): Promise<CrossServerConfig> {
  return request('/v1/admin/crossserver/config', undefined, ADMIN_BASE);
}

export function updateCrossServerConfig(spec: CrossServerSpec): Promise<CrossServerSaveResponse> {
  return request('/v1/admin/crossserver/config', {
    method: 'PUT',
    body: JSON.stringify(spec),
  }, ADMIN_BASE);
}

// ── Index queue status (/v1/admin/indexqueue/status) ─────────────────

export function getIndexQueueStatus(): Promise<IndexQueueStatus> {
  return request('/v1/admin/indexqueue/status', undefined, ADMIN_BASE);
}
