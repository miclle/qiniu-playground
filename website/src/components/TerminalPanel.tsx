import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { IconButton } from '@radix-ui/themes'
import { RotateCw } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'

interface TerminalPanelProps {
  sandboxID: string
  workspacePath?: string
  disabled?: boolean
  active?: boolean
}

const defaultTerminalSize = {
  cols: 120,
  rows: 32,
}

function shellQuote(value: string) {
  return `'${value.replace(/'/g, `'\\''`)}'`
}

function sendInput(socket: WebSocket, data: string) {
  socket.send(JSON.stringify({ type: 'input', data }))
}

function sendResize(socket: WebSocket, cols: number, rows: number) {
  socket.send(JSON.stringify({ type: 'resize', cols, rows }))
}

function TerminalPanel({ sandboxID, workspacePath, disabled = false, active = true }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const terminalRef = useRef<Terminal | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const activeRef = useRef(active)
  const readyRef = useRef(false)
  const lastSentSizeRef = useRef<{ cols: number; rows: number } | null>(null)
  const inputBufferRef = useRef('')
  const inputTimerRef = useRef<number | null>(null)
  const [connectionID, setConnectionID] = useState(0)
  const [status, setStatus] = useState(disabled ? 'Waiting for sandbox' : 'Connecting')
  const [error, setError] = useState('')

  const reconnect = useCallback(() => {
    setStatus('Connecting')
    setError('')
    setConnectionID((value) => value + 1)
  }, [])

  useEffect(() => {
    activeRef.current = active
  }, [active])

  useEffect(() => {
    if (!containerRef.current || disabled) {
      return
    }
    let disposed = false
    containerRef.current.innerHTML = ''

    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      allowProposedApi: false,
      theme: {
        background: '#0b0f14',
        foreground: '#e5edf6',
        cursor: '#f7c948',
        selectionBackground: '#375a7f',
      },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(containerRef.current)
    terminalRef.current = terminal
    fitRef.current = fit
    lastSentSizeRef.current = null
    terminal.writeln('Connecting to sandbox shell...')

    const sendResizeIfChanged = (cols: number, rows: number) => {
      const socket = socketRef.current
      if (!readyRef.current || socket?.readyState !== WebSocket.OPEN) {
        return
      }
      const lastSize = lastSentSizeRef.current
      if (lastSize?.cols === cols && lastSize.rows === rows) {
        return
      }
      lastSentSizeRef.current = { cols, rows }
      sendResize(socket, cols, rows)
    }

    const fitTerminal = () => {
      if (!activeRef.current) {
        return null
      }
      try {
        fit.fit()
      } catch {
        return null
      }
      const cols = terminal.cols || defaultTerminalSize.cols
      const rows = terminal.rows || defaultTerminalSize.rows
      sendResizeIfChanged(cols, rows)
      return { cols, rows }
    }

    let cancelFit: (() => void) | null = null

    const fitWhenVisible = (callback?: () => void) => {
      cancelFit?.()
      let completed = false
      let observer: ResizeObserver | null = null
      let fallbackTimer: number | null = null
      let animationFrame: number | null = null

      const complete = () => {
        if (completed || disposed) {
          return
        }
        completed = true
        observer?.disconnect()
        if (fallbackTimer !== null) {
          window.clearTimeout(fallbackTimer)
          fallbackTimer = null
        }
        if (animationFrame !== null) {
          window.cancelAnimationFrame(animationFrame)
          animationFrame = null
        }
        callback?.()
      }

      const run = () => {
        if (completed || disposed) {
          return
        }
        const size = fitTerminal()
        if (size && size.cols >= 40) {
          complete()
        }
      }

      if (typeof ResizeObserver !== 'undefined' && containerRef.current) {
        observer = new ResizeObserver(run)
        observer.observe(containerRef.current)
      }

      animationFrame = window.requestAnimationFrame(run)
      fallbackTimer = window.setTimeout(complete, 300)

      cancelFit = () => {
        completed = true
        observer?.disconnect()
        if (fallbackTimer !== null) {
          window.clearTimeout(fallbackTimer)
          fallbackTimer = null
        }
        if (animationFrame !== null) {
          window.cancelAnimationFrame(animationFrame)
          animationFrame = null
        }
      }
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/sandboxes/${sandboxID}/pty`)
    socketRef.current = socket

    const flushInput = () => {
      const data = inputBufferRef.current
      inputBufferRef.current = ''
      inputTimerRef.current = null
      if (!data || socket.readyState !== WebSocket.OPEN) {
        return
      }
      sendInput(socket, data)
    }

    const scheduleInput = (data: string) => {
      inputBufferRef.current += data
      if (inputTimerRef.current !== null) {
        return
      }
      inputTimerRef.current = window.setTimeout(flushInput, 10)
    }

    socket.addEventListener('open', () => {
      readyRef.current = true
      terminal.reset()
      setStatus('Connected')
      fitWhenVisible(() => {
        if (workspacePath && socket.readyState === WebSocket.OPEN) {
          sendInput(socket, `cd ${shellQuote(workspacePath)}\r`)
        }
        terminal.focus()
      })
    })
    socket.addEventListener('message', (event) => {
      terminal.write(String(event.data))
    })
    socket.addEventListener('error', () => {
      if (disposed) {
        return
      }
      setError('Connection error')
      setStatus('Failed')
    })
    socket.addEventListener('close', () => {
      if (disposed) {
        return
      }
      readyRef.current = false
      setStatus('Disconnected')
      terminal.writeln('\r\nDisconnected.')
    })
    const disposeInput = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        scheduleInput(data)
      }
    })
    const disposeResize = terminal.onResize(({ cols, rows }) => {
      sendResizeIfChanged(cols, rows)
    })
    const onResize = () => fitTerminal()
    const resizeObserver = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(fitTerminal)
    resizeObserver?.observe(containerRef.current)
    window.addEventListener('resize', onResize)
    fitWhenVisible()

    return () => {
      disposed = true
      readyRef.current = false
      lastSentSizeRef.current = null
      cancelFit?.()
      resizeObserver?.disconnect()
      window.removeEventListener('resize', onResize)
      disposeInput.dispose()
      disposeResize.dispose()
      if (inputTimerRef.current !== null) {
        window.clearTimeout(inputTimerRef.current)
        inputTimerRef.current = null
      }
      inputBufferRef.current = ''
      socket.close()
      terminal.dispose()
      terminalRef.current = null
      fitRef.current = null
      socketRef.current = null
    }
  }, [connectionID, disabled, sandboxID, workspacePath])

  useEffect(() => {
    if (!active) {
      return
    }
    let attempts = 0
    let requestID: number
    const refit = () => {
      attempts += 1
      let fitSuccess = false
      try {
        fitRef.current?.fit()
        fitSuccess = true
      } catch {
        // xterm can throw while the tab is visible but layout dimensions are still settling.
      }
      const terminal = terminalRef.current
      const socket = socketRef.current
      if (fitSuccess && terminal && readyRef.current && socket?.readyState === WebSocket.OPEN) {
        const cols = terminal.cols || defaultTerminalSize.cols
        const rows = terminal.rows || defaultTerminalSize.rows
        const lastSize = lastSentSizeRef.current
        if (lastSize?.cols !== cols || lastSize.rows !== rows) {
          lastSentSizeRef.current = { cols, rows }
          sendResize(socket, cols, rows)
        }
      }
      if (fitSuccess) {
        terminal?.focus()
        return
      }
      if (attempts < 3) {
        requestID = window.requestAnimationFrame(refit)
        return
      }
      terminal?.focus()
    }
    requestID = window.requestAnimationFrame(refit)
    return () => window.cancelAnimationFrame(requestID)
  }, [active])

  const displayStatus = disabled ? 'Waiting for sandbox' : status
  const displayError = disabled ? '' : error

  return (
    <div className="flex h-full min-h-0 w-full flex-col overflow-hidden bg-[#0b0f14] text-slate-200">
      <div className="flex min-h-10 shrink-0 items-center gap-3 border-b border-white/10 px-3 text-xs">
        <span className="rounded-sm border border-white/10 px-2 py-0.5 font-mono text-[11px] text-slate-300">
          {displayStatus}
        </span>
        {displayError ? <span className="min-w-0 truncate text-red-300">{displayError}</span> : null}
        <IconButton
          type="button"
          variant="ghost"
          color="gray"
          size="1"
          className="ml-auto text-slate-300 hover:bg-white/10 hover:text-white"
          onClick={reconnect}
          disabled={disabled}
          aria-label="Reconnect terminal"
        >
          <RotateCw className="h-4 w-4" />
        </IconButton>
      </div>
      <div className="min-h-0 w-full flex-1 overflow-hidden px-2 pt-2 pb-[5px]">
        <div ref={containerRef} className="h-full min-h-0 w-full overflow-hidden" />
      </div>
    </div>
  )
}

export default TerminalPanel
