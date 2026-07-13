import { QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { createMemoryRouter, MemoryRouter, Route, RouterProvider, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import type { ConnectWorkspaceOptions, Workspace } from 'src/api/workspaces'
import type { WorkspaceChatMessage, WorkspaceChatStreamHandlers } from 'src/api/workspace-chat'
import type { SandboxFileEntry } from 'src/api/filesystem'
import type { SandboxMetric } from 'src/api/sandboxes'
import { queryClient } from 'src/lib/query-client'
import routes from 'src/router'
import WorkspaceDetail from './index'

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
Object.defineProperty(Range.prototype, 'getClientRects', {
  configurable: true,
  value: () => [],
})

function apiResponse<T>(data: T) {
  return Promise.resolve({ data })
}

const connectWorkspace = vi.fn<(
  workspaceID: string,
  options?: ConnectWorkspaceOptions,
) => Promise<{ data: Workspace }>>(() => apiResponse({
  id: 'wks_123',
  name: 'VisionTube',
  repo_full_name: 'qiniu/vision-tube',
  region: 'us-south-1',
  sandbox_id: 'sbox_456',
  template_id: 'tmpl_react',
  state: 'running',
  endpoint: 'sandbox.example.com',
  workspace_path: '/workspace/qiniu__vision-tube',
  ide_url: '/api/v1/sandboxes/sbox_456/ide/',
}))

const heartbeatWorkspace = vi.fn<(workspaceID: string) => Promise<{ data: { ok: boolean, timeout_seconds: number } }>>(() => apiResponse({
  ok: true,
  timeout_seconds: 86400,
}))

const pauseWorkspaceSandbox = vi.fn<(
  workspaceID: string,
  options?: { keepalive?: boolean },
) => Promise<{ data: Workspace } | Response>>(() => apiResponse({
  id: 'wks_123',
  name: 'VisionTube',
  repo_full_name: 'qiniu/vision-tube',
  region: 'us-south-1',
  sandbox_id: 'sbox_456',
  template_id: 'tmpl_react',
  state: 'paused',
  endpoint: 'sandbox.example.com',
  workspace_path: '/workspace/qiniu__vision-tube',
  ide_url: '/api/v1/sandboxes/sbox_456/ide/',
}))

const fetchWorkspaces = vi.fn(() => apiResponse({
  workspaces: [
    {
      id: 'wks_123',
      name: 'VisionTube',
      repo_full_name: 'qiniu/vision-tube',
      region: 'us-south-1',
      sandbox_id: 'sbox_456',
      template_id: 'tmpl_react',
      state: 'running',
      endpoint: 'sandbox.example.com',
      workspace_path: '/workspace/qiniu__vision-tube',
      ide_url: '/api/v1/sandboxes/sbox_456/ide/',
    },
  ],
}))

const fetchSandboxFiles = vi.fn((
  sandboxID: string,
  path: string,
) => apiResponse({
  sandbox_id: sandboxID,
  entries: [
    {
      name: 'README.md',
      type: 'file',
      path: `${path}/README.md`,
      size: 42,
      owner: 'user',
      group: 'user',
      permissions: '-rw-r--r--',
    },
    {
      name: 'src',
      type: 'dir',
      path: `${path}/src`,
      size: 0,
      owner: 'user',
      group: 'user',
      permissions: 'drwxr-xr-x',
    },
  ] satisfies SandboxFileEntry[],
}))

const fetchSandboxFileContent = vi.fn<(
  sandboxID: string,
  path: string,
) => Promise<{ data: Blob, headers: { 'content-type': string } }>>(() => Promise.resolve({
  data: new Blob(['# Readme'], { type: 'text/markdown' }),
  headers: { 'content-type': 'text/markdown' },
}))

const fetchSandboxMetrics = vi.fn((
  sandboxID: string,
  options?: { start?: number, end?: number },
) => apiResponse({
  sandbox_id: sandboxID,
  metrics: [
    {
      timestamp: '2026-06-01T10:00:00Z',
      timestamp_unix: 1780308000,
      cpu_count: 2,
      cpu_used_pct: 16.5,
      mem_total: 4 * 1024 * 1024 * 1024,
      mem_used: 1536 * 1024 * 1024,
      disk_total: 20 * 1024 * 1024 * 1024,
      disk_used: 5 * 1024 * 1024 * 1024,
    },
  ] satisfies SandboxMetric[],
  options,
}))

const fetchQiniuCredentialStatus = vi.fn(() => apiResponse({
  configured: true,
  key_hint: '...box',
  maas_configured: true,
  access_key_configured: true,
  secret_key_configured: true,
}))

const fetchWorkspaceChatMessages = vi.fn<(
  workspaceID: string,
) => Promise<{ data: { messages: WorkspaceChatMessage[] } }>>(() => apiResponse({
  messages: [
    {
      id: 'msg_1',
      created_at: '2026-07-07T10:00:00Z',
      role: 'assistant',
      content: 'I can inspect this workspace from the sandbox.',
      provider: 'codex',
    },
  ] satisfies WorkspaceChatMessage[],
}))

const streamWorkspaceChatMessage = vi.fn(async (
  workspaceID: string,
  message: string,
  handlers: WorkspaceChatStreamHandlers = {},
) => {
  handlers.onUserMessage?.({
    id: 'msg_2',
    created_at: '2026-07-07T10:01:00Z',
    role: 'user',
    content: message,
  } satisfies WorkspaceChatMessage)
  handlers.onStatus?.('Running AI Chat in the sandbox...')
  handlers.onAssistantDelta?.('Sandbox answer ')
  handlers.onAssistantDelta?.(`for ${workspaceID}`)
  handlers.onAssistantMessage?.({
    id: 'msg_3',
    created_at: '2026-07-07T10:01:01Z',
    role: 'assistant',
    content: `Sandbox answer for ${workspaceID}`,
    provider: 'codex',
  } satisfies WorkspaceChatMessage)
})

vi.mock('src/api/workspaces', () => ({
  workspaces: () => fetchWorkspaces(),
  connectWorkspace: (workspaceID: string, options?: { recreate?: boolean }) => connectWorkspace(workspaceID, options),
  heartbeatWorkspace: (workspaceID: string) => heartbeatWorkspace(workspaceID),
  pauseWorkspaceSandbox: (workspaceID: string, options?: { keepalive?: boolean }) => pauseWorkspaceSandbox(workspaceID, options),
}))

vi.mock('src/api/qiniu', () => ({
  qiniuCredentialStatus: () => fetchQiniuCredentialStatus(),
}))

vi.mock('src/api/workspace-chat', () => ({
  workspaceChatMessages: (workspaceID: string) => fetchWorkspaceChatMessages(workspaceID),
  streamWorkspaceChatMessage: (
    workspaceID: string,
    message: string,
    handlers?: WorkspaceChatStreamHandlers,
  ) => streamWorkspaceChatMessage(workspaceID, message, handlers),
}))

vi.mock('src/api/filesystem', () => ({
  sandboxFiles: (sandboxID: string, path: string) => fetchSandboxFiles(sandboxID, path),
  sandboxFileContent: (sandboxID: string, path: string) => fetchSandboxFileContent(sandboxID, path),
  sandboxFilePreviewURL: (sandboxID: string, path: string) => (
    `/api/v1/sandboxes/${encodeURIComponent(sandboxID)}/preview${path.split('/').map((segment) => encodeURIComponent(segment)).join('/')}`
  ),
  workspaceFilePreviewURL: (workspaceID: string, path: string) => (
    `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/preview${path.split('/').map((segment) => encodeURIComponent(segment)).join('/')}`
  ),
}))

vi.mock('src/api/sandboxes', () => ({
  sandboxMetrics: (sandboxID: string, options?: { start?: number, end?: number }) => fetchSandboxMetrics(sandboxID, options),
}))

vi.mock('src/components/TerminalPanel', () => ({
  default: ({ sandboxID, workspacePath, active }: { sandboxID: string, workspacePath?: string, active?: boolean }) => (
    <div data-testid="terminal-panel">
      Terminal for {sandboxID} at {workspacePath} active {String(active)}
    </div>
  ),
}))

vi.mock('src/api/auth', () => ({
  currentUser: () => apiResponse({
    account_id: 'acct_1',
    provider: 'github',
    subject: '123',
    login: 'miclle',
    name: 'Miclle Zheng',
    avatar_url: '',
  }),
}))

async function waitFor(assertion: () => void) {
  const startedAt = Date.now()
  let lastError: unknown
  while (Date.now() - startedAt < 3000) {
    try {
      assertion()
      return
    } catch (error) {
      lastError = error
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 10))
      })
    }
  }
  throw lastError
}

beforeEach(() => {
  document.documentElement.classList.remove('dark')
  queryClient.clear()
  fetchWorkspaces.mockReset()
  fetchWorkspaces.mockImplementation(() => apiResponse({
    workspaces: [
      {
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_456',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '/api/v1/sandboxes/sbox_456/ide/',
      },
    ],
  }))
  connectWorkspace.mockReset()
  connectWorkspace.mockImplementation(() => apiResponse({
    id: 'wks_123',
    name: 'VisionTube',
    repo_full_name: 'qiniu/vision-tube',
    region: 'us-south-1',
    sandbox_id: 'sbox_456',
    template_id: 'tmpl_react',
    state: 'running',
    endpoint: 'sandbox.example.com',
    workspace_path: '/workspace/qiniu__vision-tube',
    ide_url: '/api/v1/sandboxes/sbox_456/ide/',
  }))
  heartbeatWorkspace.mockReset()
  heartbeatWorkspace.mockImplementation(() => apiResponse({
    ok: true,
    timeout_seconds: 86400,
  }))
  pauseWorkspaceSandbox.mockReset()
  pauseWorkspaceSandbox.mockImplementation(() => apiResponse({
    id: 'wks_123',
    name: 'VisionTube',
    repo_full_name: 'qiniu/vision-tube',
    region: 'us-south-1',
    sandbox_id: 'sbox_456',
    template_id: 'tmpl_react',
    state: 'paused',
    endpoint: 'sandbox.example.com',
    workspace_path: '/workspace/qiniu__vision-tube',
    ide_url: '/api/v1/sandboxes/sbox_456/ide/',
  }))
  fetchSandboxFiles.mockClear()
  fetchSandboxFileContent.mockClear()
  fetchSandboxMetrics.mockClear()
  fetchQiniuCredentialStatus.mockReset()
  fetchQiniuCredentialStatus.mockImplementation(() => apiResponse({
    configured: true,
    key_hint: '...box',
    maas_configured: true,
    access_key_configured: true,
    secret_key_configured: true,
  }))
  fetchWorkspaceChatMessages.mockReset()
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({
    messages: [
      {
        id: 'msg_1',
        created_at: '2026-07-07T10:00:00Z',
        role: 'assistant',
        content: 'I can inspect this workspace from the sandbox.',
        provider: 'codex',
      },
    ] satisfies WorkspaceChatMessage[],
  }))
  streamWorkspaceChatMessage.mockReset()
  streamWorkspaceChatMessage.mockImplementation(async (workspaceID, message, handlers = {}) => {
    handlers.onUserMessage?.({
      id: 'msg_2',
      created_at: '2026-07-07T10:01:00Z',
      role: 'user',
      content: message,
    } satisfies WorkspaceChatMessage)
    handlers.onStatus?.('Running AI Chat in the sandbox...')
    handlers.onAssistantDelta?.('Sandbox answer ')
    handlers.onAssistantDelta?.(`for ${workspaceID}`)
    handlers.onAssistantMessage?.({
      id: 'msg_3',
      created_at: '2026-07-07T10:01:01Z',
      role: 'assistant',
      content: `Sandbox answer for ${workspaceID}`,
      provider: 'codex',
    } satisfies WorkspaceChatMessage)
  })
  window.URL.createObjectURL = vi.fn(() => 'blob:download')
  window.URL.revokeObjectURL = vi.fn()
  HTMLAnchorElement.prototype.click = vi.fn()
  document.body.innerHTML = ''
})

afterEach(() => {
  vi.restoreAllMocks()
})

test('renders a workspace workbench with assistant, files, and terminal panels', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })

  expect(container.textContent).toContain('AI Chat')
  expect(container.querySelector('textarea[aria-label="Message AI Chat"]')).toBeTruthy()
  expect(container.textContent).toContain('Workspace context attached')
  expect(container.querySelector('button[aria-label="Attach context"]')).toBeNull()
  expect(container.textContent).toContain('I can inspect this workspace from the sandbox.')
  expect(container.textContent).not.toContain('AI Chat · codex')
  expect(container.textContent).not.toContain('You')
  expect(container.textContent).toContain('Files')
  expect(container.textContent).toContain('Monitor')
  expect(container.textContent).toContain('Terminal')
  expect(container.textContent).not.toContain('Terminal for sbox_456 at /workspace/qiniu__vision-tube')
  expect(container.textContent).toContain('..')
  expect(container.textContent).not.toContain('Files Tree')
  expect(container.textContent).not.toContain('Size')
  expect(container.textContent).not.toContain('42 B')
  expect(container.querySelector('[role="separator"][aria-label="Resize AI Chat sidebar"]')).toBeTruthy()
  expect(container.querySelector('[role="separator"][aria-label="Resize file browser panes"]')).toBeTruthy()
  expect(container.querySelector('[role="separator"][aria-label="Resize AI Chat sidebar"]')?.firstElementChild?.className).toBe(
    container.querySelector('[role="separator"][aria-label="Resize file browser panes"]')?.firstElementChild?.className,
  )
  expect(container.querySelector('[role="status"][aria-label="File browser status"]')).toBeTruthy()
  expect(container.innerHTML.indexOf('Select a file to preview it here.')).toBeLessThan(container.innerHTML.indexOf('aria-label="Filesystem path"'))
  expect(container.textContent).toContain('/workspace/qiniu__vision-tube')
  expect(container.textContent).not.toContain('running')
  expect(connectWorkspace).toHaveBeenCalledWith('wks_123', undefined)
  expect(container.textContent).not.toContain('Workspace metadata')
  expect(container.textContent).not.toContain('metadata.json')
  expect(container.textContent).not.toContain('us-south-1 · tmpl_react')
  expect(container.textContent).not.toContain('Region')
  expect(container.textContent).not.toContain('sandbox.example.com')
  expect(container.textContent).not.toContain('Runtime surface')
  expect(Array.from(container.querySelectorAll('a')).some((link) => link.textContent === 'Open IDE')).toBe(true)
  expect(container.querySelector('a[href="https://github.com/qiniu/vision-tube"]')).toBeTruthy()
  expect(container.querySelector('a[href="/api/v1/sandboxes/sbox_456/ide/"]')).toBeTruthy()
  expect(container.querySelector('iframe[title="Code server IDE"]')).toBeNull()
  expect(fetchSandboxFiles).toHaveBeenCalledWith('sbox_456', '/workspace/qiniu__vision-tube')
  expect(fetchSandboxMetrics).not.toHaveBeenCalled()

  const srcDirectory = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'src'
  ))
  expect(srcDirectory).toBeTruthy()

  await act(async () => {
    srcDirectory?.click()
  })

  await waitFor(() => {
    expect(fetchSandboxFiles).toHaveBeenCalledWith('sbox_456', '/workspace/qiniu__vision-tube/src')
  })

  const parentButton = container.querySelector('button[aria-label="Open parent directory"]')
  expect(parentButton).toBeTruthy()

  await act(async () => {
    parentButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  await waitFor(() => {
    expect(fetchSandboxFiles).toHaveBeenCalledWith('sbox_456', '/workspace')
  })

  const terminalTab = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Terminal'
  ))
  expect(terminalTab).toBeTruthy()

  await act(async () => {
    terminalTab?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Terminal for sbox_456 at /workspace/qiniu__vision-tube active true')
  })
  expect(container.querySelectorAll('[data-testid="terminal-panel"]')).toHaveLength(1)

  const fileFetchCountAfterTerminalOpen = fetchSandboxFiles.mock.calls.length
  const filesTab = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Files'
  ))
  expect(filesTab).toBeTruthy()

  await act(async () => {
    filesTab?.click()
  })

  expect(fetchSandboxFiles).toHaveBeenCalledTimes(fileFetchCountAfterTerminalOpen)
  expect(container.textContent).toContain('Terminal for sbox_456 at /workspace/qiniu__vision-tube active false')
  expect(container.querySelectorAll('[data-testid="terminal-panel"]')).toHaveLength(1)

  await act(async () => {
    terminalTab?.click()
  })

  expect(container.textContent).toContain('Terminal for sbox_456 at /workspace/qiniu__vision-tube active true')
  expect(fetchSandboxFiles).toHaveBeenCalledTimes(fileFetchCountAfterTerminalOpen)

  const newTerminalButton = container.querySelector('button[aria-label="Open new terminal"]')
  expect(newTerminalButton).toBeTruthy()

  await act(async () => {
    newTerminalButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Terminal 2')
    expect(container.querySelectorAll('[data-testid="terminal-panel"]')).toHaveLength(2)
  })
  expect(fetchSandboxFiles).toHaveBeenCalledTimes(fileFetchCountAfterTerminalOpen)

  const closeTerminal2 = container.querySelector('[aria-label="Close Terminal 2"]')
  expect(closeTerminal2).toBeTruthy()

  await act(async () => {
    closeTerminal2?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  await waitFor(() => {
    expect(container.textContent).not.toContain('Terminal 2')
    expect(container.querySelectorAll('[data-testid="terminal-panel"]')).toHaveLength(1)
  })
  expect(container.textContent).toContain('Terminal for sbox_456 at /workspace/qiniu__vision-tube active true')
  expect(fetchSandboxFiles).toHaveBeenCalledTimes(fileFetchCountAfterTerminalOpen)
})

test('renders AI Chat messages as Markdown', async () => {
  fetchWorkspaceChatMessages.mockImplementationOnce(() => apiResponse({
    messages: [
      {
        id: 'msg_user',
        created_at: '2026-07-07T09:59:00Z',
        role: 'user',
        content: '在浏览器中打开这个 /home/user/snake.html 文件',
      },
      {
        id: 'msg_markdown',
        created_at: '2026-07-07T10:00:00Z',
        role: 'assistant',
        content: [
          'Done. Created `/home/user/snake.html`.',
          '',
          '**How to play:**',
          '- Open it in a browser',
          '',
          '| Error | Real cause | Fixable in snake.html? |',
          '| --- | --- | --- |',
          '| CSP block | Preview proxy sets connect-src none | No |',
        ].join('\n'),
        provider: 'claude',
      },
    ] satisfies WorkspaceChatMessage[],
  }))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Done. Created /home/user/snake.html.')
  })

  expect(container.textContent).not.toContain('`/home/user/snake.html`')
  expect(container.textContent).not.toContain('**How to play:**')
  expect(container.textContent).not.toContain('| Error | Real cause | Fixable in snake.html? |')
  expect(container.querySelector('code')?.textContent).toBe('/home/user/snake.html')
  expect(container.querySelector('strong')?.textContent).toBe('How to play:')
  expect(container.querySelector('li')?.textContent).toBe('Open it in a browser')
  expect(container.querySelector('table')).toBeTruthy()
  expect(container.querySelectorAll('th')).toHaveLength(3)
  expect(container.querySelector('th')?.textContent).toBe('Error')
  expect(container.querySelector('td')?.textContent).toBe('CSP block')

  const userBubble = Array.from(container.querySelectorAll('[data-chat-role="user"]')).find((element) => (
    element.textContent?.includes('在浏览器中打开这个 /home/user/snake.html 文件')
  ))
  expect(userBubble?.parentElement?.className).toContain('justify-end')
  expect(userBubble?.className).toContain('w-fit')
  expect(userBubble?.className).toContain('max-w-[calc(100%-2rem)]')
})

test('resizes workspace columns with a 300px minimum', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })

  const assistantResizeHandle = container.querySelector('[role="separator"][aria-label="Resize AI Chat sidebar"]')
  expect(assistantResizeHandle?.getAttribute('aria-valuemin')).toBe('300')
  expect(assistantResizeHandle?.hasAttribute('aria-valuemax')).toBe(false)
  expect(assistantResizeHandle?.getAttribute('aria-valuenow')).toBe('300')
  expect(assistantResizeHandle?.parentElement?.style.getPropertyValue('--assistant-sidebar-width')).toBe('50%')
  expect(assistantResizeHandle?.parentElement?.className).toContain('minmax(300px,1fr)')

  await act(async () => {
    assistantResizeHandle?.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }))
    assistantResizeHandle?.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }))
  })

  expect(assistantResizeHandle?.getAttribute('aria-valuenow')).toBe('300')

  await act(async () => {
    for (let index = 0; index < 10; index += 1) {
      assistantResizeHandle?.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    }
  })

  expect(assistantResizeHandle?.getAttribute('aria-valuenow')).toBe('540')
})

test('keeps active workspace sandboxes alive and pauses them when the page closes', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(heartbeatWorkspace).toHaveBeenCalledWith('wks_123')
  })

  await act(async () => {
    window.dispatchEvent(new Event('pagehide'))
  })

  expect(pauseWorkspaceSandbox).toHaveBeenCalledWith('wks_123', { keepalive: true })
})

test('keeps workspace sandboxes running across browser tab visibility changes', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(heartbeatWorkspace).toHaveBeenCalledWith('wks_123')
  })
  pauseWorkspaceSandbox.mockClear()
  heartbeatWorkspace.mockClear()

  const visibilitySpy = vi.spyOn(document, 'visibilityState', 'get')

  await act(async () => {
    visibilitySpy.mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
  })

  expect(pauseWorkspaceSandbox).not.toHaveBeenCalled()

  await act(async () => {
    visibilitySpy.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
  })

  expect(heartbeatWorkspace).toHaveBeenCalledWith('wks_123')
  visibilitySpy.mockRestore()
  root.unmount()
})

test('pauses active workspace sandboxes when leaving the detail route', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(heartbeatWorkspace).toHaveBeenCalledWith('wks_123')
  })
  pauseWorkspaceSandbox.mockClear()

  await act(async () => {
    root.unmount()
  })

  expect(pauseWorkspaceSandbox).toHaveBeenCalledWith('wks_123', undefined)
})

test('sends AI Chat messages through the workspace chat API', async () => {
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({ messages: [] }))
  let streamHandlers: WorkspaceChatStreamHandlers | undefined
  let resolveStream = () => {}
  streamWorkspaceChatMessage.mockImplementationOnce(async (_, __, handlers = {}) => {
    streamHandlers = handlers
    await new Promise<void>((resolve) => {
      resolveStream = resolve
    })
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Ready to work in VisionTube')
  })

  const textarea = container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null
  expect(textarea).toBeTruthy()

  await act(async () => {
    if (textarea) {
      const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set
      valueSetter?.call(textarea, 'List the project files')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  const sendButton = container.querySelector('button[aria-label="Send message"]') as HTMLButtonElement | null
  expect(sendButton?.disabled).toBe(false)

  await act(async () => {
    sendButton?.click()
  })

  expect(streamWorkspaceChatMessage).toHaveBeenCalledWith('wks_123', 'List the project files', expect.any(Object))
  expect(streamHandlers?.signal).toBeInstanceOf(AbortSignal)
  expect(container.textContent).toContain('List the project files')

  await act(async () => {
    streamHandlers?.onStatus?.('Running AI Chat in the sandbox...')
    streamHandlers?.onAssistantDelta?.('Sandbox answer ')
    streamHandlers?.onAssistantDelta?.('for wks_123')
    streamHandlers?.onUserMessage?.({
      id: 'msg_2',
      created_at: '2026-07-07T10:01:00Z',
      role: 'user',
      content: 'List the project files',
    })
    streamHandlers?.onAssistantMessage?.({
      id: 'msg_3',
      created_at: '2026-07-07T10:01:01Z',
      role: 'assistant',
      content: 'Sandbox answer for wks_123',
      provider: 'codex',
    })
    resolveStream()
  })

  await waitFor(() => {
    expect(container.textContent).not.toContain('Execution')
    expect(container.textContent).not.toContain('Prepared workspace context.')
    expect(container.textContent).not.toContain('Ran AI Chat in the sandbox with codex.')
    expect(container.textContent).toContain('Sandbox answer for wks_123')
  })
})

test('aborts an active AI Chat stream on unmount', async () => {
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({ messages: [] }))
  let streamHandlers: WorkspaceChatStreamHandlers | undefined
  streamWorkspaceChatMessage.mockImplementationOnce(async (_, __, handlers = {}) => {
    streamHandlers = handlers
    await new Promise<void>(() => {})
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Ready to work in VisionTube')
  })

  const textarea = container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null
  const sendButton = container.querySelector('button[aria-label="Send message"]') as HTMLButtonElement | null
  const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set

  await act(async () => {
    if (textarea) {
      valueSetter?.call(textarea, 'Keep working')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  await act(async () => {
    sendButton?.click()
  })

  expect(streamHandlers?.signal?.aborted).toBe(false)

  await act(async () => {
    root.unmount()
  })

  expect(streamHandlers?.signal?.aborted).toBe(true)
})

test('removes optimistic AI Chat messages when the stream fails before persistence', async () => {
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({ messages: [] }))
  streamWorkspaceChatMessage.mockRejectedValueOnce(new Error('Failed to save chat message.'))
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Ready to work in VisionTube')
  })

  const textarea = container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null
  const sendButton = container.querySelector('button[aria-label="Send message"]') as HTMLButtonElement | null
  const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set

  await act(async () => {
    if (textarea) {
      valueSetter?.call(textarea, 'Will fail')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  await act(async () => {
    sendButton?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Failed to save chat message.')
    expect(container.textContent).not.toContain('Will fail')
  })
})

test('keeps transient AI Chat runtime errors visible in the chat panel', async () => {
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({ messages: [] }))
  streamWorkspaceChatMessage.mockImplementation(async (_, message, handlers = {}) => {
    handlers.onUserMessage?.({
      id: 'temp-user-1',
      created_at: '2026-07-07T10:01:00Z',
      role: 'user',
      content: message,
    } satisfies WorkspaceChatMessage)
    handlers.onAssistantMessage?.({
      id: 'temp-assistant-1',
      created_at: '2026-07-07T10:01:01Z',
      role: 'assistant',
      content: 'AI Chat failed before the sandbox command completed: sandbox timed out',
      provider: 'codex',
      exit_code: -1,
    } satisfies WorkspaceChatMessage)
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Ready to work in VisionTube')
  })

  const textarea = container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null
  const sendButton = container.querySelector('button[aria-label="Send message"]') as HTMLButtonElement | null
  const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set

  await act(async () => {
    if (textarea) {
      valueSetter?.call(textarea, 'Try again')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  await act(async () => {
    sendButton?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Try again')
    expect(container.textContent).toContain('sandbox timed out')
  })
})

test('sends AI Chat messages with Enter and keeps Shift Enter for multiline editing', async () => {
  fetchWorkspaceChatMessages.mockImplementation(() => apiResponse({ messages: [] }))
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Ready to work in VisionTube')
  })

  const textarea = container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null
  expect(textarea).toBeTruthy()
  const valueSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set

  await act(async () => {
    if (textarea) {
      valueSetter?.call(textarea, 'Write a plan')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  await act(async () => {
    textarea?.dispatchEvent(new KeyboardEvent('keydown', {
      bubbles: true,
      cancelable: true,
      key: 'Enter',
    }))
  })

  expect(streamWorkspaceChatMessage).toHaveBeenCalledWith('wks_123', 'Write a plan', expect.any(Object))

  await waitFor(() => {
    expect((textarea as HTMLTextAreaElement).value).toBe('')
  })
  streamWorkspaceChatMessage.mockClear()

  await act(async () => {
    if (textarea) {
      valueSetter?.call(textarea, 'Line one')
      textarea.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  const shiftEnterEvent = new KeyboardEvent('keydown', {
    bubbles: true,
    cancelable: true,
    key: 'Enter',
    shiftKey: true,
  })

  await act(async () => {
    textarea?.dispatchEvent(shiftEnterEvent)
  })

  expect(shiftEnterEvent.defaultPrevented).toBe(false)
  expect(streamWorkspaceChatMessage).not.toHaveBeenCalled()
})

test('reminds users to configure MAAS before using AI Chat', async () => {
  fetchQiniuCredentialStatus.mockImplementation(() => apiResponse({
    configured: true,
    key_hint: '...box',
    maas_configured: false,
    access_key_configured: true,
    secret_key_configured: true,
  }))
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Qiniu MAAS API Key is not configured.')
  })

  expect(container.querySelector('a[href="/credentials"]')?.textContent).toContain('Configure credentials')
  expect((container.querySelector('textarea[aria-label="Message AI Chat"]') as HTMLTextAreaElement | null)?.disabled).toBe(true)
})

test('loads sandbox metrics from the monitor tab', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })
  expect(fetchSandboxMetrics).not.toHaveBeenCalled()

  const monitorTab = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Monitor'
  ))
  expect(monitorTab).toBeTruthy()

  await act(async () => {
    monitorTab?.click()
  })

  await waitFor(() => {
    expect(fetchSandboxMetrics).toHaveBeenCalledWith('sbox_456', expect.objectContaining({
      start: expect.any(Number),
      end: expect.any(Number),
    }))
    expect(container.textContent).toContain('Resource trend')
    expect(container.textContent).toContain('16.5%')
  })

  expect(container.textContent).toContain('Runtime')
  expect(container.textContent).toContain('CPU')
  expect(container.textContent).toContain('Memory')
  expect(container.textContent).toContain('Disk')
  expect(container.textContent).toContain('1.50 GiB / 4.00 GiB')
})

test('falls back to the sandbox home directory when the workspace path is missing', async () => {
  fetchSandboxFiles.mockImplementation((sandboxID: string, path: string) => {
    if (path === '/workspace/qiniu__vision-tube') {
      return Promise.reject({
        message: 'Request failed with status code 404',
        response: {
          status: 404,
          data: { error: 'no such file or directory' },
        },
      })
    }

    return apiResponse({
      sandbox_id: sandboxID,
      entries: [
        {
          name: '.profile',
          type: 'file',
          path: `${path}/.profile`,
          size: 12,
          owner: 'user',
          group: 'user',
          permissions: '-rw-r--r--',
        },
      ] satisfies SandboxFileEntry[],
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(fetchSandboxFiles).toHaveBeenCalledWith('sbox_456', '/home/user')
    expect(container.textContent).toContain('.profile')
  })

  expect(fetchSandboxFiles).toHaveBeenCalledWith('sbox_456', '/workspace/qiniu__vision-tube')
  expect(container.textContent).toContain('/home/user')
  expect(container.textContent).not.toContain('Failed to load files.')
})

test('renders previewable file content in a read-only CodeMirror editor', async () => {
  document.documentElement.classList.add('dark')
  fetchSandboxFiles.mockImplementation((sandboxID: string, path: string) => apiResponse({
    sandbox_id: sandboxID,
    entries: [
      {
        name: 'README.md',
        type: 'file',
        path: `${path}/README.md`,
        size: 42,
        owner: 'user',
        group: 'user',
        permissions: '-rw-r--r--',
      },
    ] satisfies SandboxFileEntry[],
  }))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('README.md')
  })

  const readmeButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'README.md'
  ))

  await act(async () => {
    readmeButton?.click()
  })

  await waitFor(() => {
    expect(container.querySelector('.cm-editor')).toBeTruthy()
  })

  expect(container.querySelector('.cm-content')?.getAttribute('contenteditable')).toBe('true')
  expect(container.querySelector('.cm-content')?.getAttribute('inputmode')).toBe('none')
  expect(container.querySelector('.cm-content')?.getAttribute('aria-label')).toBe('File content preview')
  expect(container.querySelector('.cm-content')?.textContent).toContain('# Readme')
  expect(container.querySelector('.cm-theme-dark')).toBeTruthy()
  expect(container.querySelector('pre')).toBeNull()

  document.documentElement.classList.remove('dark')
  await waitFor(() => {
    expect(container.querySelector('.cm-theme-light')).toBeTruthy()
  })
})

test.each(['cjs', 'cts', 'mts'])('previews .%s modules in CodeMirror', async (extension) => {
  const filename = `module.${extension}`
  fetchSandboxFiles.mockImplementation((sandboxID: string, path: string) => apiResponse({
    sandbox_id: sandboxID,
    entries: [
      {
        name: filename,
        type: 'file',
        path: `${path}/${filename}`,
        size: 42,
        owner: 'user',
        group: 'user',
        permissions: '-rw-r--r--',
      },
    ] satisfies SandboxFileEntry[],
  }))
  fetchSandboxFileContent.mockImplementation(() => Promise.resolve({
    data: new Blob(['const value = 1'], { type: 'application/octet-stream' }),
    headers: { 'content-type': 'application/octet-stream' },
  }))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain(filename)
  })

  const fileButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === filename
  ))
  await act(async () => {
    fileButton?.click()
  })

  await waitFor(() => {
    expect(container.querySelector('.cm-editor')).toBeTruthy()
  })
  expect(container.querySelector('.cm-content')?.textContent).toContain('const value = 1')
})

test('downloads an unpreviewed large file from the selected sandbox path', async () => {
  fetchSandboxFiles.mockImplementation((sandboxID: string, path: string) => apiResponse({
    sandbox_id: sandboxID,
    entries: [
      {
        name: 'archive.zip',
        type: 'file',
        path: `${path}/archive.zip`,
        size: 2 * 1024 * 1024,
        owner: 'user',
        group: 'user',
        permissions: '-rw-r--r--',
      },
    ] satisfies SandboxFileEntry[],
  }))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('archive.zip')
  })

  const archiveButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'archive.zip'
  ))
  expect(archiveButton).toBeTruthy()

  await act(async () => {
    archiveButton?.click()
  })

  expect(container.textContent).toContain('Preview is unavailable for this file')
  expect(fetchSandboxFileContent).not.toHaveBeenCalled()

  const downloadButton = container.querySelector('button[aria-label="Download file"]')
  expect(downloadButton).toBeTruthy()
  expect(downloadButton?.hasAttribute('disabled')).toBe(false)

  await act(async () => {
    downloadButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  await waitFor(() => {
    expect(fetchSandboxFileContent).toHaveBeenCalledWith('sbox_456', '/workspace/qiniu__vision-tube/archive.zip')
  })
  expect(window.URL.createObjectURL).toHaveBeenCalled()
})

test('opens HTML files through the sandbox preview route', async () => {
  fetchSandboxFiles.mockImplementation((sandboxID: string, path: string) => apiResponse({
    sandbox_id: sandboxID,
    entries: [
      {
        name: 'snake.html',
        type: 'file',
        path: `${path}/snake.html`,
        size: 2048,
        owner: 'user',
        group: 'user',
        permissions: '-rw-r--r--',
      },
    ] satisfies SandboxFileEntry[],
  }))
  fetchSandboxFileContent.mockImplementation(() => Promise.resolve({
    data: new Blob(['<html><body>Snake</body></html>'], { type: 'text/html' }),
    headers: { 'content-type': 'text/html' },
  }))

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('snake.html')
  })

  const htmlButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'snake.html'
  ))
  expect(htmlButton).toBeTruthy()

  await act(async () => {
    htmlButton?.click()
  })

  await waitFor(() => {
    expect(fetchSandboxFileContent).toHaveBeenCalledWith('sbox_456', '/workspace/qiniu__vision-tube/snake.html')
  })

  const previewLink = container.querySelector('a[aria-label="Open HTML preview"]')
  expect(previewLink?.getAttribute('href')).toBe('/api/v1/workspaces/wks_123/preview/workspace/qiniu__vision-tube/snake.html')
  expect(previewLink?.getAttribute('target')).toBe('_blank')
})

test('opens workspace metadata in a settings drawer', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })
  expect(document.body.textContent).not.toContain('Workspace metadata')

  const settingsButton = container.querySelector('button[aria-label="Workspace settings"]')
  expect(settingsButton).toBeTruthy()

  await act(async () => {
    settingsButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  expect(document.body.textContent).toContain('Workspace metadata')
  expect(document.body.textContent).toContain('"id": "wks_123"')
  expect(document.body.textContent).toContain('Launch checklist')
  expect(document.body.textContent).toContain('Region')
  expect(document.body.textContent).toContain('Template')
  expect(document.body.textContent).toContain('Sandbox')
  expect(document.body.textContent).toContain('Endpoint')
  expect(document.body.textContent).toContain('sandbox.example.com')
})

test('workspace detail route does not render the main app sidebar', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  const router = createMemoryRouter(routes, {
    initialEntries: ['/workspaces/wks_123'],
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })

  expect(container.textContent).not.toContain('Qiniu Playground')
  expect(container.textContent).not.toContain('Codebases')
  expect(container.querySelector('aside')).toBeNull()
})

test('resets connection state when navigating between workspaces', async () => {
  fetchWorkspaces.mockImplementation(() => apiResponse({
    workspaces: [
      {
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_456',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '/api/v1/sandboxes/sbox_456/ide/',
      },
      {
        id: 'wks_789',
        name: 'DocsKit',
        repo_full_name: 'qiniu/docs-kit',
        region: 'us-south-1',
        sandbox_id: 'sbox_789',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox-2.example.com',
        workspace_path: '/workspace/qiniu__docs-kit',
        ide_url: '/api/v1/sandboxes/sbox_789/ide/',
      },
    ],
  }))
  connectWorkspace.mockImplementation((workspaceID) => {
    if (workspaceID === 'wks_789') {
      return apiResponse({
        id: 'wks_789',
        name: 'DocsKit',
        repo_full_name: 'qiniu/docs-kit',
        region: 'us-south-1',
        sandbox_id: 'sbox_789',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox-2.example.com',
        workspace_path: '/workspace/qiniu__docs-kit',
        ide_url: '/api/v1/sandboxes/sbox_789/ide/',
      })
    }
    return Promise.reject({
      response: {
        status: 500,
        data: { error: 'sandbox service unavailable' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  const router = createMemoryRouter([
    {
      path: '/workspaces/:workspaceId',
      element: <WorkspaceDetail />,
    },
  ], {
    initialEntries: ['/workspaces/wks_123'],
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Workspace connection failed')
  })

  await act(async () => {
    await router.navigate('/workspaces/wks_789')
  })

  await waitFor(() => {
    expect(container.querySelector('a[href="/api/v1/sandboxes/sbox_789/ide/"]')).toBeTruthy()
  })

  expect(container.textContent).toContain('DocsKit')
  expect(container.textContent).not.toContain('sandbox service unavailable')
  expect(connectWorkspace).toHaveBeenCalledWith('wks_789', undefined)
})

test('prompts to recreate when the workspace sandbox no longer exists', async () => {
  connectWorkspace.mockImplementation((workspaceID, options) => {
    void workspaceID
    if (options?.recreate) {
      return apiResponse({
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_new',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox-new.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '/api/v1/sandboxes/sbox_new/ide/',
      })
    }
    return Promise.reject({
      response: {
        status: 409,
        data: { error: 'workspace sandbox no longer exists' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(document.body.textContent).toContain('Sandbox unavailable')
  })

  expect(connectWorkspace).toHaveBeenCalledWith('wks_123', undefined)
  expect(container.querySelector('iframe[title="Code server IDE"]')).toBeNull()
  expect(container.textContent).toContain('Create a new sandbox to continue working in this workspace.')

  const dismissButton = Array.from(document.body.querySelectorAll('button')).find((button) => (
    button.textContent === 'Not now'
  ))
  expect(dismissButton).toBeTruthy()

  await act(async () => {
    dismissButton?.click()
  })

  expect(container.textContent).toContain('Sandbox unavailable')
  expect(container.textContent).toContain('Missing sandbox')
  expect(container.textContent).toContain('sbox_456')
  expect(container.textContent).toContain('Create a new sandbox to continue working in this workspace.')

  const createButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Create new sandbox'
  ))
  expect(createButton).toBeTruthy()

  await act(async () => {
    createButton?.click()
  })

  await waitFor(() => {
    expect(container.querySelector('a[href="/api/v1/sandboxes/sbox_new/ide/"]')).toBeTruthy()
  })

  expect(connectWorkspace).toHaveBeenLastCalledWith('wks_123', { recreate: true })
})

test('shows a sandbox creation overlay while recreating a workspace sandbox', async () => {
  connectWorkspace.mockImplementation((workspaceID, options) => {
    void workspaceID
    if (options?.recreate) {
      return new Promise(() => {})
    }
    return Promise.reject({
      response: {
        status: 409,
        data: { error: 'workspace sandbox no longer exists' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Create a new sandbox to continue working in this workspace.')
  })

  const createButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Create new sandbox'
  ))
  expect(createButton).toBeTruthy()

  await act(async () => {
    createButton?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Creating sandbox')
  })

  expect(container.textContent).toContain('Mounting qiniu/vision-tube and preparing the runtime.')
})

test('resets dismissed missing sandbox dialog when workspace changes', async () => {
  fetchWorkspaces.mockImplementation(() => apiResponse({
    workspaces: [
      {
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_456',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '',
      },
      {
        id: 'wks_789',
        name: 'DocsKit',
        repo_full_name: 'qiniu/docs-kit',
        region: 'us-south-1',
        sandbox_id: 'sbox_789',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox-2.example.com',
        workspace_path: '/workspace/qiniu__docs-kit',
        ide_url: '',
      },
    ],
  }))
  connectWorkspace.mockRejectedValue({
    response: {
      status: 409,
      data: { error: 'workspace sandbox no longer exists' },
    },
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  const router = createMemoryRouter([
    {
      path: '/workspaces/:workspaceId',
      element: <WorkspaceDetail />,
    },
  ], {
    initialEntries: ['/workspaces/wks_123'],
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(document.body.textContent).toContain('Sandbox unavailable')
  })

  const firstDismissButton = Array.from(document.body.querySelectorAll('button')).find((button) => (
    button.textContent === 'Not now'
  ))
  expect(firstDismissButton).toBeTruthy()

  await act(async () => {
    firstDismissButton?.click()
  })

  await waitFor(() => {
    expect(Array.from(document.body.querySelectorAll('button')).some((button) => (
      button.textContent === 'Not now'
    ))).toBe(false)
  })

  await act(async () => {
    await router.navigate('/workspaces/wks_789')
  })

  await waitFor(() => {
    expect(Array.from(document.body.querySelectorAll('button')).some((button) => (
      button.textContent === 'Not now'
    ))).toBe(true)
  })
})

test('shows a retry action when workspace connection fails', async () => {
  let retryAllowed = false
  connectWorkspace.mockImplementation(() => {
    if (retryAllowed) {
      return apiResponse({
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_456',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '/api/v1/sandboxes/sbox_456/ide/',
      })
    }
    return Promise.reject({
      response: {
        status: 500,
        data: { error: 'sandbox service unavailable' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Workspace connection failed')
  })

  expect(container.textContent).toContain('sandbox service unavailable')
  expect(container.querySelector('iframe[title="Code server IDE"]')).toBeNull()

  const retryButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Retry'
  ))
  expect(retryButton).toBeTruthy()

  await act(async () => {
    retryAllowed = true
    retryButton?.click()
  })

  await waitFor(() => {
    expect(container.querySelector('a[href="/api/v1/sandboxes/sbox_456/ide/"]')).toBeTruthy()
  })

  expect(connectWorkspace).toHaveBeenLastCalledWith('wks_123', undefined)
})

test('shows a retry action when loading workspaces fails', async () => {
  let retryAllowed = false
  fetchWorkspaces.mockImplementation(() => {
    if (retryAllowed) {
      return apiResponse({
        workspaces: [
          {
            id: 'wks_123',
            name: 'VisionTube',
            repo_full_name: 'qiniu/vision-tube',
            region: 'us-south-1',
            sandbox_id: 'sbox_456',
            template_id: 'tmpl_react',
            state: 'running',
            endpoint: 'sandbox.example.com',
            workspace_path: '/workspace/qiniu__vision-tube',
            ide_url: '/api/v1/sandboxes/sbox_456/ide/',
          },
        ],
      })
    }
    return Promise.reject({
      response: {
        status: 500,
        data: { error: 'workspace list unavailable' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Failed to load workspaces')
  })

  expect(container.textContent).toContain('workspace list unavailable')
  expect(container.textContent).not.toContain('Workspace not found')

  const retryButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Retry'
  ))
  expect(retryButton).toBeTruthy()

  await act(async () => {
    retryAllowed = true
    retryButton?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('VisionTube')
  })
})

test('hides stale connection errors while retrying', async () => {
  let retryAllowed = false
  let resolveRetry: ((value: { data: Workspace }) => void) | undefined
  connectWorkspace.mockImplementation(() => {
    if (retryAllowed) {
      return new Promise((resolve) => {
        resolveRetry = resolve
      })
    }
    return Promise.reject({
      response: {
        status: 500,
        data: { error: 'sandbox service unavailable' },
      },
    })
  })

  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/workspaces/wks_123']}>
          <Routes>
            <Route path="/workspaces/:workspaceId" element={<WorkspaceDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Workspace connection failed')
  })

  const retryButton = Array.from(container.querySelectorAll('button')).find((button) => (
    button.textContent === 'Retry'
  ))
  expect(retryButton).toBeTruthy()

  await act(async () => {
    retryAllowed = true
    retryButton?.click()
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Checking sandbox...')
  })

  expect(container.textContent).not.toContain('Workspace connection failed')
  expect(container.textContent).not.toContain('sandbox service unavailable')

  await act(async () => {
    resolveRetry?.({
      data: {
        id: 'wks_123',
        name: 'VisionTube',
        repo_full_name: 'qiniu/vision-tube',
        region: 'us-south-1',
        sandbox_id: 'sbox_456',
        template_id: 'tmpl_react',
        state: 'running',
        endpoint: 'sandbox.example.com',
        workspace_path: '/workspace/qiniu__vision-tube',
        ide_url: '/api/v1/sandboxes/sbox_456/ide/',
      },
    })
  })

  await waitFor(() => {
    expect(container.querySelector('a[href="/api/v1/sandboxes/sbox_456/ide/"]')).toBeTruthy()
  })
})
