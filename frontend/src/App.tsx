import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Chat from './pages/Chat'
import Database from './pages/Database'
import Search from './pages/Search'
import Alerts from './pages/Alerts'

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/" element={<Layout />}>
        <Route index element={<Navigate to="/chat" replace />} />
        <Route path="dashboard" element={<Dashboard />} />
        <Route path="chat" element={<Chat />} />
        <Route path="search" element={<Search />} />
        <Route path="alerts" element={<Alerts />} />
        <Route path="database" element={<Database />} />
      </Route>
    </Routes>
  )
}

export default App