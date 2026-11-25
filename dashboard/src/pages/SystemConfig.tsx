// Gateway/system config read-only page (item 8): parsed effective rate-limit
// rules (longest-prefix match, per-client token bucket) plus the default rule
// and 429 RATE_LIMITED counts by endpoint rule / client IP. Read-only — the
// config itself comes from environment variables (ATLAS_RATE_LIMITS /
// ATLAS_RATE_LIMIT_DEFAULT) and takes effect after a restart; this page
// offers no write path.
import { useEffect, useState, useCallback } from 'react';
import { Card, Table, Tag, Alert, Descriptions, Typography } from 'antd';
import { useLang, t } from '../i18n';
import { getRateLimits } from '../api/client';
import type { RateLimitStats, RateLimitRuleView } from '../types';

export default function SystemConfig() {
  useLang();
  const [stats, setStats] = useState<RateLimitStats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      setStats(await getRateLimits());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetch();
    const id = setInterval(fetch, 30_000);
    return () => clearInterval(id);
  }, [fetch]);

  const ruleColumns = [
    {
      title: t('rulePrefix'),
      dataIndex: 'prefix',
      render: (v: string) => <Typography.Text code>{v}</Typography.Text>,
    },
    { title: t('rps'), dataIndex: 'rps', width: 120 },
    { title: t('burst'), dataIndex: 'burst', width: 120 },
  ];

  const endpointRows = Object.entries(stats?.rejected_by_endpoint ?? {}).sort((a, b) => b[1] - a[1]);
  const clientRows = Object.entries(stats?.rejected_by_client ?? {}).sort((a, b) => b[1] - a[1]);

  return (
    <>
      <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('readOnlyNote')} />

      <Card title={t('rateLimitRules')} style={{ marginBottom: 16 }} loading={loading}>
        {stats && !stats.enabled ? (
          <Alert type="warning" showIcon message={t('rateLimitDisabled')} />
        ) : (
          <>
            {stats?.default && (
              <Descriptions size="small" column={3} style={{ marginBottom: 12 }} bordered>
                <Descriptions.Item label={t('rateLimitDefaultRule')}>
                  <Typography.Text code>{stats.default.prefix}</Typography.Text>
                </Descriptions.Item>
                <Descriptions.Item label={t('rps')}>{stats.default.rps}</Descriptions.Item>
                <Descriptions.Item label={t('burst')}>{stats.default.burst}</Descriptions.Item>
              </Descriptions>
            )}
            <Table<RateLimitRuleView>
              scroll={{ x: 'max-content' }}
              rowKey="prefix"
              size="small"
              columns={ruleColumns}
              dataSource={stats?.rules ?? []}
              pagination={false}
              locale={{ emptyText: t('noData') }}
            />
          </>
        )}
      </Card>

      <Card title={t('rejected429')} loading={loading}>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
          <div style={{ flex: '1 1 320px', minWidth: 280 }}>
            <Typography.Text strong style={{ display: 'block', marginBottom: 8 }}>
              {t('byEndpoint')}
            </Typography.Text>
            <Table
              scroll={{ x: 'max-content' }}
              rowKey={(r) => r[0]}
              size="small"
              pagination={false}
              locale={{ emptyText: t('no429Yet') }}
              dataSource={endpointRows}
              columns={[
                {
                  title: t('rulePrefix'),
                  render: (_: unknown, r: [string, number]) => <Typography.Text code>{r[0]}</Typography.Text>,
                },
                {
                  title: '429',
                  width: 90,
                  render: (_: unknown, r: [string, number]) =>
                    r[1] > 0 ? <Tag color="red">{r[1]}</Tag> : r[1],
                },
              ]}
            />
          </div>
          <div style={{ flex: '1 1 320px', minWidth: 280 }}>
            <Typography.Text strong style={{ display: 'block', marginBottom: 8 }}>
              {t('byClient')}
            </Typography.Text>
            <Table
              scroll={{ x: 'max-content' }}
              rowKey={(r) => r[0]}
              size="small"
              pagination={false}
              locale={{ emptyText: t('no429Yet') }}
              dataSource={clientRows}
              columns={[
                {
                  title: t('ip'),
                  render: (_: unknown, r: [string, number]) => <Typography.Text code>{r[0]}</Typography.Text>,
                },
                {
                  title: '429',
                  width: 90,
                  render: (_: unknown, r: [string, number]) =>
                    r[1] > 0 ? <Tag color="red">{r[1]}</Tag> : r[1],
                },
              ]}
            />
          </div>
        </div>
      </Card>
    </>
  );
}
