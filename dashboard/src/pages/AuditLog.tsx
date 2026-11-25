// Admin operation audit stream (GET /v1/admin/audit): who changed what, when,
// on which path, and with what result. Write operations carry a change body;
// read operations only have path and query string. On the server this is a
// ring buffer (ATLAS_AUDIT_MAX, default 1000 entries) lost on restart — shown
// honestly as "latest N entries", not passed off as a persistent audit archive.
import { useEffect, useState, useCallback } from 'react';
import { Card, Table, Button, Space, Tag, Result, Typography, Select, Tooltip } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useLang, t } from '../i18n';
import { listAuditEntries } from '../api/client';
import type { AuditEntry } from '../types';

const LIMIT_OPTIONS = [50, 100, 200, 500];

const METHOD_COLOR: Record<string, string> = {
  POST: 'blue',
  PUT: 'purple',
  PATCH: 'gold',
  DELETE: 'red',
};

const isWrite = (m: string) => m !== 'GET' && m !== 'HEAD';

function statusColor(s: number): string {
  if (s >= 500) return 'red';
  if (s >= 400) return 'volcano';
  if (s >= 300) return 'gold';
  return 'green';
}

/** Change body: objects are formatted as-is; non-JSON payloads (bad payloads
 * the server stringified as a fallback) are still shown verbatim. */
function bodyText(body: unknown): string | null {
  if (body == null) return null;
  if (typeof body === 'string') return body;
  try {
    return JSON.stringify(body, null, 2);
  } catch {
    return String(body);
  }
}

export default function AuditLog() {
  useLang();
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [limit, setLimit] = useState(50);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const fetchEntries = useCallback(async () => {
    setLoading(true);
    setError(false);
    try {
      const res = await listAuditEntries({ limit });
      setEntries(res.entries ?? []);
    } catch {
      setEntries([]);
      setError(true);
    } finally {
      setLoading(false);
    }
  }, [limit]);

  useEffect(() => { fetchEntries(); }, [fetchEntries]);

  const columns = [
    {
      title: t('auditTime'),
      dataIndex: 'time',
      width: 180,
      render: (v?: string) => (v ? new Date(v).toLocaleString() : '—'),
    },
    { title: t('auditActor'), dataIndex: 'actor', width: 160, ellipsis: true },
    {
      title: t('auditMethod'),
      dataIndex: 'method',
      width: 110,
      render: (m: string) => (
        <Space size={4}>
          <Tag color={METHOD_COLOR[m] ?? 'default'}>{m}</Tag>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {isWrite(m) ? t('auditWrite') : t('auditRead')}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: t('auditPath'),
      dataIndex: 'path',
      ellipsis: true,
      render: (v: string, row: AuditEntry) => (
        <Tooltip title={row.query ? `${v}?${row.query}` : v}>
          <span>{v}</span>
        </Tooltip>
      ),
    },
    {
      title: t('auditStatus'),
      dataIndex: 'status',
      width: 90,
      render: (s: number) => <Tag color={statusColor(s)}>{s}</Tag>,
    },
    {
      title: t('auditChange'),
      key: 'change',
      width: 280,
      ellipsis: true,
      render: (_: unknown, row: AuditEntry) => {
        const text = bodyText(row.body);
        if (!text) return '—';
        return (
          <Tooltip title={<pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{text}</pre>}>
            <code style={{ fontSize: 12 }}>{text.replace(/\s+/g, ' ')}</code>
          </Tooltip>
        );
      },
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={t('auditLog')}
        extra={
          <Space>
            <Select
              value={limit}
              onChange={setLimit}
              options={LIMIT_OPTIONS.map((n) => ({ value: n, label: `${n}` }))}
              style={{ width: 90 }}
              aria-label={t('auditLimit')}
            />
            <Button icon={<ReloadOutlined />} onClick={fetchEntries} loading={loading}>
              {t('refresh')}
            </Button>
          </Space>
        }
      >
        {error ? (
          <Result
            status="warning"
            title={t('auditLoadFailed')}
            extra={
              <Button type="primary" onClick={fetchEntries}>
                {t('retry')}
              </Button>
            }
          />
        ) : (
          <Table
            scroll={{ x: 'max-content' }}
            rowKey={(row) => `${row.time}-${row.method}-${row.path}-${row.status}`}
            size="small"
            loading={loading}
            columns={columns}
            dataSource={entries}
            pagination={{ pageSize: 20, hideOnSinglePage: true }}
            locale={{ emptyText: t('noData') }}
          />
        )}
      </Card>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        {t('auditHint')}
      </Typography.Paragraph>
    </Space>
  );
}