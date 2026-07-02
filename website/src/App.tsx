import { RouterProvider, createBrowserRouter } from 'react-router-dom'
import { AppContext } from 'src/context/app'
import routes from './router'

const router = createBrowserRouter(routes)

// App is the root component wrapping providers and the router.
function App() {
  return (
    <AppContext.Provider value={{ appName: 'Qiniu Playground' }}>
      <RouterProvider router={router} />
    </AppContext.Provider>
  )
}

export default App
