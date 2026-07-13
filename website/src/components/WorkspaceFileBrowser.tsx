import { useQuery } from '@tanstack/react-query'
import type { CSSProperties, PointerEvent, ReactNode } from 'react'
import {
  ArrowRight,
  ChevronDown,
  ChevronRight,
  CornerUpLeft,
  Copy,
  Download,
  ExternalLink,
  File,
  Folder,
  Loader2,
  RefreshCw,
  X,
} from 'lucide-react'
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'

import { sandboxFileContent, sandboxFilePreviewURL, sandboxFiles, workspaceFilePreviewURL } from 'src/api/filesystem'
import type { SandboxFileEntry } from 'src/api/filesystem'
import { Button, buttonVariants } from 'src/components/ui/button'
import { Input } from 'src/components/ui/input'
import { fileExtension } from 'src/lib/file-extension'
import { cn } from 'src/lib/utils'

const maxPreviewFileSize = 1024 * 1024
const fallbackFilesystemPath = '/home/user'
const minTreeWidth = 240
const defaultTreeWidth = minTreeWidth
const minPreviewWidth = 360
const FileContentPreview = lazy(() => import('src/components/FileContentPreview'))
const textExtensions = new Set([
  'bash',
  'conf',
  'css',
  'cjs',
  'cts',
  'csv',
  'env',
  'go',
  'html',
  'ini',
  'js',
  'json',
  'jsx',
  'log',
  'md',
  'mjs',
  'mts',
  'py',
  'rs',
  'sh',
  'sql',
  'toml',
  'ts',
  'tsx',
  'txt',
  'xml',
  'yaml',
  'yml',
])

interface DirectoryState {
  entries: SandboxFileEntry[]
  expanded: boolean
  loading: boolean
  error: string
}

interface WorkspaceFileBrowserProps {
  workspaceID?: string
  sandboxID?: string
  workspacePath?: string
  disabled?: boolean
  emptyMessage?: string
}

function sortEntries(entries: SandboxFileEntry[]) {
  return [...entries].sort((left, right) => {
    if (left.type !== right.type) {
      if (left.type === 'dir') return -1
      if (right.type === 'dir') return 1
    }
    return left.name.localeCompare(right.name)
  })
}

function entryExtension(entry: SandboxFileEntry) {
  const name = entry.name || entry.path
  return fileExtension(name)
}

function isPreviewable(entry: SandboxFileEntry, contentType: string) {
  const mimeType = contentType.split(';')[0]?.trim().toLowerCase()
  if (mimeType?.startsWith('text/')) return true
  if (['application/json', 'application/javascript', 'application/xml', 'application/yaml'].includes(mimeType)) {
    return true
  }
  if (mimeType && mimeType !== 'application/octet-stream') {
    return false
  }
  return textExtensions.has(entryExtension(entry))
}

function isHTMLFile(entry: SandboxFileEntry) {
  return ['html', 'htm'].includes(entryExtension(entry))
}

function downloadBlob(blob: Blob, filename: string) {
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

function entryDetails(entry: SandboxFileEntry, contentType?: string) {
  return [
    entry.owner,
    entry.group,
    entry.permissions,
    entry.modified_time,
    contentType,
  ].filter(Boolean)
}

function clampTreeWidth(width: number, containerWidth?: number) {
  const maxTreeWidth = containerWidth ? Math.max(minTreeWidth, containerWidth - minPreviewWidth) : 640
  return Math.min(Math.max(width, minTreeWidth), maxTreeWidth)
}

function parentPath(filePath: string) {
  const normalized = filePath.trim().replace(/\/+$/, '') || '/'
  if (normalized === '/') {
    return '/'
  }
  const index = normalized.lastIndexOf('/')
  if (index <= 0) {
    return '/'
  }
  return normalized.slice(0, index)
}

export function WorkspaceFileBrowser({
  workspaceID,
  sandboxID,
  workspacePath,
  disabled,
  emptyMessage = 'Connect the workspace to browse files.',
}: WorkspaceFileBrowserProps) {
  const initialPath = workspacePath || '/workspace'
  const [path, setPath] = useState(initialPath)
  const [pathInput, setPathInput] = useState(initialPath)
  const [selectedFile, setSelectedFile] = useState<SandboxFileEntry | null>(null)
  const [fileContent, setFileContent] = useState('')
  const [fileBlob, setFileBlob] = useState<Blob | null>(null)
  const [fileContentType, setFileContentType] = useState('')
  const [filePreviewable, setFilePreviewable] = useState(true)
  const [fileLoading, setFileLoading] = useState(false)
  const [fileError, setFileError] = useState('')
  const [directoryStates, setDirectoryStates] = useState<Record<string, DirectoryState>>({})
  const [treeWidth, setTreeWidth] = useState(defaultTreeWidth)
  const splitContainerRef = useRef<HTMLDivElement | null>(null)
  const selectedFilePathRef = useRef<string | null>(null)
  const canBrowse = Boolean(sandboxID && !disabled)

  useEffect(() => {
    const nextPath = workspacePath || '/workspace'
    setPath(nextPath)
    setPathInput(nextPath)
    setSelectedFile(null)
    selectedFilePathRef.current = null
    setDirectoryStates({})
  }, [sandboxID, workspacePath])

  const filesQuery = useQuery({
    queryKey: ['sandbox-files', sandboxID, path],
    queryFn: () => sandboxFiles(sandboxID || '', path),
    enabled: canBrowse,
    retry: false,
  })

  useEffect(() => {
    if (!canBrowse || !filesQuery.isError) {
      return
    }
    if (path !== initialPath || path === fallbackFilesystemPath) {
      return
    }

    setPath(fallbackFilesystemPath)
    setPathInput(fallbackFilesystemPath)
    setSelectedFile(null)
    selectedFilePathRef.current = null
    setDirectoryStates({})
  }, [canBrowse, filesQuery.error, filesQuery.isError, initialPath, path])

  const entries = useMemo(
    () => sortEntries(filesQuery.data?.data.entries ?? []),
    [filesQuery.data?.data.entries],
  )
  async function openFile(entry: SandboxFileEntry) {
    setSelectedFile(entry)
    selectedFilePathRef.current = entry.path
    setFileContent('')
    setFileBlob(null)
    setFileContentType('')
    setFilePreviewable(true)
    setFileError('')

    if (!sandboxID) {
      return
    }
    if (entry.size > maxPreviewFileSize) {
      setFilePreviewable(false)
      return
    }

    setFileLoading(true)
    try {
      const response = await sandboxFileContent(sandboxID, entry.path)
      if (selectedFilePathRef.current !== entry.path) {
        return
      }
      const blob = response.data
      const contentTypeHeader = response.headers?.['content-type']
      const contentType = blob.type || (typeof contentTypeHeader === 'string' ? contentTypeHeader : '')
      const previewable = isPreviewable(entry, contentType)
      setFileBlob(blob)
      setFileContentType(contentType)
      setFilePreviewable(previewable)
      const content = previewable ? await blob.text() : ''
      if (selectedFilePathRef.current !== entry.path) {
        return
      }
      setFileContent(content)
    } catch (error) {
      if (selectedFilePathRef.current === entry.path) {
        setFileError(error instanceof Error ? error.message : String(error))
      }
    } finally {
      if (selectedFilePathRef.current === entry.path) {
        setFileLoading(false)
      }
    }
  }

  function openPath(nextPath: string) {
    const normalized = nextPath.trim()
    if (!normalized || !normalized.startsWith('/')) {
      return
    }
    setPath(normalized)
    setPathInput(normalized)
    setSelectedFile(null)
    selectedFilePathRef.current = null
    setDirectoryStates({})
  }

  async function downloadSelectedFile() {
    if (!selectedFile || !sandboxID) {
      return
    }

    const entry = selectedFile
    let blob = fileBlob

    if (!blob) {
      setFileLoading(true)
      setFileError('')
      try {
        const response = await sandboxFileContent(sandboxID, entry.path)
        if (selectedFilePathRef.current !== entry.path) {
          return
        }
        blob = response.data
        const contentTypeHeader = response.headers?.['content-type']
        setFileBlob(blob)
        setFileContentType(blob.type || (typeof contentTypeHeader === 'string' ? contentTypeHeader : ''))
      } catch (error) {
        if (selectedFilePathRef.current === entry.path) {
          setFileError(error instanceof Error ? error.message : String(error))
        }
        return
      } finally {
        if (selectedFilePathRef.current === entry.path) {
          setFileLoading(false)
        }
      }
    }

    if (blob && selectedFilePathRef.current === entry.path) {
      downloadBlob(blob, entry.name || entry.path.split('/').pop() || 'download')
    }
  }

  async function toggleDirectory(entry: SandboxFileEntry) {
    if (!sandboxID) {
      return
    }

    const current = directoryStates[entry.path]
    if (current?.loading) {
      return
    }
    if (current) {
      setDirectoryStates((previous) => ({
        ...previous,
        [entry.path]: {
          ...current,
          expanded: !current.expanded,
        },
      }))
      return
    }

    setDirectoryStates((previous) => ({
      ...previous,
      [entry.path]: {
        entries: [],
        expanded: true,
        loading: true,
        error: '',
      },
    }))

    try {
      const response = await sandboxFiles(sandboxID, entry.path)
      setDirectoryStates((previous) => ({
        ...previous,
        [entry.path]: {
          entries: response.data.entries,
          expanded: true,
          loading: false,
          error: '',
        },
      }))
    } catch (error) {
      setDirectoryStates((previous) => ({
        ...previous,
        [entry.path]: {
          entries: [],
          expanded: true,
          loading: false,
          error: error instanceof Error ? error.message : String(error),
        },
      }))
    }
  }

  function refreshRoot() {
    setDirectoryStates({})
    void filesQuery.refetch()
  }

  function updateTreeWidth(nextWidth: number) {
    setTreeWidth(clampTreeWidth(nextWidth, splitContainerRef.current?.getBoundingClientRect().width))
  }

  function handleSeparatorPointerDown(event: PointerEvent<HTMLDivElement>) {
    if (event.button !== 0) {
      return
    }
    event.preventDefault()
    const startX = event.clientX
    const startWidth = treeWidth
    const containerWidth = splitContainerRef.current?.getBoundingClientRect().width

    const handlePointerMove = (moveEvent: globalThis.PointerEvent) => {
      setTreeWidth(clampTreeWidth(startWidth + startX - moveEvent.clientX, containerWidth))
    }
    const handlePointerUp = () => {
      document.removeEventListener('pointermove', handlePointerMove)
      document.removeEventListener('pointerup', handlePointerUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    document.addEventListener('pointermove', handlePointerMove)
    document.addEventListener('pointerup', handlePointerUp)
  }

  function renderEntry(entry: SandboxFileEntry, depth = 0): ReactNode {
    const isDirectory = entry.type === 'dir'
    const directoryState = isDirectory ? directoryStates[entry.path] : undefined
    const childEntries = directoryState?.expanded ? sortEntries(directoryState.entries) : []
    const selected = selectedFile?.path === entry.path
    const ToggleIcon = directoryState?.expanded ? ChevronDown : ChevronRight
    const Icon = isDirectory ? Folder : File

    return (
      <div key={entry.path || entry.name}>
        <Button
          type="button"
          variant="ghost"
          className={cn(
            'flex h-8 w-full justify-start rounded-none px-1.5 font-normal',
            selected && 'bg-muted text-foreground',
          )}
          title={entry.path}
          onClick={() => (isDirectory ? void toggleDirectory(entry) : void openFile(entry))}
        >
          <span className="flex min-w-0 items-center gap-1.5" style={{ paddingLeft: depth * 16 }}>
            {isDirectory ? (
              <ToggleIcon className="h-3.5 w-3.5 text-muted-foreground" />
            ) : (
              <span className="h-3.5 w-3.5 shrink-0" />
            )}
            <Icon className="h-4 w-4 text-muted-foreground" />
            <span className="truncate font-mono text-xs">{entry.name || entry.path}</span>
          </span>
        </Button>

        {directoryState?.expanded && directoryState.loading ? (
          <div
            className="flex h-8 items-center gap-2 px-1.5 font-mono text-xs text-muted-foreground"
            style={{ paddingLeft: 46 + depth * 16 }}
          >
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            Loading...
          </div>
        ) : null}
        {directoryState?.expanded && directoryState.error ? (
          <div className="px-1.5 py-2 text-xs text-destructive" style={{ paddingLeft: 46 + depth * 16 }}>
            {directoryState.error}
          </div>
        ) : null}
        {directoryState?.expanded && !directoryState.loading && !directoryState.error && childEntries.length === 0 ? (
          <div className="px-1.5 py-2 text-xs text-muted-foreground" style={{ paddingLeft: 46 + depth * 16 }}>
            Empty directory
          </div>
        ) : null}
        {childEntries.map((child) => renderEntry(child, depth + 1))}
      </div>
    )
  }

  if (!canBrowse) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
        {emptyMessage}
      </div>
    )
  }

  const selectedFileDetails = selectedFile ? entryDetails(selectedFile, fileContentType) : []
  const selectedFilePreviewURL = selectedFile && isHTMLFile(selectedFile)
    ? workspaceID
      ? workspaceFilePreviewURL(workspaceID, selectedFile.path)
      : sandboxID
        ? sandboxFilePreviewURL(sandboxID, selectedFile.path)
        : ''
    : ''
  const treePane = (
    <div className="flex min-w-0 flex-col border-b lg:border-b-0">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b px-3 focus-within:border-primary focus-within:shadow-[inset_0_-1px_0_hsl(var(--primary)),0_1px_0_hsl(var(--primary)/0.12)]">
        <span className="font-mono text-xs text-muted-foreground">$</span>
        <Input
          value={pathInput}
          onChange={(event) => setPathInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              openPath(pathInput)
            }
          }}
          className="h-7 min-w-0 flex-1 border-transparent bg-transparent px-2 font-mono text-xs shadow-none focus-visible:border-transparent focus-visible:ring-0"
          aria-label="Filesystem path"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground"
          aria-label="Open path"
          title="Open path"
          onClick={() => openPath(pathInput)}
          disabled={!pathInput.trim() || pathInput.trim() === path}
        >
          <ArrowRight className="h-4 w-4" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground"
          aria-label="Refresh files"
          title="Refresh files"
          onClick={refreshRoot}
          disabled={filesQuery.isFetching}
        >
          <RefreshCw className={cn('h-4 w-4', filesQuery.isFetching && 'animate-spin')} />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {filesQuery.isLoading ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            Loading files...
          </div>
        ) : filesQuery.isError ? (
          <div className="p-4 text-sm text-destructive">Failed to load files.</div>
        ) : (
          <div>
            {path !== '/' ? (
              <Button
                type="button"
                variant="ghost"
                className="flex h-8 w-full justify-start rounded-none px-1.5 font-normal"
                title={parentPath(path)}
                aria-label="Open parent directory"
                onClick={() => openPath(parentPath(path))}
              >
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className="h-3.5 w-3.5 shrink-0" />
                  <CornerUpLeft className="h-4 w-4 text-muted-foreground" />
                  <span className="truncate font-mono text-xs">..</span>
                </span>
              </Button>
            ) : null}
            {entries.map((entry) => renderEntry(entry))}
            {entries.length === 0 ? (
              <div className="p-4 text-sm text-muted-foreground">No files in this directory.</div>
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
  const previewPane = (
    <div className="flex min-w-0 flex-col">
      {selectedFile ? (
        <>
          <div className="flex h-10 shrink-0 items-center gap-3 border-b px-3">
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <File className="h-4 w-4 text-muted-foreground" />
              <span className="truncate font-mono text-xs">{selectedFile.path}</span>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground"
              aria-label="Copy file preview"
              title="Copy file preview"
              disabled={!filePreviewable || !fileContent}
              onClick={() => void navigator.clipboard?.writeText(fileContent)}
            >
              <Copy className="h-4 w-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground"
              aria-label="Download file"
              title="Download file"
              disabled={fileLoading}
              onClick={() => void downloadSelectedFile()}
            >
              <Download className="h-4 w-4" />
            </Button>
            {selectedFilePreviewURL ? (
              <a
                className={cn(
                  buttonVariants({ variant: 'ghost', size: 'icon' }),
                  'h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground',
                )}
                aria-label="Open HTML preview"
                title="Open HTML preview"
                href={selectedFilePreviewURL}
                target="_blank"
                rel="noreferrer"
              >
                <ExternalLink className="h-4 w-4" />
              </a>
            ) : null}
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-6 w-6 rounded-sm text-muted-foreground hover:text-foreground"
              aria-label="Close preview"
              title="Close preview"
              onClick={() => {
                selectedFilePathRef.current = null
                setSelectedFile(null)
              }}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <div className="min-h-0 w-full flex-1 overflow-auto bg-muted/20">
            {fileLoading ? (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Loading preview...
              </div>
            ) : fileError ? (
              <div className="p-4 text-sm text-destructive">{fileError}</div>
            ) : filePreviewable ? (
              <Suspense fallback={(
                <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Loading preview...
                </div>
              )}>
                <FileContentPreview
                  content={fileContent}
                  filename={selectedFile.name || selectedFile.path}
                />
              </Suspense>
            ) : (
              <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
                Preview is unavailable for this file. Use download to inspect it locally.
              </div>
            )}
          </div>
        </>
      ) : (
        <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
          Select a file to preview it here.
        </div>
      )}
    </div>
  )

  return (
    <div
      ref={splitContainerRef}
      className="grid h-full min-h-0 w-full grid-cols-1 grid-rows-[minmax(0,1fr)_auto_minmax(0,1fr)_auto] overflow-hidden bg-background lg:grid-cols-[minmax(0,1fr)_4px_var(--file-tree-width)] lg:grid-rows-[minmax(0,1fr)_auto]"
      style={{ '--file-tree-width': `${treeWidth}px` } as CSSProperties}
    >
      {previewPane}

      <div
        role="separator"
        aria-label="Resize file browser panes"
        aria-orientation="vertical"
        aria-valuemin={minTreeWidth}
        aria-valuemax={splitContainerRef.current ? Math.max(minTreeWidth, Math.round(splitContainerRef.current.getBoundingClientRect().width - minPreviewWidth)) : undefined}
        aria-valuenow={Math.round(treeWidth)}
        tabIndex={0}
        className="group hidden cursor-col-resize bg-transparent focus-visible:outline-none lg:block"
        onPointerDown={handleSeparatorPointerDown}
        onKeyDown={(event) => {
          if (event.key === 'ArrowLeft') {
            event.preventDefault()
            updateTreeWidth(treeWidth + 24)
          }
          if (event.key === 'ArrowRight') {
            event.preventDefault()
            updateTreeWidth(treeWidth - 24)
          }
        }}
      >
        <div className="mx-auto h-full w-px bg-border transition-[width,background-color] group-hover:w-1 group-hover:bg-primary/60 group-focus-visible:w-1 group-focus-visible:bg-primary/60" />
      </div>

      {treePane}

      <div
        role="status"
        aria-label="File browser status"
        className="col-span-full flex h-8 min-w-0 items-center justify-between gap-4 border-t px-3 text-xs text-muted-foreground"
      >
        <span className="min-w-0 truncate font-mono">{path}</span>
        {selectedFileDetails.length > 0 ? (
          <span className="flex min-w-0 items-center justify-end overflow-hidden text-right">
            {selectedFileDetails.map((detail, index) => (
              <span key={`${detail}-${index}`} className="flex min-w-0 items-center">
                {index > 0 ? <span className="mx-2 shrink-0 text-muted-foreground/70">·</span> : null}
                <span className="min-w-0 truncate">{detail}</span>
              </span>
            ))}
          </span>
        ) : null}
      </div>
    </div>
  )
}
