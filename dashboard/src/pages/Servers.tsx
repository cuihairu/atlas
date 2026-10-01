import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Space, Select, Progress, message, Popconfirm } from 'antd';
import { useNavigate } from 'react-router-dom';
import StatusTag from '../components/StatusTag';
import {
  listServers,
  serverMaintenance,
  serverDrain,
  serverEnable,
  serverDisable,
} from '../api/client';
import type { Server } from '../types';

const REGIONS = ['cn', 'us', 'eu', 'ap'];
const STATUSES = ['online', 'maintenance', 'suspect', 'offline', 'draining'];

export default function Servers() {
  const [servers, setServers] = useState<Server[]>([]);
  const [loading, setLoading] = useState(true);
  const [region, setRegion] = useState<string | undefined>();
  const [status, setStatus] = useState<string | undefined>();
  const [cursor, setCursor] = useState<string | undefined>();
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const navigate = useNavigate();

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listServers({ region, status, limit: 50, cursor });
      setServers(res.servers);
      setNextCursor(res.next_cursor);
    } finally {
      setLoading(false);
    }
  }, [region, status, cursor]);

  useEffect(() => {
    setCursor(undefined);
    fetch();
  }, [region, status, fetch]);

  const doAction = async (id: string, action: string) => {
    try {
      if (action === 'maintenance') await serverMaintenance(id);
      else if (action === 'drain') await serverDrain(id);
      else if (action === 'enable') await serverEnable(id);
      else if (action === 'disable') await serverDisable(id);
      message.success('操作成功');
      fetch();
    } catch {
      // error handled by client
    }
  };

  const actionsFor = (s: Server) => {
    const btns: { key: string; label: string; danger?: boolean }[] = [];
    if (s.status === 'online') {
      btns.push({ key: 'maintenance', label: '进入维护' });
      btns.push({ key: 'drain', label: '排水' });
      btns.push({ key: 'disable', label: '禁用', danger: true });
    } else if (s.status === 'maintenance' || s.status === 'suspect' || s.status === 'draining') {
      btns.push({ key: 'enable', label: '启用' });
    } else if (s.status === 'offline') {
      btns.push({ key: 'enable', label: '启用' });
    }
    return btns;
  };

  return (
    <>
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder="区域"
          style={{ width: 120 }}
          value={region}
          onChange={setRegion}
          options={REGIONS.map((r) => ({ label: r.toUpperCase(), value: r }))}
        />
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 120 }}
          value={status}
          onChange={setStatus}
          options={STATUSES.map((s) => ({ label: s, value: s }))}
        />
      </Space>
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
          { title: 'ID', dataIndex: 'id', ellipsis: true, width: 200 },
          { title: '名称', dataIndex: 'name' },
          { title: '区域', dataIndex: 'region', width: 80 },
          { title: '版本', dataIndex: 'version', width: 100 },
          {
            title: '状态',
            dataIndex: 'status',
            width: 100,
            render: (s: string) => <StatusTag status={s} />,
          },
          {
            title: '玩家 / 容量',
            width: 200,
            render: (_: unknown, r: Server) => {
              const pct = r.capacity > 0 ? Math.round((r.player_count / r.capacity) * 100) : 0;
              return (
                <Space>
                  <Progress
                    percent={pct}
                    size="small"
                    style={{ width: 80 }}
                    strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
                  />
                  <span>{r.player_count}/{r.capacity}</span>
                </Space>
              );
            },
          },
          {
            title: '负载',
            dataIndex: 'load',
            width: 80,
            render: (v: number) => `${(v * 100).toFixed(0)}%`,
          },
          {
            title: '操作',
            width: 240,
            render: (_: unknown, record: Server) => (
              <Space>
                {actionsFor(record).map((a) => (
                  <Popconfirm
                    key={a.key}
                    title={`确认${a.label}？`}
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
          加载更多
        </Button>
      )}
    </>
  );
}