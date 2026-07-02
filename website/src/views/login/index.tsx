import { GitBranch } from 'lucide-react'
import { githubLoginURL } from 'src/api/auth'
import { buttonVariants } from 'src/components/ui/button'
import { cn } from 'src/lib/utils'

function Login() {
  return (
    <main className="min-h-screen bg-background">
      <div className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6">
        <div className="space-y-6">
          <div className="space-y-2">
            <p className="text-sm font-medium text-muted-foreground">Qiniu Playground</p>
            <h1 className="text-3xl font-semibold tracking-normal text-foreground">
              Sign in to your workspace.
            </h1>
            <p className="text-sm leading-6 text-muted-foreground">
              Use GitHub to connect repositories before launching sandbox IDE sessions.
            </p>
          </div>
          <a
            className={cn(buttonVariants({ size: 'lg' }), 'w-full gap-2 no-underline')}
            href={githubLoginURL()}
          >
            <GitBranch className="h-4 w-4" />
            Continue with GitHub
          </a>
        </div>
      </div>
    </main>
  )
}

export default Login
