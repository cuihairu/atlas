import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'

import { buildBatchRequest, submitBatch, BatchOutcome } from './operationsBatch'
import * as client from '../api/client'
import { t } from '../i18n'
import dayjs from 'dayjs'

vi.mock('../api/client', () => ({
  createMaintenanceWindowsBatch: vi.fn(),
}))

const createMaintenanceWindowsBatch = vi.mocked(client.createMaintenanceWindowsBatch)

const range: [dayjs.Dayjs, dayjs.Dayjs] = [
  dayjs('2026-10-20T02:00:00Z'),
  dayjs('2026-10-20T04:00:00Z'),
]

beforeEach(() => {
  vi.clearAllMocks()
  createMaintenanceWindowsBatch.mockResolvedValue({ windows: [], failed: [] })
})

afterEach(() => cleanup())

describe('buildBatchRequest', () => {
  it('trims, drops blanks and dedupes server ids preserving order', () => {
    const body = buildBatchRequest(
      { server_ids: [' game-1001 ', 'game-1002', '', 'game-1001', '  '] },
      range,
    )
    expect(body.server_ids).toEqual(['game-1001', 'game-1002'])
  })

  it('defaults announce to true, stagger to 0 and dry_run to false', () => {
    const body = buildBatchRequest({ server_ids: ['game-1001'] }, range)
    expect(body.announce).toBe(true)
    expect(body.stagger_seconds).toBe(0)
    expect(body.dry_run).toBe(false)
  })

  it('converts the picked range to ISO timestamps', () => {
    const body = buildBatchRequest({ server_ids: ['game-1001'] }, range)
    expect(new Date(body.start_at).toISOString()).toBe('2026-10-20T02:00:00.000Z')
    expect(new Date(body.end_at).toISOString()).toBe('2026-10-20T04:00:00.000Z')
  })

  it('keeps operator flags and floors a fractional stagger', () => {
    const body = buildBatchRequest(
      { server_ids: ['game-1001'], stagger_seconds: 90.7, announce: false, dry_run: true },
      range,
    )
    expect(body.stagger_seconds).toBe(90)
    expect(body.announce).toBe(false)
    expect(body.dry_run).toBe(true)
  })
})

describe('submitBatch', () => {
  it('posts the built template to the batch endpoint', async () => {
    createMaintenanceWindowsBatch.mockResolvedValue({ windows: [], failed: [] })
    await submitBatch({ server_ids: ['game-1001', 'game-1002'], stagger_seconds: 600 }, range)
    expect(createMaintenanceWindowsBatch).toHaveBeenCalledTimes(1)
    const body = createMaintenanceWindowsBatch.mock.calls[0][0]
    expect(body.server_ids).toEqual(['game-1001', 'game-1002'])
    expect(body.stagger_seconds).toBe(600)
  })

  it('normalizes a partial-success response', async () => {
    createMaintenanceWindowsBatch.mockResolvedValue({
      windows: [
        {
          id: 'mwin-1',
          server_id: 'game-1001',
          start_at: range[0].toISOString(),
          end_at: range[1].toISOString(),
          previous_status: 'online',
        },
      ],
      failed: [{ server_id: 'game-1002', error: 'unknown server' }],
    })
    const res = await submitBatch({ server_ids: ['game-1001', 'game-1002'] }, range)
    expect(res.windows).toHaveLength(1)
    expect(res.failed).toEqual([{ server_id: 'game-1002', error: 'unknown server' }])
  })

  it('treats a bare response as an empty batch instead of crashing', async () => {
    createMaintenanceWindowsBatch.mockResolvedValue({} as never)
    const res = await submitBatch({ server_ids: ['game-1001'] }, range)
    expect(res).toEqual({ windows: [], failed: [] })
  })
})

describe('BatchOutcome', () => {
  it('lists one row per server with its window and outcome', () => {
    render(
      <BatchOutcome
        dryRun={false}
        result={{
          windows: [
            {
              id: 'mwin-1',
              server_id: 'game-1001',
              start_at: range[0].toISOString(),
              end_at: range[1].toISOString(),
              previous_status: 'online',
            },
          ],
          failed: [{ server_id: 'game-1002', error: 'unknown server game-1002' }],
        }}
      />,
    )
    expect(screen.getByText('game-1001')).toBeTruthy()
    expect(screen.getByText('game-1002')).toBeTruthy()
    expect(screen.getByText('unknown server game-1002')).toBeTruthy()
    expect(screen.getAllByText(t('winBatchOk')).length).toBeGreaterThan(0)
    expect(screen.getAllByText(t('winBatchFailed')).length).toBeGreaterThan(0)
  })

  it('flags preview mode so a dry run is not read as a real rollout', () => {
    render(<BatchOutcome dryRun result={{ windows: [], failed: [] }} />)
    expect(screen.getByText(t('winBatchPreviewHint'))).toBeTruthy()
  })

  it('shows nothing persisted when a dry run planned no windows', () => {
    render(
      <BatchOutcome
        dryRun
        result={{ windows: [], failed: [{ server_id: 'game-1001', error: 'bad range' }] }}
      />,
    )
    expect(screen.getByText('bad range')).toBeTruthy()
  })
})