// Real-time server map (TODO v0.1.18): geographic distribution grouped by
// region, polled on a short interval. Each region card shows live status
// counts, players and average load; a footer marks the last refresh time.
import { useEffect, useState, useCallback } from 'react';
import { Row, Col, Card, Badge, Progress, Empty } from 'antd';
import { useLang, t } from '../i18n';
import { listServers } from '../api/client';
import type { Server } from '../types';

const REFRESH_MS = 10_000;

const STATUS_COLOR: Record<string, string> = {
  online: '#52c41a',
  suspect: '#faad14',
  maintenance: '#1677ff',
  draining: '#722ed1',
  offline: '#ff4d4f',
};

interface RegionSummary {
  region: string;
  servers: Server[];
  players: number;
  capacity: number;
  byStatus: Record<string, number>;
}

function summarize(servers: Server[]): RegionSummary[] {
  const byRegion = new Map<string, RegionSummary>();
  for (const s of servers) {
    const key = s.region || '-';
    let r = byRegion.get(key);
    if (!r) {
      r = { region: key, servers: [], players: 0, capacity: 0, byStatus: {} };
      byRegion.set(key, r);
    }
    r.servers.push(s);
    r.players += s.player_count;
    r.capacity += s.capacity;
    r.byStatus[s.status] = (r.byStatus[s.status] ?? 0) + 1;
  }
  return [...byRegion.values()].sort((a, b) => a.region.localeCompare(b.region));
}

export default function ServerMap() {
  useLang(); // re-render on language switch
  const [regions, setRegions] = useState<RegionSummary[]>([]);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await listServers({ limit: 200 });
      setRegions(summarize(res.servers));
      setUpdatedAt(new Date());
    } catch {
      // error already surfaced by the API client
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(id);
  }, [refresh]);

  return (
    <Card
      title={t('serverMap')}
      extra={
        <Badge
          status="processing"
          text={
            <span style={{ fontSize: 12, opacity: 0.65 }}>
              {t('realTime')}
              {updatedAt ? ` · ${t('updated')} ${updatedAt.toLocaleTimeString()}` : ''}
            </span>
          }
        />
      }
    >
      {regions.length === 0 ? (
        <Empty description={t('noData')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
      ) : (
        <Row gutter={[16, 16]}>
          {regions.map((r) => {
            const pct = r.capacity > 0 ? Math.round((r.players / r.capacity) * 100) : 0;
            return (
              <Col xs={24} sm={12} lg={8} xl={6} key={r.region}>
                <Card
                  size="small"
                  title={<span style={{ textTransform: 'uppercase' }}>{r.region}</span>}
                  styles={{ body: { padding: '12px 16px' } }}
                >
                  <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 8 }}>
                    {Object.entries(r.byStatus).map(([status, count]) => (
                      <span key={status} style={{ fontSize: 12 }}>
                        <Badge color={STATUS_COLOR[status] ?? '#999'} text={`${status} ${count}`} />
                      </span>
                    ))}
                  </div>
                  <div style={{ fontSize: 12, opacity: 0.75, marginBottom: 6 }}>
                    {t('players')} {r.players} / {r.capacity} · {r.servers.length} {t('servers').toLowerCase()}
                  </div>
                  <Progress
                    percent={pct}
                    size="small"
                    strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
                  />
                </Card>
              </Col>
            );
          })}
        </Row>
      )}
    </Card>
  );
}
