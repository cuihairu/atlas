// 负载时间视图（item 11）：players / load 双线，窗口分层 5m/10m/30m/1h/10h，
// 支持全舰队 → 区域 → 单服下钻（server_id > region > fleet 优先级）。
// 数据来自后端内存环形采样（15s 采样 / ≥10h 保留），切换窗口/范围即时重拉。
import { useEffect, useState, useCallback } from 'react';
import { Card, Segmented, Select, Input, Empty } from 'antd';
import { Line } from '@ant-design/charts';
import { useLang, t } from '../i18n';
import { getLoadSeries, getStats } from '../api/client';
import type { SeriesWindow, LoadSeriesPoint, AdminStats } from '../types';

export default function LoadTimeView() {
  useLang();
  const [win, setWin] = useState<SeriesWindow>('1h');
  const [region, setRegion] = useState<string | undefined>();
  const [serverId, setServerId] = useState<string | undefined>();
  const [points, setPoints] = useState<LoadSeriesPoint[]>([]);
  const [regions, setRegions] = useState<AdminStats | null>(null);
  const [loading, setLoading] = useState(false);

  // 窗口选项里的文案走 t()，切语言时由 useLang 触发重渲染重建。
  const windowOptions = [
    { label: t('win5m'), value: '5m' as SeriesWindow },
    { label: t('win10m'), value: '10m' as SeriesWindow },
    { label: t('win30m'), value: '30m' as SeriesWindow },
    { label: t('win1h'), value: '1h' as SeriesWindow },
    { label: t('win10h'), value: '10h' as SeriesWindow },
  ];

  useEffect(() => {
    getStats().then((s) => setRegions(s)).catch(() => undefined);
  }, []);

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getLoadSeries({
        window: win,
        server_id: serverId,
        region: serverId ? undefined : region,
      });
      setPoints(res.points ?? []);
    } catch {
      setPoints([]);
    } finally {
      setLoading(false);
    }
  }, [win, region, serverId]);

  useEffect(() => {
    fetch();
    // 轻轮询：采样 15s 一格，30s 刷新跟得上又不刷屏。
    const id = setInterval(fetch, 30_000);
    return () => clearInterval(id);
  }, [fetch]);

  const data = points.flatMap((p) => [
    {
      time: new Date(p.t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
      value: p.players,
      kind: t('players'),
    },
    {
      time: new Date(p.t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
      value: Number((p.load * 100).toFixed(1)),
      kind: t('load'),
    },
  ]);

  const scopeLabel = serverId
    ? `${t('serverIdSearch')}: ${serverId}`
    : region
      ? `${t('region')}: ${region}`
      : t('fleetScope');

  return (
    <Card
      title={`${t('loadTimeView')} · ${scopeLabel}`}
      loading={loading && points.length === 0}
      extra={
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <Input.Search
            size="small"
            allowClear
            placeholder={t('serverIdSearch')}
            style={{ width: 150 }}
            onSearch={(v) => { setServerId(v || undefined); }}
          />
          <Select
            size="small"
            allowClear
            placeholder={t('region')}
            style={{ width: 110 }}
            value={region}
            onChange={(v) => setRegion(v)}
            disabled={!!serverId}
            options={Object.keys(regions?.servers_by_region ?? {}).sort().map((r) => ({ label: r, value: r }))}
          />
          <Segmented
            size="small"
            value={win}
            onChange={(v) => setWin(v as SeriesWindow)}
            options={windowOptions}
          />
        </div>
      }
    >
      {data.length < 2 ? (
        <Empty
          description={t('noData')}
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ padding: '48px 0' }}
        />
      ) : (
        <Line
          data={data}
          xField="time"
          yField="value"
          seriesField="kind"
          height={280}
          smooth
          color={['#d97706', '#1677ff']}
          point={{ size: 0 }}
          axis={{ x: { labelAutoRotate: false, labelAutoHide: true } }}
          legend={{ position: 'top' }}
        />
      )}
    </Card>
  );
}
