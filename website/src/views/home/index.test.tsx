import { QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, expect, test, vi } from 'vitest'

import { queryClient } from 'src/lib/query-client'
import Home from './index'

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function apiResponse<T>(data: T) {
  return Promise.resolve({ data })
}

let workspaceFixtures: Array<{
  id: string
  name?: string
  repo_full_name?: string
  region: string
  sandbox_id?: string
  template_id: string
  state?: string
  endpoint?: string
  workspace_path?: string
  ide_url?: string
  created_at?: string
  updated_at?: string
}> = []
let sandboxFixtures: Array<{
  id: string
  sandbox_id: string
  template_id: string
  state: string
  endpoint?: string
  repo_full_name?: string
  workspace_path?: string
  region?: string
  metadata?: Record<string, string>
}> = []

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

vi.mock('src/api/github', () => ({
  githubAppInstall: () => apiResponse({ url: 'https://github.com/apps/qiniu-playground/installations/new' }),
  githubInstallations: () => apiResponse({ installations: [] }),
  githubRepositories: () => apiResponse({ repositories: [] }),
  openRepository: vi.fn(),
}))

vi.mock('src/api/qiniu', () => ({
  qiniuCredentialStatus: () => apiResponse({
    configured: true,
    maas_configured: false,
    access_key_configured: false,
    secret_key_configured: false,
  }),
  saveQiniuCredential: vi.fn(),
  deleteQiniuCredential: vi.fn(),
}))

vi.mock('src/api/sandboxes', () => ({
  sandboxSessions: () => apiResponse({ sandboxes: sandboxFixtures }),
  createSandbox: vi.fn(),
  connectSandbox: vi.fn(),
}))

vi.mock('src/api/templates', () => ({
  sandboxTemplates: () => apiResponse({ default_template_id: '', templates: [] }),
}))

vi.mock('src/api/workspaces', () => ({
  workspaces: () => apiResponse({ workspaces: workspaceFixtures }),
  createWorkspace: vi.fn(),
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
  workspaceFixtures = []
  sandboxFixtures = []
  document.body.innerHTML = ''
})

test('shows GitHub App setup prompt only inside create workspace dialog', async () => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Home page="workspaces" />
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('No workspaces yet.')
  })
  expect(container.textContent).not.toContain('Configure GitHub App')

  const newWorkspaceButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent === 'New workspace')
  expect(newWorkspaceButton).toBeTruthy()

  await act(async () => {
    newWorkspaceButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })

  expect(document.body.textContent).toContain('Configure GitHub App to choose repositories for new workspaces.')
})

test('renders workspace rows as detail links with timestamps and no action buttons', async () => {
  workspaceFixtures = [
    {
      id: '58f84632-dbe4-482e-88d8-079ffbcb1f72',
      name: 'Foo',
      repo_full_name: 'qiniu/playground',
      region: 'https://us-south-1-sandbox.qiniuapi.com',
      sandbox_id: 'sbox_hidden',
      template_id: 'tmpl_react',
      state: 'running',
      endpoint: 'us-south-1.sandbox.qibox.com',
      workspace_path: '/workspace/Foo',
      ide_url: '/api/v1/sandboxes/sbox_hidden/ide/',
      created_at: '2026-07-02T08:15:00Z',
      updated_at: '2026-07-02T09:30:00Z',
    },
  ]
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Home page="workspaces" />
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('Foo')
  })

  const rowLink = container.querySelector('a[href="/workspaces/58f84632-dbe4-482e-88d8-079ffbcb1f72"]')
  expect(rowLink?.textContent).toContain('qiniu/playground')
  expect(rowLink?.textContent).toContain('Created')
  expect(rowLink?.textContent).toContain('Updated')
  expect(rowLink?.textContent).toContain('/workspace/Foo')
  expect(rowLink?.textContent).not.toContain('running')
  expect(rowLink?.textContent).not.toContain('sbox_hidden')
  expect(rowLink?.textContent).not.toContain('us-south-1.sandbox.qibox.com')
  expect(rowLink?.textContent).not.toContain('tmpl_react')
  expect(container.textContent).not.toContain('Details')
  expect(container.textContent).not.toContain('IDE')
})

test('renders sandbox metadata on sandbox sessions', async () => {
  sandboxFixtures = [
    {
      id: 'sbx_123',
      sandbox_id: 'sbox_123',
      template_id: 'tmpl_react',
      state: 'running',
      repo_full_name: 'qiniu/playground',
      workspace_path: '/workspace/qiniu__playground',
      region: 'https://us-south-1-sandbox.qiniuapi.com',
      metadata: {
        created_by: 'qiniu-playground',
        kind: 'workspace',
        repo_full_name: 'qiniu/playground',
        workspace_path: '/workspace/qiniu__playground',
      },
    },
  ]
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <Home page="sandbox" />
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await waitFor(() => {
    expect(container.textContent).toContain('sbox_123')
  })
  expect(container.textContent).toContain('created_by: qiniu-playground')
  expect(container.textContent).toContain('kind: workspace')
  expect(container.textContent).toContain('repo_full_name: qiniu/playground')
  expect(container.textContent).toContain('workspace_path: /workspace/qiniu__playground')
})
