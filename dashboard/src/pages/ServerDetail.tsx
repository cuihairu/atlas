import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Descriptions, Card, Table, Button, Space, Progress, Spin, message, Popconfirm,
  Select, Input, Switch, Tooltip, Tag,
} from 'antd';
import { ArrowLeftOutlined, PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import StatusTag from '../components/StatusTag';
import TagBadge from '../components/TagBadge';
import { useLang, t, getLang } from '../i18n';
import {
  getServer,
  getServerTags,
  addServerTag,
  removeServerTag,
  listCharactersByServer,
  serverMaintenance,
  serverDrain,
  serverEnable,
  serverDisable,
} from '../api/client';
import type { Server, ServerTag, TagTier, Character } from '../types';
import { humanDuration } from '../types';

function timeLocale(): string {
  return getLang() === 'zh' ? 'zh-CN' : 'en-US';
}

// Mirrors model.PresetTags on the Go side (code → default label + tier).
const PRESETS: { code: string; label: string; tier: TagTier }[] = [
  { code: 'hot', label: '火热', tier: 'hot' },
  { code: 'full', label: '爆满', tier: 'warning' },
  { code: 'no_register', label: '禁止注册', tier: 'warning' },
  { code: 'maintenance', label: '维护中', tier: 'warning' },
  { code: 'new', label: '新服', tier: 'new' },
  { code: 'recommended', label: '推荐', tier: 'info' },
];

export default function ServerDetail() {
  useLang(); // re-render on language switch
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [server, setServer] = useState<Server | null>(null);
  const [characters, setCharacters] = useState<Character[]>([]);
  const [adminTags, setAdminTags] = useState<ServerTag[]>([]);
  const [loading, setLoading] = useState(true);

  // Add-tag form state.
  const [preset, setPreset] = useState<string | undefined>();
  const [customCode, setCustomCode] = useState('');
  const [customLabel, setCustomLabel] = useState('');
  const [customTier, setCustomTier] = useState<TagTier>('neutral');
  const [isPublic, setIsPublic] = useState(true);
  const [submitting, setSubmitting] = useState(false);

  const fetch = useCallback(async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [s, c, tags] = await Promise.all([
        getServer(id),
        listCharactersByServer(id),
        getServerTags(id).catch(() => ({ server_id: id, tags: [] as ServerTag[] })),
      ]);
      setServer(s);
      setCharacters(c);
      setAdminTags(tags.tags);
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

  const submitTag = async () => {
    if (!id) return;
    const body = preset
      ? { code: preset }
      : {
          code: customCode.trim(),
          label: customLabel.trim(),
          tier: customTier,
          public: isPublic,
        };
    if (!body.code) return;
    setSubmitting(true);
    try {
      const res = await addServerTag(id, body);
      setAdminTags(res.tags);
      message.success(t('tagAdded'));
      setPreset(undefined);
      setCustomCode('');
      setCustomLabel('');
      setIsPublic(true);
    } catch {
      // handled by client
    } finally {
      setSubmitting(false);
    }
  };

  const dropTag = async (code: string) => {
    if (!id) return;
    try {
      await removeServerTag(id, code);
      setAdminTags((prev) => prev.filter((tag) => tag.code !== code));
      message.success(t('tagRemoved'));
    } catch {
      // handled by client
    }
  };

  if (loading || !server) return <Spin size="large" style={{ display: 'block', margin: '120px auto' }} />;

  const pct = server.capacity > 0 ? Math.round((server.players / server.capacity) * 100) : 0;

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
          <Descriptions.Item label={t('type')}>{server.type || '—'}</Descriptions.Item>
          <Descriptions.Item label={t('version')}>{server.version}</Descriptions.Item>
          <Descriptions.Item label={t('status')}>
            <StatusTag status={server.status} />
          </Descriptions.Item>
          {server.realm_id && (
            <Descriptions.Item label={t('realm')}>{server.realm_id}</Descriptions.Item>
          )}
          {server.shard_id && (
            <Descriptions.Item label={t('shard')}>{server.shard_id}</Descriptions.Item>
          )}
          {server.platform && (
            <Descriptions.Item label={t('platform')}>{server.platform}</Descriptions.Item>
          )}
          {server.endpoint && (
            <Descriptions.Item label={t('endpoint')}>
              {server.endpoint.host}:{server.endpoint.port}
            </Descriptions.Item>
          )}
        </Descriptions>
        {/* 时间三件套（item 9）：开服时间 / 最近心跳 / 在线时长（人性化）。 */}
        <Descriptions column={{ xs: 1, sm: 3 }} style={{ marginTop: 8 }} size="small">
          <Descriptions.Item label={t('serverStarted')}>
            {server.started_at ? new Date(server.started_at).toLocaleString(timeLocale()) : '—'}
          </Descriptions.Item>
          <Descriptions.Item label={t('lastSeen')}>
            {server.last_seen_at ? new Date(server.last_seen_at).toLocaleString(timeLocale()) : '—'}
          </Descriptions.Item>
          <Descriptions.Item label={t('onlineDuration')}>
            {/* 在线时长 = 开服 → 最近心跳（无心跳则到当前时刻）。 */}
            {humanDuration(server.started_at, server.last_seen_at ?? undefined)}
          </Descriptions.Item>
        </Descriptions>
        <div style={{ marginTop: 8 }}>
          <span style={{ marginRight: 8 }}>{t('tags')}:</span>
          {server.tags && server.tags.length > 0 ? (
            server.tags.map((tag) => <TagBadge key={tag.code} tag={tag} />)
          ) : (
            <span style={{ color: '#999' }}>-</span>
          )}
        </div>
      </Card>

      <Card title={t('metadata')} style={{ marginBottom: 24 }} size="small">
        {server.metadata && Object.keys(server.metadata).length > 0 ? (
          <Space size={[8, 8]} wrap>
            {Object.entries(server.metadata).map(([k, v]) => (
              <Tag key={k} color="geekblue">{k}={v}</Tag>
            ))}
          </Space>
        ) : (
          <span style={{ color: '#999' }}>{t('noData')}</span>
        )}
      </Card>

      <Card title={t('liveMetrics')} style={{ marginBottom: 24 }}>
        <Descriptions column={{ xs: 1, sm: 3 }}>
          <Descriptions.Item label={t('playerCount')}>{server.players}</Descriptions.Item>
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

      <Card title={t('tagManage')} style={{ marginBottom: 24 }}>
        <div style={{ marginBottom: 16 }}>
          {adminTags.length > 0 ? (
            <Space size={[12, 8]} wrap>
              {adminTags.map((tag) => (
                <span
                  key={tag.code}
                  style={{
                    display: 'inline-flex', alignItems: 'center', gap: 4,
                    border: '1px solid #eee', borderRadius: 6, padding: '2px 8px',
                  }}
                >
                  <TagBadge tag={tag} />
                  <span style={{ color: '#888', fontSize: 12 }}>{tag.code}</span>
                  <Tag
                    color={tag.public ? 'geekblue' : 'default'}
                    style={{ marginInlineEnd: 0, fontSize: 12 }}
                  >
                    {tag.public ? t('publicTag') : t('internalTag')}
                  </Tag>
                  <Popconfirm
                    title={t('confirmRemoveTag', { name: tag.label })}
                    onConfirm={() => dropTag(tag.code)}
                  >
                    <Button type="text" size="small" danger icon={<DeleteOutlined />} />
                  </Popconfirm>
                </span>
              ))}
            </Space>
          ) : (
            <span style={{ color: '#999' }}>{t('noTags')}</span>
          )}
        </div>

        <Space wrap>
          <Select
            allowClear
            placeholder={t('presetTag')}
            style={{ width: 160 }}
            value={preset}
            onChange={(v) => setPreset(v)}
            options={PRESETS.map((p) => ({
              value: p.code,
              label: `${p.label} (${p.code})`,
            }))}
          />
          {!preset && (
            <>
              <Input
                placeholder={t('tagCode')}
                style={{ width: 140 }}
                value={customCode}
                onChange={(e) => setCustomCode(e.target.value)}
              />
              <Input
                placeholder={t('tagLabel')}
                style={{ width: 120 }}
                value={customLabel}
                onChange={(e) => setCustomLabel(e.target.value)}
              />
              <Select
                placeholder={t('tagTier')}
                style={{ width: 110 }}
                value={customTier}
                onChange={setCustomTier}
                options={[
                  { value: 'hot', label: t('tierHot') },
                  { value: 'new', label: t('tierNew') },
                  { value: 'warning', label: t('tierWarning') },
                  { value: 'info', label: t('tierInfo') },
                  { value: 'neutral', label: t('tierNeutral') },
                ]}
              />
              <Tooltip title={t('publicHint')}>
                <span>
                  <Switch checked={isPublic} onChange={setIsPublic} checkedChildren={t('publicTag')} unCheckedChildren={t('internalTag')} />
                </span>
              </Tooltip>
            </>
          )}
          <Button
            type="primary"
            icon={<PlusOutlined />}
            loading={submitting}
            disabled={preset ? false : !customCode.trim()}
            onClick={submitTag}
          >
            {t('addTag')}
          </Button>
        </Space>
        <div style={{ marginTop: 8, color: '#999', fontSize: 12 }}>{t('tagHint')}</div>
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
            { title: t('accountId'), dataIndex: 'account_id', ellipsis: true },
            {
              title: t('lastLogin'),
              dataIndex: 'last_login_at',
              render: (v?: string | null) => (v ? new Date(v).toLocaleString(timeLocale()) : '-'),
            },
          ]}
        />
      </Card>
    </>
  );
}
