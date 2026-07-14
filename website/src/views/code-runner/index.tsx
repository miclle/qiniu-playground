import { java } from '@codemirror/lang-java'
import { javascript } from '@codemirror/lang-javascript'
import { python } from '@codemirror/lang-python'
import { StreamLanguage } from '@codemirror/language'
import { r } from '@codemirror/legacy-modes/mode/r'
import { shell } from '@codemirror/legacy-modes/mode/shell'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Badge, Button, Card, Dialog, Flex, IconButton, Select, TextField } from '@radix-ui/themes'
import CodeMirror, { EditorView } from '@uiw/react-codemirror'
import type { CSSProperties, FormEvent, KeyboardEvent, PointerEvent } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, ArrowLeft, Code2, History, LoaderCircle, Play, Plus, RotateCw } from 'lucide-react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import { currentUser } from 'src/api/auth'
import {
  codeRunnerSessions,
  codeRuns,
  connectCodeRunnerSession,
  createCodeRunnerSession,
  runCode,
} from 'src/api/code-runner'
import type { CodeRun, CodeRunnerLanguage, CodeRunnerSession, RunCodePayload } from 'src/api/code-runner'
import { qiniuCredentialStatus } from 'src/api/qiniu'
import { queryClient } from 'src/lib/query-client'

const codeRunnerRegions = [
  {
    id: 'cn-yangzhou-1',
    label: 'China (Yangzhou 1)',
    endpoint: 'https://cn-yangzhou-1-sandbox.qiniuapi.com',
  },
  {
    id: 'us-south-1',
    label: 'US (Dallas 1)',
    endpoint: 'https://us-south-1-sandbox.qiniuapi.com',
  },
]

const codeRunnerLanguages: Array<{ value: CodeRunnerLanguage; label: string }> = [
  { value: 'python', label: 'Python' },
  { value: 'javascript', label: 'JavaScript' },
  { value: 'typescript', label: 'TypeScript' },
  { value: 'r', label: 'R' },
  { value: 'java', label: 'Java' },
  { value: 'bash', label: 'Bash' },
]

const starterCode: Record<CodeRunnerLanguage, string> = {
  python: `import math

print("Hello from Qiniu Code Runner")
print("sqrt(144) =", math.sqrt(144))
`,
  javascript: `const value = Math.sqrt(144)

console.log("Hello from Qiniu Code Runner")
console.log("sqrt(144) =", value)
`,
  typescript: `const value: number = Math.sqrt(144)

console.log("Hello from Qiniu Code Runner")
console.log("sqrt(144) =", value)
`,
  r: `value <- sqrt(144)

cat("Hello from Qiniu Code Runner\\n")
cat("sqrt(144) =", value, "\\n")
`,
  java: `public class Main {
  public static void main(String[] args) {
    double value = Math.sqrt(144);

    System.out.println("Hello from Qiniu Code Runner");
    System.out.println("sqrt(144) = " + value);
  }
}
`,
  bash: `#!/usr/bin/env bash
set -euo pipefail

echo "Hello from Qiniu Code Runner"
echo "sqrt(144) = 12"
`,
}

const minCodeRunnerPaneHeight = 180
const resultResizeHandleHeight = 6

const codeEditorTheme = EditorView.theme({
  '&': { height: '100%', minHeight: '0', backgroundColor: 'transparent' },
  '.cm-scroller': {
    height: '100%',
    overflow: 'auto',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
    fontSize: '0.875rem',
    lineHeight: '1.5rem',
  },
  '.cm-content': { padding: '0.75rem 0' },
  '.cm-line': { padding: '0 1rem' },
  '.cm-gutters': {
    alignSelf: 'stretch',
    minHeight: '100%',
    backgroundColor: 'var(--gray-2)',
    border: 'none',
  },
  '.cm-gutter': { minHeight: '100%' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--accent-3)' },
  '&.cm-focused': { outline: 'none' },
  '.cm-focused': { outline: 'none' },
  '.cm-content:focus': { outline: 'none' },
})

const codeEditorBasicSetup = {
  autocompletion: true,
  bracketMatching: true,
  closeBrackets: true,
  foldGutter: true,
  highlightActiveLine: true,
  highlightActiveLineGutter: true,
  lineNumbers: true,
}

function apiErrorMessage(error: unknown) {
  if (!error || typeof error !== 'object') {
    return 'Unknown error'
  }
  const maybeResponse = error as { response?: { data?: unknown }; message?: string }
  const data = maybeResponse.response?.data
  if (typeof data === 'string' && data.trim()) {
    return data.trim()
  }
  if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
    return data.error
  }
  return maybeResponse.message || 'Unknown error'
}

function formatDateTime(value?: string) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function languageExtension(language: CodeRunnerLanguage) {
  switch (language) {
    case 'javascript':
      return javascript()
    case 'typescript':
      return javascript({ typescript: true })
    case 'r':
      return StreamLanguage.define(r)
    case 'java':
      return java()
    case 'bash':
      return StreamLanguage.define(shell)
    case 'python':
    default:
      return python()
  }
}

function regionLabel(endpoint: string) {
  return codeRunnerRegions.find((region) => region.endpoint === endpoint)?.label ?? endpoint
}

function codeRunnerLanguageLabel(language: string) {
  return codeRunnerLanguages.find((item) => item.value === language)?.label ?? language
}

function codeRunSnippet(code: string) {
  return code.split('\n').map((line) => line.trim()).find(Boolean) || 'Empty code'
}

function codeRunStatusColor(run: CodeRun) {
  return run.exit_code === 0 && !run.error && !run.stderr ? 'green' : 'amber'
}

function SessionStateBadge({ session }: { session: CodeRunnerSession }) {
  const running = session.state === 'running'
  return (
    <Badge color={running ? 'green' : 'gray'} variant="soft">
      {session.state || 'unknown'}
    </Badge>
  )
}

function RunOutput({ run, running }: { run?: CodeRun; running: boolean }) {
  if (running) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center bg-background p-6 text-center text-sm text-muted-foreground">
        <div className="flex items-center gap-2">
          <LoaderCircle className="h-4 w-4 animate-spin text-primary" />
          <span>Running code...</span>
        </div>
      </div>
    )
  }
  if (!run) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center bg-background p-6 text-center text-sm text-muted-foreground">
        Run code to see output.
      </div>
    )
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <pre className="m-0 min-h-0 flex-1 overflow-auto whitespace-pre-wrap bg-background p-4 font-mono text-xs leading-5 text-foreground">
        {run.stdout || ''}
        {run.stderr || run.error ? `\n${run.stderr || run.error}` : ''}
      </pre>
    </div>
  )
}

function RunMeta({ run, running, count }: { run?: CodeRun; running: boolean; count: number }) {
  if (running) {
    return <span>Running...</span>
  }
  if (!run) {
    return <span>{count} runs</span>
  }
  return (
    <span className="flex min-w-0 items-center gap-4">
      <span>Exit {run.exit_code}</span>
      <span>{run.duration_ms} ms</span>
      <span>{formatDateTime(run.created_at)}</span>
    </span>
  )
}

function RunHistoryDialog({
  open,
  runs,
  selectedRun,
  onOpenChange,
  onSelectRun,
  onRestoreRun,
}: {
  open: boolean
  runs: CodeRun[]
  selectedRun?: CodeRun
  onOpenChange: (open: boolean) => void
  onSelectRun: (run: CodeRun) => void
  onRestoreRun: (run: CodeRun) => void
}) {
  function handlePreviewKeyDown(event: KeyboardEvent<HTMLDivElement>, run: CodeRun) {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return
    }
    event.preventDefault()
    onSelectRun(run)
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Content size="3" maxWidth="1120px">
        <Dialog.Title>Run history</Dialog.Title>
        <Dialog.Description size="2" color="gray">
          Review previous executions and restore a version to the editor.
        </Dialog.Description>
        <div className="mt-4 grid h-[40rem] max-h-[80vh] grid-rows-[14rem_1fr] overflow-hidden rounded-md lg:grid-cols-[20rem_1fr] lg:grid-rows-1">
          <div className="min-h-0 overflow-hidden border-b lg:border-b-0 lg:border-r">
            <div className="h-full overflow-auto p-1">
              {runs.length ? (
                runs.slice().reverse().map((run) => {
                  const selected = selectedRun?.id === run.id
                  return (
                    <div
                      key={run.id}
                      role="button"
                      tabIndex={0}
                      aria-pressed={selected}
                      className={[
                        'cursor-pointer rounded-sm px-2.5 py-2 text-left outline-none transition-[background-color,box-shadow]',
                        'hover:bg-secondary/60 focus-visible:ring-1 focus-visible:ring-[var(--accent-8)] focus-visible:ring-inset',
                        selected ? 'bg-[var(--accent-3)] shadow-[inset_2px_0_0_var(--accent-9)]' : '',
                      ].join(' ')}
                      onClick={() => onSelectRun(run)}
                      onKeyDown={(event) => handlePreviewKeyDown(event, run)}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="flex min-w-0 items-center gap-2">
                          <Badge color={codeRunStatusColor(run)} variant="soft">
                            Exit {run.exit_code}
                          </Badge>
                          <span className="truncate text-xs text-muted-foreground">{codeRunnerLanguageLabel(run.language)}</span>
                        </div>
                        <span className="shrink-0 text-xs text-muted-foreground">{run.duration_ms} ms</span>
                      </div>
                      <p className="mt-1 truncate font-mono text-xs text-foreground">{codeRunSnippet(run.code)}</p>
                      <p className="mt-1 text-xs text-muted-foreground">{formatDateTime(run.created_at)}</p>
                    </div>
                  )
                })
              ) : (
                <div className="px-3 py-8 text-center text-sm text-muted-foreground">No runs yet.</div>
              )}
            </div>
          </div>

          <div className="flex min-h-0 flex-col bg-background">
            {selectedRun ? (
              <div className="grid min-h-0 flex-1 grid-rows-2 divide-y">
                <div className="flex min-h-0 flex-col">
                  <div className="flex min-h-10 shrink-0 items-center justify-between gap-3 bg-secondary/25 px-4 py-2">
                    <span className="text-xs font-semibold">Code</span>
                    <Button type="button" variant="soft" color="gray" size="1" onClick={() => onRestoreRun(selectedRun)}>
                      Restore to editor
                    </Button>
                  </div>
                  <div className="min-h-0 flex-1 overflow-hidden">
                    <CodeRunnerEditor
                      language={selectedRun.language as CodeRunnerLanguage}
                      value={selectedRun.code}
                      ariaLabel="History code"
                      readOnly
                    />
                  </div>
                </div>
                <div className="flex min-h-0 flex-col">
                  <div className="flex min-h-10 shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-1 bg-secondary/25 px-4 py-2">
                    <span className="text-xs font-semibold">Output</span>
                    <span className="flex min-w-0 flex-wrap items-center justify-end gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <Badge color={codeRunStatusColor(selectedRun)} variant="soft">
                        Exit {selectedRun.exit_code}
                      </Badge>
                      <span>{codeRunnerLanguageLabel(selectedRun.language)}</span>
                      <span>{selectedRun.duration_ms} ms</span>
                      <span>{formatDateTime(selectedRun.created_at)}</span>
                    </span>
                  </div>
                  <pre className="m-0 min-h-0 flex-1 overflow-auto whitespace-pre-wrap px-4 py-3 font-mono text-xs leading-5 text-foreground">
                    {selectedRun.stdout || ''}
                    {selectedRun.stderr || selectedRun.error ? `\n${selectedRun.stderr || selectedRun.error}` : ''}
                  </pre>
                </div>
              </div>
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center text-sm text-muted-foreground">
                Run code to build history.
              </div>
            )}
          </div>
        </div>
      </Dialog.Content>
    </Dialog.Root>
  )
}

function CodeRunnerEditor({
  language,
  value,
  onChange,
  ariaLabel = 'Code',
  readOnly = false,
}: {
  language: CodeRunnerLanguage
  value: string
  onChange?: (value: string) => void
  ariaLabel?: string
  readOnly?: boolean
}) {
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (
    document.documentElement.classList.contains('dark') ? 'dark' : 'light'
  ))
  const editorAttributes = useMemo(() => EditorView.contentAttributes.of({
    'aria-label': ariaLabel,
    ...(readOnly ? { 'aria-readonly': 'true' } : {}),
  }), [ariaLabel, readOnly])
  const extensions = useMemo(() => [languageExtension(language), codeEditorTheme, editorAttributes], [editorAttributes, language])
  const basicSetup = readOnly ? {
    ...codeEditorBasicSetup,
    autocompletion: false,
    closeBrackets: false,
    foldGutter: false,
    highlightActiveLine: false,
    highlightActiveLineGutter: false,
  } : codeEditorBasicSetup

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setTheme(document.documentElement.classList.contains('dark') ? 'dark' : 'light')
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return (
    <CodeMirror
      value={value}
      height="100%"
      theme={theme}
      basicSetup={basicSetup}
      extensions={extensions}
      editable={!readOnly}
      readOnly={readOnly}
      className="h-full min-h-0 [&_.cm-editor]:h-full [&_.cm-scroller]:h-full"
      onChange={onChange}
    />
  )
}

function CodeRunnerList() {
  const navigate = useNavigate()
  const [name, setName] = useState('Scratch')
  const [region, setRegion] = useState(codeRunnerRegions[0].endpoint)
  const authQuery = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: currentUser,
    retry: false,
  })
  const qiniuQuery = useQuery({
    queryKey: ['qiniu', 'credentials'],
    queryFn: qiniuCredentialStatus,
    enabled: Boolean(authQuery.data),
  })
  const sessionsQuery = useQuery({
    queryKey: ['code-runner', 'sessions'],
    queryFn: codeRunnerSessions,
    enabled: Boolean(authQuery.data),
  })
  const createSession = useMutation({
    mutationFn: createCodeRunnerSession,
    onSuccess: (response) => {
      void queryClient.invalidateQueries({ queryKey: ['code-runner', 'sessions'] })
      navigate(`/code-runner/${response.data.id}`)
    },
  })

  if (authQuery.isLoading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading Code Runner...</div>
  }
  if (authQuery.isError || !authQuery.data) {
    return <Navigate to="/login" replace />
  }

  const sessions = sessionsQuery.data?.data.sessions ?? []
  const qiniuStatus = qiniuQuery.data?.data
  const disabled = createSession.isPending || !qiniuStatus?.configured

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (disabled) {
      return
    }
    createSession.mutate({
      name: name.trim() || 'Scratch',
      region,
    })
  }

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 p-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Code Runner</h1>
          <p className="mt-1 text-sm text-muted-foreground">Multi-language sessions backed by the code-interpreter-v1 sandbox template.</p>
        </div>
      </div>

      {!qiniuQuery.isLoading && !qiniuStatus?.configured ? (
        <Card asChild size="2">
          <section className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-base font-semibold">Sandbox API Key required</h2>
              <p className="mt-1 text-sm text-muted-foreground">Configure credentials before creating a Code Runner session.</p>
            </div>
            <Button asChild size="2" className="w-fit no-underline">
              <Link to="/credentials">Configure API key</Link>
            </Button>
          </section>
        </Card>
      ) : null}

      <Card asChild size="2">
        <form onSubmit={handleCreate}>
          <div className="grid gap-4 lg:grid-cols-[1fr_16rem_8rem] lg:items-end">
            <label className="grid gap-2">
              <span className="text-sm font-medium">Session name</span>
              <TextField.Root value={name} onChange={(event) => setName(event.target.value)} placeholder="Scratch" />
            </label>
            <label className="grid gap-2">
              <span className="text-sm font-medium">Region</span>
              <Select.Root value={region} onValueChange={(value) => setRegion(value)}>
                <Select.Trigger />
                <Select.Content>
                  {codeRunnerRegions.map((item) => (
                    <Select.Item key={item.id} value={item.endpoint}>
                      {item.label}
                    </Select.Item>
                  ))}
                </Select.Content>
              </Select.Root>
            </label>
            <Button type="submit" disabled={disabled}>
              {createSession.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
              New
            </Button>
          </div>
          {createSession.isError ? (
            <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
              {apiErrorMessage(createSession.error)}
            </div>
          ) : null}
        </form>
      </Card>

      <Card asChild size="2">
        <section>
          <div className="flex items-center justify-between border-b pb-3">
            <h2 className="text-sm font-semibold">Sessions</h2>
            <span className="text-xs text-muted-foreground">{sessions.length} sessions</span>
          </div>
          {sessionsQuery.isError ? (
            <div className="mt-3 rounded-md bg-destructive/10 px-4 py-3 text-sm text-destructive">{apiErrorMessage(sessionsQuery.error)}</div>
          ) : null}
          {sessions.length ? (
            <div className="mt-3 divide-y">
              {sessions.map((session) => (
                <Link
                  key={session.id}
                  to={`/code-runner/${session.id}`}
                  className="flex flex-col gap-3 rounded-sm px-2 py-3 text-sm text-foreground no-underline transition-colors hover:bg-secondary/70 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 sm:flex-row sm:items-start sm:justify-between"
                >
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{session.name}</span>
                      <SessionStateBadge session={session} />
                    </div>
                    <p className="mt-1 truncate text-xs text-muted-foreground">{regionLabel(session.region)}</p>
                    <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{session.workspace_path || session.sandbox_id}</p>
                  </div>
                  <div className="grid gap-1 text-xs text-muted-foreground sm:min-w-48 sm:text-right">
                    <span>Template {session.template_id}</span>
                    <span>Updated {formatDateTime(session.updated_at)}</span>
                  </div>
                </Link>
              ))}
            </div>
          ) : (
            <div className="flex items-center gap-3 pt-4 text-sm text-muted-foreground">
              <Code2 className="h-4 w-4" />
              <span>{sessionsQuery.isLoading ? 'Loading sessions...' : 'No Code Runner sessions yet.'}</span>
            </div>
          )}
        </section>
      </Card>
    </div>
  )
}

function CodeRunnerDetail() {
  const { sessionId } = useParams()
  const [language, setLanguage] = useState<CodeRunnerLanguage>('python')
  const [codeByLanguage, setCodeByLanguage] = useState<Record<CodeRunnerLanguage, string>>(starterCode)
  const [resultPanelHeight, setResultPanelHeight] = useState<number | null>(null)
  const [resultPanelResizing, setResultPanelResizing] = useState(false)
  const [resultResizeHovering, setResultResizeHovering] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [selectedHistoryRunID, setSelectedHistoryRunID] = useState<string | null>(null)
  const splitLayoutRef = useRef<HTMLDivElement | null>(null)
  const dragCleanupRef = useRef<((isUnmounting?: boolean) => void) | null>(null)
  const authQuery = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: currentUser,
    retry: false,
  })
  const sessionsQuery = useQuery({
    queryKey: ['code-runner', 'sessions'],
    queryFn: codeRunnerSessions,
    enabled: Boolean(authQuery.data),
  })
  const session = sessionsQuery.data?.data.sessions?.find((item) => item.id === sessionId)
  const runsQuery = useQuery({
    queryKey: ['code-runner', sessionId, 'runs'],
    queryFn: () => codeRuns(sessionId || ''),
    enabled: Boolean(authQuery.data && sessionId),
  })
  const connectSession = useMutation({
    mutationFn: () => connectCodeRunnerSession(sessionId || ''),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['code-runner', 'sessions'] })
    },
  })
  const runMutation = useMutation({
    mutationFn: (payload: RunCodePayload) => runCode(sessionId || '', payload),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['code-runner', sessionId, 'runs'] })
    },
  })

  useEffect(() => () => {
    dragCleanupRef.current?.(true)
  }, [])

  const runs = useMemo(() => runsQuery.data?.data.runs ?? [], [runsQuery.data?.data.runs])
  const latestRun = runs[runs.length - 1]
  const selectedHistoryRun = runs.find((run) => run.id === selectedHistoryRunID) ?? latestRun
  const code = codeByLanguage[language]
  const currentRunPayload: RunCodePayload = {
    language,
    code,
    timeout_seconds: 30,
  }

  if (authQuery.isLoading || sessionsQuery.isLoading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading Code Runner...</div>
  }
  if (authQuery.isError || !authQuery.data) {
    return <Navigate to="/login" replace />
  }
  if (!session) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <section className="w-full max-w-md rounded-md border p-6">
          <AlertTriangle className="h-8 w-8 text-muted-foreground" />
          <h1 className="mt-4 text-xl font-semibold">Session not found</h1>
          <Button asChild variant="outline" color="gray" className="mt-5 no-underline">
            <Link to="/code-runner">
              <ArrowLeft className="h-4 w-4" />
              Back to Code Runner
            </Link>
          </Button>
        </section>
      </div>
    )
  }

  const sessionRunning = Boolean(session.sandbox_id && session.state === 'running')
  const showConnectAction = !sessionRunning || connectSession.isError
  const canRun = Boolean(sessionRunning && code.trim() && !runMutation.isPending)
  const resultPanelHeightValue = resultPanelHeight === null ? '50%' : `${resultPanelHeight}px`
  const splitLayoutStyle = {
    '--code-runner-result-height': resultPanelHeightValue,
  } as CSSProperties

  const clampResultPanelHeight = (height: number, containerHeight?: number) => {
    if (!containerHeight || containerHeight <= minCodeRunnerPaneHeight * 2 + resultResizeHandleHeight) {
      return Math.max(minCodeRunnerPaneHeight, height)
    }
    return Math.min(
      Math.max(minCodeRunnerPaneHeight, height),
      containerHeight - minCodeRunnerPaneHeight - resultResizeHandleHeight,
    )
  }

  const updateResultPanelHeight = (height: number, containerHeight?: number) => {
    setResultPanelHeight(clampResultPanelHeight(
      height,
      containerHeight ?? splitLayoutRef.current?.getBoundingClientRect()?.height,
    ))
  }

  const nudgeResultPanelHeight = (offset: number) => {
    const containerHeight = splitLayoutRef.current?.getBoundingClientRect()?.height
    const currentHeight = resultPanelHeight ?? ((containerHeight ?? 0) / 2)
    updateResultPanelHeight(currentHeight + offset, containerHeight)
  }

  const handleResultResizePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return
    }
    event.preventDefault()
    const containerHeight = splitLayoutRef.current?.getBoundingClientRect()?.height
    const startHeight = resultPanelHeight ?? ((containerHeight ?? 0) / 2)
    const startY = event.clientY

    const handlePointerMove = (moveEvent: globalThis.PointerEvent) => {
      updateResultPanelHeight(startHeight - (moveEvent.clientY - startY), containerHeight)
    }
    const cleanup = (isUnmounting = false) => {
      document.removeEventListener('pointermove', handlePointerMove)
      document.removeEventListener('pointerup', handlePointerUp)
      if (!isUnmounting) {
        setResultPanelResizing(false)
      }
      dragCleanupRef.current = null
    }
    const handlePointerUp = () => {
      cleanup()
    }

    dragCleanupRef.current?.()
    setResultPanelResizing(true)
    document.addEventListener('pointermove', handlePointerMove)
    document.addEventListener('pointerup', handlePointerUp)
    dragCleanupRef.current = cleanup
  }

  const handleResultResizeKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      nudgeResultPanelHeight(24)
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      nudgeResultPanelHeight(-24)
    }
    if (event.key === 'Home') {
      event.preventDefault()
      updateResultPanelHeight(minCodeRunnerPaneHeight)
    }
    if (event.key === 'End') {
      event.preventDefault()
      updateResultPanelHeight(splitLayoutRef.current?.getBoundingClientRect()?.height ?? minCodeRunnerPaneHeight)
    }
  }

  const handleLanguageChange = (value: string) => {
    setLanguage(value as CodeRunnerLanguage)
  }

  const handleCodeChange = (value: string) => {
    setCodeByLanguage((current) => ({
      ...current,
      [language]: value,
    }))
  }

  const handleHistoryOpenChange = (open: boolean) => {
    setHistoryOpen(open)
    if (open) {
      setSelectedHistoryRunID((current) => current ?? latestRun?.id ?? null)
    }
  }

  const handleRestoreRun = (run: CodeRun) => {
    const runLanguage = run.language as CodeRunnerLanguage
    setLanguage(runLanguage)
    setCodeByLanguage((current) => ({
      ...current,
      [runLanguage]: run.code,
    }))
    setHistoryOpen(false)
  }

  return (
    <div className="flex h-screen min-h-0 flex-col overflow-hidden bg-background">
      <RunHistoryDialog
        open={historyOpen}
        runs={runs}
        selectedRun={selectedHistoryRun}
        onOpenChange={handleHistoryOpenChange}
        onSelectRun={(run) => setSelectedHistoryRunID(run.id)}
        onRestoreRun={handleRestoreRun}
      />
      <header className="shrink-0 border-b bg-background px-5 py-3">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <IconButton asChild variant="ghost" color="gray" size="2" className="rounded-sm no-underline">
              <Link to="/code-runner" aria-label="Back to Code Runner">
                <ArrowLeft className="h-4 w-4" />
              </Link>
            </IconButton>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-lg font-semibold">{session.name}</h1>
                <SessionStateBadge session={session} />
              </div>
              <p className="mt-1 truncate text-xs text-muted-foreground">
                {regionLabel(session.region)} · {session.template_id} · {session.workspace_path || session.sandbox_id}
              </p>
            </div>
          </div>
          <Flex gap="2" wrap="wrap">
            <Button type="button" onClick={() => {
              connectSession.reset()
              runMutation.mutate(currentRunPayload)
            }} disabled={!canRun}>
              {runMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
              Run
            </Button>
            <Button
              type="button"
              variant="outline"
              color="gray"
              disabled={!runs.length}
              onClick={() => handleHistoryOpenChange(true)}
            >
              <History className="h-4 w-4" />
              History
            </Button>
            {showConnectAction ? (
              <Button
                type="button"
                variant="outline"
                color="gray"
                onClick={() => {
                  runMutation.reset()
                  connectSession.mutate()
                }}
                disabled={connectSession.isPending}
              >
                {connectSession.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCw className="h-4 w-4" />}
                Connect
              </Button>
            ) : null}
          </Flex>
        </div>
        {connectSession.isError || runMutation.isError ? (
          <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            {apiErrorMessage(connectSession.error || runMutation.error)}
          </div>
        ) : null}
      </header>

      <main
        ref={splitLayoutRef}
        className={`flex min-h-0 flex-1 flex-col ${resultPanelResizing ? 'select-none' : ''}`}
        style={splitLayoutStyle}
      >
        <section className="flex min-h-0 flex-1 flex-col">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex h-10 shrink-0 items-center justify-between border-b bg-secondary/20 px-4">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Code2 className="h-4 w-4" />
                <Select.Root value={language} onValueChange={handleLanguageChange}>
                  <Select.Trigger variant="ghost" aria-label="Language" />
                  <Select.Content>
                    {codeRunnerLanguages.map((item) => (
                      <Select.Item key={item.value} value={item.value}>
                        {item.label}
                      </Select.Item>
                    ))}
                  </Select.Content>
                </Select.Root>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">30s timeout</span>
              </div>
            </div>
            <div className="flex min-h-0 flex-1 flex-col">
              <CodeRunnerEditor language={language} value={code} onChange={handleCodeChange} />
            </div>
          </div>
        </section>

        <div
          role="separator"
          aria-label="Resize result panel"
          aria-orientation="horizontal"
          aria-valuemin={minCodeRunnerPaneHeight}
          aria-valuenow={Math.round(resultPanelHeight ?? minCodeRunnerPaneHeight)}
          tabIndex={0}
          className="flex h-1.5 shrink-0 cursor-row-resize items-center outline-none"
          onPointerEnter={() => setResultResizeHovering(true)}
          onPointerLeave={() => setResultResizeHovering(false)}
          onFocus={() => setResultResizeHovering(true)}
          onBlur={() => setResultResizeHovering(false)}
          onPointerDown={handleResultResizePointerDown}
          onKeyDown={handleResultResizeKeyDown}
        >
          <div className={`mx-auto w-full transition-[height,background-color] ${resultPanelResizing || resultResizeHovering ? 'h-1 bg-[var(--accent-9)]' : 'h-px bg-border'}`} />
        </div>

        <section
          className="min-h-0 shrink-0 bg-secondary/20"
          style={{ height: 'var(--code-runner-result-height)' }}
        >
          <div className="flex min-h-0 flex-col">
            <div className="flex h-10 shrink-0 items-center justify-between border-b bg-background px-4">
              <h2 className="text-sm font-semibold">Result</h2>
              <div className="text-xs text-muted-foreground">
                <RunMeta run={latestRun} running={runMutation.isPending} count={runs.length} />
              </div>
            </div>
            <div className="flex min-h-0 flex-1 flex-col overflow-auto">
              <RunOutput run={latestRun} running={runMutation.isPending} />
            </div>
          </div>
        </section>
      </main>
    </div>
  )
}

function CodeRunner() {
  const { sessionId } = useParams()
  return sessionId ? <CodeRunnerDetail /> : <CodeRunnerList />
}

export default CodeRunner
