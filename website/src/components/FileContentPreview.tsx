import { css } from '@codemirror/lang-css'
import { html } from '@codemirror/lang-html'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'
import { python } from '@codemirror/lang-python'
import { sql } from '@codemirror/lang-sql'
import CodeMirror, { EditorView } from '@uiw/react-codemirror'
import { useEffect, useMemo, useState } from 'react'

import { fileExtension } from 'src/lib/file-extension'

function languageExtensions(extension: string) {
  if (['html', 'htm'].includes(extension)) return [html()]
  if (extension === 'css') return [css()]
  if (extension === 'json') return [json()]
  if (['js', 'jsx', 'mjs', 'cjs'].includes(extension)) return [javascript({ jsx: extension === 'jsx' })]
  if (['ts', 'tsx', 'mts', 'cts'].includes(extension)) return [javascript({ jsx: extension === 'tsx', typescript: true })]
  if (extension === 'md') return [markdown()]
  if (extension === 'py') return [python()]
  if (extension === 'sql') return [sql()]

  return []
}

const editorTheme = EditorView.theme({
  '&': { height: '100%', backgroundColor: 'transparent' },
  '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)' },
  '.cm-content': { padding: '1rem 0' },
  '.cm-line': { padding: '0 1rem' },
  '.cm-gutters': { backgroundColor: 'transparent', border: 'none' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'transparent' },
})
const editorContentAttributes = EditorView.contentAttributes.of({
  'aria-label': 'File content preview',
  inputmode: 'none',
})
const editorBasicSetup = {
  autocompletion: false,
  bracketMatching: false,
  closeBrackets: false,
  foldGutter: false,
  highlightActiveLine: false,
  highlightActiveLineGutter: false,
  lineNumbers: true,
}

interface FileContentPreviewProps {
  content: string
  filename: string
}

export default function FileContentPreview({ content, filename }: FileContentPreviewProps) {
  const [theme, setTheme] = useState<'light' | 'dark'>(() => (
    document.documentElement.classList.contains('dark') ? 'dark' : 'light'
  ))
  const extension = fileExtension(filename)
  const extensions = useMemo(() => [
    ...languageExtensions(extension),
    editorTheme,
    editorContentAttributes,
  ], [extension])

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setTheme(document.documentElement.classList.contains('dark') ? 'dark' : 'light')
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return (
    <CodeMirror
      value={content}
      height="100%"
      readOnly
      theme={theme}
      basicSetup={editorBasicSetup}
      extensions={extensions}
      className="h-full text-xs"
    />
  )
}
