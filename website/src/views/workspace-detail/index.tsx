import { useMutation, useQuery } from '@tanstack/react-query'
import type { AxiosError } from 'axios'
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent, PointerEvent, ReactNode } from 'react'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { AxisPointerComponent, GridComponent, TooltipComponent } from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
import type { ECharts, EChartsCoreOption } from 'echarts/core'
import ReactMarkdown from 'react-markdown'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  ArrowUp,
  Bot,
  Cpu,
  ExternalLink,
  FolderTree,
  Gauge,
  GitBranch,
  HardDrive,
  LoaderCircle,
  PanelsTopLeft,
  Plus,
  Rocket,
  Settings,
  Sparkles,
  SquareTerminal,
  X,
} from 'lucide-react'
import { Link, useParams } from 'react-router-dom'

import { qiniuCredentialStatus } from 'src/api/qiniu'
import { sendWorkspaceChatMessage, workspaceChatMessages } from 'src/api/workspace-chat'
import type { WorkspaceChatMessage } from 'src/api/workspace-chat'
import {
  connectWorkspace,
  heartbeatWorkspace,
  pauseWorkspaceSandbox,
  workspaces as fetchWorkspaces,
} from 'src/api/workspaces'
import type { Workspace } from 'src/api/workspaces'
import { sandboxMetrics } from 'src/api/sandboxes'
import type { SandboxMetric } from 'src/api/sandboxes'
import { Button, buttonVariants } from 'src/components/ui/button'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupText,
  InputGroupTextarea,
} from 'src/components/ui/input-group'
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

echarts.use([AxisPointerComponent, GridComponent, LineChart, SVGRenderer, TooltipComponent])

const TerminalPanel = lazy(() => import('src/components/TerminalPanel'))
const metricsChartGroup = 'workspace-monitor-metrics'
const defaultAssistantSidebarWidth = '50%'
const minAssistantSidebarWidth = 300
const minWorkbenchColumnWidth = 300
const assistantResizeHandleWidth = 4
const workspaceHeartbeatIntervalMs = 30_000

type WorkbenchTab = string

interface TerminalSession {
  id: string
  label: string
  opened: boolean
}

function initialTerminalSessions(): TerminalSession[] {
  return [{ id: 'terminal-1', label: 'Terminal', opened: false }]
}

function clampAssistantSidebarWidth(width: number, containerWidth?: number) {
  const minWidth = Math.max(minAssistantSidebarWidth, width)
  if (!containerWidth || containerWidth <= minAssistantSidebarWidth + assistantResizeHandleWidth + minWorkbenchColumnWidth) {
    return minWidth
  }
  return Math.min(minWidth, containerWidth - assistantResizeHandleWidth - minWorkbenchColumnWidth)
}

function defaultAssistantSidebarWidthValue(containerWidth?: number) {
  return containerWidth ? clampAssistantSidebarWidth(containerWidth / 2, containerWidth) : minAssistantSidebarWidth
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

function SandboxCreationOverlay({ repository }: { repository?: string }) {
  return (
    <div
      className="absolute inset-0 z-50 flex items-center justify-center bg-background/80 p-6 backdrop-blur-sm"
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

function bytesToGiB(value?: number) {
  if (!value || value <= 0) {
    return '-'
  }
  return `${(value / 1024 / 1024 / 1024).toFixed(value >= 10 * 1024 * 1024 * 1024 ? 1 : 2)} GiB`
}

function metricTime(metric: SandboxMetric) {
  if (metric.timestamp_unix) {
    return metric.timestamp_unix > 1e12 ? metric.timestamp_unix : metric.timestamp_unix * 1000
  }
  return metric.timestamp ? new Date(metric.timestamp).getTime() : 0
}

function metricPercent(used?: number, total?: number) {
  if (!used || !total) {
    return 0
  }
  return Math.min(100, Math.max(0, (used / total) * 100))
}

function formatPercent(value?: number) {
  if (!Number.isFinite(value)) {
    return '-'
  }
  return `${Number(value).toFixed(Number(value) >= 10 ? 1 : 2)}%`
}

function formatMetricClock(metric?: SandboxMetric) {
  if (!metric) {
    return '-'
  }
  const time = metricTime(metric)
  return time ? new Date(time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '-'
}

function chatErrorMessage(error: unknown) {
  return connectionErrorMessage(error) || 'Unable to send this message.'
}

function ChatMarkdown({ content }: { content: string }) {
  return (
    <ReactMarkdown
      components={{
        a: ({ children, href }) => (
          <a className="font-medium underline underline-offset-3" href={href} target="_blank" rel="noreferrer">
            {children}
          </a>
        ),
        code: ({ children, className, ...props }) => {
          const codeProps = { ...props } as typeof props & { node?: unknown }
          delete codeProps.node
          const isInline = typeof children === 'string' && !children.includes('\n')
          return (
            <code
              className={cn('font-mono text-[0.92em]', isInline && 'rounded bg-secondary px-1 py-0.5', className)}
              {...codeProps}
            >
              {children}
            </code>
          )
        },
        li: ({ children }) => <li className="pl-1">{children}</li>,
        ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>,
        p: ({ children }) => <p className="my-1 first:mt-0 last:mb-0">{children}</p>,
        pre: ({ children }) => (
          <pre className="my-2 overflow-auto rounded-md bg-secondary p-3 text-xs leading-5">
            {children}
          </pre>
        ),
        strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
        ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>,
      }}
    >
      {content}
    </ReactMarkdown>
  )
}

function metricLevel(value: number) {
  if (value >= 85) {
    return 'bg-destructive'
  }
  if (value >= 65) {
    return 'bg-amber-500'
  }
  return 'bg-emerald-600'
}

function MonitorMetricCard({
  icon,
  label,
  value,
  detail,
  percent,
}: {
  icon: ReactNode
  label: string
  value: string
  detail: string
  percent: number
}) {
  return (
    <div className="rounded-md border bg-background p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-sm font-medium">
          <span className="flex h-8 w-8 items-center justify-center rounded-md border bg-secondary/50 text-muted-foreground">
            {icon}
          </span>
          {label}
        </div>
        <span className="text-sm font-semibold">{value}</span>
      </div>
      <div className="mt-4 h-2 rounded-full bg-secondary">
        <div
          className={cn('h-2 rounded-full transition-all', metricLevel(percent))}
          style={{ width: `${Math.max(2, Math.min(100, percent))}%` }}
        />
      </div>
      <p className="mt-3 truncate text-xs text-muted-foreground">{detail}</p>
    </div>
  )
}

function normalizeMetricSeries(
  metrics: SandboxMetric[],
  pick: (metric: SandboxMetric) => number,
) {
  const samples = metrics
    .map((metric) => ({ metric, time: metricTime(metric), value: Math.min(100, Math.max(0, pick(metric))) }))
    .filter((sample) => Number.isFinite(sample.time) && sample.time > 0)
    .sort((left, right) => left.time - right.time)
    .slice(-48)
  if (samples.length === 1) {
    return [{ ...samples[0], time: samples[0].time - 60_000 }, samples[0]]
  }
  return samples
}

function sharedMetricTimeDomain(metrics: SandboxMetric[]) {
  const times = metrics
    .map(metricTime)
    .filter((time) => Number.isFinite(time) && time > 0)
    .sort((left, right) => left - right)
  if (times.length === 0) {
    return null
  }
  if (times.length === 1) {
    return { min: times[0] - 60_000, max: times[0] }
  }
  return { min: times[0], max: times[times.length - 1] }
}

function metricTimeStep(metrics: SandboxMetric[]) {
  const times = metrics
    .map(metricTime)
    .filter((time) => Number.isFinite(time) && time > 0)
    .sort((left, right) => left - right)
  const intervals = times
    .slice(1)
    .map((time, index) => time - times[index])
    .filter((interval) => interval > 0)
    .sort((left, right) => left - right)
  if (intervals.length === 0) {
    return undefined
  }
  return intervals[Math.floor(intervals.length / 2)]
}

function formatChartTime(value: number, step?: number) {
  return new Date(value).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    ...(step && step < 60_000 ? { second: '2-digit' } : {}),
  })
}

function useEChart(option: EChartsCoreOption, group: string) {
  const chartRef = useRef<HTMLDivElement | null>(null)
  const chartInstanceRef = useRef<ECharts | null>(null)

  useEffect(() => {
    const element = chartRef.current
    if (!element) {
      return undefined
    }

    const chart = echarts.init(element, undefined, { renderer: 'svg' })
    chart.group = group
    chartInstanceRef.current = chart
    echarts.connect(group)
    let resizeObserver: ResizeObserver | undefined
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => chart.resize())
      resizeObserver.observe(element)
    }

    return () => {
      resizeObserver?.disconnect()
      echarts.disconnect(group)
      chart.dispose()
      chartInstanceRef.current = null
    }
  }, [group])

  useEffect(() => {
    chartInstanceRef.current?.setOption(option, true)
  }, [option])

  return chartRef
}

function EChartPanel({ option, title }: { option: EChartsCoreOption; title: string }) {
  const chartRef = useEChart(option, metricsChartGroup)
  return <div ref={chartRef} className="mt-3 h-56 w-full" role="img" aria-label={`${title} chart`} data-chart-group={metricsChartGroup} />
}

function MetricLineChart({
  title,
  metrics,
  series,
  timeDomain,
  timeStep,
}: {
  title: string
  metrics: SandboxMetric[]
  series: Array<{ label: string; color: string; pick: (metric: SandboxMetric) => number }>
  timeDomain: { min: number; max: number } | null
  timeStep?: number
}) {
  const normalized = useMemo(
    () => series.map((item) => ({ ...item, samples: normalizeMetricSeries(metrics, item.pick) })),
    [metrics, series],
  )
  const allSamples = useMemo(() => normalized.flatMap((item) => item.samples), [normalized])
  const sampleTimes = useMemo(() => (
    Array.from(new Set(allSamples.map((sample) => sample.time).filter((time) => Number.isFinite(time) && time > 0))).sort((left, right) => left - right)
  ), [allSamples])
  const sampleLabels = useMemo(() => sampleTimes.map((time) => formatChartTime(time, timeStep)), [sampleTimes, timeStep])
  const minTime = timeDomain?.min ?? Math.min(...allSamples.map((sample) => sample.time))
  const maxTime = timeDomain?.max ?? Math.max(...allSamples.map((sample) => sample.time))
  const hasSamples = allSamples.length > 0 && Number.isFinite(minTime) && Number.isFinite(maxTime)
  const option = useMemo<EChartsCoreOption>(() => ({
    animation: false,
    axisPointer: {
      link: [{ xAxisIndex: 'all' }],
      snap: true,
    },
    color: normalized.map((item) => item.color),
    grid: { top: 14, right: 14, bottom: 28, left: 42 },
    tooltip: {
      trigger: 'axis',
      axisPointer: {
        type: 'line',
        lineStyle: { color: 'rgba(100, 116, 139, 0.45)', type: 'dashed', width: 1 },
      },
      borderWidth: 1,
      padding: 8,
      formatter: (params: unknown) => {
        const items = (Array.isArray(params) ? params : [params]) as Array<{
          axisValue?: string
          marker?: string
          seriesName?: string
          value?: number
        }>
        const lines = [items[0]?.axisValue ? `<div>${items[0].axisValue}</div>` : '']
        items.forEach((item) => {
          const value = Number(item.value)
          lines.push(`<div>${item.marker ?? ''}${item.seriesName ?? ''}: ${formatPercent(value)}</div>`)
        })
        return lines.join('')
      },
    },
    xAxis: {
      type: 'category',
      data: sampleLabels,
      axisLine: { lineStyle: { color: 'rgba(148, 163, 184, 0.24)' } },
      axisTick: { show: false },
      axisLabel: {
        color: 'rgb(100, 116, 139)',
        hideOverlap: true,
      },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: 100,
      interval: 50,
      axisLabel: {
        color: 'rgb(100, 116, 139)',
        formatter: '{value}%',
      },
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: 'rgba(148, 163, 184, 0.24)', type: 'dashed' } },
    },
    series: normalized.map((item) => ({
      name: item.label,
      type: 'line',
      data: sampleTimes.map((time) => item.samples.find((sample) => sample.time === time)?.value ?? null),
      showSymbol: false,
      smooth: true,
      lineStyle: { width: 2 },
      areaStyle: { opacity: 0.08 },
      emphasis: { focus: 'series' },
    })),
  }), [normalized, sampleLabels, sampleTimes])

  if (!hasSamples) {
    return (
      <div className="rounded-md bg-background p-3">
        <div className="flex items-center justify-between gap-3">
          <h4 className="text-sm font-medium">{title}</h4>
          <span className="text-xs text-muted-foreground">0 samples</span>
        </div>
        <div className="mt-3 flex h-52 items-center justify-center rounded-md bg-secondary/30 text-sm text-muted-foreground">
          No metric samples yet.
        </div>
      </div>
    )
  }

  return (
    <div className="bg-background py-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-sm font-medium">{title}</h4>
        <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
          {normalized.map((item) => (
            <span key={item.label} className="inline-flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} />
              {item.label}
            </span>
          ))}
        </div>
      </div>
      <EChartPanel option={option} title={title} />
    </div>
  )
}

function SandboxMonitor({
  workspace,
  metrics,
  loading,
  error,
  onRefresh,
}: {
  workspace: Workspace
  metrics: SandboxMetric[]
  loading: boolean
  error: unknown
  onRefresh: () => void
}) {
  const latest = metrics[metrics.length - 1]
  const cpuPercent = latest?.cpu_used_pct ?? 0
  const memoryPercent = latest ? metricPercent(latest.mem_used, latest.mem_total) : 0
  const diskPercent = latest ? metricPercent(latest.disk_used, latest.disk_total) : 0
  const timeDomain = sharedMetricTimeDomain(metrics)
  const timeStep = metricTimeStep(metrics)
  const cpuSeries = useMemo(() => [
    { label: `CPU ${formatPercent(cpuPercent)}`, color: '#f97316', pick: (metric: SandboxMetric) => metric.cpu_used_pct || 0 },
  ], [cpuPercent])
  const memorySeries = useMemo(() => [
    { label: `Memory ${formatPercent(memoryPercent)}`, color: '#64748b', pick: (metric: SandboxMetric) => metricPercent(metric.mem_used, metric.mem_total) },
  ], [memoryPercent])
  const diskSeries = useMemo(() => [
    { label: `Disk ${formatPercent(diskPercent)}`, color: '#0f766e', pick: (metric: SandboxMetric) => metricPercent(metric.disk_used, metric.disk_total) },
  ], [diskPercent])

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto bg-secondary/20">
      <div className="grid gap-4 border-b bg-background p-4 md:grid-cols-2 xl:grid-cols-4">
        <div className="rounded-md border bg-background p-4">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Activity className="h-4 w-4 text-emerald-600" />
            Runtime
          </div>
          <p className="mt-3 text-2xl font-semibold">{workspace.state || 'unknown'}</p>
          <p className="mt-2 truncate text-xs text-muted-foreground">{workspace.sandbox_id || 'Sandbox pending'}</p>
        </div>
        <MonitorMetricCard
          icon={<Cpu className="h-4 w-4" />}
          label="CPU"
          value={formatPercent(cpuPercent)}
          detail={`${latest?.cpu_count || '-'} vCPU · latest ${formatMetricClock(latest)}`}
          percent={cpuPercent}
        />
        <MonitorMetricCard
          icon={<Gauge className="h-4 w-4" />}
          label="Memory"
          value={formatPercent(memoryPercent)}
          detail={`${bytesToGiB(latest?.mem_used)} / ${bytesToGiB(latest?.mem_total)}`}
          percent={memoryPercent}
        />
        <MonitorMetricCard
          icon={<HardDrive className="h-4 w-4" />}
          label="Disk"
          value={formatPercent(diskPercent)}
          detail={`${bytesToGiB(latest?.disk_used)} / ${bytesToGiB(latest?.disk_total)}`}
          percent={diskPercent}
        />
      </div>
      <div className="p-4">
        <section className="space-y-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-semibold">Resource trend</h3>
              <p className="mt-1 text-xs text-muted-foreground">Recent CPU, memory, and disk pressure from the sandbox metrics API.</p>
            </div>
            <Button type="button" variant="outline" onClick={onRefresh} disabled={loading}>
              {loading ? 'Refreshing...' : 'Refresh'}
            </Button>
          </div>
          {error ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
              {connectionErrorMessage(error)}
            </div>
          ) : null}
          <div className="divide-y">
            <MetricLineChart
              title="CPU"
              metrics={metrics}
              timeDomain={timeDomain}
              timeStep={timeStep}
              series={cpuSeries}
            />
            <MetricLineChart
              title="Memory"
              metrics={metrics}
              timeDomain={timeDomain}
              timeStep={timeStep}
              series={memorySeries}
            />
            <MetricLineChart
              title="Disk"
              metrics={metrics}
              timeDomain={timeDomain}
              timeStep={timeStep}
              series={diskSeries}
            />
          </div>
        </section>
      </div>
    </div>
  )
}

function WorkspaceDetail() {
  const { workspaceId } = useParams()
  const [dismissedMissingWorkspaceID, setDismissedMissingWorkspaceID] = useState('')
  const [workbenchTab, setWorkbenchTab] = useState<WorkbenchTab>('files')
  const [terminalSessions, setTerminalSessions] = useState<TerminalSession[]>(initialTerminalSessions)
  const [nextTerminalNumber, setNextTerminalNumber] = useState(2)
  const [assistantSidebarWidth, setAssistantSidebarWidth] = useState<number | null>(null)
  const [assistantSidebarResizing, setAssistantSidebarResizing] = useState(false)
  const [workspaceLayoutWidth, setWorkspaceLayoutWidth] = useState(0)
  const [chatMessage, setChatMessage] = useState('')
  const previousWorkspaceIDRef = useRef<string | undefined>(undefined)
  const workspaceLayoutRef = useRef<HTMLDivElement | null>(null)
  const chatContainerRef = useRef<HTMLDivElement | null>(null)
  const workspaceActivityRef = useRef({
    pauseRequested: false,
    sandboxID: '',
    workspaceID: '',
  })
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
  const monitorSandboxID = connectedWorkspace?.sandbox_id ?? workspace?.sandbox_id
  const metricsQuery = useQuery({
    queryKey: ['sandbox-metrics', monitorSandboxID],
    queryFn: () => {
      if (!monitorSandboxID) {
        throw new Error('sandbox id is required')
      }
      const end = Math.floor(Date.now() / 1000)
      return sandboxMetrics(monitorSandboxID, { start: end - 30 * 60, end })
    },
    enabled: Boolean(monitorSandboxID && workbenchTab === 'monitor' && !connectWorkspaceMutation.isPending),
    refetchInterval: workbenchTab === 'monitor' ? 15_000 : false,
    retry: false,
  })
  const qiniuCredentialsQuery = useQuery({
    queryKey: ['qiniu', 'credentials'],
    queryFn: qiniuCredentialStatus,
    retry: false,
  })
  const chatMessagesQuery = useQuery({
    queryKey: ['workspace-chat', workspaceId],
    queryFn: () => workspaceChatMessages(workspaceId || ''),
    enabled: Boolean(workspaceId),
    retry: false,
  })
  const chatMutation = useMutation({
    mutationFn: (message: string) => {
      if (!workspaceId) {
        throw new Error('workspace id is required')
      }
      return sendWorkspaceChatMessage(workspaceId, message)
    },
    onSuccess: (response) => {
      setChatMessage('')
      queryClient.setQueryData<{ data: { messages: WorkspaceChatMessage[] } }>(['workspace-chat', workspaceId], (current) => {
        const responseMessages = [response.data.user_message, response.data.assistant_message]
        if (!current) {
          return { data: { messages: responseMessages } }
        }
        const existingIDs = new Set(current.data.messages.map((message: WorkspaceChatMessage) => message.id))
        return {
          ...current,
          data: {
            ...current.data,
            messages: [
              ...current.data.messages,
              ...responseMessages.filter((message) => !existingIDs.has(message.id)),
            ],
          },
        }
      })
      void queryClient.invalidateQueries({ queryKey: ['qiniu', 'credentials'] })
    },
  })

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
    setChatMessage('')
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

  const updateAssistantSidebarWidth = (nextWidth: number, containerWidth?: number) => {
    setAssistantSidebarWidth(clampAssistantSidebarWidth(
      nextWidth,
      containerWidth ?? workspaceLayoutRef.current?.getBoundingClientRect().width,
    ))
  }

  const nudgeAssistantSidebarWidth = (offset: number) => {
    const containerWidth = workspaceLayoutRef.current?.getBoundingClientRect().width
    setAssistantSidebarWidth((currentWidth) => clampAssistantSidebarWidth(
      (currentWidth ?? defaultAssistantSidebarWidthValue(containerWidth)) + offset,
      containerWidth,
    ))
  }

  const handleAssistantResizePointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return
    }
    event.preventDefault()
    const startX = event.clientX
    const containerWidth = workspaceLayoutRef.current?.getBoundingClientRect().width
    const startWidth = assistantSidebarWidth ?? defaultAssistantSidebarWidthValue(containerWidth)

    const handlePointerMove = (moveEvent: globalThis.PointerEvent) => {
      updateAssistantSidebarWidth(startWidth + moveEvent.clientX - startX, containerWidth)
    }
    const handlePointerUp = () => {
      document.removeEventListener('pointermove', handlePointerMove)
      document.removeEventListener('pointerup', handlePointerUp)
      setAssistantSidebarResizing(false)
    }

    setAssistantSidebarResizing(true)
    document.addEventListener('pointermove', handlePointerMove)
    document.addEventListener('pointerup', handlePointerUp)
  }

  const handleAssistantResizeKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      nudgeAssistantSidebarWidth(-24)
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault()
      nudgeAssistantSidebarWidth(24)
    }
    if (event.key === 'Home') {
      event.preventDefault()
      updateAssistantSidebarWidth(minAssistantSidebarWidth)
    }
  }

  useEffect(() => {
    const layout = workspaceLayoutRef.current
    if (!layout) {
      return
    }
    const updateLayoutWidth = () => {
      setWorkspaceLayoutWidth(layout.getBoundingClientRect().width)
    }
    updateLayoutWidth()
    if (typeof ResizeObserver === 'undefined') {
      return
    }
    const resizeObserver = new ResizeObserver(updateLayoutWidth)
    resizeObserver.observe(layout)
    return () => resizeObserver.disconnect()
  }, [])

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

  useEffect(() => {
    const activeWorkspaceID = workspace?.id
    const activeSandboxID = connectedWorkspace?.sandbox_id ?? workspace?.sandbox_id
    if (!activeWorkspaceID || !activeSandboxID || connectWorkspaceMutation.isPending) {
      return undefined
    }

    let cancelled = false
    workspaceActivityRef.current = {
      pauseRequested: false,
      sandboxID: activeSandboxID,
      workspaceID: activeWorkspaceID,
    }

    const heartbeat = () => {
      if (cancelled || workspaceActivityRef.current.pauseRequested) {
        return
      }
      void heartbeatWorkspace(activeWorkspaceID).catch(() => {})
    }

    const pause = (keepalive = false) => {
      if (cancelled || workspaceActivityRef.current.pauseRequested) {
        return
      }
      workspaceActivityRef.current.pauseRequested = true
      void pauseWorkspaceSandbox(activeWorkspaceID, keepalive ? { keepalive: true } : undefined).catch(() => {
        workspaceActivityRef.current.pauseRequested = false
      })
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState !== 'visible') {
        return
      }
      workspaceActivityRef.current.pauseRequested = false
      heartbeat()
    }

    const handlePageHide = () => pause(true)

    document.addEventListener('visibilitychange', handleVisibilityChange)
    window.addEventListener('pagehide', handlePageHide)

    heartbeat()
    const heartbeatTimer = window.setInterval(() => {
      if (document.visibilityState === 'hidden') {
        return
      }
      heartbeat()
    }, workspaceHeartbeatIntervalMs)

    return () => {
      cancelled = true
      window.clearInterval(heartbeatTimer)
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      window.removeEventListener('pagehide', handlePageHide)
    }
  }, [
    connectWorkspaceMutation.isPending,
    connectedWorkspace?.sandbox_id,
    workspace?.id,
    workspace?.sandbox_id,
  ])

  useEffect(() => () => {
    const { pauseRequested, sandboxID, workspaceID } = workspaceActivityRef.current
    if (!workspaceID || !sandboxID || pauseRequested) {
      return
    }
    workspaceActivityRef.current.pauseRequested = true
    void pauseWorkspaceSandbox(workspaceID).catch(() => {})
  }, [workspaceId])

  useEffect(() => {
    document.body.classList.toggle('workspace-assistant-resizing', assistantSidebarResizing)
    return () => {
      document.body.classList.remove('workspace-assistant-resizing')
    }
  }, [assistantSidebarResizing])

  useEffect(() => {
    if (!chatContainerRef.current) {
      return
    }
    chatContainerRef.current.scrollTop = chatContainerRef.current.scrollHeight
  }, [chatMessagesQuery.data?.data.messages, chatMutation.isPending])

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
  const creatingSandbox = reconnecting && (connectWorkspaceMutation.variables?.recreate || !currentWorkspace?.sandbox_id)
  const connectError = reconnecting ? null : connectWorkspaceMutation.error
  const sandboxMissing = !connectWorkspaceMutation.data && isMissingSandboxError(connectError)
  const connectFailed = Boolean(connectError && !sandboxMissing)
  const canOpenIDE = Boolean(currentWorkspace?.ide_url && !sandboxMissing && !connectFailed && !reconnecting)
  const missingSandboxLabel = currentWorkspace?.sandbox_id || '-'
  const missingSandboxOpen = sandboxMissing && dismissedMissingWorkspaceID !== workspace?.id
  const maasMissing = qiniuCredentialsQuery.data?.data.maas_configured === false
  const chatMessages = chatMessagesQuery.data?.data.messages ?? []
  const chatPending = chatMutation.isPending
  const chatDisabled = chatPending || maasMissing || !currentWorkspace?.sandbox_id || reconnecting || sandboxMissing || connectFailed || currentWorkspace?.state !== 'running'
  const chatError = chatMutation.isError ? chatErrorMessage(chatMutation.error) : ''
  const submitChatMessage = () => {
    const message = chatMessage.trim()
    if (!message || chatDisabled) {
      return
    }
    chatMutation.mutate(message)
  }
  const handleChatMessageKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) {
      return
    }
    event.preventDefault()
    submitChatMessage()
  }
  const assistantSidebarAriaValue = assistantSidebarWidth ?? defaultAssistantSidebarWidthValue(workspaceLayoutWidth)
  const workspaceLayoutStyle = {
    '--assistant-sidebar-width': assistantSidebarWidth === null ? defaultAssistantSidebarWidth : `${assistantSidebarWidth}px`,
  } as CSSProperties

  return (
    <div className="relative flex h-screen flex-col overflow-hidden bg-background">
      {creatingSandbox ? <SandboxCreationOverlay repository={currentWorkspace?.repo_full_name} /> : null}
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

      <div
        ref={workspaceLayoutRef}
        className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden xl:grid-cols-[var(--assistant-sidebar-width)_4px_minmax(300px,1fr)]"
        style={workspaceLayoutStyle}
      >
        <section className="flex min-h-0 flex-col border-b xl:border-b-0">
          <div className="flex h-10 items-center justify-between border-b px-4">
            <div className="flex items-center gap-2">
              <Bot className="h-4 w-4 text-primary" />
              <h2 className="text-sm font-semibold">AI Chat</h2>
            </div>
            <span className="text-xs text-muted-foreground">Ready</span>
          </div>
          <div className="flex min-h-0 flex-1 flex-col">
            <div ref={chatContainerRef} className="min-h-0 flex-1 space-y-4 overflow-auto p-4" aria-live="polite">
              {chatMessages.length === 0 ? (
                <div className="flex gap-3">
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground">
                    <Sparkles className="h-3.5 w-3.5" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium">Ready to work in {title}</p>
                    <p className="mt-1 text-sm leading-6 text-muted-foreground">
                      Ask about files, run commands, or prepare a launch target for this workspace.
                    </p>
                  </div>
                </div>
              ) : null}
              {chatMessages.map((message) => (
                <div
                  key={message.id}
                  className={cn(
                    'rounded-md px-3 py-2 text-sm',
                    message.role === 'user' ? 'ml-8 bg-secondary/50' : 'mr-6 bg-transparent px-0',
                  )}
                >
                  <div className="break-words leading-6">
                    <ChatMarkdown content={message.content} />
                  </div>
                </div>
              ))}
              {chatPending ? (
                <div className="mr-5 flex items-center gap-2 rounded-md border px-3 py-2 text-sm text-muted-foreground">
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Running AI Chat in the sandbox...
                </div>
              ) : null}
              {maasMissing ? (
                <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
                  <div className="flex gap-2">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                    <div className="min-w-0">
                      <p className="font-medium">Qiniu MAAS API Key is not configured.</p>
                      <p className="mt-1 leading-6 text-amber-800">
                        Configure it before using AI Chat for this workspace.
                      </p>
                      <Link
                        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'mt-3 bg-background no-underline')}
                        to="/credentials"
                      >
                        Configure credentials
                      </Link>
                    </div>
                  </div>
                </div>
              ) : null}
              {chatError ? (
                <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                  {chatError}
                </div>
              ) : null}
            </div>
            <form
              className="shrink-0 bg-background p-3"
              onSubmit={(event) => {
                event.preventDefault()
                submitChatMessage()
              }}
            >
              <InputGroup className="min-h-24 items-stretch rounded-2xl bg-secondary/40 shadow-sm has-[[data-slot=input-group-control]:focus-visible]:ring-2">
                <InputGroupTextarea
                  aria-label="Message AI Chat"
                  placeholder="Ask AI Chat to inspect, run, or explain..."
                  className="min-h-14 px-3 text-sm"
                  value={chatMessage}
                  onChange={(event) => setChatMessage(event.target.value)}
                  onKeyDown={handleChatMessageKeyDown}
                  disabled={chatDisabled}
                />
                <InputGroupAddon align="block-end" className="justify-between">
                  <InputGroupText className="text-xs">
                    {currentWorkspace?.sandbox_id ? 'Workspace context attached' : 'Connect a sandbox first'}
                  </InputGroupText>
                  <div className="flex items-center gap-1">
                    <InputGroupButton
                      type="submit"
                      size="icon-xs"
                      aria-label="Send message"
                      disabled={chatDisabled || !chatMessage.trim()}
                      className="rounded-full bg-primary text-primary-foreground hover:bg-primary/80"
                    >
                      {chatPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <ArrowUp className="h-3.5 w-3.5" />}
                    </InputGroupButton>
                  </div>
                </InputGroupAddon>
              </InputGroup>
            </form>
          </div>
        </section>

        <div
          role="separator"
          aria-label="Resize AI Chat sidebar"
          aria-orientation="vertical"
          aria-valuemin={minAssistantSidebarWidth}
          aria-valuenow={Math.round(assistantSidebarAriaValue)}
          tabIndex={0}
          className="group hidden cursor-col-resize bg-transparent focus-visible:outline-none xl:block"
          onPointerDown={handleAssistantResizePointerDown}
          onKeyDown={handleAssistantResizeKeyDown}
        >
          <div className="mx-auto h-full w-px bg-border transition-[width,background-color] group-hover:w-1 group-hover:bg-primary/60 group-focus-visible:w-1 group-focus-visible:bg-primary/60" />
        </div>

        <section className="flex min-h-0 flex-col">
          <Tabs value={workbenchTab} onValueChange={handleWorkbenchTabChange} className="min-h-0 flex-1">
            <div className="flex h-10 shrink-0 items-center border-b bg-background">
              <TabsList className="border-b-0">
                <TabsTrigger value="files">
                  <FolderTree className="h-4 w-4" />
                  Files
                </TabsTrigger>
                <TabsTrigger value="monitor">
                  <Activity className="h-4 w-4" />
                  Monitor
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
                  workspaceID={currentWorkspace?.id}
                  sandboxID={currentWorkspace?.sandbox_id}
                  workspacePath={currentWorkspace?.workspace_path}
                  disabled={reconnecting}
                  emptyMessage={reconnecting ? 'Checking sandbox...' : 'Preparing workspace files...'}
                />
              )}
            </TabsContent>
            <TabsContent value="monitor" className="flex min-h-0 min-w-0">
              {sandboxMissing ? (
                <div className="flex h-full w-full items-center justify-center bg-background p-6 text-center text-sm text-muted-foreground">
                  Create a new sandbox to view runtime metrics.
                </div>
              ) : connectFailed ? (
                <div className="flex h-full w-full items-center justify-center bg-background p-6 text-center text-sm text-muted-foreground">
                  Reconnect the workspace before viewing runtime metrics.
                </div>
              ) : (
                <SandboxMonitor
                  workspace={currentWorkspace}
                  metrics={metricsQuery.data?.data.metrics ?? []}
                  loading={metricsQuery.isFetching}
                  error={metricsQuery.error}
                  onRefresh={() => void metricsQuery.refetch()}
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
                ) : currentWorkspace?.sandbox_id && session.opened ? (
                  <Suspense
                    fallback={
                      <div className="flex h-full items-center justify-center bg-[#0b0f14] p-6 text-sm text-slate-300">
                        Loading terminal...
                      </div>
                    }
                  >
                    <TerminalPanel
                      sandboxID={currentWorkspace?.sandbox_id}
                      workspacePath={currentWorkspace?.workspace_path}
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
