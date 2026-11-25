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
  setServerWeight: vi.fn(),
  serverMaintenance: vi.fn(),
  serverDrain: vi.fn(),
  serverEnable: vi.fn(),
  serverDisable: vi.fn(),
}))

const getServer = vi.mocked(client.getServer)
const listCharactersByServer = vi.mocked(client.listCharactersByServer)
const getServerTags = vi.mocked(client.getServerTags)
const setServerWeight = vi.mocked(client.setServerWeight)

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

    // Error panel: title + hint + retry/back buttons must all be present, and
    // it must not spin forever.
    expect(await screen.findByText(t('loadFailed'))).toBeTruthy()
    expect(screen.getByText(t('loadFailedHint'))).toBeTruthy()
    // antd inserts a space between the two characters of a CJK button label,
    // so the accessible name is matched with a whitespace-tolerant regex.
    const retryName = new RegExp([...t('retry')].join('\\s*'))
    expect(screen.getByRole('button', { name: retryName })).toBeTruthy()
    expect(screen.getByRole('button', { name: t('backToServers') })).toBeTruthy()
    expect(getServer).toHaveBeenCalledWith('srv-1')
  })

  it('listCharactersByServer 失败时服务器信息照常渲染、角色列表降级为空', async () => {
    listCharactersByServer.mockRejectedValue(new Error('boom'))

    renderPage()

    expect(await screen.findByText('Alpha')).toBeTruthy()
    // Character list count degrades to 0; the page no longer gets stuck in a Spin.
    expect(await screen.findByText(`${t('characterList')} (0)`)).toBeTruthy()
  })

  it('正常路径渲染服务器信息与角色列表', async () => {
    renderPage()

    expect(await screen.findByText('Alpha')).toBeTruthy()
    expect(await screen.findByText('Hero')).toBeTruthy()
    expect(screen.getByText(`${t('characterList')} (1)`)).toBeTruthy()
  })
})

describe('ServerDetail 路由权重', () => {
  // The weight Tag is the only ant-tag on the page with unique content (the
  // tier dropdown also contains the word "neutral"), so narrow to Tag elements
  // by class.
  const weightTag = async () => {
    const tags = await screen.findAllByText(t('weightNeutral'))
    return tags.find((el) => el.className.includes('ant-tag'))
  }

  it('权重为 0 时显示「中性」', async () => {
    renderPage()

    expect(await weightTag()).toBeTruthy()
  })

  it('权重为 200 时显示数值', async () => {
    getServer.mockResolvedValue({ ...srv, weight: 200 })
    renderPage()

    const tags = await screen.findAllByText('200')
    const tag = tags.find((el) => el.className.includes('ant-tag'))
    expect(tag).toBeTruthy()
  })

  it('设置权重后调用 API 并刷新页面', async () => {
    setServerWeight.mockResolvedValue({ server_id: 'srv-1', weight: 200 })
    renderPage()

    expect(await weightTag()).toBeTruthy()

    const input = screen.getByDisplayValue('0') as HTMLInputElement
    // antd InputNumber renders a nested input; set the value via the native setter.
    const nativeSetter = Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype, 'value',
    )?.set
    nativeSetter?.call(input, '200')
    input.dispatchEvent(new Event('input', { bubbles: true }))

    const saveName = new RegExp([...t('weightSave')].join('\\s*'))
    await screen.findByRole('button', { name: saveName })
    screen.getByRole('button', { name: saveName }).click()

    expect(setServerWeight).toHaveBeenCalledWith('srv-1', 200)
  })
})
