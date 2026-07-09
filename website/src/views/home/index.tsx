import { useMutation, useQuery } from '@tanstack/react-query'
import type { FormEvent, ReactNode } from 'react'
import { lazy, Suspense, useState } from 'react'
import {
  ChevronDown,
  GitBranch,
  LoaderCircle,
  PanelsTopLeft,
  Plus,
  RefreshCw,
  Server,
  Settings,
  TerminalSquare,
} from 'lucide-react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { Button, buttonVariants } from 'src/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from 'src/components/ui/command'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from 'src/components/ui/dialog'
import { Input } from 'src/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from 'src/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from 'src/components/ui/select'
import { currentUser } from 'src/api/auth'
import { githubAppInstall, githubInstallations, githubRepositories, openRepository } from 'src/api/github'
import { deleteQiniuCredential, qiniuCredentialStatus, saveQiniuCredential } from 'src/api/qiniu'
import { connectSandbox, createSandbox, sandboxSessions } from 'src/api/sandboxes'
import { sandboxTemplates } from 'src/api/templates'
import { createWorkspace, workspaces as fetchWorkspaces } from 'src/api/workspaces'
import { cn } from 'src/lib/utils'
import { queryClient } from 'src/lib/query-client'

const TerminalPanel = lazy(() => import('src/components/TerminalPanel'))

type WorkspacePage = 'workspaces' | 'codebase' | 'credentials' | 'templates' | 'sandbox'

interface HomeProps {
  page: WorkspacePage
}

const pageCopy: Record<WorkspacePage, { title: string; description: string }> = {
  workspaces: {
    title: 'Workspaces',
    description: 'Create and reopen repository workspaces with their region, sandbox template, and state.',
  },
  codebase: {
    title: 'Codebases',
    description: 'Review repositories synced through the GitHub App and adjust repository access.',
  },
  credentials: {
    title: 'Credentials',
    description: 'Store Qiniu credentials used to create sandboxes and connect service integrations.',
  },
  templates: {
    title: 'Templates',
    description: 'Manage sandbox templates available for new development environments.',
  },
  sandbox: {
    title: 'Sandbox',
    description: 'Create sandboxes with the stored Qiniu API key, or reconnect existing instances.',
  },
}

const workspaceRegions = [
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

function sanitizeWorkspaceName(value: string) {
  return value.replace(/[^A-Za-z0-9_-]/g, '')
}

function workspaceNameFromRepository(fullName: string) {
  return fullName.trim().replace(/[^A-Za-z0-9_-]+/g, '-').replace(/^[-_]+|[-_]+$/g, '') || 'workspace'
}

function SandboxCreationOverlay({ repository }: { repository?: string }) {
  return (
    <div
      className="absolute inset-0 z-10 flex items-center justify-center bg-background/80 p-6 backdrop-blur-sm"
      role="status"
      aria-live="polite"
      aria-label="Creating sandbox"
    >
      <div className="flex w-full max-w-sm flex-col items-center rounded-md border bg-background px-5 py-6 text-center shadow-lg">
        <LoaderCircle className="h-8 w-8 animate-spin text-primary" />
        <h2 className="mt-4 text-base font-semibold">Creating sandbox</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          {repository
            ? `Mounting ${repository} and preparing the runtime.`
            : 'Preparing the runtime and workspace files.'}
        </p>
      </div>
    </div>
  )
}

function formatWorkspaceTime(value?: string) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

function metadataEntries(metadata?: Record<string, string>) {
  return Object.entries(metadata ?? {})
    .filter(([, value]) => Boolean(value))
    .sort(([left], [right]) => left.localeCompare(right))
}

function Home({ page }: HomeProps) {
  const navigate = useNavigate()
  const [credentials, setCredentials] = useState({
    sandboxAPIKey: '',
    maasAPIKey: '',
    accessKey: '',
    secretKey: '',
  })
  const [workspaceDialogOpen, setWorkspaceDialogOpen] = useState(false)
  const [repoPickerOpen, setRepoPickerOpen] = useState(false)
  const [selectedRepoID, setSelectedRepoID] = useState('')
  const [workspaceName, setWorkspaceName] = useState('')
  const [workspaceConfig, setWorkspaceConfig] = useState({
    region: workspaceRegions[0].endpoint,
    templateID: '',
  })
  const [terminalSandboxID, setTerminalSandboxID] = useState<string | null>(null)
  const [deleteCredentialsOpen, setDeleteCredentialsOpen] = useState(false)
  const { data, isLoading, isError } = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: currentUser,
    retry: false,
  })
  const installQuery = useQuery({
    queryKey: ['github', 'app-install'],
    queryFn: githubAppInstall,
    enabled: Boolean(data),
  })
  const installationsQuery = useQuery({
    queryKey: ['github', 'installations'],
    queryFn: githubInstallations,
    enabled: Boolean(data),
  })
  const reposQuery = useQuery({
    queryKey: ['github', 'repositories'],
    queryFn: githubRepositories,
    enabled: Boolean(data),
  })
  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: fetchWorkspaces,
    enabled: Boolean(data),
  })
  const qiniuQuery = useQuery({
    queryKey: ['qiniu', 'credentials'],
    queryFn: qiniuCredentialStatus,
    enabled: Boolean(data),
  })
  const qiniuStatusForTemplates = qiniuQuery.data?.data
  const templatesQuery = useQuery({
    queryKey: ['sandbox', 'templates', workspaceConfig.region],
    queryFn: () => sandboxTemplates(workspaceConfig.region),
    enabled: Boolean(data && qiniuStatusForTemplates?.configured),
  })
  const sandboxesQuery = useQuery({
    queryKey: ['sandboxes'],
    queryFn: sandboxSessions,
    enabled: Boolean(data),
  })
  const saveCredential = useMutation({
    mutationFn: saveQiniuCredential,
    onSuccess: () => {
      setCredentials({
        sandboxAPIKey: '',
        maasAPIKey: '',
        accessKey: '',
        secretKey: '',
      })
      void queryClient.invalidateQueries({ queryKey: ['qiniu', 'credentials'] })
      void queryClient.invalidateQueries({ queryKey: ['sandbox', 'templates'] })
    },
  })
  const deleteCredential = useMutation({
    mutationFn: deleteQiniuCredential,
    onSuccess: () => {
      setDeleteCredentialsOpen(false)
      void queryClient.invalidateQueries({ queryKey: ['qiniu', 'credentials'] })
      void queryClient.invalidateQueries({ queryKey: ['sandbox', 'templates'] })
    },
  })
  const createSandboxMutation = useMutation({
    mutationFn: () => createSandbox(),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
    },
  })
  const connectSandboxMutation = useMutation({
    mutationFn: connectSandbox,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
    },
  })
  const openRepositoryMutation = useMutation({
    mutationFn: openRepository,
    onSuccess: () => {
      setWorkspaceDialogOpen(false)
      setRepoPickerOpen(false)
      void queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
      void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
  })
  const createWorkspaceMutation = useMutation({
    mutationFn: createWorkspace,
    onSuccess: () => {
      setWorkspaceDialogOpen(false)
      setRepoPickerOpen(false)
      setWorkspaceName('')
      setSelectedRepoID('')
      void queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
      void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
  })

  if (isLoading) {
    return <div className="p-6 text-sm text-muted-foreground">Loading workspace...</div>
  }

  if (isError || !data) {
    return <Navigate to="/login" replace />
  }

  const copy = pageCopy[page]
  const installURL = installQuery.data?.data.url
  const installations = installationsQuery.data?.data.installations ?? []
  const repos = reposQuery.data?.data.repositories ?? []
  const selectedRepo = repos.find((repo) => repo.id === selectedRepoID)
  const creatingWorkspace = openRepositoryMutation.isPending || createWorkspaceMutation.isPending
  const selectedRegion = workspaceRegions.find((region) => region.endpoint === workspaceConfig.region) ?? workspaceRegions[0]
  const workspaceRows = workspacesQuery.data?.data.workspaces ?? []
  const hasGitHubInstallation = installations.length > 0 || repos.length > 0
  const qiniuStatus = qiniuQuery.data?.data
  const templates = templatesQuery.data?.data.templates ?? []
  const defaultTemplateID = templatesQuery.data?.data.default_template_id || ''
  const selectedTemplate =
    templates.find((template) => template.template_id === workspaceConfig.templateID) ??
    templates.find((template) => template.template_id === defaultTemplateID) ??
    templates[0]
  const selectedTemplateID = selectedTemplate?.template_id || ''
  const sandboxes = sandboxesQuery.data?.data.sandboxes ?? []
  const templateResources = (template: (typeof templates)[number]) =>
    template.cpu_count || template.memory_mb || template.disk_size_mb
      ? `${template.cpu_count || '-'} CPU / ${template.memory_mb || '-'} MB / ${template.disk_size_mb || '-'} MB`
      : '-'

  function handleCredentialSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    saveCredential.mutate({
      sandbox_api_key: credentials.sandboxAPIKey,
      maas_api_key: credentials.maasAPIKey,
      access_key: credentials.accessKey,
      secret_key: credentials.secretKey,
    })
  }

  function updateCredential(field: keyof typeof credentials, value: string) {
    setCredentials((current) => ({ ...current, [field]: value }))
  }

  function handleWorkspaceSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const selectedRepo = repos.find((repo) => repo.id === selectedRepoID)
    const name = workspaceName.trim() || (selectedRepo ? workspaceNameFromRepository(selectedRepo.full_name) : '')
    const payload = {
      ...(name ? { name } : {}),
      region: workspaceConfig.region,
      template_id: selectedTemplateID,
    }
    if (!selectedRepo) {
      createWorkspaceMutation.mutate(payload)
      return
    }
    openRepositoryMutation.mutate({
      repositoryID: selectedRepo.id,
      ...payload,
    })
  }

	function handleRepositoryWorkspaceClick(repo: (typeof repos)[number]) {
		const workspace = workspaceRows.find((item) => item.github_repo_id === repo.github_repo_id)
		if (workspace) {
			navigate(`/workspaces/${workspace.id}`)
			return
		}
    setSelectedRepoID(repo.id)
    setWorkspaceName(workspaceNameFromRepository(repo.full_name))
    setRepoPickerOpen(false)
    setWorkspaceDialogOpen(true)
  }

  function credentialPlaceholder(fallback: string, configured?: boolean, hint?: string) {
    return configured && hint ? `Saved key ending in ${hint}` : fallback
  }

  function credentialHelp(configured?: boolean) {
    return configured
      ? 'Leave blank to keep the saved credential. Enter a new value to replace it.'
      : 'Enter a value to save this credential.'
  }

  const repositoryError = reposQuery.isError ? apiErrorMessage(reposQuery.error) : ''
  const workspaceError = workspacesQuery.isError ? apiErrorMessage(workspacesQuery.error) : ''

  const workspaceDialog = (
    <Dialog
      open={workspaceDialogOpen}
      onOpenChange={(open) => {
        setWorkspaceDialogOpen(open)
        if (!open) {
          setRepoPickerOpen(false)
        }
      }}
    >
      <DialogContent className="relative max-w-4xl gap-0 overflow-hidden rounded-md p-0 sm:max-w-4xl" showCloseButton={false}>
        {creatingWorkspace ? <SandboxCreationOverlay repository={selectedRepo?.full_name} /> : null}
        <form onSubmit={handleWorkspaceSubmit}>
          <DialogHeader className="border-b px-5 py-4">
            <DialogTitle>Create workspace</DialogTitle>
          </DialogHeader>
          <div>
            {!qiniuQuery.isLoading && !qiniuStatus?.configured ? (
              <div className="flex flex-col gap-2 border-b bg-secondary/40 px-5 py-4 text-sm sm:flex-row sm:items-center sm:justify-between">
                <span className="text-muted-foreground">Configure a Sandbox API Key before creating this workspace.</span>
                <Link className="font-medium text-foreground no-underline hover:underline" to="/credentials">
                  Configure API key
                </Link>
              </div>
            ) : null}
            {!installationsQuery.isLoading && !hasGitHubInstallation ? (
              <div className="flex flex-col gap-2 border-b bg-secondary/40 px-5 py-4 text-sm sm:flex-row sm:items-center sm:justify-between">
                <span className="text-muted-foreground">Configure GitHub App to choose repositories for new workspaces.</span>
                {installURL ? (
                  <a className="font-medium text-foreground no-underline hover:underline" href={installURL}>
                    Install GitHub App
                  </a>
                ) : null}
              </div>
            ) : null}
            <div className="flex flex-col gap-3 px-5 py-5 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <span className="text-sm font-semibold">Name</span>
                <p className="mt-1 text-sm text-muted-foreground">Use letters, numbers, underscores, or hyphens.</p>
              </div>
              <Input
                className="rounded-md sm:w-80"
                placeholder="workspace_name"
                inputMode="text"
                pattern="[A-Za-z0-9_-]*"
                value={workspaceName}
                onChange={(event) => setWorkspaceName(sanitizeWorkspaceName(event.target.value))}
              />
            </div>
            <div className="flex flex-col gap-3 border-t px-5 py-5 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <span className="text-sm font-semibold">Code repository</span>
                <p className="mt-1 text-sm text-muted-foreground">Optional repository to clone into this workspace.</p>
              </div>
              <Popover open={repoPickerOpen} onOpenChange={(open) => setRepoPickerOpen(open)}>
                <PopoverTrigger
                  render={
                    <Button
                      type="button"
                      variant="outline"
                      size="lg"
                      className="w-full justify-between rounded-md px-3 text-left sm:w-80"
                    />
                  }
                >
                  <span className="truncate">{selectedRepo?.full_name || 'No repository'}</span>
                  <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                </PopoverTrigger>
                <PopoverContent align="end" className="w-[min(24rem,calc(100vw-2rem))] gap-0 rounded-md p-0" sideOffset={2}>
                  <div className="border-b px-3 py-2.5">
                    <h3 className="text-sm font-medium">Select repository</h3>
                  </div>
                  <Command>
                    <CommandInput placeholder="Search repositories" />
                    <CommandList>
                      <CommandEmpty>No repositories found.</CommandEmpty>
                      <CommandGroup>
                        <CommandItem
                          data-checked={selectedRepoID === ''}
                          value="No repository"
                          onSelect={() => {
                            setSelectedRepoID('')
                            setRepoPickerOpen(false)
                          }}
                        >
                          No repository
                        </CommandItem>
                        {repos.map((repo) => (
                          <CommandItem
                            key={repo.id}
                            data-checked={selectedRepoID === repo.id}
                            value={repo.full_name}
                            onSelect={() => {
                              setSelectedRepoID(repo.id)
                              setWorkspaceName((current) => current.trim() || workspaceNameFromRepository(repo.full_name))
                              setRepoPickerOpen(false)
                            }}
                          >
                            <span className="truncate">{repo.full_name}</span>
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
            </div>
            <div className="flex flex-col gap-3 border-t px-5 py-5 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <span className="block text-sm font-semibold">Region</span>
                <span className="mt-1 block text-sm text-muted-foreground">Your workspace will run in the selected region.</span>
              </div>
              <Select
                value={workspaceConfig.region}
                onValueChange={(value) => {
                  if (typeof value === 'string') {
                    setWorkspaceConfig((current) => ({ ...current, region: value, templateID: '' }))
                  }
                }}
              >
                <SelectTrigger size="lg" className="w-full rounded-md sm:w-80">
                  <SelectValue>{selectedRegion.label}</SelectValue>
                </SelectTrigger>
                <SelectContent align="end">
                  {workspaceRegions.map((region) => (
                    <SelectItem key={region.id} value={region.endpoint}>
                      {region.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-3 border-t px-5 py-5 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <h3 className="text-sm font-semibold">Sandbox template</h3>
                <p className="mt-1 text-sm text-muted-foreground">Template determines CPU, memory, image, and tools.</p>
              </div>
              <div className="flex w-full flex-col gap-2 sm:w-80">
                <Select
                  value={selectedTemplateID}
                  onValueChange={(value) => {
                    if (typeof value === 'string') {
                      setWorkspaceConfig((current) => ({ ...current, templateID: value }))
                    }
                  }}
                >
                  <SelectTrigger size="lg" className="w-full rounded-md">
                    <SelectValue>{selectedTemplate?.aliases?.[0] || selectedTemplate?.template_id || 'Select template'}</SelectValue>
                  </SelectTrigger>
                  <SelectContent align="end">
                    {templates.map((template) => (
                      <SelectItem key={template.template_id} value={template.template_id}>
                        {template.aliases?.[0] || template.template_id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {templatesQuery.isLoading ? (
                  <span className="text-xs text-muted-foreground">Loading templates...</span>
                ) : templatesQuery.isError ? (
                  <span className="text-xs text-destructive">Failed to load templates.</span>
                ) : selectedTemplate ? (
                  <span className="truncate text-xs text-muted-foreground">{templateResources(selectedTemplate)}</span>
                ) : (
                  <span className="text-xs text-muted-foreground">No templates available.</span>
                )}
              </div>
            </div>
          </div>
          <DialogFooter className="mx-0 mb-0 rounded-b-md px-5 py-4">
            <Button
              type="button"
              variant="outline"
              size="lg"
              disabled={openRepositoryMutation.isPending || createWorkspaceMutation.isPending}
              onClick={() => {
                setWorkspaceDialogOpen(false)
                setRepoPickerOpen(false)
              }}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="lg"
              disabled={openRepositoryMutation.isPending || createWorkspaceMutation.isPending || !qiniuStatus?.configured || !selectedTemplateID}
            >
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )

  const workspacesPanel = (
    <>
      {!qiniuQuery.isLoading && !qiniuStatus?.configured ? (
        <section className="mb-4 flex flex-col gap-3 rounded-md border p-5 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold">Sandbox API Key required</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Configure a Qiniu Sandbox API Key before creating repository workspaces.
            </p>
          </div>
          <Link
            className={cn(buttonVariants({ size: 'lg' }), 'w-fit no-underline')}
            to="/credentials"
          >
            Configure API key
          </Link>
        </section>
      ) : null}
      <section className="rounded-md border">
        <div className="flex items-center justify-between border-b px-5 py-3">
          <h2 className="text-sm font-semibold">Configured workspaces</h2>
          <span className="text-xs text-muted-foreground">{workspaceRows.length} workspaces</span>
        </div>
        {workspaceError ? (
          <div className="border-b bg-destructive/10 px-5 py-3 text-sm text-destructive">{workspaceError}</div>
        ) : null}
        {workspaceRows.length > 0 ? (
          <div className="divide-y">
            {workspaceRows.map((workspace) => (
              <Link
                key={workspace.id}
                className="flex flex-col gap-3 px-5 py-3 text-sm text-foreground no-underline transition-colors hover:bg-secondary/70 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 sm:flex-row sm:items-start sm:justify-between"
                to={`/workspaces/${workspace.id}`}
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{workspace.name || workspace.repo_full_name || workspace.sandbox_id || 'Workspace'}</span>
                  </div>
                  {workspace.repo_full_name ? (
                    <p className="mt-1 truncate text-xs text-muted-foreground">{workspace.repo_full_name}</p>
                  ) : null}
                  {workspace.workspace_path ? (
                    <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{workspace.workspace_path}</p>
                  ) : null}
                </div>
                <div className="grid gap-1 text-xs text-muted-foreground sm:min-w-56 sm:text-right">
                  <span>Created {formatWorkspaceTime(workspace.created_at)}</span>
                  <span>Updated {formatWorkspaceTime(workspace.updated_at)}</span>
                </div>
              </Link>
            ))}
          </div>
        ) : (
          <div className="flex flex-col gap-3 p-5 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-3">
              <TerminalSquare className="h-4 w-4 shrink-0" />
              <span>
                {workspacesQuery.isLoading
                  ? 'Loading workspaces...'
                  : 'No workspaces yet. Create one now.'}
              </span>
            </div>
          </div>
        )}
      </section>
    </>
  )

  const codebasePanel = (
    <section className="rounded-md border">
      <div className="flex items-center justify-between border-b px-5 py-3">
        <h2 className="text-sm font-semibold">GitHub repositories</h2>
        <span className="text-xs text-muted-foreground">{repos.length} repositories</span>
      </div>
      {repositoryError ? (
        <div className="border-b bg-destructive/10 px-5 py-3 text-sm text-destructive">{repositoryError}</div>
      ) : null}
      {repos.length > 0 ? (
        <div className="divide-y">
          {repos.map((repo) => {
            const workspace = workspaceRows.find((item) => item.github_repo_id === repo.github_repo_id)
            const RepoActionIcon = workspace ? PanelsTopLeft : Plus
            return (
              <div key={repo.id} className="flex flex-col gap-3 px-5 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{repo.full_name}</span>
                    {repo.private ? (
                      <span className="rounded-md border px-2 py-0.5 text-xs text-muted-foreground">Private</span>
                    ) : null}
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">{repo.default_branch || 'No default branch'}</p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="text-muted-foreground hover:text-foreground"
                  aria-label={workspace ? `Open workspace for ${repo.full_name}` : `Create workspace for ${repo.full_name}`}
                  title={workspace ? 'Open workspace' : 'Create workspace'}
                  onClick={() => handleRepositoryWorkspaceClick(repo)}
                >
                  <RepoActionIcon className="h-4 w-4" />
                </Button>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="flex flex-col gap-3 p-5 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <GitBranch className="h-4 w-4 shrink-0" />
            <span>
              {reposQuery.isLoading
                ? 'Loading repositories...'
                : hasGitHubInstallation
                  ? 'No repositories synced. Configure repository access, then refresh.'
                  : 'Install or configure the GitHub App to sync repositories.'}
            </span>
          </div>
          {installURL ? (
            <a
              className={cn(buttonVariants({ size: 'lg' }), 'w-fit no-underline')}
              href={installURL}
            >
              {hasGitHubInstallation ? 'Configure app' : 'Install app'}
            </a>
          ) : null}
        </div>
      )}
    </section>
  )

  const apiKeyPanel = (
    <form className="space-y-4" onSubmit={handleCredentialSubmit}>
      <section className="rounded-md border p-5">
        <div className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold">Qiniu Sandbox API Key</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Required for creating and connecting sandbox instances. Get it from{' '}
              <a
                className="font-medium text-foreground underline-offset-4 hover:underline"
                href="https://portal.qiniu.com/developer/user/api-key"
                target="_blank"
                rel="noreferrer"
              >
                Qiniu Sandbox API Key
              </a>
              .
            </p>
          </div>
          <span className="text-xs text-muted-foreground">
            {qiniuStatus?.configured ? 'Configured' : 'Required'}
          </span>
        </div>
        <label className="mt-4 grid gap-2">
          <span className="sr-only">Sandbox API Key</span>
          <Input
            className="rounded-md"
            placeholder={credentialPlaceholder('Sandbox API key', qiniuStatus?.configured, qiniuStatus?.key_hint)}
            type="password"
            value={credentials.sandboxAPIKey}
            onChange={(event) => updateCredential('sandboxAPIKey', event.target.value)}
          />
          <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.configured)}</span>
        </label>
      </section>

      <section className="rounded-md border p-5">
        <div className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold">Qiniu MAAS API Key</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Optional credential for Qiniu MAAS service integrations. Get it from{' '}
              <a
                className="font-medium text-foreground underline-offset-4 hover:underline"
                href="https://portal.qiniu.com/ai-inference/api-key"
                target="_blank"
                rel="noreferrer"
              >
                Qiniu MAAS API Key
              </a>
              .
            </p>
          </div>
          <span className="text-xs text-muted-foreground">
            {qiniuStatus?.maas_configured ? 'Configured' : 'Optional'}
          </span>
        </div>
        <label className="mt-4 grid gap-2">
          <span className="sr-only">MAAS API Key</span>
          <Input
            className="rounded-md"
            placeholder={credentialPlaceholder('MAAS API key', qiniuStatus?.maas_configured, qiniuStatus?.maas_key_hint)}
            type="password"
            value={credentials.maasAPIKey}
            onChange={(event) => updateCredential('maasAPIKey', event.target.value)}
          />
          <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.maas_configured)}</span>
        </label>
      </section>

      <section className="rounded-md border p-5">
        <div className="border-b pb-4">
          <h2 className="text-lg font-semibold">Qiniu Access Key / Secret Key</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Optional AK/SK pair for Qiniu account-level service integrations. Get them from{' '}
            <a
              className="font-medium text-foreground underline-offset-4 hover:underline"
              href="https://portal.qiniu.com/developer/user/key"
              target="_blank"
              rel="noreferrer"
            >
              Qiniu AccessKey management
            </a>
            .
          </p>
        </div>
        <div className="grid gap-4 pt-4 md:grid-cols-2">
          <label className="grid gap-2">
            <span className="flex items-center justify-between gap-3 text-sm font-medium">
              Access Key
              <span className="text-xs font-normal text-muted-foreground">
                {qiniuStatus?.access_key_configured ? 'Configured' : 'Optional'}
              </span>
            </span>
            <Input
              className="rounded-md"
              placeholder={credentialPlaceholder('AK', qiniuStatus?.access_key_configured, qiniuStatus?.access_key_hint)}
              type="password"
              value={credentials.accessKey}
              onChange={(event) => updateCredential('accessKey', event.target.value)}
            />
            <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.access_key_configured)}</span>
          </label>
          <label className="grid gap-2">
            <span className="flex items-center justify-between gap-3 text-sm font-medium">
              Secret Key
              <span className="text-xs font-normal text-muted-foreground">
                {qiniuStatus?.secret_key_configured ? 'Configured' : 'Optional'}
              </span>
            </span>
            <Input
              className="rounded-md"
              placeholder={credentialPlaceholder('SK', qiniuStatus?.secret_key_configured, qiniuStatus?.secret_key_hint)}
              type="password"
              value={credentials.secretKey}
              onChange={(event) => updateCredential('secretKey', event.target.value)}
            />
            <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.secret_key_configured)}</span>
          </label>
        </div>
      </section>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="submit"
          size="lg"
          disabled={(!qiniuStatus?.configured && !credentials.sandboxAPIKey) || saveCredential.isPending}
        >
          Save all credentials
        </Button>
        {qiniuStatus?.configured ? (
          <Button
            type="button"
            variant="outline"
            size="lg"
            disabled={deleteCredential.isPending}
            onClick={() => setDeleteCredentialsOpen(true)}
          >
            Delete stored credentials
          </Button>
        ) : null}
      </div>
      <Dialog open={deleteCredentialsOpen} onOpenChange={setDeleteCredentialsOpen}>
        <DialogContent className="max-w-md rounded-md">
          <DialogHeader>
            <DialogTitle>
              Delete stored credentials?
            </DialogTitle>
            <DialogDescription>
              This will remove all saved Qiniu credentials for this account. Sandbox creation and integrations will stop
              working until credentials are saved again.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={deleteCredential.isPending}
              onClick={() => setDeleteCredentialsOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteCredential.isPending}
              onClick={() => deleteCredential.mutate()}
            >
              Delete credentials
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </form>
  )

  const templatesPanel = (
    <section className="rounded-md border">
      {qiniuQuery.isLoading ? (
        <div className="p-5 text-sm text-muted-foreground">Checking Sandbox API Key...</div>
      ) : !qiniuStatus?.configured ? (
        <div className="flex flex-col gap-3 p-5 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold">Sandbox API Key required</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Configure a Qiniu Sandbox API Key before loading sandbox templates.
            </p>
          </div>
          <Link
            className={cn(buttonVariants({ size: 'lg' }), 'w-fit no-underline')}
            to="/credentials"
          >
            Configure credentials
          </Link>
        </div>
      ) : templatesQuery.isLoading ? (
        <div className="p-5 text-sm text-muted-foreground">Loading templates...</div>
      ) : templatesQuery.isError ? (
        <div className="p-5 text-sm text-muted-foreground">Failed to load templates. Check the Sandbox API Key and try again.</div>
      ) : templates.length > 0 ? (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="border-b bg-secondary/40 text-xs font-medium uppercase text-muted-foreground">
              <tr>
                <th className="px-5 py-3">Aliases</th>
                <th className="px-5 py-3">Template ID</th>
                <th className="px-5 py-3">Resources</th>
                <th className="px-5 py-3">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {templates.map((template) => (
                <tr key={template.template_id} className="align-top">
                  <td className="max-w-[260px] px-5 py-4">
                    {template.aliases?.length ? (
                      <div className="flex flex-wrap gap-1.5">
                        {template.aliases.map((alias) => (
                          <span key={alias} className="rounded-md bg-secondary px-2 py-1 font-medium text-foreground">
                            {alias}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground">No aliases</span>
                    )}
                  </td>
                  <td className="max-w-[260px] px-5 py-4 font-mono text-xs text-muted-foreground">
                    <span className="block truncate">{template.template_id}</span>
                  </td>
                  <td className="whitespace-nowrap px-5 py-4 text-muted-foreground">{templateResources(template)}</td>
                  <td className="px-5 py-4">
                    <div className="flex flex-wrap gap-1.5">
                      {template.build_status ? (
                        <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">{template.build_status}</span>
                      ) : null}
                      {template.default ? (
                        <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">Default</span>
                      ) : null}
                      {template.public ? (
                        <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">Public</span>
                      ) : null}
                      {!template.build_status && !template.default && !template.public ? (
                        <span className="text-muted-foreground">-</span>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="flex items-center gap-3 p-5 text-sm text-muted-foreground">
          <Server className="h-4 w-4" />
          <span>No templates found for this Sandbox API Key.</span>
        </div>
      )}
    </section>
  )

  const sandboxPanel = (
    <section className="rounded-md border">
      <div className="flex flex-col gap-3 border-b px-5 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold">Sandbox sessions</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Create a sandbox with the stored Qiniu API key, or reconnect an existing one.
          </p>
        </div>
        <Button
          type="button"
          size="lg"
          disabled={!qiniuStatus?.configured || createSandboxMutation.isPending}
          onClick={() => createSandboxMutation.mutate()}
        >
          Create sandbox
        </Button>
      </div>
      {sandboxes.length > 0 ? (
        <div className="divide-y">
          {sandboxes.map((sandbox) => (
            <div key={sandbox.id} className="flex flex-col gap-3 px-5 py-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p className="text-sm font-medium">{sandbox.sandbox_id}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {sandbox.repo_full_name || sandbox.template_id} · {sandbox.state}
                </p>
                {sandbox.region || sandbox.cpu_count || sandbox.memory_gb ? (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {[sandbox.region, sandbox.cpu_count ? `${sandbox.cpu_count} CPU` : '', sandbox.memory_gb ? `${sandbox.memory_gb}G` : '']
                      .filter(Boolean)
                      .join(' · ')}
                  </p>
                ) : null}
                {metadataEntries(sandbox.metadata).length > 0 ? (
                  <div className="mt-2 flex max-w-3xl flex-wrap gap-1.5">
                    {metadataEntries(sandbox.metadata).map(([key, value]) => (
                      <span key={key} className="rounded-md border bg-secondary/30 px-2 py-1 text-xs text-muted-foreground">
                        <span className="font-medium text-foreground">{key}</span>: {value}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
              <div className="flex items-center gap-2">
                {sandbox.ide_url ? (
                  <a
                    className={cn(buttonVariants({ variant: 'outline' }), 'text-muted-foreground no-underline hover:text-foreground')}
                    href={sandbox.ide_url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    IDE
                  </a>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setTerminalSandboxID(sandbox.sandbox_id)}
                >
                  Terminal
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={connectSandboxMutation.isPending}
                  onClick={() => connectSandboxMutation.mutate(sandbox.sandbox_id)}
                >
                  Connect
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex items-center gap-3 p-5 text-sm text-muted-foreground">
          <Server className="h-4 w-4" />
          No sandboxes yet.
        </div>
      )}
      {terminalSandboxID ? (
        <div className="border-t p-5">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold">Terminal {terminalSandboxID}</h3>
            <Button
              type="button"
              variant="outline"
              onClick={() => setTerminalSandboxID(null)}
            >
              Close
            </Button>
          </div>
          <Suspense fallback={<div className="h-72 rounded-md border p-4 text-sm text-muted-foreground">Loading terminal...</div>}>
            <TerminalPanel sandboxID={terminalSandboxID} />
          </Suspense>
        </div>
      ) : null}
    </section>
  )

  const panels: Record<WorkspacePage, ReactNode> = {
    workspaces: workspacesPanel,
    codebase: codebasePanel,
    credentials: apiKeyPanel,
    templates: templatesPanel,
    sandbox: sandboxPanel,
  }
  const pageActions =
    page === 'workspaces' ? (
      <Button
        type="button"
        size="lg"
        className="w-fit"
        disabled={qiniuQuery.isLoading || !qiniuStatus?.configured || openRepositoryMutation.isPending || createWorkspaceMutation.isPending}
        onClick={() => {
          setSelectedRepoID('')
          setWorkspaceName('')
          setRepoPickerOpen(false)
          setWorkspaceDialogOpen(true)
        }}
      >
        New workspace
      </Button>
    ) : page === 'codebase' ? (
      <div className="flex flex-wrap items-center gap-2">
        {installURL ? (
          <a
            className={cn(buttonVariants({ size: 'lg' }), 'gap-2 no-underline')}
            href={installURL}
          >
            {hasGitHubInstallation ? <Settings className="h-4 w-4" /> : <GitBranch className="h-4 w-4" />}
            {hasGitHubInstallation ? 'Configure app' : 'Install app'}
          </a>
        ) : null}
        <Button
          type="button"
          variant="outline"
          size="lg"
          disabled={reposQuery.isFetching}
          onClick={() => void queryClient.invalidateQueries({ queryKey: ['github', 'repositories'] })}
        >
          <RefreshCw className="h-4 w-4" />
          Refresh
        </Button>
      </div>
    ) : null

  return (
    <div className="mx-auto max-w-6xl px-6 py-8">
      <section className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 className="text-3xl font-semibold tracking-normal">{copy.title}</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{copy.description}</p>
        </div>
        {pageActions}
      </section>

      {panels[page]}
      {workspaceDialog}
    </div>
  )
}

export default Home
