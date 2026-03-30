import { useState, useEffect } from 'react'
import { Outlet, Link, useLocation } from 'react-router-dom'

function Layout() {
  const location = useLocation()
  const [alertCount, setAlertCount] = useState(0)

  const navItems = [
    { path: '/dashboard', label: 'Dashboard', icon: '📊' },
    { path: '/inbox', label: 'Inbox', icon: '💬' },
    { path: '/search', label: 'Search', icon: '🔍' },
    { path: '/alerts', label: 'Alerts', icon: '🚨', badge: alertCount },
    { path: '/database', label: 'Database', icon: '🗄️' },
  ]

  // Fetch alert count periodically
  useEffect(() => {
    const fetchAlertCount = async () => {
      try {
        const response = await fetch('/api/alerts?hours=24')
        if (response.ok) {
          const data = await response.json()
          setAlertCount(data.total || 0)
        }
      } catch (error) {
        // Silently fail - not critical for navigation
      }
    }

    // Fetch immediately and then every 5 minutes
    fetchAlertCount()
    const interval = setInterval(fetchAlertCount, 5 * 60 * 1000)
    
    return () => clearInterval(interval)
  }, [])

  return (
    <div className="h-screen flex bg-gray-900 text-white">
      {/* Sidebar */}
      <div className="w-64 bg-gray-800 border-r border-gray-700 flex flex-col">
        {/* Logo/Header */}
        <div className="p-4 border-b border-gray-700">
          <h1 className="text-xl font-bold text-center">MultiChat</h1>
        </div>
        
        {/* Navigation */}
        <nav className="flex-1 p-4">
          <ul className="space-y-2">
            {navItems.map((item) => (
              <li key={item.path}>
                <Link
                  to={item.path}
                  className={`
                    flex items-center justify-between px-3 py-2 rounded-lg transition-colors
                    ${location.pathname === item.path 
                      ? 'bg-blue-600 text-white' 
                      : 'text-gray-300 hover:bg-gray-700 hover:text-white'
                    }
                  `}
                >
                  <div className="flex items-center space-x-3">
                    <span>{item.icon}</span>
                    <span>{item.label}</span>
                  </div>
                  {item.badge && item.badge > 0 && (
                    <span className="bg-red-500 text-white text-xs font-medium px-2 py-0.5 rounded-full min-w-[20px] text-center">
                      {item.badge > 99 ? '99+' : item.badge}
                    </span>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        
        {/* User section */}
        <div className="p-4 border-t border-gray-700">
          <div className="flex items-center space-x-3">
            <div className="w-8 h-8 bg-blue-500 rounded-full flex items-center justify-center text-sm">
              👤
            </div>
            <span className="text-sm text-gray-300">—</span>
          </div>
        </div>
      </div>
      
      {/* Main content area */}
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <header className="bg-gray-800 border-b border-gray-700 p-4">
          <h2 className="text-lg font-semibold">
            {navItems.find(item => item.path === location.pathname)?.label || 'MultiChat'}
          </h2>
        </header>
        
        {/* Content */}
        <main className="flex-1 p-6 overflow-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

export default Layout