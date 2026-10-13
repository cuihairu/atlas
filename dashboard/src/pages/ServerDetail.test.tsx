import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import ServerDetail from './ServerDetail'
import * as client from '../api/client'
import { t } from '../i18n'
import type { Server, Character } from '../types'

vi.mock('../api/client', () => ({
  getServer: vi.fn(),
  listCharactersByServer: vi.fn(),
  getServerTags: vi.fn(),
  addServerTag: vi.fn(),
  removeServerTag: vi.fn(),
  serverMaintenance: vi.fn(),
  serverDrain: vi.fn(),
  serverEnable: vi.fn(),
  serverDisable: vi.fn(),
}))

const getServer = vi.mocked(client.getServer)
const listCharactersByServer = vi.mocked(client.listCharactersByServer)
const getServerTags = vi.mocked(client.getServerTags)

const srv: Server = {
  id: 'srv-1',
  name: 'Alpha',
  region: 'cn-east',
  version: '1.0.0',
  status: 'online',
  players: 100,
  capacity: 200,
  load: 0.5,
}

const chars: Character[] = [
  { account_id: 'a1', server_id: 'srv-1', character_id: 'c1', name: 'Hero', level: 30, class_id: 'warrior' },
]

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/servers/srv-1']}>
      <Routes>
        <Route path="/servers/:id" element={<ServerDetail />} />
      </Routes>
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  getServer.mockResolvedValue(srv)
  listCharactersByServer.mockResolvedValue(chars)
  getServerTags.mockResolvedValue({ server_id: 'srv-1', tags: [] })
})

afterEach(() => cleanup())

describe('ServerDetail 详情页数据加载', () => {
  it('getServer 失败时渲染错误面板而不是停在加载态', async () => {
    getServer.mockRejectedValue(new Error('boom'))

    renderPage()

    // 错误面板：标题 + 说明 + 重试/返回按钮都要在，且不是永久转圈。
    expect(await screen.findByText(t('loadFailed'))).toBeTruthy()
    expect(screen.getByText(t('loadFailedHint'))).toBeTruthy()
    // antd 给两字中文按钮插空格（「重 试」），可访问名按可容忍空白的正则匹配。
    const retryName = new RegExp([...t('retry')].join('\\s*'))
    expect(screen.getByRole('button', { name: retryName })).toBeTruthy()
    expect(screen.getByRole('button', { name: t('backToServers') })).toBeTruthy()
    expect(getServer).toHaveBeenCalledWith('srv-1')
  })

  it('listCharactersByServer 失败时服务器信息照常渲染、角色列表降级为空', async () => {
    listCharactersByServer.mockRejectedValue(new Error('boom'))

    renderPage()

    expect(await screen.findByText('Alpha')).toBeTruthy()
    // 角色列表数量降级为 0，页面不再整体卡在 Spin。
    expect(await screen.findByText(`${t('characterList')} (0)`)).toBeTruthy()
  })

  it('正常路径渲染服务器信息与角色列表', async () => {
    renderPage()

    expect(await screen.findByText('Alpha')).toBeTruthy()
    expect(await screen.findByText('Hero')).toBeTruthy()
    expect(screen.getByText(`${t('characterList')} (1)`)).toBeTruthy()
  })
})
