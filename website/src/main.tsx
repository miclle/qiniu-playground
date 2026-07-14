import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { Theme } from '@radix-ui/themes'

import { queryClient } from 'src/lib/query-client'
import App from './App'

import '@radix-ui/themes/styles.css'
import 'src/globals.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Theme accentColor="blue" grayColor="slate" radius="medium" scaling="100%">
        <App />
      </Theme>
    </QueryClientProvider>
  </StrictMode>,
)
