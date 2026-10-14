// 公告与计划维护：面向玩家的公告（全局 / 服务器级）与声明式维护窗口。
// 窗口到点自动把 auto-managed 服务器切到维护、窗口结束恢复原状态；
// 创建时缺省联动一条覆盖同时段的 warning 公告（announcement_id 关联）。
import { useEffect, useState, useCallback } from 'react';
import {
  Card, Table, Button, Modal, Form, Input, Select, Switch, Space, Tag,
  message, Popconfirm, DatePicker, InputNumber, Typography,
} from 'antd';
import { PlusOutlined, NotificationOutlined, ClockCircleOutlined } from '@ant-design/icons';
import dayjs, { type Dayjs } from 'dayjs';
import { useLang, t } from '../i18n';
import {
  listAnnouncements, createAnnouncement, deleteAnnouncement,
  listMaintenanceWindows, createMaintenanceWindow, deleteMaintenanceWindow,
} from '../api/client';
import { submitBatch, BatchOutcome, type BatchFormValues } from './operationsBatch';
import type { Announcement, MaintenanceWindow, MaintenanceWindowBatchResponse } from '../types';

const LEVEL_COLOR: Record<string, string> = {
  info: 'blue',
  warning: 'orange',
  critical: 'red',
};

function fmtTime(v?: string | null): string {
  if (!v) return '—';
  return dayjs(v).format('MM-DD HH:mm');
}

export default function Operations() {
  useLang();
  const [announcements, setAnnouncements] = useState<Announcement[]>([]);
  const [windows, setWindows] = useState<MaintenanceWindow[]>([]);
  const [loading, setLoading] = useState(true);
  const [annModal, setAnnModal] = useState(false);
  const [winModal, setWinModal] = useState(false);
  const [batchModal, setBatchModal] = useState(false);
  const [batchResult, setBatchResult] = useState<MaintenanceWindowBatchResponse | null>(null);
  const [saving, setSaving] = useState(false);
  const [annForm] = Form.useForm();
  const [winForm] = Form.useForm();
  const [batchForm] = Form.useForm();

  const fetchAll = useCallback(async () => {
    setLoading(true);
    try {
      const [ann, win] = await Promise.all([
        listAnnouncements(),
        listMaintenanceWindows(),
      ]);
      setAnnouncements(ann.announcements ?? []);
      setWindows(win.maintenance_windows ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchAll(); }, [fetchAll]);

  // ── 公告 ───────────────────────────────────────────────────────────

  const submitAnnouncement = async () => {
    const v = await annForm.validateFields();
    const range = v.range as [Dayjs, Dayjs];
    setSaving(true);
    try {
      await createAnnouncement({
        title: v.title,
        body: v.body ?? '',
        level: v.level,
        server_id: v.server_id || undefined,
        starts_at: range[0].toISOString(),
        ends_at: range[1].toISOString(),
      });
      message.success(t('actionOk'));
      setAnnModal(false);
      annForm.resetFields();
      await fetchAll();
    } finally {
      setSaving(false);
    }
  };

  const removeAnnouncement = async (id: string) => {
    await deleteAnnouncement(id);
    message.success(t('actionOk'));
    await fetchAll();
  };

  // ── 维护窗口 ───────────────────────────────────────────────────────

  const submitWindow = async () => {
    const v = await winForm.validateFields();
    const range = v.range as [Dayjs, Dayjs];
    setSaving(true);
    try {
      await createMaintenanceWindow(v.server_id, {
        start_at: range[0].toISOString(),
        end_at: range[1].toISOString(),
        announce: v.announce ?? true,
      });
      message.success(t('actionOk'));
      setWinModal(false);
      winForm.resetFields();
      await fetchAll();
    } finally {
      setSaving(false);
    }
  };

  const removeWindow = async (id: string) => {
    await deleteMaintenanceWindow(id);
    message.success(t('actionOk'));
    await fetchAll();
  };

  // ── 舰队批量窗口 ───────────────────────────────────────────────────
  // 预览（dry_run）与真实创建走同一条提交路径：结果留在弹窗里逐台列出，
  // 运营确认无误再关；部分成功不静默——失败台数单独提示。

  const submitWindowsBatch = async () => {
    let v: BatchFormValues & { range: [Dayjs, Dayjs] };
    try {
      v = await batchForm.validateFields();
    } catch {
      return; // 校验失败就地红字提示，不发请求
    }
    setSaving(true);
    try {
      const result = await submitBatch(v, v.range);
      setBatchResult(result);
      if (v.dry_run) return; // 预览不落库，不刷新列表
      if (result.failed.length > 0) {
        message.warning(`${t('winBatchPartialMsg')}: ${result.failed.length}`);
      } else {
        message.success(t('winBatchOkMsg'));
      }
      await fetchAll();
    } finally {
      setSaving(false);
    }
  };

  const closeBatch = () => {
    setBatchModal(false);
    setBatchResult(null);
    batchForm.resetFields();
  };

  const annColumns = [
    {
      title: t('annLevel'),
      dataIndex: 'level',
      width: 90,
      render: (level: string) => <Tag color={LEVEL_COLOR[level] ?? 'default'}>{level}</Tag>,
    },
    { title: t('annTitle'), dataIndex: 'title', ellipsis: true },
    {
      title: t('annScope'),
      dataIndex: 'server_id',
      width: 140,
      render: (v: string | null) => (v ? <Tag>{v}</Tag> : <Tag color="geekblue">{t('annGlobal')}</Tag>),
    },
    {
      title: t('annWindow'),
      key: 'window',
      width: 200,
      render: (_: unknown, a: Announcement) => `${fmtTime(a.starts_at)} → ${fmtTime(a.ends_at)}`,
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 90,
      render: (_: unknown, a: Announcement) => (
        <Popconfirm
          title={t('confirmDelete')}
          onConfirm={() => removeAnnouncement(a.id)}
        >
          <Button size="small" danger>{t('delete')}</Button>
        </Popconfirm>
      ),
    },
  ];

  const winColumns = [
    { title: t('id'), dataIndex: 'id', width: 170, ellipsis: true },
    { title: t('servers'), dataIndex: 'server_id', width: 140, render: (v: string) => <Tag>{v}</Tag> },
    {
      title: t('winWindow'),
      key: 'window',
      width: 210,
      render: (_: unknown, w: MaintenanceWindow) => `${fmtTime(w.start_at)} → ${fmtTime(w.end_at)}`,
    },
    {
      title: t('winRestore'),
      dataIndex: 'previous_status',
      width: 110,
      render: (v: string) => (v ? <Tag color="purple">{v}</Tag> : <Tag>{t('winNotApplied')}</Tag>),
    },
    {
      title: t('winLinkedAnn'),
      dataIndex: 'announcement_id',
      width: 170,
      ellipsis: true,
      render: (v: string | null) => v ?? '—',
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 90,
      render: (_: unknown, w: MaintenanceWindow) => (
        <Popconfirm title={t('confirmDelete')} onConfirm={() => removeWindow(w.id)}>
          <Button size="small" danger>{t('delete')}</Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={<Space><NotificationOutlined />{t('announcements')}</Space>}
        extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setAnnModal(true)}>{t('annCreate')}</Button>}
      >
        <Table
          rowKey="id"
          size="small"
          loading={loading}
          columns={annColumns}
          dataSource={announcements}
          pagination={{ pageSize: 8, hideOnSinglePage: true }}
          locale={{ emptyText: t('noData') }}
        />
      </Card>

      <Card
        title={<Space><ClockCircleOutlined />{t('maintenanceWindows')}</Space>}
        extra={
          <Space>
            <Button icon={<PlusOutlined />} onClick={() => setBatchModal(true)}>{t('winBatch')}</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setWinModal(true)}>{t('winCreate')}</Button>
          </Space>
        }
      >
        <Table
          rowKey="id"
          size="small"
          loading={loading}
          columns={winColumns}
          dataSource={windows}
          pagination={{ pageSize: 8, hideOnSinglePage: true }}
          locale={{ emptyText: t('noData') }}
        />
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0, marginTop: 8 }}>
          {t('winHint')}
        </Typography.Paragraph>
      </Card>

      <Modal
        title={t('annCreate')}
        open={annModal}
        onOk={submitAnnouncement}
        confirmLoading={saving}
        onCancel={() => setAnnModal(false)}
        destroyOnClose
      >
        <Form form={annForm} layout="vertical" initialValues={{ level: 'info' }}>
          <Form.Item name="title" label={t('annTitle')} rules={[{ required: true }]}>
            <Input placeholder={t('annTitlePlaceholder')} />
          </Form.Item>
          <Space size={12} style={{ display: 'flex' }}>
            <Form.Item name="level" label={t('annLevel')} rules={[{ required: true }]} style={{ width: 120 }}>
              <Select
                options={[
                  { value: 'info', label: 'info' },
                  { value: 'warning', label: 'warning' },
                  { value: 'critical', label: 'critical' },
                ]}
              />
            </Form.Item>
            <Form.Item name="server_id" label={t('annScope')} style={{ width: 220 }} tooltip={t('annScopeTip')}>
              <Input placeholder={t('annScopePlaceholder')} />
            </Form.Item>
          </Space>
          <Form.Item name="range" label={t('annWindow')} rules={[{ required: true }]}>
            <DatePicker.RangePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="body" label={t('annBody')}>
            <Input.TextArea rows={3} placeholder={t('annBodyPlaceholder')} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('winCreate')}
        open={winModal}
        onOk={submitWindow}
        confirmLoading={saving}
        onCancel={() => setWinModal(false)}
        destroyOnClose
      >
        <Form form={winForm} layout="vertical" initialValues={{ announce: true }}>
          <Form.Item name="server_id" label={t('servers')} rules={[{ required: true }]}>
            <Input placeholder="game-1001" />
          </Form.Item>
          <Form.Item name="range" label={t('winWindow')} rules={[{ required: true }]}>
            <DatePicker.RangePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="announce" label={t('winAnnounce')} valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('winBatchTitle')}
        open={batchModal}
        onOk={submitWindowsBatch}
        confirmLoading={saving}
        onCancel={closeBatch}
        width={720}
        destroyOnClose
      >
        <Form
          form={batchForm}
          layout="vertical"
          initialValues={{ announce: true, stagger_seconds: 0, dry_run: false }}
        >
          <Form.Item
            name="server_ids"
            label={t('winBatchServers')}
            tooltip={t('winBatchServersTip')}
            rules={[{ required: true, message: t('winBatchServersTip') }]}
          >
            <Select
              mode="tags"
              tokenSeparators={[',', ' ', '\n']}
              placeholder="game-1001, game-1002"
              open={false}
            />
          </Form.Item>
          <Form.Item name="range" label={t('winWindow')} rules={[{ required: true }]}>
            <DatePicker.RangePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Space size={24} align="start">
            <Form.Item
              name="stagger_seconds"
              label={t('winBatchStagger')}
              tooltip={t('winBatchStaggerTip')}
            >
              <InputNumber min={0} max={86400} step={60} style={{ width: 180 }} />
            </Form.Item>
            <Form.Item name="dry_run" label={t('winBatchDryRun')} valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="announce" label={t('winAnnounce')} valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
        </Form>
        {batchResult && (
          <BatchOutcome result={batchResult} dryRun={Boolean(batchForm.getFieldValue('dry_run'))} />
        )}
      </Modal>
    </Space>
  );
}
