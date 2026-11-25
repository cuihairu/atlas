// Fleet-level batch maintenance windows: one time-span template spread
// across multiple servers (POST /v1/admin/maintenance-windows/batch). Three
// operational semantics converge here:
//   - stagger: the i-th server's window shifts back by i × stagger_seconds,
//     rolling maintenance;
//   - preview: dry_run only validates and returns the planned windows,
//     nothing is persisted;
//   - partial success: unknown/invalid servers each go into failed, the rest
//     are created as usual.
// Assembling form values into the request body, and rendering the result, is
// extracted here so it can be unit-tested without antd forms (RangePicker
// value interaction is unreliable in jsdom, and logic tests are steadier than
// UI tests).
import { Table, Tag, Alert, Typography } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import { t } from '../i18n';
import { createMaintenanceWindowsBatch } from '../api/client';
import type { MaintenanceWindowBatchRequest, MaintenanceWindowBatchResponse } from '../types';

export interface BatchFormValues {
  server_ids?: string[];
  stagger_seconds?: number | null;
  announce?: boolean;
  dry_run?: boolean;
}

/**
 * Form values → batch request body. Server IDs are trimmed, empty entries
 * dropped, and duplicates removed while preserving order; defaults match the
 * backend's semantics (announce=true / stagger=0 / dry_run=false), and the
 * frontend sends them explicitly so it does not rely on server-side defaults.
 * Stagger is rounded down to whole seconds to avoid sending floats.
 */
export function buildBatchRequest(
  values: BatchFormValues,
  range: [Dayjs, Dayjs],
): MaintenanceWindowBatchRequest {
  const seen = new Set<string>();
  const serverIds: string[] = [];
  for (const raw of values.server_ids ?? []) {
    const id = (raw ?? '').trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    serverIds.push(id);
  }
  const stagger = values.stagger_seconds ?? 0;
  return {
    server_ids: serverIds,
    start_at: range[0].toISOString(),
    end_at: range[1].toISOString(),
    stagger_seconds: stagger > 0 ? Math.floor(stagger) : 0,
    announce: values.announce ?? true,
    dry_run: values.dry_run ?? false,
  };
}

/** Call the batch endpoint and fill in missing fields (the backend always
 * returns both arrays; this guards against older-version responses). */
export async function submitBatch(
  values: BatchFormValues,
  range: [Dayjs, Dayjs],
): Promise<MaintenanceWindowBatchResponse> {
  const res = await createMaintenanceWindowsBatch(buildBatchRequest(values, range));
  return { windows: res?.windows ?? [], failed: res?.failed ?? [] };
}

type OutcomeRow = {
  key: string;
  server_id: string;
  window: string;
  ok: boolean;
  error?: string;
};

function fmtTime(v?: string | null): string {
  if (!v) return '—';
  return dayjs(v).format('MM-DD HH:mm');
}

/** Batch outcome: successes and failures combined in one table so operators
 * can see at a glance which servers were not scheduled. */
export function BatchOutcome({
  result,
  dryRun,
}: {
  result: MaintenanceWindowBatchResponse;
  dryRun: boolean;
}) {
  const rows: OutcomeRow[] = [
    ...result.windows.map((w, i) => ({
      key: `ok-${w.id || w.server_id}-${i}`,
      server_id: w.server_id,
      window: `${fmtTime(w.start_at)} → ${fmtTime(w.end_at)}`,
      ok: true,
    })),
    ...result.failed.map((f, i) => ({
      key: `fail-${f.server_id}-${i}`,
      server_id: f.server_id,
      window: '—',
      ok: false,
      error: f.error,
    })),
  ];

  return (
    <div>
      {dryRun && (
        <Alert type="info" showIcon message={t('winBatchPreviewHint')} style={{ marginBottom: 12 }} />
      )}
      {!dryRun && result.failed.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`${t('winBatchPartialMsg')}（${result.failed.length}）`}
          style={{ marginBottom: 12 }}
        />
      )}
      <Table
        scroll={{ x: 'max-content' }}
        rowKey="key"
        size="small"
        dataSource={rows}
        pagination={false}
        locale={{ emptyText: t('noData') }}
        columns={[
          { title: t('serverId'), dataIndex: 'server_id', width: 150, ellipsis: true },
          { title: t('winWindow'), dataIndex: 'window', width: 200 },
          {
            title: t('winBatchOutcome'),
            key: 'outcome',
            render: (_: unknown, row: OutcomeRow) =>
              row.ok ? (
                <Tag color="green">{t('winBatchOk')}</Tag>
              ) : (
                <Tag color="red">
                  {t('winBatchFailed')}
                  <Typography.Text type="secondary" style={{ marginLeft: 6 }}>
                    {row.error}
                  </Typography.Text>
                </Tag>
              ),
          },
        ]}
      />
    </div>
  );
}