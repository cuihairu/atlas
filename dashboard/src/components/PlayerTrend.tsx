// Player trend chart (TODO v0.1.18): the backend keeps no history yet, so
// the dashboard builds its own time series — each poll appends a sample to a
// rolling localStorage buffer (7 days) and the line chart renders the last
// 24 hours or 7 days.
import { useEffect, useState, useCallback } from 'react';
import { Card, Segmented, Empty } from 'antd';
import { Line } from '@ant-design/charts';
import { useLang, t } from '../i18n';
import { getStats } from '../api/client';

const REFRESH_MS = 30_000;
const STORE_KEY = 'atlas-player-trend';
const MAX_AGE_MS = 7 * 24 * 3600 * 1000;
const MAX_SAMPLES = 5000;
const MIN_SAMPLE_GAP_MS = 60_000;

interface TrendSample {
  t: number; // epoch ms
  players: number;
  capacity: number;
}

function loadSamples(): TrendSample[] {
  try {
    const raw = localStorage.getItem(STORE_KEY);
    if (!raw) return [];
    const arr = JSON.parse(raw) as TrendSample[];
    return Array.isArray(arr) ? arr.filter((s) => typeof s.t === 'number') : [];
  } catch {
    return [];
  }
}

function appendSample(samples: TrendSample[], players: number, capacity: number): TrendSample[] {
  const now = Date.now();
  const last = samples[samples.length - 1];
  // One sample per minute across tabs/pages sharing this browser.
  if (last && now - last.t < MIN_SAMPLE_GAP_MS) {
    last.players = players;
    last.capacity = capacity;
    return samples;
  }
  const next = [...samples, { t: now, players, capacity }];
  const cutoff = now - MAX_AGE_MS;
  const trimmed = next.filter((s) => s.t >= cutoff);
  return trimmed.length > MAX_SAMPLES ? trimmed.slice(trimmed.length - MAX_SAMPLES) : trimmed;
}

function saveSamples(samples: TrendSample[]): void {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(samples));
  } catch {
    // quota exceeded → trend history simply stops growing
  }
}

type TrendWindow = '24h' | '7d';

export default function PlayerTrend() {
  useLang(); // re-render on language switch
  const [samples, setSamples] = useState<TrendSample[]>(() => loadSamples());
  const [win, setWin] = useState<TrendWindow>('24h');

  const refresh = useCallback(async () => {
    try {
      const stats = await getStats();
      const next = appendSample(loadSamples(), stats.total_players, stats.total_capacity);
      saveSamples(next);
      setSamples(next);
    } catch {
      // error already surfaced by the API client
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, REFRESH_MS);
    return () => clearInterval(id);
  }, [refresh]);

  const cutoff = Date.now() - (win === '24h' ? 24 * 3600 * 1000 : MAX_AGE_MS);
  const windowed = samples.filter((s) => s.t >= cutoff);
  const data = windowed.map((s) => ({
    time: new Date(s.t).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }),
    players: s.players,
  }));

  return (
    <Card
      title={t('playerTrend')}
      extra={
        <Segmented
          size="small"
          value={win}
          onChange={(v) => setWin(v as TrendWindow)}
          options={[
            { label: t('last24h'), value: '24h' },
            { label: t('last7d'), value: '7d' },
          ]}
        />
      }
    >
      {data.length < 2 ? (
        <Empty
          description={`${t('noData')} — ${t('realTime').toLowerCase()}`}
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          style={{ padding: '48px 0' }}
        />
      ) : (
        <Line
          data={data}
          xField="time"
          yField="players"
          height={280}
          smooth
          color="#d97706"
          point={{ size: win === '24h' ? 3 : 0 }}
          axis={{ x: { labelAutoRotate: true, labelAutoHide: true } }}
          tooltip={{ title: 'time', items: [{ channel: 'y', name: t('onlinePlayers') }] }}
        />
      )}
    </Card>
  );
}
