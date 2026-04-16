import { Routes, Route, NavLink, Navigate } from 'react-router-dom'
import {
  LayoutDashboard, Server, Palette, KeyRound, Radio, LogOut, Shield, Settings,
} from 'lucide-react'
import { AuthProvider, useAuth } from './hooks/useAuth'
import { ToastProviderWithRender } from './hooks/useToast'
import { ToastContainer, ErrorBoundary, NetworkBanner } from './components'

// Pages
import LoginPage from './pages/Login.jsx'
import DashboardPage from './pages/Dashboard.jsx'
import ServersPage from './pages/Servers.jsx'
import BrandingPage from './pages/Branding.jsx'
import CodesPage from './pages/Codes.jsx'
import AgentsPage from './pages/Agents.jsx'
import SettingsPage from './pages/Settings.jsx'

export default function App() {
  return (
    <ErrorBoundary>
      <ToastProviderWithRender>
        <AuthProvider>
          <NetworkBanner />
          <AppShell />
          <ToastContainer />
        </AuthProvider>
      </ToastProviderWithRender>
    </ErrorBoundary>
  )
}

// --- App Shell (auth-gated) ---
function AppShell() {
  const { authed, tenant, loading, login, logout } = useAuth()

  // Show nothing during initial auth check
  if (loading) return null

  if (!authed) {
    return <LoginPage onLogin={login} />
  }

  return (
    <>
      <div className="app-bg" />
      <div className="layout">
        {/* Top bar */}
        <header className="topbar">
          <div className="topbar-logo">
            <Shield size={20} strokeWidth={1.5} />
            SHIGUANG CLOUD
          </div>
          <div className="topbar-spacer" />
          {tenant && (
            <div className="topbar-user">
              <span>{tenant.name}</span>
              <span className={`topbar-plan ${tenant.plan}`}>{tenant.plan}</span>
            </div>
          )}
          <NavLink to="/settings" className="topbar-logout" title="Settings">
            <Settings size={14} />
          </NavLink>
        </header>

        {/* Sidebar navigation */}
        <nav className="sidebar">
          <div className="sidebar-section">
            <div className="sidebar-label">Overview</div>
            <NavLink to="/" end className="nav-link">
              <LayoutDashboard /> Dashboard
            </NavLink>
          </div>
          <div className="sidebar-section">
            <div className="sidebar-label">Infrastructure</div>
            <NavLink to="/servers" className="nav-link">
              <Server /> Server Lines
            </NavLink>
            <NavLink to="/agents" className="nav-link">
              <Radio /> Gate Agents
            </NavLink>
          </div>
          <div className="sidebar-section">
            <div className="sidebar-label">Configuration</div>
            <NavLink to="/branding" className="nav-link">
              <Palette /> Branding
            </NavLink>
            <NavLink to="/codes" className="nav-link">
              <KeyRound /> Invite Codes
            </NavLink>
          </div>
          <div className="sidebar-section">
            <div className="sidebar-label">Account</div>
            <NavLink to="/settings" className="nav-link">
              <Settings /> Settings
            </NavLink>
          </div>
          <div className="sidebar-footer">
            <div className="sidebar-version">ShiguangCloud v2</div>
          </div>
        </nav>

        {/* Main content */}
        <main className="main">
          <Routes>
            <Route path="/" element={<DashboardPage tenant={tenant} />} />
            <Route path="/servers" element={<ServersPage />} />
            <Route path="/branding" element={<BrandingPage />} />
            <Route path="/codes" element={<CodesPage />} />
            <Route path="/agents" element={<AgentsPage />} />
            <Route path="/settings" element={<SettingsPage tenant={tenant} onLogout={logout} />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
    </>
  )
}
