import { useCallback, useEffect, useState } from 'react';
import { Card, Segmented, Spin, Tooltip } from 'antd';
import { FireOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import StatusTag from '../components/StatusTag';
import { getLoadHeatmap } from '../api/client';
import { useLang, t } from '../i18n';
import type { HeatmapWindow, LoadHeatmapResponse, ServerHeatmap } from '../types';

const REFRESH_INTERVAL = 30_000;

// Load tiers on the Okabe-Ito warm subset (sky blue → yellow → orange →
// vermillion): monotonic "heat" ramp with no red-green axis, so every pair
// stays separable under deutan/protan. Color never works alone — each cell
// carries its numeric load and the legend spells out the ranges.
const TIERS = [
  { max: 0.3, color: '#56B4E9', label: 'heatIdle' },
  { max: 0.7, color: '#F0E442', label: 'heatNormal' },
  { max: 0.9, color: '#E69F00', label: 'heatHigh' },
  { max: Infinity, color: '#D55E00', label: 'heatOverload' },
] as const;

const NO_DATA_COLOR = '#94a3b8';

function tierOf(load: number) {
  return TIERS.find((tr) => load < tr.max) ?? TIERS[TIERS.length - 1];
}

function tierLabel(tr: (typeof TIERS)[number]): string {
  switch (tr.label) {
    case 'heatIdle':
      return `${t('heatIdle')} < 30%`;
    case 'heatNormal':
      return `${t('heatNormal')} 30–70%`;
    case 'heatHigh':
      return `${t('heatHigh')} 70–90%`;
    default:
      return `${t('heatOverload')} ≥ 90%`;
  }
}

function fmtPct(v: number): string {
  return `${Math.round(v * 100)}%`;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleTimeString();
}

// LoadCell is one grid cell: live record + the per-bucket strip (time
// dimension). Numbers and status are always on the cell itself, so the
// tier color is redundant encoding, never the only channel.
function LoadCell({ row }: { row: ServerHeatmap }) {
  const navigate = useNavigate();
  const tr = tierOf(row.load);
  const hasSamples = row.buckets.some((b) => b.samples > 0);

  const tooltip = (
    <div style={{ minWidth: 220 }}>
      <div style={{ fontWeight: 600, marginBottom: 4 }}>
        {row.name || row.server_id} <span style={{ opacity: 0.7 }}>({row.server_id})</span>
      </div>
      <div>
        {t('load')}: {fmtPct(row.load)} · {t('players')}: {row.players}/{row.capacity}
      </div>
      <div style={{ marginTop: 4, opacity: 0.85 }}>
        {t('heatPerBucket')}:
      </div>
      {row.buckets.map((b, i) => (
        <div key={i} style={{ display: 'flex', justifyContent: 'space-between', opacity: 0.85 }}>
          <span>{fmtTime(b.t)}</span>
          <span>
            {b.samples > 0
              ? `${fmtPct(b.load)} · ${Math.round(b.players)} · ×${b.samples}`
              : t('heatEmptyBucket')}
          </span>
        </div>
      ))}
    </div>
  );

  return (
    <Tooltip title={tooltip} styles={{ root: { maxWidth: 340 } }}>
      <div
        onClick={() => navigate(`/servers/${row.server_id}`)}
        style={{
          cursor: 'pointer',
          borderLeft: `4px solid ${tr.color}`,
          background: `color-mix(in srgb, ${tr.color} 18%, transparent)`,
          borderRadius: 6,
          padding: '8px 10px',
          minHeight: 86,
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {row.name || row.server_id}
          </span>
          <StatusTag status={row.status} />
        </div>
        <div style={{ fontSize: 12, opacity: 0.85 }}>
          {t('load')} <b>{fmtPct(row.load)}</b> · {t('players')} {row.players}/{row.capacity}
        </div>
        {/* Per-bucket strip: the time dimension inside the cell. Grey =
            no samples in that bucket, never rendered as a load color. */}
        <div style={{ display: 'flex', gap: 2, marginTop: 2 }}>
          {row.buckets.map((b, i) => (
            <div
              key={i}
              title={b.samples > 0 ? fmtPct(b.load) : t('heatEmptyBucket')}
              style={{
                flex: 1,
                height: 8,
                borderRadius: 2,
                background: b.samples > 0 ? tierOf(b.load).color : 'transparent',
                border: b.samples > 0 ? 'none' : `1px dashed ${NO_DATA_COLOR}`,
                opacity: b.samples > 0 ? 0.9 : 1,
              }}
            />
          ))}
        </div>
        {!hasSamples && (
          <div style={{ fontSize: 11, color: NO_DATA_COLOR }}>{t('heatNoData')}</div>
        )}
      </div>
    </Tooltip>
  );
}

export default function Heatmap() {
  useLang(); // re-render on language switch
  const [win, setWin] = useState<HeatmapWindow>('1h');
  const [data, setData] = useState<LoadHeatmapResponse | null>(null);
  const [loading, setLoading] = useState(true);

  const fetch = useCallback(async () => {
    try {
      setData(await getLoadHeatmap({ window: win }));
    } finally {
      setLoading(false);
    }
  }, [win]);

  useEffect(() => {
    fetch();
    const id = setInterval(fetch, REFRESH_INTERVAL);
    return () => clearInterval(id);
  }, [fetch]);

  if (loading && !data) {
    return <Spin size="large" style={{ display: 'block', margin: '120px auto' }} />;
  }

  // Group rows by region (the backend already sorts region → server id).
  const regions: { region: string; rows: ServerHeatmap[] }[] = [];
  for (const row of data?.servers ?? []) {
    const last = regions[regions.length - 1];
    if (last && last.region === row.region) last.rows.push(row);
    else regions.push({ region: row.region, rows: [row] });
  }

  return (
    <div>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 12,
          marginBottom: 16,
        }}
      >
        <h2 style={{ margin: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
          <FireOutlined /> {t('serverHeatmap')}
        </h2>
        <Segmented
          value={win}
          onChange={(v) => setWin(v as HeatmapWindow)}
          options={[
            { label: t('heatWindow1h'), value: '1h' },
            { label: t('heatWindow24h'), value: '24h' },
          ]}
        />
        <span style={{ opacity: 0.6, fontSize: 12 }}>
          {t('heatLastUpdated')} {data ? fmtTime(data.generated_at) : '—'}
        </span>
      </div>

      {/* Legend: swatch + tier label + numeric range — color plus words. */}
      <Card size="small" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 16 }}>
          <span style={{ fontWeight: 600 }}>{t('heatLegend')}</span>
          {TIERS.map((tr) => (
            <span key={tr.label} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span
                style={{
                  width: 14,
                  height: 14,
                  borderRadius: 3,
                  background: tr.color,
                  display: 'inline-block',
                }}
              />
              {tierLabel(tr)}
            </span>
          ))}
          <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span
              style={{
                width: 14,
                height: 14,
                borderRadius: 3,
                border: `1px dashed ${NO_DATA_COLOR}`,
                display: 'inline-block',
              }}
            />
            {t('heatNoData')}
          </span>
        </div>
      </Card>

      {regions.length === 0 && (
        <Card>
          <div style={{ textAlign: 'center', padding: '40px 0', opacity: 0.6 }}>
            {t('heatNoServers')}
          </div>
        </Card>
      )}

      {regions.map(({ region, rows }) => (
        <Card
          key={region}
          size="small"
          title={`${t('region')}: ${region || '—'} · ${rows.length}`}
          style={{ marginBottom: 16 }}
        >
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))',
              gap: 10,
            }}
          >
            {rows.map((row) => (
              <LoadCell key={row.server_id} row={row} />
            ))}
          </div>
        </Card>
      ))}
    </div>
  );
}
