import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Inbox from './pages/Inbox'
import Search from './pages/Search'
import Alerts from './pages/Alerts'
import Database from './pages/Database'

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/" element={<Layout />}>
        <Route index element={<Navigate to="/dashboard" replace />} />
        <Route path="dashboard" element={<Dashboard />} />
        <Route path="inbox" element={<Inbox />} />
        <Route path="search" element={<Search />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="database" element={<Database />} />
      </Route>
    </Routes>
  )
}

export default App