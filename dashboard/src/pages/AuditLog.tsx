// Admin 操作审计流（GET /v1/admin/audit）：谁、什么时候、对哪个路径做了什么
// 改动、结果如何。写操作带变更体（body），读操作只有路径与查询串。
// 服务端是环形缓冲（ATLAS_AUDIT_MAX，默认 1000 条），重启即丢——这里如实
// 呈现"最近 N 条"，不假装是持久化审计归档。
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

/** 变更体：对象原样格式化，非 JSON（服务端兜底成字符串的坏载荷）也照显。 */
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