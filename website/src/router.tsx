import type { RouteObject } from 'react-router-dom'
import { Navigate } from 'react-router-dom'

import MainLayout from 'src/layouts/MainLayout'
import Home from 'src/views/home'
import Login from 'src/views/login'
import NotFound from 'src/views/errors/NotFound'
import WorkspaceDetail from 'src/views/workspace-detail'
import CodeRunner from 'src/views/code-runner'

const routes: RouteObject[] = [
  {
    path: '/',
    element: <MainLayout />,
    children: [
      { index: true, element: <Navigate to="/workspaces" replace /> },
      { path: 'workspaces', element: <Home page="workspaces" /> },
      { path: 'code-runner', element: <CodeRunner /> },
      { path: 'code-runner/:sessionId', element: <CodeRunner /> },
      { path: 'codebases', element: <Home page="codebase" /> },
      { path: 'credentials', element: <Home page="credentials" /> },
      { path: 'templates', element: <Home page="templates" /> },
      { path: 'sandboxes', element: <Home page="sandbox" /> },
    ],
  },
  { path: '/workspaces/:workspaceId', element: <WorkspaceDetail /> },
  { path: '/login', element: <Login /> },
  { path: '*', element: <NotFound /> },
]

export default routes
