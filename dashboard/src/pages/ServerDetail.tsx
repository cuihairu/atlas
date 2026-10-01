import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Descriptions, Card, Table, Button, Space, Progress, Spin, message, Popconfirm } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import StatusTag from '../components/StatusTag';
import { useLang, t, getLang } from '../i18n';
import {
  getServer,
  listCharactersByServer,
  serverMaintenance,
  serverDrain,
  serverEnable,
  serverDisable,
} from '../api/client';
import type { Server, Character } from '../types';

function timeLocale(): string {
  return getLang() === 'zh' ? 'zh-CN' : 'en-US';
}

export default function ServerDetail() {
  useLang(); // re-render on language switch
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
      message.success(t('actionOk'));
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
      btns.push({ key: 'maintenance', label: t('enterMaintenance') });
      btns.push({ key: 'drain', label: t('drain') });
      btns.push({ key: 'disable', label: t('disable'), danger: true });
    } else {
      btns.push({ key: 'enable', label: t('enable') });
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
        {t('backToServers')}
      </Button>

      <Card title={t('basicInfo')} style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 2, lg: 3 }}>
          <Descriptions.Item label={t('id')}>{server.id}</Descriptions.Item>
          <Descriptions.Item label={t('name')}>{server.name}</Descriptions.Item>
          <Descriptions.Item label={t('region')}>{server.region}</Descriptions.Item>
          <Descriptions.Item label={t('version')}>{server.version}</Descriptions.Item>
          <Descriptions.Item label={t('status')}>
            <StatusTag status={server.status} />
          </Descriptions.Item>
          {server.ip && <Descriptions.Item label="IP">{server.ip}</Descriptions.Item>}
          {server.port && <Descriptions.Item label={t('port')}>{server.port}</Descriptions.Item>}
        </Descriptions>
      </Card>

      <Card title={t('liveMetrics')} style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 3 }}>
          <Descriptions.Item label={t('playerCount')}>{server.player_count}</Descriptions.Item>
          <Descriptions.Item label={t('capacity')}>{server.capacity}</Descriptions.Item>
          <Descriptions.Item label={t('load')}>{(server.load * 100).toFixed(1)}%</Descriptions.Item>
        </Descriptions>
        <div style={{ marginTop: 16 }}>
          <span style={{ marginRight: 8 }}>{t('utilization')}:</span>
          <Progress
            percent={pct}
            strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
            style={{ maxWidth: 400 }}
          />
        </div>
      </Card>

      <Card title={t('actions')} style={{ marginBottom: 24 }}>
        <Space>
          {actionsFor().map((a) => (
            <Popconfirm key={a.key} title={t('confirmAction', { name: a.label })} onConfirm={() => doAction(a.key)}>
              <Button type={a.danger ? 'primary' : 'default'} danger={a.danger}>
                {a.label}
              </Button>
            </Popconfirm>
          ))}
        </Space>
      </Card>

      <Card title={`${t('characterList')} (${characters.length})`}>
        <Table
          dataSource={characters}
          rowKey="character_id"
          size="small"
          pagination={{ pageSize: 20 }}
          columns={[
            { title: t('characterId'), dataIndex: 'character_id', ellipsis: true },
            { title: t('name'), dataIndex: 'name' },
            { title: t('level'), dataIndex: 'level', width: 80 },
            { title: t('class'), dataIndex: 'class_id', width: 100 },
            { title: t('accountId'), dataIndex: 'account_id', ellipsis: true },
            {
              title: t('lastLogin'),
              dataIndex: 'last_login',
              render: (v?: string) => (v ? new Date(v).toLocaleString(timeLocale()) : '-'),
            },
          ]}
        />
      </Card>
    </>
  );
}
