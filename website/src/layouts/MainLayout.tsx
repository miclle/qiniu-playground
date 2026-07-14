import { useQuery } from '@tanstack/react-query'
import { Button, IconButton } from '@radix-ui/themes'
import { Code2, GitBranch, KeyRound, Layers, LogOut, PanelsTopLeft, PlaySquare, Server } from 'lucide-react'
import { Outlet, NavLink, useNavigate } from 'react-router-dom'
import { currentUser, logout } from 'src/api/auth'

const navigationItems = [
  { label: 'Workspaces', to: '/workspaces', icon: PanelsTopLeft },
  { label: 'Code Runner', to: '/code-runner', icon: PlaySquare },
  { label: 'Codebases', to: '/codebases', icon: Code2 },
  { label: 'Credentials', to: '/credentials', icon: KeyRound },
  { label: 'Templates', to: '/templates', icon: Layers },
  { label: 'Sandboxes', to: '/sandboxes', icon: Server },
]

// MainLayout provides the top navigation bar and content area.
function MainLayout() {
  const navigate = useNavigate()
  const { data } = useQuery({
    queryKey: ['auth', 'me'],
    queryFn: currentUser,
    retry: false,
  })
  const user = data?.data

  async function handleLogout() {
    await logout()
    navigate('/login')
  }

  return (
    <div className="min-h-screen bg-background text-foreground lg:grid lg:grid-cols-[260px_1fr]">
      <aside className="z-40 flex flex-col border-b bg-background px-3 py-4 lg:sticky lg:top-0 lg:h-screen lg:border-b-0 lg:border-r">
        <NavLink
          to="/"
          className="mb-5 flex h-10 items-center gap-3 rounded-md px-3 text-base font-semibold text-foreground no-underline"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <GitBranch className="h-4 w-4" />
          </span>
          Qiniu Playground
        </NavLink>

        <nav className="space-y-1">
          {navigationItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                className={({ isActive }) =>
                  `flex h-9 items-center gap-3 rounded-md px-3 text-sm no-underline hover:bg-secondary hover:text-foreground ${isActive ? 'bg-secondary text-foreground' : 'text-muted-foreground'}`
                }
                to={item.to}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </NavLink>
            )
          })}
        </nav>

        <div className="mt-auto border-t pt-3">
          {user ? (
            <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
              <img className="h-8 w-8 rounded-full" src={user.avatar_url} alt="" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium leading-none">{user.login}</p>
                <p className="mt-1 truncate text-xs text-muted-foreground">{user.name || 'GitHub account'}</p>
              </div>
              <IconButton
                type="button"
                aria-label="Sign out"
                variant="outline"
                color="gray"
                size="2"
                className="shrink-0 text-muted-foreground hover:text-foreground"
                onClick={handleLogout}
              >
                <LogOut className="h-4 w-4" />
              </IconButton>
            </div>
          ) : (
            <Button asChild variant="outline" color="gray" size="2" className="gap-2 no-underline">
              <a href="/login">
                <GitBranch className="h-4 w-4" />
                Sign in
              </a>
            </Button>
          )}
        </div>
      </aside>

      <main className="min-w-0">
        <Outlet />
      </main>
    </div>
  )
}

export default MainLayout
