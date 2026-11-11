import { useEffect, useState, useCallback } from 'react';
import { Row, Col, Card, Table, Spin } from 'antd';
import {
  CloudServerOutlined,
  CheckCircleOutlined,
  TeamOutlined,
  DatabaseOutlined,
} from '@ant-design/icons';
import { Pie, Column } from '@ant-design/charts';
import StatsCard from '../components/StatsCard';
import StatusTag from '../components/StatusTag';
import ServerMap from '../components/ServerMap';
import PlayerTrend from '../components/PlayerTrend';
import LoadTimeView from '../components/LoadTimeView';
import BusPanel from '../components/BusPanel';
import QueueStatusCard from '../components/QueueStatusCard';
import { getStats, listServers } from '../api/client';
import { useLang, t } from '../i18n';
import type { AdminStats, Server } from '../types';

const REFRESH_INTERVAL = 30_000;

export default function Overview() {
  useLang(); // re-render on language switch
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [recent, setRecent] = useState<Server[]>([]);
  const [loading, setLoading] = useState(true);

  const fetch = useCallback(async () => {
    try {
      const [s, r] = await Promise.all([
        getStats(),
        listServers({ limit: 5 }),
      ]);
      setStats(s);
      setRecent(r.servers);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetch();
    const id = setInterval(fetch, REFRESH_INTERVAL);
    return () => clearInterval(id);
  }, [fetch]);

  if (loading || !stats) return <Spin size="large" style={{ display: 'block', margin: '120px auto' }} />;

  const pieData = Object.entries(stats.servers_by_status).map(([status, count]) => ({
    status,
    count,
  }));

  const regionData = Object.entries(stats.servers_by_region).map(([region, count]) => ({
    region,
    count,
  }));

  return (
    <>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title={t('totalServers')} value={stats.total_servers} prefix={<CloudServerOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title={t('onlineServers')} value={stats.online_servers} prefix={<CheckCircleOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title={t('totalPlayers')} value={stats.total_players} prefix={<TeamOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title={t('totalCapacity')} value={stats.total_capacity} prefix={<DatabaseOutlined />} loading={loading} />
        </Col>
      </Row>

      <div style={{ marginTop: 24 }}>
        <ServerMap />
      </div>

      {/* 负载时间视图（item 11）：窗口分层 + 舰队/区域/单服下钻。 */}
      <div style={{ marginTop: 24 }}>
        <LoadTimeView />
      </div>

      {/* 消息总线积压（item 13）：深度曲线 + 生产/消费速率。 */}
      <div style={{ marginTop: 24 }}>
        <BusPanel />
      </div>

      {/* 存储队列（TODO v0.2 ④）：压力灯 + 水位 + 最近 flush；SQL 存储显示未启用。 */}
      <div style={{ marginTop: 24 }}>
        <QueueStatusCard />
      </div>

      <Row gutter={[16, 16]} style={{ marginTop: 24 }}>
        <Col xs={24} lg={12}>
          <PlayerTrend />
        </Col>
        <Col xs={24} lg={12}>
          <Card title={t('statusDistribution')}>
            <Pie
              data={pieData}
              angleField="count"
              colorField="status"
              radius={0.8}
              innerRadius={0.5}
              label={{ text: 'status', position: 'outside' }}
              legend={{ position: 'bottom' }}
              height={300}
            />
          </Card>
        </Col>
      </Row>

      <Card title={t('regionDistribution')} style={{ marginTop: 24 }}>
        <Column
          data={regionData}
          xField="region"
          yField="count"
          color="#d97706"
          height={300}
          label={{ position: 'outside' }}
          axis={{ x: { labelAutoRotate: true } }}
        />
      </Card>

      <Card title={t('recentServers')} style={{ marginTop: 24 }}>
        <Table
          dataSource={recent}
          rowKey="id"
          pagination={false}
          size="small"
          columns={[
            { title: t('id'), dataIndex: 'id', ellipsis: true },
            { title: t('name'), dataIndex: 'name' },
            { title: t('region'), dataIndex: 'region' },
            {
              title: t('status'),
              dataIndex: 'status',
              render: (s: string) => <StatusTag status={s} />,
            },
            {
              title: t('players'),
              render: (_: unknown, r: Server) => `${r.players} / ${r.capacity}`,
            },
          ]}
        />
      </Card>
    </>
  );
}
