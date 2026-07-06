import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { RotateCw } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'

import { Button } from 'src/components/ui/button'

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
    let ready = false
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
    terminal.writeln('Connecting to sandbox shell...')

    const fitTerminal = () => {
      if (!activeRef.current) {
        return
      }
      try {
        fit.fit()
      } catch {
        return
      }
      const cols = terminal.cols || defaultTerminalSize.cols
      const rows = terminal.rows || defaultTerminalSize.rows
      const socket = socketRef.current
      if (ready && socket?.readyState === WebSocket.OPEN) {
        sendResize(socket, cols, rows)
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
      ready = true
      terminal.reset()
      fitTerminal()
      if (workspacePath) {
        sendInput(socket, `cd ${shellQuote(workspacePath)}\r`)
      }
      setStatus('Connected')
      terminal.focus()
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
      ready = false
      setStatus('Disconnected')
      terminal.writeln('\r\nDisconnected.')
    })
    const disposeInput = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        scheduleInput(data)
      }
    })
    const disposeResize = terminal.onResize(({ cols, rows }) => {
      if (socket.readyState === WebSocket.OPEN) {
        sendResize(socket, cols, rows)
      }
    })
    const onResize = () => fitTerminal()
    const resizeObserver = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(fitTerminal)
    resizeObserver?.observe(containerRef.current)
    window.addEventListener('resize', onResize)
    window.requestAnimationFrame(fitTerminal)

    return () => {
      disposed = true
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
    window.requestAnimationFrame(() => {
      try {
        fitRef.current?.fit()
      } catch {
        return
      }
      terminalRef.current?.focus()
    })
  }, [active])

  const displayStatus = disabled ? 'Waiting for sandbox' : status
  const displayError = disabled ? '' : error

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-[#0b0f14] text-slate-200">
      <div className="flex min-h-10 shrink-0 items-center gap-3 border-b border-white/10 px-3 text-xs">
        <span className="rounded-sm border border-white/10 px-2 py-0.5 font-mono text-[11px] text-slate-300">
          {displayStatus}
        </span>
        {displayError ? <span className="min-w-0 truncate text-red-300">{displayError}</span> : null}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="ml-auto h-7 w-7 text-slate-300 hover:bg-white/10 hover:text-white"
          onClick={reconnect}
          disabled={disabled}
          aria-label="Reconnect terminal"
        >
          <RotateCw className="h-4 w-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-2 pt-2 pb-[5px]">
        <div ref={containerRef} className="h-full min-h-0 overflow-hidden" />
      </div>
    </div>
  )
}

export default TerminalPanel
