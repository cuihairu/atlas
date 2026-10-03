// Migration management (TODO v0.1.18 live progress): while any migration is
// pending/running the list silently re-polls every 5s (no spinner flash), and
// each row expands to a Steps timeline showing created → migrating → done.
import { useEffect, useState, useCallback } from 'react';
import {
  Table, Button, Modal, Form, Input, Space, Tag, Badge, Steps, message, Popconfirm,
} from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useLang, t } from '../i18n';
import { listMigrations, createMigration, rollbackMigration } from '../api/client';
import type { Migration } from '../types';

const LIVE_POLL_MS = 5_000;

const STATUS_COLOR: Record<string, string> = {
  pending: 'blue',
  running: 'orange',
  completed: 'green',
  failed: 'red',
  rolled_back: 'purple',
};

type MessageKey = Parameters<typeof t>[0];

function migStatusKey(status: string): MessageKey {
  switch (status) {
    case 'pending': return 'migPending';
    case 'running': return 'migRunning';
    case 'completed': return 'migCompleted';
    case 'failed': return 'migFailed';
    case 'rolled_back': return 'migRolledBack';
    default: return 'status';
  }
}

/** Step position + health for the expanded timeline. */
function progressOf(status: string): { current: number; stepStatus: 'wait' | 'process' | 'finish' | 'error' } {
  switch (status) {
    case 'pending': return { current: 1, stepStatus: 'wait' };
    case 'running': return { current: 1, stepStatus: 'process' };
    case 'completed': return { current: 3, stepStatus: 'finish' };
    case 'failed': return { current: 1, stepStatus: 'error' };
    case 'rolled_back': return { current: 2, stepStatus: 'wait' };
    default: return { current: 0, stepStatus: 'wait' };
  }
}

const isActive = (m: Migration) => m.status === 'pending' || m.status === 'running';

export default function Migrations() {
  useLang(); // re-render on language switch
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm();
  const [cursor, setCursor] = useState<string | undefined>();
  const [nextCursor, setNextCursor] = useState<string | undefined>();

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listMigrations({ limit: 50, cursor });
      setMigrations(res.migrations ?? []);
      setNextCursor(res.next_cursor);
    } finally {
      setLoading(false);
    }
  }, [cursor]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  const hasActive = migrations.some(isActive);

  // Silent live refresh while anything is in flight — no spinner flash.
  useEffect(() => {
    if (!hasActive) return;
    const id = setInterval(async () => {
      try {
        const res = await listMigrations({ limit: 50, cursor });
        setMigrations(res.migrations ?? []);
        setNextCursor(res.next_cursor);
      } catch {
        // transient poll failure: keep the interval, client already toasts
      }
    }, LIVE_POLL_MS);
    return () => clearInterval(id);
  }, [hasActive, cursor]);

  const onCreate = async () => {
    try {
      const values = await form.validateFields();
      setCreating(true);
      await createMigration({
        source_server_id: values.source_server_id,
        target_server_id: values.target_server_id,
      });
      message.success(t('migrationCreated'));
      setModalOpen(false);
      form.resetFields();
      fetch();
    } catch {
      // validation or api error
    } finally {
      setCreating(false);
    }
  };

  const onRollback = async (id: string) => {
    try {
      await rollbackMigration(id);
      message.success(t('rollbackOk'));
      fetch();
    } catch {
      // handled by client
    }
  };

  const stepTitles = [t('stepCreated'), t('stepMigrating'), t('stepDone')];

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          {t('createMigration')}
        </Button>
        {hasActive && <Badge status="processing" text={t('liveUpdating')} />}
      </Space>

      <Table
        dataSource={migrations}
        rowKey="id"
        loading={loading}
        pagination={false}
        expandable={{
          defaultExpandAllRows: true,
          expandedRowRender: (record) => {
            const { current, stepStatus } = progressOf(record.status);
            return (
              <div style={{ padding: '8px 0' }}>
                {isActive(record) && (
                  <Badge status="processing" text={t('liveUpdating')} style={{ marginBottom: 12 }} />
                )}
                <Steps
                  size="small"
                  current={current}
                  status={stepStatus}
                  items={stepTitles.map((title) => ({ title }))}
                />
              </div>
            );
          },
        }}
        columns={[
          { title: t('id'), dataIndex: 'id', ellipsis: true, width: 200 },
          { title: t('sourceServer'), dataIndex: 'source_server_id', ellipsis: true },
          { title: t('targetServer'), dataIndex: 'target_server_id', ellipsis: true },
          {
            title: t('status'),
            dataIndex: 'status',
            width: 110,
            render: (s: string) => (
              <Tag color={STATUS_COLOR[s] ?? 'default'}>{t(migStatusKey(s))}</Tag>
            ),
          },
          {
            title: t('startedAt'),
            dataIndex: 'started_at',
            width: 180,
            render: (v?: string) => (v ? new Date(v).toLocaleString() : '-'),
          },
          {
            title: t('completedAt'),
            dataIndex: 'completed_at',
            width: 180,
            render: (v?: string) => (v ? new Date(v).toLocaleString() : '-'),
          },
          {
            title: t('actions'),
            width: 120,
            render: (_: unknown, record: Migration) =>
              record.status === 'completed' ? (
                <Popconfirm title={t('confirmRollback')} onConfirm={() => onRollback(record.id)}>
                  <Button size="small" danger>
                    {t('rollback')}
                  </Button>
                </Popconfirm>
              ) : null,
          },
        ]}
      />

      {nextCursor && (
        <Button style={{ marginTop: 16 }} onClick={() => setCursor(nextCursor)}>
          {t('loadMore')}
        </Button>
      )}

      <Modal
        title={t('createMigration')}
        open={modalOpen}
        onOk={onCreate}
        onCancel={() => setModalOpen(false)}
        confirmLoading={creating}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="source_server_id"
            label={t('sourceServerId')}
            rules={[{ required: true, message: t('inputSourceServerId') }]}
          >
            <Input placeholder={t('sourceServerId')} />
          </Form.Item>
          <Form.Item
            name="target_server_id"
            label={t('targetServerId')}
            rules={[{ required: true, message: t('inputTargetServerId') }]}
          >
            <Input placeholder={t('targetServerId')} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
