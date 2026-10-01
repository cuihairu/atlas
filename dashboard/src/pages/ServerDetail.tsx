import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Descriptions, Card, Table, Button, Space, Progress, Spin, message, Popconfirm } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import StatusTag from '../components/StatusTag';
import {
  getServer,
  listCharactersByServer,
  serverMaintenance,
  serverDrain,
  serverEnable,
  serverDisable,
} from '../api/client';
import type { Server, Character } from '../types';

export default function ServerDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [server, setServer] = useState<Server | null>(null);
  const [characters, setCharacters] = useState<Character[]>([]);
  const [loading, setLoading] = useState(true);

  const fetch = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [s, c] = await Promise.all([getServer(id), listCharactersByServer(id)]);
      setServer(s);
      setCharacters(c);
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  const doAction = async (action: string) => {
    if (!id) return;
    try {
      if (action === 'maintenance') await serverMaintenance(id);
      else if (action === 'drain') await serverDrain(id);
      else if (action === 'enable') await serverEnable(id);
      else if (action === 'disable') await serverDisable(id);
      message.success('操作成功');
      fetch();
    } catch {
      // handled by client
    }
  };

  if (loading || !server) return <Spin size="large" style={{ display: 'block', margin: '120px auto' }} />;

  const pct = server.capacity > 0 ? Math.round((server.player_count / server.capacity) * 100) : 0;

  const actionsFor = () => {
    const btns: { key: string; label: string; danger?: boolean }[] = [];
    if (server.status === 'online') {
      btns.push({ key: 'maintenance', label: '进入维护' });
      btns.push({ key: 'drain', label: '排水' });
      btns.push({ key: 'disable', label: '禁用', danger: true });
    } else {
      btns.push({ key: 'enable', label: '启用' });
    }
    return btns;
  };

  return (
    <>
      <Button
        icon={<ArrowLeftOutlined />}
        type="link"
        onClick={() => navigate('/servers')}
        style={{ marginBottom: 16, padding: 0 }}
      >
        返回服务器列表
      </Button>

      <Card title="基本信息" style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 2, lg: 3 }}>
          <Descriptions.Item label="ID">{server.id}</Descriptions.Item>
          <Descriptions.Item label="名称">{server.name}</Descriptions.Item>
          <Descriptions.Item label="区域">{server.region}</Descriptions.Item>
          <Descriptions.Item label="版本">{server.version}</Descriptions.Item>
          <Descriptions.Item label="状态">
            <StatusTag status={server.status} />
          </Descriptions.Item>
          {server.ip && <Descriptions.Item label="IP">{server.ip}</Descriptions.Item>}
          {server.port && <Descriptions.Item label="端口">{server.port}</Descriptions.Item>}
        </Descriptions>
      </Card>

      <Card title="实时指标" style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 3 }}>
          <Descriptions.Item label="玩家数">{server.player_count}</Descriptions.Item>
          <Descriptions.Item label="容量">{server.capacity}</Descriptions.Item>
          <Descriptions.Item label="负载">{(server.load * 100).toFixed(1)}%</Descriptions.Item>
        </Descriptions>
        <div style={{ marginTop: 16 }}>
          <span style={{ marginRight: 8 }}>使用率:</span>
          <Progress
            percent={pct}
            strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
            style={{ maxWidth: 400 }}
          />
        </div>
      </Card>

      <Card title="操作" style={{ marginBottom: 24 }}>
        <Space>
          {actionsFor().map((a) => (
            <Popconfirm key={a.key} title={`确认${a.label}？`} onConfirm={() => doAction(a.key)}>
              <Button type={a.danger ? 'primary' : 'default'} danger={a.danger}>
                {a.label}
              </Button>
            </Popconfirm>
          ))}
        </Space>
      </Card>

      <Card title={`角色列表 (${characters.length})`}>
        <Table
          dataSource={characters}
          rowKey="character_id"
          size="small"
          pagination={{ pageSize: 20 }}
          columns={[
            { title: '角色ID', dataIndex: 'character_id', ellipsis: true },
            { title: '名称', dataIndex: 'name' },
            { title: '等级', dataIndex: 'level', width: 80 },
            { title: '职业', dataIndex: 'class_id', width: 100 },
            { title: '账户ID', dataIndex: 'account_id', ellipsis: true },
            {
              title: '最后登录',
              dataIndex: 'last_login',
              render: (v?: string) => (v ? new Date(v).toLocaleString('zh-CN') : '-'),
            },
          ]}
        />
      </Card>
    </>
  );
}