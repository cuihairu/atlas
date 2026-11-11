// 消息总线积压面板（item 13）：每主题积压深度曲线 + 生产/消费速率与
// 累计量。深度超过红色阈值（默认 1000）时曲线标注告警红线。数据来自
// 事件适配器的逐主题计数 + 环形采样曲线；与 /metrics Prometheus 口径
// 同源（同一计数器）。
import { useEffect, useState, useCallback, useMemo } from 'react';
import { Card, Segmented, Table, Tag, Tooltip, InputNumber, Typography } from 'antd';
import { Line } from '@ant-design/charts';
import { useLang, t } from '../i18n';
import { getBusSeries } from '../api/client';
import type { SeriesWindow, BusTopicSeries } from '../types';

const DEFAULT_THRESHOLD = 1000;

export default function BusPanel() {
  useLang();
  const [win, setWin] = useState<SeriesWindow>('1h');
  const [topics, setTopics] = useState<BusTopicSeries[]>([]);
  const [adapter, setAdapter] = useState('');
  const [threshold, setThreshold] = useState<number>(DEFAULT_THRESHOLD);
  const [loading, setLoading] = useState(false);

  const fetch = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getBusSeries({ window: win });
      setTopics(res.topics ?? []);
      setAdapter(res.adapter ?? '');
    } catch {
      setTopics([]);
    } finally {
      setLoading(false);
    }
  }, [win]);

  useEffect(() => {
    fetch();
    const id = setInterval(fetch, 30_000);
    return () => clearInterval(id);
  }, [fetch]);

  // 深度曲线：各主题一条线（kind=topic）；超阈值点位标红由红线注释表达。
  const depthData = topics.flatMap((tp) =>
    (tp.depth ?? []).map((p) => ({
      time: new Date(p.t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }),
      value: p.v,
      kind: tp.topic,
    })),
  );

  const maxDepth = useMemo(
    () => Math.max(0, ...topics.map((tp) => Math.max(0, ...(tp.depth ?? []).map((p) => p.v)))),
    [topics],
  );

  const overThreshold = maxDepth > threshold;

  const columns = [
    { title: t('busTopic'), dataIndex: 'topic', render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
    { title: t('busInFlight'), dataIndex: 'in_flight', width: 90 },
    { title: t('busPublished'), dataIndex: 'published', width: 100 },
    { title: t('busConsumed'), dataIndex: 'consumed', width: 100 },
    {
      title: t('busProduceRate'),
      dataIndex: 'produce_rate',
      width: 130,
      render: (v: number) => t('eventsPerSec', { n: v.toFixed(1) }),
    },
    {
      title: t('busConsumeRate'),
      dataIndex: 'consume_rate',
      width: 130,
      render: (v: number) => t('eventsPerSec', { n: v.toFixed(1) }),
    },
    {
      title: t('busDepth'),
      dataIndex: 'in_flight',
      width: 90,
      render: (v: number) =>
        v > threshold ? <Tag color="red">{v}</Tag> : <span>{v}</span>,
    },
  ];

  const windowOptions = [
    { label: t('win5m'), value: '5m' as SeriesWindow },
    { label: t('win10m'), value: '10m' as SeriesWindow },
    { label: t('win30m'), value: '30m' as SeriesWindow },
    { label: t('win1h'), value: '1h' as SeriesWindow },
    { label: t('win10h'), value: '10h' as SeriesWindow },
  ];

  return (
    <Card
      title={t('busPanel')}
      loading={loading && topics.length === 0}
      extra={
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          {adapter && <Tag color="geekblue">{t('busAdapter')}: {adapter}</Tag>}
          <Tooltip title={t('busAlertTip')}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12 }}>
              {t('busAlertThreshold')}
              <InputNumber
                size="small"
                min={1}
                value={threshold}
                onChange={(v) => setThreshold(v ?? DEFAULT_THRESHOLD)}
                style={{ width: 80 }}
              />
            </span>
          </Tooltip>
          <Segmented
            size="small"
            value={win}
            onChange={(v) => setWin(v as SeriesWindow)}
            options={windowOptions}
          />
        </div>
      }
    >
      {depthData.length >= 2 && (
        <div style={{ marginBottom: 16 }}>
          <Line
            data={depthData}
            xField="time"
            yField="value"
            seriesField="kind"
            height={220}
            smooth
            point={{ size: 0 }}
            axis={{ x: { labelAutoRotate: false, labelAutoHide: true } }}
            legend={{ position: 'top' }}
            // 红色告警阈值线（item 13）：深度越线一眼可见。
            annotations={[{
              type: 'lineY',
              yField: threshold,
              style: { stroke: '#ff4d4f', lineWidth: 1, lineDash: [4, 4] },
              text: overThreshold
                ? { content: `${t('busAlertThreshold')} ${threshold}`, position: 'right', dx: -4, style: { fill: '#ff4d4f' } }
                : undefined,
            }]}
          />
        </div>
      )}
      <Table
        rowKey="topic"
        size="small"
        columns={columns}
        dataSource={topics}
        pagination={false}
        locale={{ emptyText: t('noData') }}
      />
    </Card>
  );
}
