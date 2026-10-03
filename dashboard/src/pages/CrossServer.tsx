// 跨服配置中心：管理台编辑跨服协调配置（拓扑 / 参与分组 / 玩法开关 /
// 匹配域 / 玩法类型表），保存即发布新版本（version/hash），变更按通知-
// 拉取语义扩散。幂等保存（内容未变）不升版本、不发信号——保存结果里
// 直接可见。类型表是舰队级声明（docs/config-center.md §2.2），任何改动
// 都以 targets=["*"] 全局扩散。
import { useEffect, useState, useCallback } from 'react';
import {
  Card, Table, Button, Space, Tag, Input, Switch, Select, Modal, Form,
  message, Popconfirm, Descriptions, Typography, Alert,
} from 'antd';
import {
  PlusOutlined, ClusterOutlined, AppstoreOutlined, PartitionOutlined,
  SaveOutlined, ReloadOutlined, TagsOutlined,
} from '@ant-design/icons';
import { useLang, t } from '../i18n';
import { getCrossServerConfig, updateCrossServerConfig } from '../api/client';
import type {
  CrossServerSpec, CrossServerCluster, CrossServerGroup, CrossServerMatchDomain,
  CrossPlayType, CrossServerNotifyResult,
} from '../types';

const emptySpec = (): CrossServerSpec => ({
  topology: { clusters: [] },
  groups: [],
  features: {},
  match_domains: [],
  // 类型表随全文发布一起扩散——服务端对 crossplay_types 变更按
  // targets=["*"] 全局下发（舰队级声明），这里保存同样整段透传。
  crossplay_types: [],
});

/** 类型行客户端校验镜像服务端 model.ValidateCrossServerSpec 的口径：
 * ID 与 id_prefix 全表唯一、ID 走 [a-z0-9._-] 首字符字母数字、前缀
 * 2–8 位小写字母；lifecycle 枚举交给下拉框。空值即「未填」，服务端
 * 缺省 persistent，客户端不替它做决定（往返不改动已发布内容）。 */
const TYPE_ID_RE = /^[a-z0-9][a-z0-9._-]{0,63}$/;
const TYPE_PREFIX_RE = /^[a-z]{2,8}$/;

/** id 列表编辑用自由输入 tags（逗号/回车分隔）。 */
function ServerIdsInput({ value, onChange }: { value?: string[]; onChange?: (v: string[]) => void }) {
  return (
    <Select
      mode="tags"
      style={{ minWidth: 220 }}
      value={value ?? []}
      onChange={onChange}
      placeholder="game-1001 game-1002"
      tokenSeparators={[',', ' ']}
    />
  );
}

export default function CrossServer() {
  useLang();
  const [spec, setSpec] = useState<CrossServerSpec>(emptySpec());
  const [meta, setMeta] = useState<{ version: number; hash: string; updated_at?: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [lastNotify, setLastNotify] = useState<CrossServerNotifyResult | null>(null);
  const [dirty, setDirty] = useState(false);
  const [clusterModal, setClusterModal] = useState<CrossServerCluster | null>(null);
  const [groupModal, setGroupModal] = useState<CrossServerGroup | null>(null);
  const [domainModal, setDomainModal] = useState<CrossServerMatchDomain | null>(null);
  const [typeModal, setTypeModal] = useState<CrossPlayType | null>(null);
  const [featureKey, setFeatureKey] = useState('');
  const [clusterForm] = Form.useForm();
  const [groupForm] = Form.useForm();
  const [domainForm] = Form.useForm();
  const [typeForm] = Form.useForm();

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    try {
      const cfg = await getCrossServerConfig();
      setSpec({ ...emptySpec(), ...cfg.spec });
      setMeta({ version: cfg.version, hash: cfg.hash, updated_at: cfg.updated_at });
      setDirty(false);
    } catch {
      // request() 已提示错误
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchConfig(); }, [fetchConfig]);

  const save = async () => {
    setSaving(true);
    try {
      const res = await updateCrossServerConfig(spec);
      setMeta({ version: res.config.version, hash: res.config.hash, updated_at: res.config.updated_at });
      setLastNotify(res.notify);
      setDirty(false);
      if (res.notify.idempotent) {
        message.info(t('csIdempotent'));
      } else {
        message.success(t('csPublished') + ` v${res.config.version}`);
      }
    } catch {
      // 校验失败等错误由 request() 提示
    } finally {
      setSaving(false);
    }
  };

  // ── 集群 ───────────────────────────────────────────────────────────

  const editCluster = (c?: CrossServerCluster) => {
    const target = c ?? { id: '', name: '', region: '', status: '', servers: [] };
    setClusterModal(target);
    clusterForm.setFieldsValue(target);
  };

  const saveCluster = async () => {
    const v = await clusterForm.validateFields();
    const next = spec.topology.clusters.map((c) => (c.id === clusterModal?.id ? { ...c, ...v } : c));
    if (!clusterModal || !spec.topology.clusters.some((c) => c.id === clusterModal.id)) {
      next.push({ servers: [], ...v });
    }
    setSpec((s) => ({ ...s, topology: { clusters: next } }));
    setDirty(true);
    setClusterModal(null);
  };

  const removeCluster = (id: string) => {
    setSpec((s) => ({ ...s, topology: { clusters: s.topology.clusters.filter((c) => c.id !== id) } }));
    setDirty(true);
  };

  // ── 分组 ───────────────────────────────────────────────────────────

  const editGroup = (g?: CrossServerGroup) => {
    const target = g ?? { id: '', name: '', servers: [] };
    setGroupModal(target);
    groupForm.setFieldsValue(target);
  };

  const saveGroup = async () => {
    const v = await groupForm.validateFields();
    const exists = groupModal && spec.groups.some((g) => g.id === groupModal.id);
    setSpec((s) => ({
      ...s,
      groups: exists
        ? s.groups.map((g) => (g.id === groupModal?.id ? { ...g, ...v } : g))
        : [...s.groups, { servers: [], ...v }],
    }));
    setDirty(true);
    setGroupModal(null);
  };

  const removeGroup = (id: string) => {
    setSpec((s) => ({ ...s, groups: s.groups.filter((g) => g.id !== id) }));
    setDirty(true);
  };

  // ── 匹配域 ─────────────────────────────────────────────────────────

  const editDomain = (d?: CrossServerMatchDomain) => {
    const target = d ?? { id: '', name: '', servers: [], params: {} };
    setDomainModal(target);
    domainForm.setFieldsValue({ ...target, paramsText: paramsToText(target.params) });
  };

  const paramsToText = (p?: Record<string, string>) =>
    Object.entries(p ?? {}).map(([k, v]) => `${k}=${v}`).join('\n');

  const textToParams = (text: string): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const line of text.split('\n')) {
      const idx = line.indexOf('=');
      if (idx > 0) out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
    }
    return out;
  };

  const saveDomain = async () => {
    const v = await domainForm.validateFields();
    const domain: CrossServerMatchDomain = {
      id: v.id, name: v.name ?? '', servers: v.servers ?? [],
      params: textToParams(v.paramsText ?? ''),
    };
    const exists = domainModal && spec.match_domains.some((d) => d.id === domainModal.id);
    setSpec((s) => ({
      ...s,
      match_domains: exists
        ? s.match_domains.map((d) => (d.id === domainModal?.id ? domain : d))
        : [...s.match_domains, domain],
    }));
    setDirty(true);
    setDomainModal(null);
  };

  const removeDomain = (id: string) => {
    setSpec((s) => ({ ...s, match_domains: s.match_domains.filter((d) => d.id !== id) }));
    setDirty(true);
  };

  // ── 玩法开关 ───────────────────────────────────────────────────────

  const toggleFeature = (key: string, on: boolean) => {
    setSpec((s) => ({ ...s, features: { ...s.features, [key]: on } }));
    setDirty(true);
  };

  const addFeature = () => {
    const key = featureKey.trim();
    if (!key) return;
    if (spec.features[key] !== undefined) {
      message.warning(t('csFeatureExists'));
      return;
    }
    setSpec((s) => ({ ...s, features: { ...s.features, [key]: true } }));
    setFeatureKey('');
    setDirty(true);
  };

  const removeFeature = (key: string) => {
    setSpec((s) => {
      const features = { ...s.features };
      delete features[key];
      return { ...s, features };
    });
    setDirty(true);
  };

  // ── 玩法类型表 ─────────────────────────────────────────────────────

  const editType = (tp?: CrossPlayType) => {
    const target = tp ?? { id: '' };
    setTypeModal(target);
    typeForm.setFieldsValue({
      id: target.id,
      name: target.name ?? '',
      summary: target.summary ?? '',
      lifecycle: target.lifecycle ?? '',
      matchmaking: target.matchmaking ?? false,
      ranking: target.ranking ?? false,
      id_prefix: target.id_prefix ?? '',
    });
  };

  const saveType = async () => {
    const v = await typeForm.validateFields();
    const row: CrossPlayType = { id: v.id };
    // 与服务端 omitempty 同口径：空串/关掉的开关不落段，保存往返
    // 不会把「未声明」改写成「显式空值」（hash 才对得上、幂等才成立）。
    if (v.name) row.name = v.name;
    if (v.summary) row.summary = v.summary;
    if (v.lifecycle) row.lifecycle = v.lifecycle;
    if (v.matchmaking) row.matchmaking = true;
    if (v.ranking) row.ranking = true;
    if (v.id_prefix) row.id_prefix = v.id_prefix;
    const exists = typeModal && spec.crossplay_types.some((tp) => tp.id === typeModal.id);
    setSpec((s) => ({
      ...s,
      crossplay_types: exists
        ? s.crossplay_types.map((tp) => (tp.id === typeModal?.id ? row : tp))
        : [...s.crossplay_types, row],
    }));
    setDirty(true);
    setTypeModal(null);
  };

  const removeType = (id: string) => {
    setSpec((s) => ({ ...s, crossplay_types: s.crossplay_types.filter((tp) => tp.id !== id) }));
    setDirty(true);
  };

  /** 类型 ID 唯一性（编辑行豁免自己，与 id_prefix 同款口径）。 */
  const uniqueTypeId = (_: unknown, value: string) => {
    if (!value || !TYPE_ID_RE.test(value)) return Promise.resolve();
    const clash = spec.crossplay_types.some(
      (tp) => tp.id === value && tp.id !== typeModal?.id,
    );
    return clash ? Promise.reject(new Error(t('csTypeIdTaken'))) : Promise.resolve();
  };

  /** id_prefix 全表唯一（编辑行豁免自己）——前缀歧义会断日志回溯。 */
  const uniqueTypePrefix = (_: unknown, value: string) => {
    if (!value) return Promise.resolve();
    if (!TYPE_PREFIX_RE.test(value)) return Promise.reject(new Error(t('csTypePrefixPattern')));
    const clash = spec.crossplay_types.some(
      (tp) => tp.id_prefix === value && tp.id !== typeModal?.id,
    );
    return clash ? Promise.reject(new Error(t('csTypePrefixTaken'))) : Promise.resolve();
  };

  const typeColumns = [
    { title: t('id'), dataIndex: 'id', width: 120, render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
    { title: t('csTypeName'), dataIndex: 'name', width: 150, render: (v: string) => v || '—' },
    { title: t('csTypeSummary'), dataIndex: 'summary', render: (v: string) => v || '—' },
    {
      title: t('csTypeLifecycle'),
      dataIndex: 'lifecycle',
      width: 110,
      // 服务端缺省 persistent，未声明按缺省渲染。
      render: (v: string) => (
        <Tag color={v === 'seasonal' ? 'gold' : v === 'ephemeral' ? 'purple' : 'green'}>
          {v || 'persistent'}
        </Tag>
      ),
    },
    {
      title: t('csTypeMatchmaking'),
      dataIndex: 'matchmaking',
      width: 90,
      render: (v: boolean) => <Tag color={v ? 'blue' : 'default'}>{v ? t('yes') : t('no')}</Tag>,
    },
    {
      title: t('csTypeRanking'),
      dataIndex: 'ranking',
      width: 80,
      render: (v: boolean) => <Tag color={v ? 'cyan' : 'default'}>{v ? t('yes') : t('no')}</Tag>,
    },
    {
      title: t('csTypeIdPrefix'),
      dataIndex: 'id_prefix',
      width: 100,
      render: (v: string) => (v ? <Typography.Text code>{v}</Typography.Text> : '—'),
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, tp: CrossPlayType) => (
        <Space>
          <Button size="small" onClick={() => editType(tp)}>{t('edit')}</Button>
          <Popconfirm title={t('confirmDelete')} onConfirm={() => removeType(tp.id)}>
            <Button size="small" danger>{t('delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const clusterColumns = [
    { title: t('id'), dataIndex: 'id', width: 130 },
    { title: t('name'), dataIndex: 'name', width: 140, render: (v: string) => v || '—' },
    { title: t('region'), dataIndex: 'region', width: 100, render: (v: string) => v || '—' },
    {
      title: t('status'),
      dataIndex: 'status',
      width: 90,
      render: (v: string) => <Tag color={v === 'disabled' ? 'red' : 'green'}>{v || 'active'}</Tag>,
    },
    {
      title: t('csServers'),
      dataIndex: 'servers',
      render: (v: string[]) => (v?.length ? v.map((id) => <Tag key={id}>{id}</Tag>) : '—'),
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, c: CrossServerCluster) => (
        <Space>
          <Button size="small" onClick={() => editCluster(c)}>{t('edit')}</Button>
          <Popconfirm title={t('confirmDelete')} onConfirm={() => removeCluster(c.id)}>
            <Button size="small" danger>{t('delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const groupColumns = [
    { title: t('id'), dataIndex: 'id', width: 140 },
    { title: t('name'), dataIndex: 'name', width: 160, render: (v: string) => v || '—' },
    {
      title: t('csServers'),
      dataIndex: 'servers',
      render: (v: string[]) => (v?.length ? v.map((id) => <Tag key={id}>{id}</Tag>) : '—'),
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, g: CrossServerGroup) => (
        <Space>
          <Button size="small" onClick={() => editGroup(g)}>{t('edit')}</Button>
          <Popconfirm title={t('confirmDelete')} onConfirm={() => removeGroup(g.id)}>
            <Button size="small" danger>{t('delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const domainColumns = [
    { title: t('id'), dataIndex: 'id', width: 130 },
    { title: t('name'), dataIndex: 'name', width: 130, render: (v: string) => v || '—' },
    {
      title: t('csServers'),
      dataIndex: 'servers',
      render: (v: string[]) => (v?.length ? v.map((id) => <Tag key={id}>{id}</Tag>) : '—'),
    },
    {
      title: t('csParams'),
      dataIndex: 'params',
      render: (p: Record<string, string>) =>
        Object.entries(p ?? {}).map(([k, v]) => <Tag key={k} color="geekblue">{k}={v}</Tag>),
    },
    {
      title: t('actions'),
      key: 'actions',
      width: 120,
      render: (_: unknown, d: CrossServerMatchDomain) => (
        <Space>
          <Button size="small" onClick={() => editDomain(d)}>{t('edit')}</Button>
          <Popconfirm title={t('confirmDelete')} onConfirm={() => removeDomain(d.id)}>
            <Button size="small" danger>{t('delete')}</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        loading={loading}
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={fetchConfig}>{t('refresh')}</Button>
            <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={save} disabled={!dirty}>
              {t('csPublish')}
            </Button>
          </Space>
        }
        title={t('crossserverConfig')}
      >
        <Descriptions size="small" column={4}>
          <Descriptions.Item label={t('csVersion')}>
            <Tag color="geekblue">v{meta?.version ?? 0}</Tag>
            {dirty && <Tag color="orange">{t('csDirty')}</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="hash">
            <Typography.Text code copyable>{meta?.hash ?? '—'}</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label={t('updated')}>
            {meta?.updated_at ? new Date(meta.updated_at).toLocaleString() : '—'}
          </Descriptions.Item>
          <Descriptions.Item label={t('csNotifyBus')}>
            {lastNotify ? lastNotify.bus : (meta?.version ? 'atlas.config' : '—')}
          </Descriptions.Item>
        </Descriptions>
        {lastNotify && !lastNotify.idempotent && (
          <Alert
            style={{ marginTop: 8 }}
            type={lastNotify.callbacks.failed > 0 ? 'warning' : 'success'}
            showIcon
            message={
              lastNotify.callbacks.failed > 0
                ? t('csNotifyPartial') + `（${lastNotify.callbacks.delivered}/${lastNotify.callbacks.targets}）`
                : t('csNotifyOk') + `（${lastNotify.callbacks.delivered}/${lastNotify.callbacks.targets}）`
            }
            description={
              <Space split="·" size={4}>
                <span>{t('csNotifyTargets')}: {lastNotify.targets.join(', ') || '—'}</span>
                {lastNotify.bus_error && <span>bus: {lastNotify.bus_error}</span>}
                {lastNotify.callbacks.errors?.map((e, i) => <span key={i}>{e}</span>)}
              </Space>
            }
          />
        )}
      </Card>

      <Card
        title={<Space><ClusterOutlined />{t('csTopology')}</Space>}
        extra={<Button icon={<PlusOutlined />} onClick={() => editCluster()}>{t('csAddCluster')}</Button>}
      >
        <Table
          rowKey="id" size="small" columns={clusterColumns}
          dataSource={spec.topology.clusters} pagination={false}
          locale={{ emptyText: t('noData') }}
        />
      </Card>

      <Card
        title={<Space><PartitionOutlined />{t('csGroups')}</Space>}
        extra={<Button icon={<PlusOutlined />} onClick={() => editGroup()}>{t('csAddGroup')}</Button>}
      >
        <Table
          rowKey="id" size="small" columns={groupColumns}
          dataSource={spec.groups} pagination={false}
          locale={{ emptyText: t('noData') }}
        />
      </Card>

      <Card
        title={<Space><AppstoreOutlined />{t('csFeatures')}</Space>}
        extra={
          <Space.Compact>
            <Input
              placeholder={t('csFeaturePlaceholder')}
              value={featureKey}
              onChange={(e) => setFeatureKey(e.target.value)}
              onPressEnter={addFeature}
              style={{ width: 220 }}
            />
            <Button icon={<PlusOutlined />} onClick={addFeature} />
          </Space.Compact>
        }
      >
        <Space wrap size={[12, 8]}>
          {Object.entries(spec.features).map(([key, on]) => (
            <Space key={key} size={4}>
              <Switch size="small" checked={on} onChange={(v) => toggleFeature(key, v)} />
              <Typography.Text code>{key}</Typography.Text>
              <Button size="small" type="text" danger onClick={() => removeFeature(key)}>×</Button>
            </Space>
          ))}
          {Object.keys(spec.features).length === 0 && (
            <Typography.Text type="secondary">{t('noData')}</Typography.Text>
          )}
        </Space>
      </Card>

      <Card
        title={<Space><PartitionOutlined />{t('csMatchDomains')}</Space>}
        extra={<Button icon={<PlusOutlined />} onClick={() => editDomain()}>{t('csAddDomain')}</Button>}
      >
        <Table
          rowKey="id" size="small" columns={domainColumns}
          dataSource={spec.match_domains} pagination={false}
          locale={{ emptyText: t('noData') }}
        />
      </Card>

      <Card
        title={<Space><TagsOutlined />{t('csCrossPlayTypes')}</Space>}
        extra={<Button icon={<PlusOutlined />} onClick={() => editType()}>{t('csAddType')}</Button>}
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={t('csTypeFleetWide')}
          description={t('csTypeFleetWideTip')}
        />
        <Table
          rowKey="id" size="small" columns={typeColumns}
          dataSource={spec.crossplay_types} pagination={false}
          locale={{ emptyText: t('noData') }}
        />
      </Card>

      <Modal
        title={t('csAddCluster')}
        open={clusterModal !== null}
        onOk={saveCluster}
        onCancel={() => setClusterModal(null)}
        destroyOnClose
      >
        <Form form={clusterForm} layout="vertical">
          <Form.Item name="id" label={t('id')} rules={[{ required: true }]}>
            <Input placeholder="cluster-ea" disabled={!!clusterModal && spec.topology.clusters.some((c) => c.id === clusterModal.id)} />
          </Form.Item>
          <Form.Item name="name" label={t('name')}><Input /></Form.Item>
          <Form.Item name="region" label={t('region')}><Input placeholder="cn-east" /></Form.Item>
          <Form.Item name="status" label={t('status')}>
            <Select allowClear options={[
              { value: 'active', label: 'active' },
              { value: 'disabled', label: 'disabled' },
            ]} />
          </Form.Item>
          <Form.Item name="servers" label={t('csServers')}>
            <ServerIdsInput />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('csAddGroup')}
        open={groupModal !== null}
        onOk={saveGroup}
        onCancel={() => setGroupModal(null)}
        destroyOnClose
      >
        <Form form={groupForm} layout="vertical">
          <Form.Item name="id" label={t('id')} rules={[{ required: true }]}>
            <Input placeholder="season-1" />
          </Form.Item>
          <Form.Item name="name" label={t('name')}><Input /></Form.Item>
          <Form.Item name="servers" label={t('csServers')}>
            <ServerIdsInput />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={t('csAddDomain')}
        open={domainModal !== null}
        onOk={saveDomain}
        onCancel={() => setDomainModal(null)}
        destroyOnClose
      >
        <Form form={domainForm} layout="vertical">
          <Form.Item name="id" label={t('id')} rules={[{ required: true }]}>
            <Input placeholder="mmr-0-3000" />
          </Form.Item>
          <Form.Item name="name" label={t('name')}><Input /></Form.Item>
          <Form.Item name="servers" label={t('csServers')}>
            <ServerIdsInput />
          </Form.Item>
          <Form.Item name="paramsText" label={t('csParams')} tooltip={t('csParamsTip')}>
            <Input.TextArea rows={3} placeholder={'mmr=0-3000\nmax_team=3'} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={typeModal?.id ? t('edit') + ' · ' + t('csCrossPlayTypes') : t('csAddType')}
        open={typeModal !== null}
        onOk={saveType}
        onCancel={() => setTypeModal(null)}
        destroyOnClose
      >
        <Form form={typeForm} layout="vertical">
          <Form.Item
            name="id"
            label={t('id')}
            tooltip={t('csTypeIdTip')}
            rules={[
              { required: true },
              { pattern: TYPE_ID_RE, message: t('csTypeIdPattern') },
              { validator: uniqueTypeId },
            ]}
          >
            <Input
              placeholder="battlefield"
              disabled={!!typeModal?.id && spec.crossplay_types.some((tp) => tp.id === typeModal.id)}
            />
          </Form.Item>
          <Form.Item name="name" label={t('csTypeName')}>
            <Input placeholder={t('csTypeNamePlaceholder')} />
          </Form.Item>
          <Form.Item name="summary" label={t('csTypeSummary')}>
            <Input.TextArea rows={2} placeholder={t('csTypeSummaryPlaceholder')} />
          </Form.Item>
          <Form.Item name="lifecycle" label={t('csTypeLifecycle')} tooltip={t('csTypeLifecycleTip')}>
            <Select
              allowClear
              placeholder="persistent"
              options={[
                { value: 'persistent', label: 'persistent' },
                { value: 'seasonal', label: 'seasonal' },
                { value: 'ephemeral', label: 'ephemeral' },
              ]}
            />
          </Form.Item>
          <Space size={24}>
            <Form.Item name="matchmaking" label={t('csTypeMatchmaking')} valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="ranking" label={t('csTypeRanking')} valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item
            name="id_prefix"
            label={t('csTypeIdPrefix')}
            tooltip={t('csTypeIdPrefixTip')}
            rules={[{ validator: uniqueTypePrefix }]}
          >
            <Input placeholder="xb" maxLength={8} style={{ width: 160 }} />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
