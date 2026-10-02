import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Space, Select, Progress, message, Popconfirm } from 'antd';
import { useNavigate } from 'react-router-dom';
import StatusTag from '../components/StatusTag';
import TagBadge from '../components/TagBadge';
import { useLang, t } from '../i18n';
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
  useLang(); // re-render on language switch
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
      message.success(t('actionOk'));
      fetch();
    } catch {
      // error handled by client
    }
  };

  const actionsFor = (s: Server) => {
    const btns: { key: string; label: string; danger?: boolean }[] = [];
    if (s.status === 'online') {
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
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder={t('region')}
          style={{ width: 120 }}
          value={region}
          onChange={setRegion}
          options={REGIONS.map((r) => ({ label: r.toUpperCase(), value: r }))}
        />
        <Select
          allowClear
          placeholder={t('status')}
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
          { title: t('id'), dataIndex: 'id', ellipsis: true, width: 200 },
          { title: t('name'), dataIndex: 'name' },
          { title: t('region'), dataIndex: 'region', width: 80 },
          { title: t('version'), dataIndex: 'version', width: 100 },
          {
            title: t('status'),
            dataIndex: 'status',
            width: 100,
            render: (s: string) => <StatusTag status={s} />,
          },
          {
            title: t('tags'),
            dataIndex: 'tags',
            width: 180,
            render: (tags: Server['tags']) =>
              tags && tags.length > 0 ? (
                <Space size={4} wrap>
                  {tags.map((tag) => (
                    <TagBadge key={tag.code} tag={tag} />
                  ))}
                </Space>
              ) : (
                '-'
              ),
          },
          {
            title: t('playersCapacity'),
            width: 200,
            render: (_: unknown, r: Server) => {
              const pct = r.capacity > 0 ? Math.round((r.players / r.capacity) * 100) : 0;
              return (
                <Space>
                  <Progress
                    percent={pct}
                    size="small"
                    style={{ width: 80 }}
                    strokeColor={pct > 80 ? '#ff4d4f' : '#d97706'}
                  />
                  <span>{r.players}/{r.capacity}</span>
                </Space>
              );
            },
          },
          {
            title: t('load'),
            dataIndex: 'load',
            width: 80,
            render: (v: number) => `${(v * 100).toFixed(0)}%`,
          },
          {
            title: t('actions'),
            width: 240,
            render: (_: unknown, record: Server) => (
              <Space>
                {actionsFor(record).map((a) => (
                  <Popconfirm
                    key={a.key}
                    title={t('confirmAction', { name: a.label })}
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
          {t('loadMore')}
        </Button>
      )}
    </>
  );
}
