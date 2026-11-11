import { useEffect, useState } from 'react';
import { Card, Row, Col, Statistic, Tag, Space, Divider, Tooltip, Alert, Table } from 'antd';
import { ExclamationCircleOutlined } from '@ant-design/icons';
import { getIndexQueueStatus } from '../api/client';
import { useLang, t } from '../i18n';
import type { IndexQueueStatus } from '../types';

const REFRESH_INTERVAL = 10_000;

// Pressure thresholds for the status light
const DEPTH_WARN_THRESHOLD = 500;  // yellow
const DEPTH_CRIT_THRESHOLD = 2000; // red
const BACKPRESSURE_WARN = 10;      // yellow
const BACKPRESSURE_CRIT = 100;     // red

function severityColor(depth: number, backpressure: number): 'success' | 'warning' | 'error' {
  if (depth >= DEPTH_CRIT_THRESHOLD || backpressure >= BACKPRESSURE_CRIT) return 'error';
  if (depth >= DEPTH_WARN_THRESHOLD || backpressure >= BACKPRESSURE_WARN) return 'warning';
  return 'success';
}

function formatDuration(ns: number): string {
  if (ns === 0) return '—';
  const ms = ns / 1_000_000;
  if (ms < 1) return `${(ns / 1_000).toFixed(0)} µs`;
  if (ms < 1000) return `${ms.toFixed(1)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function formatTimestamp(iso: string): string {
  if (!iso || iso === '0001-01-01T00:00:00Z') return '—';
  try {
    return new Date(iso).toLocaleTimeString();
  } catch {
    return iso;
  }
}

export default function QueueStatusCard() {
  useLang(); // re-render on language switch
  const [status, setStatus] = useState<IndexQueueStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetch = async () => {
    try {
      const data = await getIndexQueueStatus();
      setStatus(data);
      setError(null);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      setStatus(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetch();
    const id = setInterval(fetch, REFRESH_INTERVAL);
    return () => clearInterval(id);
  }, []);

  if (loading) {
    return (
      <Card title={t('queueStatus')} size="small" loading>
        <Row gutter={16}>
          <Col span={12}><Statistic loading /></Col>
          <Col span={12}><Statistic loading /></Col>
        </Row>
      </Card>
    );
  }

  if (error) {
    // 503 INDEX_QUEUE_DISABLED = SQL store, queue not available
    const disabled = error.includes('503') || error.includes('INDEX_QUEUE_DISABLED');
    return (
      <Card title={t('queueStatus')} size="small">
        <Alert
          message={disabled ? t('queueDisabled') : t('queueError')}
          description={disabled ? t('queueDisabledDesc') : error}
          type={disabled ? 'info' : 'error'}
          icon={disabled ? <Tooltip title={t('queueDisabledDesc')}><ExclamationCircleOutlined /></Tooltip> : undefined}
          showIcon
          style={{ marginBottom: 0 }}
        />
      </Card>
    );
  }

  if (!status || !status.enabled) {
    return (
      <Card title={t('queueStatus')} size="small">
        <Alert message={t('queueUnavailable')} type="info" showIcon />
      </Card>
    );
  }

  const s = status;
  const depthTotal = s.depth_control + s.depth_hot;
  const sev = severityColor(depthTotal, s.backpressure_sync);
  const mergeRate = s.enqueued > 0 ? ((s.merged / s.enqueued) * 100).toFixed(1) : '0';

  return (
    <Card title={t('queueStatus')} size="small">
      {/* Pressure light + version */}
      <Row gutter={16} align="middle" style={{ marginBottom: 12 }}>
        <Col span={12}>
          <Space direction="vertical" style={{ width: '100%' }}>
            <Statistic
              title={t('watermark')}
              value={s.watermark.toLocaleString()}
              precision={0}
            />
            <Row gutter={8}>
              <Col span={12}>
                <Statistic
                  title={t('depthControl')}
                  value={s.depth_control}
                  valueStyle={{ color: s.depth_control > DEPTH_CRIT_THRESHOLD ? '#ff4d4f' : s.depth_control > DEPTH_WARN_THRESHOLD ? '#faad14' : undefined }}
                />
              </Col>
              <Col span={12}>
                <Statistic
                  title={t('depthHot')}
                  value={s.depth_hot}
                  valueStyle={{ color: s.depth_hot > DEPTH_CRIT_THRESHOLD ? '#ff4d4f' : s.depth_hot > DEPTH_WARN_THRESHOLD ? '#faad14' : undefined }}
                />
              </Col>
            </Row>
          </Space>
        </Col>
        <Col span={12}>
          <Space direction="vertical" style={{ width: '100%', alignItems: 'flex-end' }}>
            <Tooltip title={t('pressureLightTooltip')}>
              <Tag color={sev} style={{ fontSize: 14, padding: '4px 12px' }}>
                {sev === 'success' ? t('pressureNormal') : sev === 'warning' ? t('pressureWarn') : t('pressureCritical')}
              </Tag>
            </Tooltip>
            <Statistic
              title={t('mergeRate')}
              value={Number(mergeRate)}
              suffix="%"
              precision={1}
            />
          </Space>
        </Col>
      </Row>

      <Divider />

      {/* Counters */}
      <Row gutter={16} style={{ marginBottom: 12 }}>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('enqueued')} value={s.enqueued.toLocaleString()} precision={0} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('applied')} value={s.applied.toLocaleString()} precision={0} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('merged')} value={s.merged.toLocaleString()} precision={0} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('idempotentHits')} value={s.idempotent_hits.toLocaleString()} precision={0} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('backpressure')} value={s.backpressure_sync.toLocaleString()} precision={0}
            valueStyle={{ color: s.backpressure_sync > BACKPRESSURE_CRIT ? '#ff4d4f' : s.backpressure_sync > BACKPRESSURE_WARN ? '#faad14' : undefined }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Statistic title={t('captureLen')} value={s.capture_len.toLocaleString()} precision={0} />
        </Col>
      </Row>

      <Divider />

      {/* Last flush */}
      <Row gutter={16}>
        <Col xs={24} sm={12} lg={8}>
          <Statistic title={t('lastFlushAt')} value={formatTimestamp(s.last_flush)} />
        </Col>
        <Col xs={24} sm={12} lg={8}>
          <Statistic title={t('lastFlushBatch')} value={s.last_flush_batch} precision={0} />
        </Col>
        <Col xs={24} sm={12} lg={8}>
          <Statistic title={t('lastFlushDuration')} value={formatDuration(s.last_flush_duration_ns)} />
        </Col>
      </Row>

      {/* Recent flushes */}
      {s.recent_flushes && s.recent_flushes.length > 0 && (
        <>
          <Divider />
          <Row gutter={16}>
            <Col span={24}>
              <strong>{t('recentFlushes')}</strong>
              <Table
                dataSource={s.recent_flushes}
                rowKey="at"
                pagination={false}
                size="small"
                columns={[
                  { title: t('flushAt'), dataIndex: 'at', render: (v: string) => formatTimestamp(v), width: '25%' },
                  { title: t('flushSize'), dataIndex: 'size', width: '15%' },
                  { title: t('flushMerged'), dataIndex: 'merged', width: '15%' },
                  { title: t('flushDuration'), dataIndex: 'duration_ns', render: (v: number) => formatDuration(v), width: '20%' },
                ]}
              />
            </Col>
          </Row>
        </>
      )}

      {/* Applied by kind */}
      {s.applied_by_kind && Object.keys(s.applied_by_kind).length > 0 && (
        <>
          <Divider />
          <Row gutter={16}>
            <Col span={24}>
              <strong>{t('appliedByKind')}</strong>
              <Space wrap>
                {Object.entries(s.applied_by_kind).map(([kind, count]) => (
                  <Tag key={kind} color="blue">{kind}: {count}</Tag>
                ))}
              </Space>
            </Col>
          </Row>
        </>
      )}
    </Card>
  );
}