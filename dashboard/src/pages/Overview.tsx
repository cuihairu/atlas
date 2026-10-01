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
import { getStats, listServers } from '../api/client';
import type { AdminStats, Server } from '../types';

const REFRESH_INTERVAL = 30_000;

export default function Overview() {
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
          <StatsCard title="总服务器数" value={stats.total_servers} prefix={<CloudServerOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title="在线服务器" value={stats.online_servers} prefix={<CheckCircleOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title="总在线玩家" value={stats.total_players} prefix={<TeamOutlined />} loading={loading} />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatsCard title="总容量" value={stats.total_capacity} prefix={<DatabaseOutlined />} loading={loading} />
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 24 }}>
        <Col xs={24} lg={12}>
          <Card title="服务器状态分布">
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
        <Col xs={24} lg={12}>
          <Card title="区域分布">
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
        </Col>
      </Row>

      <Card title="最近服务器" style={{ marginTop: 24 }}>
        <Table
          dataSource={recent}
          rowKey="id"
          pagination={false}
          size="small"
          columns={[
            { title: 'ID', dataIndex: 'id', ellipsis: true },
            { title: '名称', dataIndex: 'name' },
            { title: '区域', dataIndex: 'region' },
            {
              title: '状态',
              dataIndex: 'status',
              render: (s: string) => <StatusTag status={s} />,
            },
            {
              title: '玩家',
              render: (_: unknown, r: Server) => `${r.player_count} / ${r.capacity}`,
            },
          ]}
        />
      </Card>
    </>
  );
}