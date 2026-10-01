import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Modal, Form, Input, Space, Tag, message, Popconfirm } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { listMigrations, createMigration, rollbackMigration } from '../api/client';
import type { Migration } from '../types';

const STATUS_COLOR: Record<string, string> = {
  pending: 'blue',
  running: 'orange',
  completed: 'green',
  failed: 'red',
  rolled_back: 'purple',
};

const STATUS_LABEL: Record<string, string> = {
  pending: '待处理',
  running: '运行中',
  completed: '已完成',
  failed: '失败',
  rolled_back: '已回滚',
};

export default function Migrations() {
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
      setMigrations(res.migrations);
      setNextCursor(res.next_cursor);
    } finally {
      setLoading(false);
    }
  }, [cursor]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  const onCreate = async () => {
    try {
      const values = await form.validateFields();
      setCreating(true);
      await createMigration({
        source_server_id: values.source_server_id,
        target_server_id: values.target_server_id,
      });
      message.success('迁移已创建');
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
      message.success('回滚成功');
      fetch();
    } catch {
      // handled by client
    }
  };

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          创建迁移
        </Button>
      </Space>

      <Table
        dataSource={migrations}
        rowKey="id"
        loading={loading}
        pagination={false}
        columns={[
          { title: 'ID', dataIndex: 'id', ellipsis: true, width: 200 },
          { title: '源服务器', dataIndex: 'source_server_id', ellipsis: true },
          { title: '目标服务器', dataIndex: 'target_server_id', ellipsis: true },
          {
            title: '状态',
            dataIndex: 'status',
            width: 100,
            render: (s: string) => (
              <Tag color={STATUS_COLOR[s] ?? 'default'}>{STATUS_LABEL[s] ?? s}</Tag>
            ),
          },
          {
            title: '开始时间',
            dataIndex: 'started_at',
            width: 180,
            render: (v?: string) => (v ? new Date(v).toLocaleString('zh-CN') : '-'),
          },
          {
            title: '完成时间',
            dataIndex: 'completed_at',
            width: 180,
            render: (v?: string) => (v ? new Date(v).toLocaleString('zh-CN') : '-'),
          },
          {
            title: '操作',
            width: 120,
            render: (_: unknown, record: Migration) =>
              record.status === 'completed' ? (
                <Popconfirm title="确认回滚？" onConfirm={() => onRollback(record.id)}>
                  <Button size="small" danger>
                    回滚
                  </Button>
                </Popconfirm>
              ) : null,
          },
        ]}
      />

      {nextCursor && (
        <Button style={{ marginTop: 16 }} onClick={() => setCursor(nextCursor)}>
          加载更多
        </Button>
      )}

      <Modal
        title="创建迁移"
        open={modalOpen}
        onOk={onCreate}
        onCancel={() => setModalOpen(false)}
        confirmLoading={creating}
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="source_server_id"
            label="源服务器 ID"
            rules={[{ required: true, message: '请输入源服务器 ID' }]}
          >
            <Input placeholder="源服务器 ID" />
          </Form.Item>
          <Form.Item
            name="target_server_id"
            label="目标服务器 ID"
            rules={[{ required: true, message: '请输入目标服务器 ID' }]}
          >
            <Input placeholder="目标服务器 ID" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}