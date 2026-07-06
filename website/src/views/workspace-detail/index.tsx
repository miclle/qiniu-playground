import { useMutation, useQuery } from '@tanstack/react-query'
import type { AxiosError } from 'axios'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import {
  AlertTriangle,
  ArrowLeft,
  Bot,
  CheckCircle2,
  ExternalLink,
  FolderTree,
  GitBranch,
  PanelsTopLeft,
  Plus,
  Rocket,
  Settings,
  SquareTerminal,
  X,
} from 'lucide-react'
import { Link, useParams } from 'react-router-dom'

import { connectWorkspace, workspaces as fetchWorkspaces } from 'src/api/workspaces'
import type { Workspace } from 'src/api/workspaces'
import { Button, buttonVariants } from 'src/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from 'src/components/ui/tabs'
import { WorkspaceFileBrowser } from 'src/components/WorkspaceFileBrowser'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from 'src/components/ui/dialog'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from 'src/components/ui/sheet'
import { cn } from 'src/lib/utils'
import { queryClient } from 'src/lib/query-client'

const TerminalPanel = lazy(() => import('src/components/TerminalPanel'))

type WorkbenchTab = string

interface TerminalSession {
  id: string
  label: string
  opened: boolean
}

function initialTerminalSessions(): TerminalSession[] {
  return [{ id: 'terminal-1', label: 'Terminal', opened: false }]
}

function githubRepositoryURL(fullName?: string) {
  return fullName ? `https://github.com/${fullName}` : ''
}

function workspaceTitle(workspace: Workspace) {
  return workspace.name || workspace.repo_full_name || workspace.sandbox_id || 'Workspace'
}

function metadata(workspace: Workspace) {
  return {
    id: workspace.id,
    name: workspace.name || null,
    repo: workspace.repo_full_name || null,
    region: workspace.region,
    sandbox: workspace.sandbox_id || null,
    template: workspace.template_id,
    path: workspace.workspace_path || null,
  }
}

function DetailRow({ label, value }: { label: string; value?: string | number | null }) {
  return (
    <div className="grid grid-cols-[96px_1fr] gap-3 border-b px-4 py-3 text-sm last:border-b-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate font-medium">{value || '-'}</span>
    </div>
  )
}

function isMissingSandboxError(error: unknown) {
  return (error as AxiosError | undefined)?.response?.status === 409
}

function connectionErrorMessage(error: unknown) {
  const axiosError = error as AxiosError | undefined
  const data = axiosError?.response?.data
  if (typeof data === 'string' && data.trim()) {
    return data.trim()
  }
  if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
    return data.error
  }
  return axiosError?.message || 'Unable to connect to this workspace.'
}

function WorkspaceDetail() {
  const { workspaceId } = useParams()
  const [dismissedMissingWorkspaceID, setDismissedMissingWorkspaceID] = useState('')
  const [workbenchTab, setWorkbenchTab] = useState<WorkbenchTab>('files')
  const [terminalSessions, setTerminalSessions] = useState<TerminalSession[]>(initialTerminalSessions)
  const [nextTerminalNumber, setNextTerminalNumber] = useState(2)
  const previousWorkspaceIDRef = useRef<string | undefined>(undefined)
  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: fetchWorkspaces,
    retry: false,
  })
  const workspace = workspacesQuery.data?.data.workspaces.find((item) => item.id === workspaceId)
  const updateWorkspaceCache = (updatedWorkspace: Workspace) => {
    queryClient.setQueryData<Awaited<ReturnType<typeof fetchWorkspaces>>>(['workspaces'], (current) => {
      if (!current) {
        return current
      }
      return {
        ...current,
        data: {
          ...current.data,
          workspaces: current.data.workspaces.map((item) => (
            item.id === updatedWorkspace.id ? updatedWorkspace : item
          )),
        },
      }
    })
  }
  const connectWorkspaceMutation = useMutation({
    mutationFn: ({ recreate = false }: { recreate?: boolean } = {}) => {
      if (!workspace?.id) {
        throw new Error('workspace id is required')
      }
      return connectWorkspace(workspace.id, recreate ? { recreate: true } : undefined)
    },
    onSuccess: (response) => {
      setDismissedMissingWorkspaceID('')
      updateWorkspaceCache(response.data)
    },
  })
  const connectedWorkspace = connectWorkspaceMutation.data?.data

  useEffect(() => {
    if (previousWorkspaceIDRef.current === workspaceId) {
      return
    }
    previousWorkspaceIDRef.current = workspaceId
    connectWorkspaceMutation.reset()
    setDismissedMissingWorkspaceID('')
    setWorkbenchTab('files')
    setTerminalSessions(initialTerminalSessions())
    setNextTerminalNumber(2)
  }, [connectWorkspaceMutation.reset, workspaceId])

  const handleWorkbenchTabChange = (value: any) => {
    const nextTab = typeof value === 'string' ? value : 'files'
    setWorkbenchTab(nextTab)
    if (nextTab.startsWith('terminal-')) {
      setTerminalSessions((current) => current.map((session) => (
        session.id === nextTab ? { ...session, opened: true } : session
      )))
    }
  }

  const openNewTerminal = () => {
    const terminalNumber = nextTerminalNumber
    const nextSession: TerminalSession = {
      id: `terminal-${terminalNumber}`,
      label: `Terminal ${terminalNumber}`,
      opened: true,
    }
    setTerminalSessions((current) => [...current, nextSession])
    setNextTerminalNumber((value) => value + 1)
    setWorkbenchTab(nextSession.id)
  }

  const closeTerminal = (sessionID: string) => {
    const closingIndex = terminalSessions.findIndex((session) => session.id === sessionID)
    if (closingIndex === -1) {
      return
    }
    const nextSessions = terminalSessions.filter((session) => session.id !== sessionID)
    setTerminalSessions(nextSessions)
    if (workbenchTab === sessionID) {
      const fallbackSession = nextSessions[Math.min(closingIndex, nextSessions.length - 1)]
      setWorkbenchTab(fallbackSession?.id ?? 'files')
    }
  }

  useEffect(() => {
    if (
      !workspace?.id ||
      connectedWorkspace?.id === workspace.id ||
      connectWorkspaceMutation.error ||
      connectWorkspaceMutation.isPending
    ) {
      return
    }
    connectWorkspaceMutation.mutate({})
  }, [
    connectWorkspaceMutation.error,
    connectWorkspaceMutation.isPending,
    connectWorkspaceMutation.mutate,
    connectedWorkspace?.id,
    workspace?.id,
  ])

  if (workspacesQuery.isLoading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading workspace...</div>
  }

  if (workspacesQuery.isError) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <section className="w-full max-w-md rounded-md border p-6 text-center">
          <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full border bg-destructive/10 text-destructive">
            <AlertTriangle className="h-5 w-5" />
          </div>
          <h1 className="mt-4 text-xl font-semibold">Failed to load workspaces</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {connectionErrorMessage(workspacesQuery.error)}
          </p>
          <Button type="button" className="mt-5" onClick={() => void workspacesQuery.refetch()}>
            Retry
          </Button>
        </section>
      </div>
    )
  }

  if (!workspace) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <section className="w-full max-w-md rounded-md border p-6">
          <PanelsTopLeft className="h-8 w-8 text-muted-foreground" />
          <h1 className="mt-4 text-xl font-semibold">Workspace not found</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            This workspace may have been removed or belongs to another account.
          </p>
          <Link className={cn(buttonVariants({ variant: 'outline' }), 'mt-5 no-underline')} to="/workspaces">
            <ArrowLeft className="h-4 w-4" />
            Back to workspaces
          </Link>
        </section>
      </div>
    )
  }

  const currentWorkspace = connectedWorkspace ?? workspace
  const title = workspaceTitle(currentWorkspace)
  const repoURL = githubRepositoryURL(currentWorkspace.repo_full_name)
  const metadataJSON = JSON.stringify(metadata(currentWorkspace), null, 2)
  const reconnecting = connectWorkspaceMutation.isPending
  const connectError = reconnecting ? null : connectWorkspaceMutation.error
  const sandboxMissing = !connectWorkspaceMutation.data && isMissingSandboxError(connectError)
  const connectFailed = Boolean(connectError && !sandboxMissing)
  const canOpenIDE = Boolean(currentWorkspace.ide_url && !sandboxMissing && !connectFailed && !reconnecting)
  const missingSandboxLabel = workspace.sandbox_id || '-'
  const missingSandboxOpen = sandboxMissing && dismissedMissingWorkspaceID !== workspace.id

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-background">
      <Dialog
        open={missingSandboxOpen}
        onOpenChange={(open) => {
          if (!open) {
            setDismissedMissingWorkspaceID(workspace.id)
          }
        }}
      >
        <DialogContent className="max-w-md rounded-md" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="h-5 w-5 text-amber-600" />
              Sandbox unavailable
            </DialogTitle>
            <DialogDescription>
              The sandbox for this workspace no longer exists. You can create a new sandbox with the same workspace
              configuration and continue from a fresh runtime.
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md border bg-secondary/30 px-4 py-3 text-sm">
            <span className="text-muted-foreground">Missing sandbox</span>
            <p className="mt-1 truncate font-mono text-xs">{missingSandboxLabel}</p>
          </div>
          <DialogFooter>
            <DialogClose
              render={
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setDismissedMissingWorkspaceID(workspace.id)}
                  disabled={connectWorkspaceMutation.isPending}
                />
              }
            >
              Not now
            </DialogClose>
            <Button
              type="button"
              onClick={() => connectWorkspaceMutation.mutate({ recreate: true })}
              disabled={connectWorkspaceMutation.isPending}
            >
              {connectWorkspaceMutation.isPending ? 'Creating...' : 'Create new sandbox'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <header className="z-20 shrink-0 border-b bg-background/95 backdrop-blur">
        <div className="flex flex-col gap-3 px-5 py-3 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <Link
              className={cn(buttonVariants({ variant: 'outline', size: 'icon' }), 'no-underline')}
              to="/workspaces"
              aria-label="Back to workspaces"
            >
              <ArrowLeft className="h-4 w-4" />
            </Link>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="truncate text-lg font-semibold">{title}</h1>
              </div>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {repoURL ? (
              <a
                className={cn(buttonVariants({ variant: 'outline' }), 'no-underline')}
                href={repoURL}
                target="_blank"
                rel="noreferrer"
              >
                <GitBranch className="h-4 w-4" />
                Repository
              </a>
            ) : null}
            {canOpenIDE ? (
              <a
                className={cn(buttonVariants({ variant: 'default' }), 'no-underline')}
                href={currentWorkspace.ide_url}
                target="_blank"
                rel="noreferrer"
              >
                <ExternalLink className="h-4 w-4" />
                Open IDE
              </a>
            ) : (
              <Button type="button" disabled>
                <ExternalLink className="h-4 w-4" />
                Open IDE
              </Button>
            )}
            <Sheet>
              <SheetTrigger
                render={
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    aria-label="Workspace settings"
                  />
                }
              >
                <Settings className="h-4 w-4" />
              </SheetTrigger>
              <SheetContent>
                <SheetHeader>
                  <SheetTitle>Workspace metadata</SheetTitle>
                  <SheetDescription>Runtime details and launch readiness for {title}.</SheetDescription>
                </SheetHeader>
                <div className="flex-1 overflow-auto p-5">
                  <div className="mb-4 rounded-md border">
                    <DetailRow label="Region" value={currentWorkspace.region} />
                    <DetailRow label="Template" value={currentWorkspace.template_id} />
                    <DetailRow label="Sandbox" value={currentWorkspace.sandbox_id} />
                    <DetailRow label="Endpoint" value={currentWorkspace.endpoint} />
                  </div>
                  <pre className="overflow-auto rounded-md border bg-secondary/30 p-4 text-xs leading-6 text-foreground">
                    <code>{metadataJSON}</code>
                  </pre>
                  <div className="mt-4 rounded-md border">
                    <div className="flex items-center gap-2 border-b px-4 py-3">
                      <Rocket className="h-4 w-4 text-muted-foreground" />
                      <h3 className="text-sm font-semibold">Launch checklist</h3>
                    </div>
                    <div className="divide-y text-sm">
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-muted-foreground">IDE proxy</span>
                        <span className="font-medium">{currentWorkspace.ide_url ? 'available' : 'missing'}</span>
                      </div>
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-muted-foreground">Repository</span>
                        <span className="font-medium">{currentWorkspace.repo_full_name || 'scratch workspace'}</span>
                      </div>
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-muted-foreground">Region</span>
                        <span className="font-medium">{currentWorkspace.region}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </SheetContent>
            </Sheet>
          </div>
        </div>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden xl:grid-cols-[320px_minmax(480px,1fr)]">
        <section className="flex min-h-0 flex-col border-b xl:border-r xl:border-b-0">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <div className="flex items-center gap-2">
              <Bot className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold">Assistant</h2>
            </div>
            <span className="text-xs text-muted-foreground">Workspace context</span>
          </div>
          <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4">
            <div className="rounded-md border bg-secondary/40 p-4">
              <p className="text-sm font-medium">Ready to work in {title}</p>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                This view keeps the repository, sandbox, and launch targets in one place so the next action is obvious.
              </p>
            </div>
            <div className="space-y-3">
              {[
                ['Sandbox prepared', currentWorkspace.sandbox_id || 'Waiting for sandbox id'],
                ['Template selected', currentWorkspace.template_id],
                ['Workspace mounted', currentWorkspace.workspace_path || 'Path unavailable'],
              ].map(([label, value]) => (
                <div key={label} className="flex gap-3 rounded-md border p-3">
                  <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />
                  <div className="min-w-0">
                    <p className="text-sm font-medium">{label}</p>
                    <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{value}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="flex min-h-0 flex-col">
          <Tabs value={workbenchTab} onValueChange={handleWorkbenchTabChange} className="min-h-0 flex-1">
            <div className="flex shrink-0 items-center border-b bg-background">
              <TabsList className="border-b-0">
                <TabsTrigger value="files">
                  <FolderTree className="h-4 w-4" />
                  Files
                </TabsTrigger>
                {terminalSessions.map((session) => (
                  <div key={session.id} role="presentation" className="inline-flex h-9 shrink-0 items-center">
                    <TabsTrigger value={session.id} className="pr-1">
                      <SquareTerminal className="h-4 w-4" />
                      {session.label}
                    </TabsTrigger>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-xs"
                      className="-ml-1 h-7 w-7 text-muted-foreground hover:text-foreground"
                      aria-label={`Close ${session.label}`}
                      onClick={() => closeTerminal(session.id)}
                    >
                      <X className="h-3 w-3" />
                    </Button>
                  </div>
                ))}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="ml-1"
                  onClick={openNewTerminal}
                  aria-label="Open new terminal"
                >
                  <Plus className="h-4 w-4" />
                </Button>
              </TabsList>
            </div>
            <TabsContent value="files" keepMounted className="flex min-h-0 min-w-0">
              {sandboxMissing ? (
                <div className="flex h-full w-full items-center justify-center bg-background p-6 text-sm text-muted-foreground">
                  <div className="flex w-full max-w-md flex-col items-center gap-4 text-center">
                    <div className="flex h-10 w-10 items-center justify-center rounded-full border bg-amber-50 text-amber-700">
                      <AlertTriangle className="h-5 w-5" />
                    </div>
                    <div className="space-y-2">
                      <h3 className="text-base font-semibold text-foreground">Sandbox unavailable</h3>
                      <p>Create a new sandbox to continue working in this workspace.</p>
                    </div>
                    <div className="w-full rounded-md border bg-secondary/30 px-4 py-3 text-left">
                      <span className="text-muted-foreground">Missing sandbox</span>
                      <p className="mt-1 truncate font-mono text-xs text-foreground">{missingSandboxLabel}</p>
                    </div>
                    <Button
                      type="button"
                      onClick={() => connectWorkspaceMutation.mutate({ recreate: true })}
                      disabled={connectWorkspaceMutation.isPending}
                    >
                      {connectWorkspaceMutation.isPending ? 'Creating...' : 'Create new sandbox'}
                    </Button>
                  </div>
                </div>
              ) : connectFailed ? (
                <div className="flex h-full w-full items-center justify-center bg-background p-6 text-sm text-muted-foreground">
                  <div className="flex w-full max-w-md flex-col items-center gap-4 text-center">
                    <div className="flex h-10 w-10 items-center justify-center rounded-full border bg-amber-50 text-amber-700">
                      <AlertTriangle className="h-5 w-5" />
                    </div>
                    <div className="space-y-2">
                      <h3 className="text-base font-semibold text-foreground">Workspace connection failed</h3>
                      <p>{connectionErrorMessage(connectError)}</p>
                    </div>
                    <Button
                      type="button"
                      onClick={() => connectWorkspaceMutation.mutate({})}
                      disabled={connectWorkspaceMutation.isPending}
                    >
                      {connectWorkspaceMutation.isPending ? 'Retrying...' : 'Retry'}
                    </Button>
                  </div>
                </div>
              ) : (
                <WorkspaceFileBrowser
                  sandboxID={currentWorkspace.sandbox_id}
                  workspacePath={currentWorkspace.workspace_path}
                  disabled={reconnecting}
                  emptyMessage={reconnecting ? 'Checking sandbox...' : 'Preparing workspace files...'}
                />
              )}
            </TabsContent>
            {terminalSessions.map((session) => (
              <TabsContent key={session.id} value={session.id} keepMounted className="bg-[#0b0f14]">
                {sandboxMissing ? (
                  <div className="flex h-full items-center justify-center rounded-md border border-dashed bg-background p-6 text-center text-sm text-muted-foreground">
                    Create a new sandbox to open a command line.
                  </div>
                ) : connectFailed ? (
                  <div className="flex h-full items-center justify-center rounded-md border border-dashed bg-background p-6 text-center text-sm text-muted-foreground">
                    Reconnect the workspace before opening a command line.
                  </div>
                ) : currentWorkspace.sandbox_id && session.opened ? (
                  <Suspense
                    fallback={
                      <div className="flex h-full items-center justify-center bg-[#0b0f14] p-6 text-sm text-slate-300">
                        Loading terminal...
                      </div>
                    }
                  >
                    <TerminalPanel
                      sandboxID={currentWorkspace.sandbox_id}
                      workspacePath={currentWorkspace.workspace_path}
                      disabled={reconnecting}
                      active={workbenchTab === session.id}
                    />
                  </Suspense>
                ) : (
                  <div className="flex h-full items-center justify-center rounded-md border border-dashed bg-background p-6 text-center text-sm text-muted-foreground">
                    Waiting for sandbox...
                  </div>
                )}
              </TabsContent>
            ))}
          </Tabs>
        </section>

      </div>
    </div>
  )
}

export default WorkspaceDetail
