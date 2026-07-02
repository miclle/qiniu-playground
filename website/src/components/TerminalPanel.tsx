import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useEffect, useRef } from 'react'
import '@xterm/xterm/css/xterm.css'

interface TerminalPanelProps {
  sandboxID: string
}

function TerminalPanel({ sandboxID }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!containerRef.current) {
      return
    }
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#0a0a0a',
        foreground: '#f4f4f5',
      },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(containerRef.current)
    fit.fit()
    terminal.writeln('Connecting to sandbox...')

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/sandboxes/${sandboxID}/pty`)

    socket.addEventListener('open', () => {
      terminal.reset()
      terminal.focus()
    })
    socket.addEventListener('message', (event) => {
      terminal.write(String(event.data))
    })
    socket.addEventListener('close', () => {
      terminal.writeln('\r\nDisconnected.')
    })
    const disposeInput = terminal.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(data)
      }
    })
    const onResize = () => fit.fit()
    window.addEventListener('resize', onResize)

    return () => {
      window.removeEventListener('resize', onResize)
      disposeInput.dispose()
      socket.close()
      terminal.dispose()
    }
  }, [sandboxID])

  return <div ref={containerRef} className="h-72 overflow-hidden rounded-md bg-black p-2" />
}

export default TerminalPanel
