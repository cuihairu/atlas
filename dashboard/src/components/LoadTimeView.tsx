// Load time view (item 11): players / load dual lines, window tiers
// 5m/10m/30m/1h/10h, with fleet → region → single-server drill-down
// (precedence: server_id > region > fleet). Data comes from the backend's
// in-memory ring sampling (15s resolution / >=10h retention); switching the
// window or scope refetches immediately.
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

  // Window option labels go through t(); useLang triggers a re-render on language switch, rebuilding them.
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
    // Light polling: samples are 15s buckets; a 30s refresh keeps up without flooding.
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
