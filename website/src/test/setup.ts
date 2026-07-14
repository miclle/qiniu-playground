import React from 'react'
import { Theme } from '@radix-ui/themes'
import type { Root } from 'react-dom/client'
import { vi } from 'vitest'

class TestResizeObserver implements ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = TestResizeObserver
}

vi.mock('react-dom/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-dom/client')>()

  return {
    ...actual,
    createRoot(container: Element | DocumentFragment, options?: Parameters<typeof actual.createRoot>[1]): Root {
      const root = actual.createRoot(container, options)

      return {
        render(children: React.ReactNode) {
          root.render(
            React.createElement(
              Theme,
              { accentColor: 'blue', grayColor: 'slate', radius: 'medium', scaling: '100%' },
              children,
            ),
          )
        },
        unmount() {
          root.unmount()
        },
      }
    },
  }
})
