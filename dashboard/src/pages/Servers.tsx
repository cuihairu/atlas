// 服务器列表（管理台）：完整筛选条（状态/区域/类型/realm/shard/版本/
// 平台/标记 + 服务器 ID 子串 + metadata 键值对），facets 来自总览同一份
// fleet 聚合（item 12 裁决：不许各页各自现查现算）。筛选状态同步进
// URL query（可分享链接）。数据走 /v1/admin/servers：比公网发现口多
// 最近心跳 / 元数据等管理面字段（item 9）。
import { useEffect, useState, useCallback, useMemo } from 'react';
import { Table, Button, Space, Select, Input, Progress, message, Popconfirm, Card, Typography } from 'antd';
import { useNavigate, useSearchParams } from 'react-router-dom';
import StatusTag from '../components/StatusTag';
import TagBadge from '../components/TagBadge';
import { useLang, t } from '../i18n';
import {
  adminListServers,
  getStats,
  serverMaintenance,
  serverDrain,
  serverEnable,
  serverDisable,
} from '../api/client';
import type { Server, AdminStats } from '../types';

const STATUSES = ['online', 'maintenance', 'suspect', 'offline', 'draining', 'starting'];

/** URL query 与筛选状态的映射：同步双向，外链可复现筛选。 */
interface Filters {
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
}

const FILTER_KEYS: (keyof Filters)[] = [
  'id', 'status', 'region', 'realm', 'shard', 'version', 'type', 'platform', 'tag',
  'metadata_key', 'metadata_value',
];

function filtersFromParams(params: URLSearchParams): Filters {
  const f: Filters = {};
  for (const k of FILTER_KEYS) {
    const v = params.get(k) ?? undefined;
    if (v) f[k] = v;
  }
  return f;
}

function paramsFromFilters(f: Filters): URLSearchParams {
  const p = new URLSearchParams();
  for (const k of FILTER_KEYS) {
    if (f[k]) p.set(k, f[k] as string);
  }
  return p;
}

export default function Servers() {
  useLang(); // re-render on language switch
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [servers, setServers] = useState<Server[]>([]);
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [cursor, setCursor] = useState<string | undefined>();
  const [nextCursor, setNextCursor] = useState<string | undefined>();

  const filters = useMemo(() => filtersFromParams(searchParams), [searchParams]);
  const setFilter = (key: keyof Filters, value?: string) => {
    const next = { ...filters };
    if (value) next[key] = value;
    else delete next[key];
    const p = paramsFromFilters(next);
    setSearchParams(p, { replace: true });
    setCursor(undefined);
  };

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const [res, st] = await Promise.all([
        adminListServers({ ...filters, limit: 50, cursor }),
        getStats().catch(() => null),
      ]);
      setServers(res.servers ?? []);
      setNextCursor(res.next_cursor);
      if (st) setStats(st);
    } finally {
      setLoading(false);
    }
  }, [filters, cursor]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  // facet 选项来自 fleet 聚合（与总览同源）；无统计时退化为常见枚举。
  const facetOptions = (m?: Record<string, number>, fallback: string[] = []) =>
    (m && Object.keys(m).length > 0 ? Object.keys(m) : fallback)
      .sort()
      .map((v) => ({ label: m?.[v] !== undefined ? `${v} (${m[v]})` : v, value: v }));

  const doAction = async (id: string, action: string) => {
    try {
      if (action === 'maintenance') await serverMaintenance(id);
      else if (action === 'drain') await serverDrain(id);
      else if (action === 'enable') await serverEnable(id);
      else if (action === 'disable') await serverDisable(id);
      message.success(t('actionOk'));
      fetch();
    } catch {
      // error handled by client
    }
  };

  const actionsFor = (s: Server) => {
    const btns: { key: string; label: string; danger?: boolean }[] = [];
    if (s.status === 'online') {
      btns.push({ key: 'maintenance', label: t('enterMaintenance') });
      btns.push({ key: 'drain', label: t('drain') });
      btns.push({ key: 'disable', label: t('disable'), danger: true });
    } else {
      btns.push({ key: 'enable', label: t('enable') });
    }
    return btns;
  };

  const hasAnyFilter = Object.keys(filters).length > 0;

  return (
    <>
      <Card size="small" style={{ marginBottom: 16 }} title={t('filter')}>
        <Space wrap size={[12, 8]}>
          <Input.Search
            allowClear
            placeholder={t('serverIdSearch')}
            style={{ width: 180 }}
            defaultValue={filters.id}
            onSearch={(v) => setFilter('id', v || undefined)}
          />
          <Select
            allowClear
            placeholder={t('status')}
            style={{ width: 140 }}
            value={filters.status}
            onChange={(v) => setFilter('status', v)}
            options={facetOptions(stats?.servers_by_status, STATUSES)}
          />
          <Select
            allowClear
            placeholder={t('region')}
            style={{ width: 130 }}
            value={filters.region}
            onChange={(v) => setFilter('region', v)}
            options={facetOptions(stats?.servers_by_region)}
          />
          <Select
            allowClear
            placeholder={t('type')}
            style={{ width: 120 }}
            value={filters.type}
            onChange={(v) => setFilter('type', v)}
            options={facetOptions(stats?.servers_by_type)}
          />
          <Select
            allowClear
            placeholder={t('realm')}
            style={{ width: 120 }}
            value={filters.realm}
            onChange={(v) => setFilter('realm', v)}
            options={facetOptions(stats?.servers_by_realm)}
          />
          <Select
            allowClear
            placeholder={t('shard')}
            style={{ width: 120 }}
            value={filters.shard}
            onChange={(v) => setFilter('shard', v)}
            options={facetOptions(stats?.servers_by_shard)}
          />
          <Select
            allowClear
            placeholder={t('version')}
            style={{ width: 120 }}
            value={filters.version}
            onChange={(v) => setFilter('version', v)}
            options={facetOptions(stats?.servers_by_version)}
          />
          <Select
            allowClear
            placeholder={t('platform')}
            style={{ width: 110 }}
            value={filters.platform}
            onChange={(v) => setFilter('platform', v)}
            options={facetOptions(undefined, ['ios', 'android', 'steam', 'web'])}
          />
          <Select
            allowClear
            placeholder={t('tags')}
            style={{ width: 110 }}
            value={filters.tag}
            onChange={(v) => setFilter('tag', v)}
            options={facetOptions(stats?.servers_by_tag)}
          />
          <Input
            allowClear
            placeholder={t('metadataKey')}
            style={{ width: 130 }}
            value={filters.metadata_key}
            onChange={(e) => setFilter('metadata_key', e.target.value || undefined)}
          />
          <Input
            allowClear
            placeholder={t('metadataValue')}
            style={{ width: 130 }}
            value={filters.metadata_value}
            onChange={(e) => setFilter('metadata_value', e.target.value || undefined)}
            disabled={!filters.metadata_key}
          />
          {hasAnyFilter && (
            <Button onClick={() => { setSearchParams(new URLSearchParams(), { replace: true }); setCursor(undefined); }}>
              {t('reset')}
            </Button>
          )}
        </Space>
      </Card>

      <Table
        dataSource={servers}
        rowKey="id"
        loading={loading}
        pagination={false}
        onRow={(record) => ({
          onClick: () => navigate(`/servers/${record.id}`),
          style: { cursor: 'pointer' },
        })}
        columns={[
          { title: t('id'), dataIndex: 'id', ellipsis: true, width: 170 },
          { title: t('name'), dataIndex: 'name', ellipsis: true },
          { title: t('region'), dataIndex: 'region', width: 70 },
          { title: t('type'), dataIndex: 'type', width: 80, render: (v?: string) => v || '—' },
          { title: t('version'), dataIndex: 'version', width: 90, ellipsis: true },
          {
            title: t('status'),
            dataIndex: 'status',
            width: 95,
            render: (s: string) => <StatusTag status={s} />,
          },
          {
            title: t('lastHeartbeat'),
            dataIndex: 'last_seen_at',
            width: 140,
            render: (v?: string | null) =>
              v ? new Date(v).toLocaleString() : <Typography.Text type="secondary">—</Typography.Text>,
          },
          {
            title: t('tags'),
            dataIndex: 'tags',
            width: 160,
            render: (tags: Server['tags']) =>
              tags && tags.length > 0 ? (
                <Space size={4} wrap>
                  {tags.map((tag) => (
                    <TagBadge key={tag.code} tag={tag} />
                  ))}
                </Space>
              ) : (
                '-'
              ),
          },
          {
            title: t('metadata'),
            dataIndex: 'metadata',
            width: 150,
            ellipsis: true,
            render: (m?: Record<string, string>) =>
              m && Object.keys(m).length > 0
                ? Object.entries(m).slice(0, 3).map(([k, v]) => (
                    <Typography.Text key={k} code style={{ fontSize: 12 }}>{k}={v}</Typography.Text>
                  ))
                : '—',
          },
          {
            title: t('playersCapacity'),
            width: 180,
            render: (_: unknown, r: Server) => {
              const pct = r.capacity > 0 ? Math.round((r.players / r.capacity) * 100) : 0;
              return (
                <Space>
                  <Progress
                    percent={pct}
                    size="small"
                    style={{ width: 70 }}
                    strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
                  />
                  <span>{r.players}/{r.capacity}</span>
                </Space>
              );
            },
          },
          {
            title: t('load'),
            dataIndex: 'load',
            width: 70,
            render: (v: number) => `${(v * 100).toFixed(0)}%`,
          },
          {
            title: t('actions'),
            width: 230,
            render: (_: unknown, record: Server) => (
              <Space>
                {actionsFor(record).map((a) => (
                  <Popconfirm
                    key={a.key}
                    title={t('confirmAction', { name: a.label })}
                    onConfirm={(e) => {
                      e?.stopPropagation();
                      doAction(record.id, a.key);
                    }}
                    onCancel={(e) => e?.stopPropagation()}
                  >
                    <Button
                      size="small"
                      type={a.danger ? 'primary' : 'default'}
                      danger={a.danger}
                      onClick={(e) => e.stopPropagation()}
                    >
                      {a.label}
                    </Button>
                  </Popconfirm>
                ))}
              </Space>
            ),
          },
        ]}
      />
      {nextCursor && (
        <Button
          style={{ marginTop: 16 }}
          onClick={() => setCursor(nextCursor)}
        >
          {t('loadMore')}
        </Button>
      )}
    </>
  );
}
