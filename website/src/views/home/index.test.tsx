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
  sandboxSessions: () => apiResponse({ sandboxes: [] }),
  createSandbox: vi.fn(),
  connectSandbox: vi.fn(),
}))

vi.mock('src/api/templates', () => ({
  sandboxTemplates: () => apiResponse({ default_template_id: '', templates: [] }),
}))

vi.mock('src/api/workspaces', () => ({
  workspaces: () => apiResponse({ workspaces: [] }),
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
