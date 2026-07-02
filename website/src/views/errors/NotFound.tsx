import { NavLink } from 'react-router-dom'

// NotFound is the 404 error page.
function NotFound() {
  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center space-y-4">
        <h1 className="text-6xl font-bold text-foreground">404</h1>
        <p className="text-muted-foreground">Page not found</p>
        <NavLink to="/" className="text-primary hover:underline text-sm">
          Back to Home
        </NavLink>
      </div>
    </div>
  )
}

export default NotFound
