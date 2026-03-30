import { Outlet, Link, useLocation } from 'react-router-dom'

function Layout() {
  const location = useLocation()

  const navItems = [
    { path: '/chat', label: 'Chat' },
    { path: '/dashboard', label: 'Dashboard' },
    { path: '/search', label: 'Search' },
    { path: '/alerts', label: 'Alerts' },
    { path: '/database', label: 'Database' },
  ]

  const isChat = location.pathname === '/chat'

  return (
    <div className="h-screen flex flex-col bg-gray-900 text-white">
      {/* Top Nav */}
      <header className="bg-gray-800 border-b border-gray-700 px-4 h-12 flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-6">
          <Link to="/chat" className="text-lg font-bold text-white hover:text-blue-400 transition-colors">
            MultiChat
          </Link>
          <nav className="flex items-center gap-1">
            {navItems.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                className={`px-3 py-1.5 rounded-md text-sm transition-colors ${
                  location.pathname === item.path
                    ? 'bg-blue-600 text-white'
                    : 'text-gray-400 hover:text-white hover:bg-gray-700'
                }`}
              >
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
      </header>

      {/* Content — Chat gets full height, other pages get padding */}
      {isChat ? (
        <main className="flex-1 overflow-hidden">
          <Outlet />
        </main>
      ) : (
        <main className="flex-1 p-6 overflow-auto">
          <Outlet />
        </main>
      )}
    </div>
  )
}

export default Layout
