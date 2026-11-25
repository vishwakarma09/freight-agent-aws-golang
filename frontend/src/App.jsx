import React, { Suspense, lazy } from 'react'
import { AppProvider, useApp } from './context/AppContext'
import LandingPage from './pages/LandingPage'
import PricingPage from './pages/PricingPage'
import LoginPage from './pages/LoginPage'
import RegisterPage from './pages/RegisterPage'
import Dashboard from './pages/Dashboard'
import {
  LayoutDashboard, Receipt, BarChart3, Mail, Bell, RefreshCw,
  Network, LogOut, ExternalLink, CreditCard, Building2
} from 'lucide-react'

const CarriersList = lazy(() => import('./pages/CarriersList'))
const CarrierDetails = lazy(() => import('./pages/CarrierDetails'))
const Billing = lazy(() => import('./pages/Billing'))
const Analytics = lazy(() => import('./pages/Analytics'))
const Simulator = lazy(() => import('./pages/Simulator'))
const Subscription = lazy(() => import('./pages/Subscription'))
const Organization = lazy(() => import('./pages/Organization'))

const AppContent = () => {
  const {
    selectedTab, setSelectedTab, notifications, loading, fetchData,
    isAuthenticated, logout, addNotification, backendEnv, billingSummary, user
  } = useApp()

  const isProd = backendEnv === 'prod' || backendEnv === 'production'
  const publicTabs = ['landing', 'pricing', 'login', 'register']
  const isPublicPage = publicTabs.includes(selectedTab)

  const authRef = React.useRef(isAuthenticated)
  React.useEffect(() => {
    authRef.current = isAuthenticated
  }, [isAuthenticated])

  // Handle activation / reset / invite URL query parameters
  React.useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const activated = params.get('activated')
    const activationError = params.get('activation_error')
    const resetToken = params.get('reset_token')
    const inviteToken = params.get('invite_token')

    if (activated === 'true') {
      window.history.replaceState({}, '', window.location.pathname)
      addNotification("Your account has been successfully activated! Please sign in.", "success")
      setSelectedTab('login')
    } else if (activationError) {
      window.history.replaceState({}, '', window.location.pathname)
      addNotification("Invalid or expired activation token. Please try registering again.", "error")
      setSelectedTab('register')
    } else if (resetToken) {
      setSelectedTab('login')
    } else if (inviteToken) {
      setSelectedTab('register')
    }
  }, [addNotification, setSelectedTab])

  // Synchronize URL path with selectedTab
  React.useEffect(() => {
    const pathMap = {
      '/': 'landing',
      '/pricing': 'pricing',
      '/login': 'login',
      '/register': 'register',
      '/dashboard': 'dashboard',
      '/connectors': 'connectors_list',
      '/analytics': 'analytics',
      '/simulator': 'simulator',
      '/ledger': 'billing',
      '/billing': 'billing',
      '/subscription': 'subscription',
      '/organization': 'organization'
    }

    const handlePopState = () => {
      const currentPath = window.location.pathname
      const tabFromPath = pathMap[currentPath] || 'landing'
      const publicTabsList = ['landing', 'pricing', 'login', 'register']
      if (!authRef.current && !publicTabsList.includes(tabFromPath)) {
        setSelectedTab('landing')
        window.history.replaceState({}, '', '/')
      } else {
        setSelectedTab(tabFromPath)
      }
    }

    handlePopState()
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [setSelectedTab])

  const isFirstRender = React.useRef(true)

  React.useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false
      return
    }

    const tabToPathMap = {
      'landing': '/',
      'pricing': '/pricing',
      'login': '/login',
      'register': '/register',
      'dashboard': '/dashboard',
      'connectors_list': '/connectors',
      'connector_details': '/connectors',
      'analytics': '/analytics',
      'simulator': '/simulator',
      'billing': '/ledger',
      'subscription': '/subscription',
      'organization': '/organization'
    }

    const targetPath = tabToPathMap[selectedTab] || '/'
    if (window.location.pathname !== targetPath) {
      window.history.pushState({}, '', targetPath)
    }
  }, [selectedTab])

  React.useEffect(() => {
    if (isPublicPage || !isAuthenticated) {
      document.body.classList.add('public-body')
    } else {
      document.body.classList.remove('public-body')
    }
    return () => document.body.classList.remove('public-body')
  }, [isPublicPage, isAuthenticated])

  const privateTabs = [
    { id: 'dashboard', name: 'Freight Pipeline', icon: LayoutDashboard },
    { id: 'connectors_list', name: 'Carriers Hub', icon: Network },
    { id: 'analytics', name: 'Analytics Center', icon: BarChart3 },
    ...(!isProd ? [{ id: 'simulator', name: 'Email Simulator', icon: Mail }] : []),
    { id: 'billing', name: 'Freight Ledger', icon: Receipt },
    { id: 'organization', name: 'Organization', icon: Building2 },
    { id: 'subscription', name: 'Subscription & Usage', icon: CreditCard }
  ]

  const renderActivePage = () => {
    if (!isAuthenticated && !isPublicPage) {
      return <LandingPage />
    }

    switch (selectedTab) {
      case 'landing':
        return <LandingPage />
      case 'pricing':
        return <PricingPage />
      case 'login':
        return <LoginPage />
      case 'register':
        return <RegisterPage />
      case 'dashboard':
        return <Dashboard />
      case 'billing':
        return <Billing />
      case 'analytics':
        return <Analytics />
      case 'simulator':
        return isProd ? <Dashboard /> : <Simulator />
      case 'connectors_list':
        return <CarriersList />
      case 'connector_details':
        return <CarrierDetails />
      case 'subscription':
        return <Subscription />
      case 'organization':
        return <Organization />
      default:
        return <LandingPage />
    }
  }

  if (isPublicPage || (!isAuthenticated && !isPublicPage)) {
    return (
      <div className="public-layout">
        <div className="toast-container">
          {notifications.map(n => (
            <div key={n.id} className={`toast-card ${n.type}`}>
              <Bell className="toast-icon" size={16} />
              <span className="toast-msg">{n.message}</span>
            </div>
          ))}
        </div>
        {renderActivePage()}
      </div>
    )
  }

  return (
    <div className="app-container">
      <div className="toast-container">
        {notifications.map(n => (
          <div key={n.id} className={`toast-card ${n.type}`}>
            <Bell className="toast-icon" size={16} />
            <span className="toast-msg">{n.message}</span>
          </div>
        ))}
      </div>

      <aside className="app-sidebar">
        <div className="sidebar-brand">
          <div className="brand-logo">D</div>
          <div className="brand-details">
            <span className="brand-name">DISPATCH</span>
            <span className="brand-sub">{user?.organization?.name || 'Mission Control'}</span>
          </div>
        </div>

        <nav className="sidebar-nav">
          {privateTabs.map(tab => {
            const Icon = tab.icon
            const isActive = selectedTab === tab.id || (tab.id === 'connectors_list' && selectedTab === 'connector_details')
            return (
              <button
                key={tab.id}
                onClick={() => setSelectedTab(tab.id)}
                className={`nav-item ${isActive ? 'active' : ''}`}
              >
                <Icon className="nav-icon" size={18} />
                <span>{tab.name}</span>
              </button>
            )
          })}
        </nav>

        <div className="sidebar-footer space-y-2 px-4 py-3 border-t border-white/5">
          <button
            onClick={() => setSelectedTab('landing')}
            className="w-full flex items-center gap-2 py-2 px-3 text-xs text-on-surface-variant hover:text-primary transition-colors hover:bg-white/5 rounded-lg"
          >
            <ExternalLink size={14} />
            <span>Go to Landing</span>
          </button>

          <button
            onClick={logout}
            className="w-full flex items-center gap-2 py-2 px-3 text-xs text-on-surface-variant hover:text-error transition-colors hover:bg-white/5 rounded-lg"
          >
            <LogOut size={14} />
            <span>Logout</span>
          </button>

          <div className="h-2"></div>

          <div className="status-indicator online">
            <div className="status-dot"></div>
            <span>{isProd ? 'API Connected' : 'Dev Sandbox'}</span>
          </div>
          <button className="sync-button w-full" onClick={() => fetchData(true)} disabled={loading}>
            <RefreshCw className={`sync-icon ${loading ? 'spinning' : ''}`} size={14} />
            <span>Force Sync</span>
          </button>
        </div>
      </aside>

      <main className="app-main">
        {selectedTab !== 'simulator' && (
          <header className="main-header">
            <div className="header-title-area">
              <h1 className="header-title">
                {selectedTab === 'connector_details'
                  ? 'Carrier Configuration'
                  : privateTabs.find(t => t.id === selectedTab)?.name}
              </h1>
              <p className="header-subtitle">
                Automated competitive carrier bidding &amp; quote pipeline
              </p>
            </div>
            <div className="header-actions flex items-center gap-3">
              {billingSummary && (
                <button
                  onClick={() => setSelectedTab('subscription')}
                  className="px-3 py-1.5 rounded-lg bg-primary/10 border border-primary/20 text-primary text-xs font-bold hover:bg-primary/20 transition-all"
                >
                  {billingSummary.plan_name} &bull; {billingSummary.usage?.quotes_used || 0}/{billingSummary.usage?.quotes_limit || 25} Quotes
                </button>
              )}
              {!isProd && (
                <div className="system-timer">
                  <span className="timer-label">Mode:</span>
                  <span className="timer-value badge-fast">SANDBOX</span>
                </div>
              )}
            </div>
          </header>
        )}

        <div className="content-viewport">
          <Suspense fallback={<div className="flex items-center justify-center min-h-[240px] text-on-surface-variant text-sm">Loading view...</div>}>
            {renderActivePage()}
          </Suspense>
        </div>
      </main>
    </div>
  )
}

const App = () => {
  return (
    <AppProvider>
      <AppContent />
    </AppProvider>
  )
}

export default App
