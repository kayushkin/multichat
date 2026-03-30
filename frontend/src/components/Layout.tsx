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
    <div className="h-screen flex flex-col bg-black text-white">
      {/* Top Nav */}
      <header className="bg-gray-950 border-b border-gray-700 px-4 h-12 flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-6">
          <Link to="/chat" className="text-lg font-bold hover:opacity-80 transition-opacity" style={{ color: '#f5d40c' }}>
            MultiChat
          </Link>
          <nav className="flex items-center gap-1">
            {navItems.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                  location.pathname === item.path
                    ? 'text-gray-900'
                    : 'text-gray-400 hover:text-white hover:bg-gray-700'
                }`}
                style={location.pathname === item.path ? { backgroundColor: '#f5d40c' } : undefined}
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
