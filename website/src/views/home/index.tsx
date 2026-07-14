import { useMutation, useQuery } from '@tanstack/react-query'
import { Badge, Button, Card, Dialog, Flex, IconButton, Inset, Popover, Select, Table, TextField } from '@radix-ui/themes'
import type { FormEvent, KeyboardEvent, ReactNode } from 'react'
import { lazy, Suspense, useState } from 'react'
import {
  Check,
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
import { currentUser } from 'src/api/auth'
import { githubAppInstall, githubInstallations, githubRepositories, openRepository } from 'src/api/github'
import { deleteQiniuCredential, qiniuCredentialStatus, saveQiniuCredential } from 'src/api/qiniu'
import { connectSandbox, createSandbox, sandboxSessions } from 'src/api/sandboxes'
import { sandboxTemplates } from 'src/api/templates'
import { createWorkspace, workspaces as fetchWorkspaces } from 'src/api/workspaces'
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

function RepositoryPickerOption({
  selected,
  children,
  onSelect,
}: {
  selected: boolean
  children: ReactNode
  onSelect: () => void
}) {
  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return
    }
    event.preventDefault()
    onSelect()
  }

  return (
    <div
      role="option"
      aria-selected={selected}
      tabIndex={0}
      className={[
        'flex min-h-8 w-full cursor-pointer items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm text-foreground outline-none',
        'hover:bg-[var(--gray-3)] focus-visible:bg-[var(--gray-3)] focus-visible:ring-2 focus-visible:ring-ring/40',
        selected ? 'bg-accent text-accent-foreground' : '',
      ].join(' ')}
      onClick={onSelect}
      onKeyDown={handleKeyDown}
    >
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {selected ? <Check className="h-4 w-4 shrink-0 text-primary" /> : null}
    </div>
  )
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

function formatResourceSizeMB(value: number) {
  if (value >= 1024 && value % 1024 === 0) {
    return `${value / 1024} GB`
  }
  return `${value} MB`
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
  const [repoSearch, setRepoSearch] = useState('')
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
    queryKey: ['sandboxes', workspaceConfig.region],
    queryFn: () => sandboxSessions(workspaceConfig.region),
    enabled: Boolean(data && qiniuStatusForTemplates?.configured),
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
    mutationFn: () => createSandbox({ region: workspaceConfig.region }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sandboxes'] })
    },
  })
  const connectSandboxMutation = useMutation({
    mutationFn: (sandboxID: string) => connectSandbox(sandboxID, { region: workspaceConfig.region }),
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
  const normalizedRepoSearch = repoSearch.trim().toLowerCase()
  const filteredRepos = normalizedRepoSearch
    ? repos.filter((repo) => (repo.full_name || '').toLowerCase().includes(normalizedRepoSearch))
    : repos
  const creatingWorkspace = openRepositoryMutation.isPending || createWorkspaceMutation.isPending
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
  const sandboxResources = (sandbox: (typeof sandboxes)[number]) => {
    if (!sandbox.cpu_count && !sandbox.memory_gb && !sandbox.disk_size_mb) {
      return '-'
    }
    const memory = sandbox.memory_gb ? `${sandbox.memory_gb} GB` : '-'
    const disk = sandbox.disk_size_mb ? `${formatResourceSizeMB(sandbox.disk_size_mb)} disk` : '-'
    return `${sandbox.cpu_count || '-'} CPU / ${memory} / ${disk}`
  }
  const sandboxWorkspaceID = (sandbox: (typeof sandboxes)[number]) =>
    sandbox.metadata?.workspace_id ||
    workspaceRows.find((workspace) => workspace.sandbox_id && workspace.sandbox_id === sandbox.sandbox_id)?.id
  const sandboxWorkspaceLabel = (sandbox: (typeof sandboxes)[number]) =>
    sandbox.repo_full_name ||
    sandbox.metadata?.workspace_name ||
    workspaceRows.find((workspace) => workspace.sandbox_id && workspace.sandbox_id === sandbox.sandbox_id)?.name ||
    sandbox.template_id

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
    setWorkspaceName(workspaceNameFromRepository(repo.full_name || ''))
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
  const repositoryCountLabel = reposQuery.isLoading ? 'Loading...' : `${repos.length} repositories`

  const workspaceDialog = (
    <Dialog.Root
      open={workspaceDialogOpen}
      onOpenChange={(open) => {
        setWorkspaceDialogOpen(open)
        if (!open) {
          setRepoPickerOpen(false)
        }
      }}
    >
      <Dialog.Content size="2" maxWidth="520px" className="relative">
        {creatingWorkspace ? <SandboxCreationOverlay repository={selectedRepo?.full_name} /> : null}
        <form onSubmit={handleWorkspaceSubmit}>
          <Dialog.Title>Create workspace</Dialog.Title>
          <Dialog.Description size="2" mb="3" color="gray">
            Launch a sandbox from a repository, region, and template.
          </Dialog.Description>
          <Flex direction="column" gap="2" mb="3">
            {!qiniuQuery.isLoading && !qiniuStatus?.configured ? (
              <div className="flex flex-col gap-2 rounded-md bg-secondary/50 px-3 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between">
                <span className="text-muted-foreground">Configure a Sandbox API Key before creating this workspace.</span>
                <Link className="font-medium text-foreground no-underline hover:underline" to="/credentials">
                  Configure API key
                </Link>
              </div>
            ) : null}
            {!installationsQuery.isLoading && !hasGitHubInstallation ? (
              <div className="flex flex-col gap-2 rounded-md bg-secondary/50 px-3 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between">
                <span className="text-muted-foreground">Configure GitHub App to choose repositories for new workspaces.</span>
                {installURL ? (
                  <a className="font-medium text-foreground no-underline hover:underline" href={installURL}>
                    Install GitHub App
                  </a>
                ) : null}
              </div>
            ) : null}
          </Flex>
          <Inset side="x" my="3">
            <div className="divide-y border-y">
              <div className="grid gap-3 px-4 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-start">
                <label htmlFor="workspace-name">
                  <span className="text-sm font-semibold">Name</span>
                  <p className="mt-0.5 text-xs leading-5 text-muted-foreground">Use letters, numbers, underscores, or hyphens.</p>
                </label>
                <TextField.Root
                  id="workspace-name"
                  className="rounded-md"
                  placeholder="workspace_name"
                  inputMode="text"
                  pattern="[A-Za-z0-9_-]*"
                  value={workspaceName}
                  onChange={(event) => setWorkspaceName(sanitizeWorkspaceName(event.target.value))}
                />
              </div>
            <div className="grid gap-3 px-4 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-start">
              <div>
                <span className="text-sm font-semibold">Code repository</span>
                <p className="mt-0.5 text-xs leading-5 text-muted-foreground">Optional repository to clone into this workspace.</p>
              </div>
              <Popover.Root
                open={repoPickerOpen}
                onOpenChange={(open) => {
                  setRepoPickerOpen(open)
                  if (!open) {
                    setRepoSearch('')
                  }
                }}
              >
                <Popover.Trigger>
                  <Button
                    type="button"
                    variant="surface"
                    size="2"
                    color="gray"
                    className="repo-picker-trigger w-full justify-between"
                  >
                    <span className="truncate">{selectedRepo?.full_name || 'No repository'}</span>
                    <ChevronDown className="h-4 w-4 text-muted-foreground" />
                  </Button>
                </Popover.Trigger>
                <Popover.Content align="end" className="w-[min(22rem,calc(100vw-2rem))] p-0" sideOffset={4}>
                  <div className="px-3 py-2.5">
                    <h3 className="text-sm font-medium">Select repository</h3>
                  </div>
                  <div className="border-t border-b p-2">
                    <TextField.Root
                      aria-label="Search repositories"
                      placeholder="Search repositories"
                      size="2"
                      value={repoSearch}
                      onChange={(event) => setRepoSearch(event.target.value)}
                    />
                  </div>
                  <div className="max-h-72 overflow-y-auto p-1" role="listbox" aria-label="Repositories">
                    <RepositoryPickerOption
                      selected={selectedRepoID === ''}
                      onSelect={() => {
                        setSelectedRepoID('')
                        setRepoPickerOpen(false)
                        setRepoSearch('')
                      }}
                    >
                      No repository
                    </RepositoryPickerOption>
                    {filteredRepos.length ? (
                      filteredRepos.map((repo) => (
                        <RepositoryPickerOption
                          key={repo.id}
                          selected={selectedRepoID === repo.id}
                          onSelect={() => {
                            setSelectedRepoID(repo.id)
                            setWorkspaceName((current) => current.trim() || workspaceNameFromRepository(repo.full_name || ''))
                            setRepoPickerOpen(false)
                            setRepoSearch('')
                          }}
                        >
                          {repo.full_name}
                        </RepositoryPickerOption>
                      ))
                    ) : (
                      <div className="py-6 text-center text-sm text-muted-foreground" role="status">No repositories found.</div>
                    )}
                  </div>
                </Popover.Content>
              </Popover.Root>
            </div>
            <div className="grid gap-3 px-4 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-start">
              <div>
                <span className="block text-sm font-semibold">Region</span>
                <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">Your workspace will run in the selected region.</span>
              </div>
              <Select.Root
                value={workspaceConfig.region}
                onValueChange={(value) => {
                  if (typeof value === 'string') {
                    setWorkspaceConfig((current) => ({ ...current, region: value, templateID: '' }))
                  }
                }}
              >
                <Select.Trigger className="w-full rounded-md" />
                <Select.Content align="end">
                  {workspaceRegions.map((region) => (
                    <Select.Item key={region.id} value={region.endpoint}>
                      {region.label}
                    </Select.Item>
                  ))}
                </Select.Content>
              </Select.Root>
            </div>
            <div className="grid gap-3 px-4 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-start">
              <div>
                <h3 className="text-sm font-semibold">Sandbox template</h3>
                <p className="mt-0.5 text-xs leading-5 text-muted-foreground">Template determines CPU, memory, image, and tools.</p>
              </div>
              <div className="flex w-full flex-col gap-2">
                <Select.Root
                  value={selectedTemplateID}
                  onValueChange={(value) => {
                    if (typeof value === 'string') {
                      setWorkspaceConfig((current) => ({ ...current, templateID: value }))
                    }
                  }}
                >
                  <Select.Trigger className="w-full rounded-md" placeholder="Select template" />
                  <Select.Content align="end">
                    {templates.map((template) => (
                      <Select.Item key={template.template_id} value={template.template_id}>
                        {template.aliases?.[0] || template.template_id}
                      </Select.Item>
                    ))}
                  </Select.Content>
                </Select.Root>
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
          </Inset>
          <Flex gap="3" mt="4" justify="end">
            <Button
              type="button"
              variant="soft"
              color="gray"
              disabled={openRepositoryMutation.isPending || createWorkspaceMutation.isPending}
              onClick={() => {
                setRepoPickerOpen(false)
                setWorkspaceDialogOpen(false)
              }}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="2"
              disabled={openRepositoryMutation.isPending || createWorkspaceMutation.isPending || !qiniuStatus?.configured || !selectedTemplateID}
            >
              Create
            </Button>
          </Flex>
        </form>
      </Dialog.Content>
    </Dialog.Root>
  )

  const workspacesPanel = (
    <>
      {!qiniuQuery.isLoading && !qiniuStatus?.configured ? (
        <Card asChild size="2" className="mb-4">
          <section className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-base font-semibold">Sandbox API Key required</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Configure a Qiniu Sandbox API Key before creating repository workspaces.
              </p>
            </div>
            <Button asChild size="2" className="w-fit no-underline">
              <Link to="/credentials">Configure API key</Link>
            </Button>
          </section>
        </Card>
      ) : null}
      <Card asChild size="2">
        <section>
          <div className="flex items-center justify-between border-b pb-3">
            <h2 className="text-sm font-semibold">Configured workspaces</h2>
            <span className="text-xs text-muted-foreground">{workspaceRows.length} workspaces</span>
          </div>
          {workspaceError ? (
            <div className="mt-3 rounded-md bg-destructive/10 px-4 py-3 text-sm text-destructive">{workspaceError}</div>
          ) : null}
          {workspaceRows.length > 0 ? (
            <div className="mt-3 divide-y">
              {workspaceRows.map((workspace) => (
                <Link
                  key={workspace.id}
                  className="flex flex-col gap-3 rounded-sm px-2 py-3 text-sm text-foreground no-underline transition-colors hover:bg-secondary/70 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50 sm:flex-row sm:items-start sm:justify-between"
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
            <div className="flex flex-col gap-3 pt-4 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
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
      </Card>
    </>
  )

  const codebasePanel = (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold">GitHub repositories</h2>
          <p className="mt-1 text-xs text-muted-foreground">Repositories available for workspace creation.</p>
        </div>
        <span className="text-xs text-muted-foreground">{repositoryCountLabel}</span>
      </div>
      {repositoryError ? (
        <div className="rounded-md border bg-destructive/10 px-5 py-3 text-sm text-destructive">{repositoryError}</div>
      ) : null}
      {reposQuery.isLoading ? (
        <div className="overflow-x-auto" role="status" aria-label="Loading repositories">
          <Table.Root variant="surface" size="2" className="min-w-[760px]">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Repository</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Default branch</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Visibility</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell justify="end" aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {Array.from({ length: 5 }).map((_, index) => (
                <Table.Row key={index} className="align-middle">
                  <Table.RowHeaderCell>
                    <span className="block h-4 w-56 animate-pulse rounded-sm bg-secondary" />
                  </Table.RowHeaderCell>
                  <Table.Cell>
                    <span className="block h-4 w-28 animate-pulse rounded-sm bg-secondary" />
                  </Table.Cell>
                  <Table.Cell>
                    <span className="block h-5 w-16 animate-pulse rounded-sm bg-secondary" />
                  </Table.Cell>
                  <Table.Cell justify="end">
                    <span className="ml-auto block h-8 w-8 animate-pulse rounded-sm bg-secondary" />
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </div>
      ) : repos.length > 0 ? (
        <div className="overflow-x-auto">
          <Table.Root variant="surface" size="2" className="min-w-[760px]">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Repository</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Default branch</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Visibility</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell justify="end" aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {repos.map((repo) => {
                const workspace = workspaceRows.find((item) => item.github_repo_id === repo.github_repo_id)
                const RepoActionIcon = workspace ? PanelsTopLeft : Plus
                return (
                  <Table.Row key={repo.id} className="align-middle">
                    <Table.RowHeaderCell className="max-w-[360px]">
                      <span className="block truncate font-medium">{repo.full_name}</span>
                    </Table.RowHeaderCell>
                    <Table.Cell className="whitespace-nowrap text-muted-foreground">
                      {repo.default_branch || 'No default branch'}
                    </Table.Cell>
                    <Table.Cell>
                      <Badge color={repo.private ? 'gray' : 'green'} variant={repo.private ? 'surface' : 'soft'}>
                        {repo.private ? 'Private' : 'Public'}
                      </Badge>
                    </Table.Cell>
                    <Table.Cell justify="end">
                      <IconButton
                        type="button"
                        variant="outline"
                        color="gray"
                        size="2"
                        className="text-muted-foreground hover:text-foreground"
                        aria-label={workspace ? `Open workspace for ${repo.full_name || ''}` : `Create workspace for ${repo.full_name || ''}`}
                        title={workspace ? 'Open workspace' : 'Create workspace'}
                        onClick={() => handleRepositoryWorkspaceClick(repo)}
                      >
                        <RepoActionIcon className="h-4 w-4" />
                      </IconButton>
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table.Root>
        </div>
      ) : (
        <div className="flex flex-col gap-3 rounded-md border p-5 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
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
            <Button asChild size="2" className="w-fit no-underline">
              <a href={installURL}>{hasGitHubInstallation ? 'Configure app' : 'Install app'}</a>
            </Button>
          ) : null}
        </div>
      )}
    </section>
  )

  const apiKeyPanel = (
    <form className="space-y-4" onSubmit={handleCredentialSubmit}>
      <Card asChild size="2">
        <section>
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
          <Badge color={qiniuStatus?.configured ? 'green' : 'amber'} variant="soft">
            {qiniuStatus?.configured ? 'Configured' : 'Required'}
          </Badge>
        </div>
        <label className="mt-4 grid gap-2">
          <span className="sr-only">Sandbox API Key</span>
          <TextField.Root
            className="rounded-md"
            placeholder={credentialPlaceholder('Sandbox API key', qiniuStatus?.configured, qiniuStatus?.key_hint)}
            type="password"
            value={credentials.sandboxAPIKey}
            onChange={(event) => updateCredential('sandboxAPIKey', event.target.value)}
          />
          <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.configured)}</span>
        </label>
        </section>
      </Card>

      <Card asChild size="2">
        <section>
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
          <Badge color={qiniuStatus?.maas_configured ? 'green' : 'gray'} variant="soft">
            {qiniuStatus?.maas_configured ? 'Configured' : 'Optional'}
          </Badge>
        </div>
        <label className="mt-4 grid gap-2">
          <span className="sr-only">MAAS API Key</span>
          <TextField.Root
            className="rounded-md"
            placeholder={credentialPlaceholder('MAAS API key', qiniuStatus?.maas_configured, qiniuStatus?.maas_key_hint)}
            type="password"
            value={credentials.maasAPIKey}
            onChange={(event) => updateCredential('maasAPIKey', event.target.value)}
          />
          <span className="text-xs text-muted-foreground">{credentialHelp(qiniuStatus?.maas_configured)}</span>
        </label>
        </section>
      </Card>

      <Card asChild size="2">
        <section>
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
              <Badge color={qiniuStatus?.access_key_configured ? 'green' : 'gray'} variant="soft">
                {qiniuStatus?.access_key_configured ? 'Configured' : 'Optional'}
              </Badge>
            </span>
            <TextField.Root
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
              <Badge color={qiniuStatus?.secret_key_configured ? 'green' : 'gray'} variant="soft">
                {qiniuStatus?.secret_key_configured ? 'Configured' : 'Optional'}
              </Badge>
            </span>
            <TextField.Root
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
      </Card>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="submit"
          size="2"
          disabled={(!qiniuStatus?.configured && !credentials.sandboxAPIKey) || saveCredential.isPending}
        >
          Save all credentials
        </Button>
        {qiniuStatus?.configured ? (
          <Button
            type="button"
            variant="outline"
            size="2"
            color="gray"
            disabled={deleteCredential.isPending}
            onClick={() => setDeleteCredentialsOpen(true)}
          >
            Delete stored credentials
          </Button>
        ) : null}
      </div>
      <Dialog.Root open={deleteCredentialsOpen} onOpenChange={setDeleteCredentialsOpen}>
        <Dialog.Content size="2" maxWidth="450px">
          <Dialog.Title>Delete stored credentials?</Dialog.Title>
          <Dialog.Description size="2" mb="4" color="gray">
            This will remove all saved Qiniu credentials for this account. Sandbox creation and integrations will stop
            working until credentials are saved again.
          </Dialog.Description>
          <Flex gap="3" mt="4" justify="end">
            <Button
              type="button"
              variant="soft"
              color="gray"
              disabled={deleteCredential.isPending}
              onClick={() => setDeleteCredentialsOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="solid"
              color="red"
              disabled={deleteCredential.isPending}
              onClick={() => deleteCredential.mutate()}
            >
              Delete credentials
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
    </form>
  )

  const templatesPanel = (
    <section className="space-y-4">
      {qiniuQuery.isLoading ? (
        <div className="rounded-md border p-5 text-sm text-muted-foreground">Checking Sandbox API Key...</div>
      ) : !qiniuStatus?.configured ? (
        <div className="flex flex-col gap-3 rounded-md border p-5 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold">Sandbox API Key required</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              Configure a Qiniu Sandbox API Key before loading sandbox templates.
            </p>
          </div>
          <Button asChild size="2" className="w-fit no-underline">
            <Link to="/credentials">Configure credentials</Link>
          </Button>
        </div>
      ) : templatesQuery.isLoading ? (
        <div className="rounded-md border p-5 text-sm text-muted-foreground">Loading templates...</div>
      ) : templatesQuery.isError ? (
        <div className="rounded-md border p-5 text-sm text-muted-foreground">Failed to load templates. Check the Sandbox API Key and try again.</div>
      ) : (
        <>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 className="text-sm font-semibold">Template catalog</h2>
              <p className="mt-1 text-xs text-muted-foreground">Region-scoped templates available for new sandboxes.</p>
            </div>
            <Select.Root
              value={workspaceConfig.region}
              onValueChange={(value) =>
                setWorkspaceConfig((current) => ({ ...current, region: value ?? current.region, templateID: '' }))
              }
            >
              <Select.Trigger className="w-full sm:w-[190px]" />
              <Select.Content>
                {workspaceRegions.map((region) => (
                  <Select.Item key={region.id} value={region.endpoint}>
                    {region.label}
                  </Select.Item>
                ))}
              </Select.Content>
            </Select.Root>
          </div>
          {templates.length > 0 ? (
            <div className="overflow-x-auto">
              <Table.Root variant="surface" size="2" className="min-w-[760px]">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeaderCell>Aliases</Table.ColumnHeaderCell>
                    <Table.ColumnHeaderCell>Template ID</Table.ColumnHeaderCell>
                    <Table.ColumnHeaderCell>Resources</Table.ColumnHeaderCell>
                    <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {templates.map((template) => (
                    <Table.Row key={template.template_id} className="align-top">
                      <Table.RowHeaderCell className="max-w-[260px]">
                        {template.aliases?.length ? (
                          <div className="flex flex-wrap gap-1.5">
                            {template.aliases.map((alias) => (
                              <Badge key={alias} color="gray" variant="soft" highContrast>
                                {alias}
                              </Badge>
                            ))}
                          </div>
                        ) : (
                          <span className="text-muted-foreground">No aliases</span>
                        )}
                      </Table.RowHeaderCell>
                      <Table.Cell className="max-w-[260px] font-mono text-xs text-muted-foreground">
                        <span className="block truncate">{template.template_id}</span>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-muted-foreground">{templateResources(template)}</Table.Cell>
                      <Table.Cell>
                        <div className="flex flex-wrap gap-1.5">
                          {template.build_status ? (
                            <Badge color="gray" variant="surface">{template.build_status}</Badge>
                          ) : null}
                          {template.default ? (
                            <Badge color="blue" variant="soft">Default</Badge>
                          ) : null}
                          {template.public ? (
                            <Badge color="green" variant="soft">Public</Badge>
                          ) : null}
                          {!template.build_status && !template.default && !template.public ? (
                            <span className="text-muted-foreground">-</span>
                          ) : null}
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </div>
          ) : (
            <div className="flex items-center gap-3 rounded-md border p-5 text-sm text-muted-foreground">
              <Server className="h-4 w-4" />
              <span>No templates found for this Sandbox API Key.</span>
            </div>
          )}
        </>
      )}
    </section>
  )

  const sandboxPanel = (
    <section className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-lg font-semibold">Sandbox sessions</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Create a sandbox with the stored Qiniu API key, or reconnect an existing one.
          </p>
        </div>
        <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
          <Select.Root
            value={workspaceConfig.region}
            onValueChange={(value) =>
              setWorkspaceConfig((current) => ({ ...current, region: value ?? current.region, templateID: '' }))
            }
          >
            <Select.Trigger className="w-full sm:w-[190px]" />
            <Select.Content>
              {workspaceRegions.map((region) => (
                <Select.Item key={region.id} value={region.endpoint}>
                  {region.label}
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
          <Button
            type="button"
            size="2"
            className="w-full sm:w-auto"
            disabled={!qiniuStatus?.configured || createSandboxMutation.isPending}
            onClick={() => createSandboxMutation.mutate()}
          >
            Create sandbox
          </Button>
        </div>
      </div>
      {sandboxes.length > 0 ? (
        <div className="overflow-x-auto">
          <Table.Root variant="surface" size="2" className="min-w-[920px]">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeaderCell>Sandbox</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Status</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Workspace</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Resources</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell>Metadata</Table.ColumnHeaderCell>
                <Table.ColumnHeaderCell justify="end" aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {sandboxes.map((sandbox) => {
                const workspaceID = sandboxWorkspaceID(sandbox)
                const workspaceLabel = sandboxWorkspaceLabel(sandbox)
                return (
                  <Table.Row key={sandbox.id} className="align-top">
                    <Table.RowHeaderCell className="max-w-[260px]">
                      <span className="block truncate font-medium">{sandbox.sandbox_id}</span>
                    </Table.RowHeaderCell>
                    <Table.Cell className="whitespace-nowrap text-muted-foreground">{sandbox.state || '-'}</Table.Cell>
                    <Table.Cell className="max-w-[260px]">
                      {workspaceID ? (
                        <Link
                          className="block truncate font-medium text-foreground underline-offset-4 hover:underline"
                          to={`/workspaces/${workspaceID}`}
                        >
                          {workspaceLabel}
                        </Link>
                      ) : (
                        <span className="block truncate font-medium">{workspaceLabel}</span>
                      )}
                      {sandbox.workspace_path ? (
                        <span className="mt-1 block truncate font-mono text-xs text-muted-foreground">{sandbox.workspace_path}</span>
                      ) : null}
                    </Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-muted-foreground">{sandboxResources(sandbox)}</Table.Cell>
                    <Table.Cell className="max-w-[320px]">
                      {metadataEntries(sandbox.metadata).length > 0 ? (
                        <div className="flex flex-wrap gap-1.5">
                          {metadataEntries(sandbox.metadata).map(([key, value]) => (
                            <Badge key={key} color="gray" variant="surface">
                              <span className="font-medium text-foreground">{key}</span>: {value}
                            </Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-muted-foreground">-</span>
                      )}
                    </Table.Cell>
                    <Table.Cell justify="end">
                      <div className="flex justify-end gap-2">
                        {sandbox.ide_url ? (
                          <Button asChild variant="outline" color="gray" size="2" className="text-muted-foreground no-underline hover:text-foreground">
                            <a
                              href={sandbox.ide_url}
                              target="_blank"
                              rel="noreferrer"
                            >
                              IDE
                            </a>
                          </Button>
                        ) : null}
                        {sandbox.local_session ? (
                          <Button
                            type="button"
                            variant="outline"
                            color="gray"
                            size="2"
                            onClick={() => setTerminalSandboxID(sandbox.sandbox_id)}
                          >
                            Terminal
                          </Button>
                        ) : null}
                        <Button
                          type="button"
                          variant="outline"
                          color="gray"
                          size="2"
                          disabled={connectSandboxMutation.isPending}
                          onClick={() => connectSandboxMutation.mutate(sandbox.sandbox_id)}
                        >
                          Connect
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table.Root>
        </div>
      ) : (
        <div className="flex items-center gap-3 rounded-md border p-5 text-sm text-muted-foreground">
          <Server className="h-4 w-4" />
          No sandboxes yet.
        </div>
      )}
      {terminalSandboxID ? (
        <div className="rounded-md border p-5">
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
        size="2"
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
          <Button asChild size="2" className="gap-2 no-underline">
            <a href={installURL}>
              {hasGitHubInstallation ? <Settings className="h-4 w-4" /> : <GitBranch className="h-4 w-4" />}
              {hasGitHubInstallation ? 'Configure app' : 'Install app'}
            </a>
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          size="2"
          color="gray"
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
