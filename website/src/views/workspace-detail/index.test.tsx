import { QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { createMemoryRouter, MemoryRouter, Route, RouterProvider, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import type { ConnectWorkspaceOptions, Workspace } from 'src/api/workspaces'
import type { SandboxFileEntry } from 'src/api/filesystem'
import { queryClient } from 'src/lib/query-client'
import routes from 'src/router'
import WorkspaceDetail from './index'

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

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

vi.mock('src/api/workspaces', () => ({
  workspaces: () => fetchWorkspaces(),
  connectWorkspace: (workspaceID: string, options?: { recreate?: boolean }) => connectWorkspace(workspaceID, options),
}))

vi.mock('src/api/filesystem', () => ({
  sandboxFiles: (sandboxID: string, path: string) => fetchSandboxFiles(sandboxID, path),
  sandboxFileContent: (sandboxID: string, path: string) => fetchSandboxFileContent(sandboxID, path),
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
  while (Date.now() - startedAt < 1000) {
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
  fetchSandboxFiles.mockClear()
  fetchSandboxFileContent.mockClear()
  window.URL.createObjectURL = vi.fn(() => 'blob:download')
  window.URL.revokeObjectURL = vi.fn()
  HTMLAnchorElement.prototype.click = vi.fn()
  document.body.innerHTML = ''
})

afterEach(() => {
  vi.restoreAllMocks()
})

test('renders a workspace workbench with assistant, files, and code panels', async () => {
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

  expect(container.textContent).toContain('Assistant')
  expect(container.textContent).toContain('Files')
  expect(container.textContent).toContain('..')
  expect(container.textContent).not.toContain('Files Tree')
  expect(container.textContent).not.toContain('Size')
  expect(container.textContent).not.toContain('42 B')
  expect(container.querySelector('[role="separator"][aria-label="Resize file browser panes"]')).toBeTruthy()
  expect(container.querySelector('[role="status"][aria-label="File browser status"]')).toBeTruthy()
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
