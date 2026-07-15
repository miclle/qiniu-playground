import { QueryClientProvider } from '@tanstack/react-query'
import { act, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import type { CodeRunnerSession } from 'src/api/code-runner'
import { queryClient } from 'src/lib/query-client'
import CodeRunner from './index'

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function apiResponse<T>(data: T) {
  return Promise.resolve({ data })
}

vi.mock('src/api/auth', () => ({
  currentUser: () => apiResponse({ account_id: 'acct_1' }),
}))

vi.mock('src/api/qiniu', () => ({
  qiniuCredentialStatus: () => apiResponse({ configured: true }),
}))

const heartbeatCodeRunnerSessionMock = vi.hoisted(() => vi.fn())
const killCodeRunnerSessionMock = vi.hoisted(() => vi.fn())
const runCodeMock = vi.hoisted(() => vi.fn())
let codeRunnerSessionFixtures: CodeRunnerSession[] = []

vi.mock('src/api/code-runner', () => ({
  codeRunnerSessions: () => apiResponse({ sessions: codeRunnerSessionFixtures }),
  codeRuns: () => apiResponse({ runs: [] }),
  connectCodeRunnerSession: vi.fn(),
  createCodeRunnerSession: vi.fn(),
  heartbeatCodeRunnerSession: heartbeatCodeRunnerSessionMock,
  killCodeRunnerSession: killCodeRunnerSessionMock,
  runCode: runCodeMock,
}))

async function waitFor(assertion: () => void) {
  let lastError: unknown
  for (let attempt = 0; attempt < 300; attempt += 1) {
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

let mountedRoots: Array<ReturnType<typeof createRoot>> = []

async function renderCodeRunner(initialEntry = '/code-runner', strictMode = false) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)

  await act(async () => {
    const app = (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route path="/code-runner" element={<CodeRunner />} />
            <Route path="/code-runner/:sessionId" element={<CodeRunner />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
    root.render(strictMode ? <StrictMode>{app}</StrictMode> : app)
  })

  return { container, root }
}

beforeEach(() => {
  queryClient.clear()
  codeRunnerSessionFixtures = []
  heartbeatCodeRunnerSessionMock.mockReset()
  heartbeatCodeRunnerSessionMock.mockImplementation(() => apiResponse({ ok: true, timeout_seconds: 1800 }))
  killCodeRunnerSessionMock.mockReset()
  killCodeRunnerSessionMock.mockImplementation(() => apiResponse({
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'killed',
  }))
  runCodeMock.mockReset()
  document.body.innerHTML = ''
})

afterEach(async () => {
  await act(async () => {
    mountedRoots.forEach((root) => root.unmount())
  })
  mountedRoots = []
  vi.useRealTimers()
  document.body.innerHTML = ''
})

test('defaults to the overseas region', async () => {
  const { container } = await renderCodeRunner()

  await waitFor(() => {
    expect(container.querySelector('[role="combobox"]')?.textContent).toContain('US (Dallas 1)')
  })
})

test('shows latest run summaries without sandbox implementation details', async () => {
  codeRunnerSessionFixtures = [
    {
      id: 'crs_1',
      name: 'Scratch',
      region: 'https://us-south-1-sandbox.qiniuapi.com',
      sandbox_id: 'sandbox-1',
      template_id: 'code-interpreter-v1',
      state: 'running',
      workspace_path: '/workspace/Scratch',
      updated_at: '2026-07-15T00:00:00Z',
      latest_run: {
        language: 'python',
        succeeded: true,
        duration_ms: 3842,
        created_at: '2026-07-15T00:00:00Z',
      },
    },
    {
      id: 'crs_2',
      name: 'Fresh',
      region: 'https://cn-yangzhou-1-sandbox.qiniuapi.com',
      template_id: 'code-interpreter-v1',
    },
  ]
  const { container } = await renderCodeRunner()

  await waitFor(() => {
    expect(container.textContent).toContain('Scratch')
    expect(container.textContent).toContain('US (Dallas 1)')
    expect(container.textContent).toContain('Python')
    expect(container.textContent).toContain('Succeeded')
    expect(container.textContent).toContain('3.8 s')
    expect(container.textContent).toContain('Fresh')
    expect(container.textContent).toContain('Not run yet')
  })
  expect(container.textContent).not.toContain('Updated')
  expect(container.textContent).not.toContain('running')
  expect(container.textContent).not.toContain('Template')
  expect(container.textContent).not.toContain('code-interpreter-v1')
  expect(container.textContent).not.toContain('/workspace/Scratch')
  expect(container.textContent).not.toContain('sandbox-1')
})

test('keeps an active code runner alive and kills it after thirty idle minutes', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date('2026-07-15T00:00:00Z'))
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'running',
    workspace_path: '/workspace/Scratch',
  }]
  await renderCodeRunner('/code-runner/crs_1')

  await waitFor(() => {
    expect(heartbeatCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1')
  })

  await act(async () => {
    vi.advanceTimersByTime(29 * 60_000)
  })
  expect(killCodeRunnerSessionMock).not.toHaveBeenCalled()

  await act(async () => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
  })
  await act(async () => {
    vi.advanceTimersByTime(29 * 60_000)
  })
  expect(killCodeRunnerSessionMock).not.toHaveBeenCalled()

  await act(async () => {
    vi.advanceTimersByTime(60_000)
  })
  expect(killCodeRunnerSessionMock).toHaveBeenCalledTimes(1)
  expect(killCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1')
})

test('refreshes the sandbox immediately when the tab becomes visible', async () => {
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date('2026-07-15T00:00:00Z'))
  let visibilityState: DocumentVisibilityState = 'visible'
  const visibilityStateSpy = vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibilityState)
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'running',
    workspace_path: '/workspace/Scratch',
  }]
  await renderCodeRunner('/code-runner/crs_1')

  await waitFor(() => {
    expect(heartbeatCodeRunnerSessionMock).toHaveBeenCalledTimes(1)
  })

  visibilityState = 'hidden'
  await act(async () => {
    vi.advanceTimersByTime(60_000)
  })
  expect(heartbeatCodeRunnerSessionMock).toHaveBeenCalledTimes(1)

  visibilityState = 'visible'
  await act(async () => {
    document.dispatchEvent(new Event('visibilitychange'))
  })
  expect(heartbeatCodeRunnerSessionMock).toHaveBeenCalledTimes(2)
  visibilityStateSpy.mockRestore()
})

test('kills a running sandbox on unmount without killing during StrictMode replay', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'running',
    workspace_path: '/workspace/Scratch',
  }]
  const { container, root } = await renderCodeRunner('/code-runner/crs_1', true)

  await waitFor(() => {
    expect(container.querySelector('h1')?.textContent).toBe('Scratch')
  })
  expect(killCodeRunnerSessionMock).not.toHaveBeenCalled()
  await act(async () => {
    root.unmount()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  mountedRoots = mountedRoots.filter((mountedRoot) => mountedRoot !== root)

  expect(killCodeRunnerSessionMock).toHaveBeenCalledTimes(1)
  expect(killCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1')
})

test('allows Run to recover a killed code runner sandbox', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-old',
    template_id: 'code-interpreter-v1',
    state: 'killed',
    workspace_path: '/workspace/Scratch',
  }]
  const { container } = await renderCodeRunner('/code-runner/crs_1')
  let runButton: HTMLButtonElement | undefined

  await waitFor(() => {
    runButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === 'Run')
    expect(runButton).toBeTruthy()
  })
  expect(runButton?.disabled).toBe(false)
})

test('hides sandbox lifecycle details and Connect from a session', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-old',
    template_id: 'code-interpreter-v1',
    state: 'killed',
    workspace_path: '/workspace/Scratch',
  }]
  const { container } = await renderCodeRunner('/code-runner/crs_1')

  await waitFor(() => {
    expect(container.querySelector('h1')?.textContent).toBe('Scratch')
  })
  expect(container.textContent).toContain('US (Dallas 1)')
  expect(container.textContent).not.toContain('killed')
  expect(container.textContent).not.toContain('code-interpreter-v1')
  expect(container.textContent).not.toContain('/workspace/Scratch')
  expect(Array.from(container.querySelectorAll('button')).some((button) => button.textContent?.trim() === 'Connect')).toBe(false)
})

test('offers Retry after a run fails', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'running',
    workspace_path: '/workspace/Scratch',
  }]
  runCodeMock.mockRejectedValueOnce(new Error('environment unavailable'))
  const { container } = await renderCodeRunner('/code-runner/crs_1')
  let runButton: HTMLButtonElement | undefined

  await waitFor(() => {
    runButton = Array.from(container.querySelectorAll('button')).find((button) => button.textContent?.trim() === 'Run')
    expect(runButton).toBeTruthy()
  })
  await act(async () => {
    runButton?.click()
  })
  await waitFor(() => {
    expect(Array.from(container.querySelectorAll('button')).some((button) => button.textContent?.trim() === 'Retry')).toBe(true)
  })
})

test('returns to the Code Runner list without waiting for sandbox cleanup', async () => {
  codeRunnerSessionFixtures = [{
    id: 'crs_1',
    name: 'Scratch',
    region: 'https://us-south-1-sandbox.qiniuapi.com',
    sandbox_id: 'sandbox-1',
    template_id: 'code-interpreter-v1',
    state: 'running',
    workspace_path: '/workspace/Scratch',
  }]
  let resolveKill: (() => void) | undefined
  killCodeRunnerSessionMock.mockImplementationOnce(() => new Promise((resolve) => {
    resolveKill = () => resolve(apiResponse({
      ...codeRunnerSessionFixtures[0],
      state: 'killed',
    }))
  }))
  const { container } = await renderCodeRunner('/code-runner/crs_1')
  let backLink: HTMLAnchorElement | null = null

  await waitFor(() => {
    backLink = container.querySelector('a[aria-label="Back to Code Runner"]')
    expect(backLink).toBeTruthy()
  })
  await act(async () => {
    backLink?.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 }))
  })

  expect(killCodeRunnerSessionMock).toHaveBeenCalledTimes(1)
  expect(killCodeRunnerSessionMock).toHaveBeenCalledWith('crs_1')

  await waitFor(() => {
    expect(container.querySelector('h1')?.textContent).toBe('Code Runner')
  })

  await act(async () => {
    resolveKill?.()
  })
})
