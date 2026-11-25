import React, { createContext, useContext, useState, useEffect, useCallback } from 'react'
import * as api from '../services/api'

const AppContext = createContext()

export const AppProvider = ({ children }) => {
  const [quotes, setQuotes] = useState([])
  const [carriers, setCarriers] = useState([])
  const [customers, setCustomers] = useState([])
  const [editingCarrierId, setEditingCarrierId] = useState(null)
  const [analytics, setAnalytics] = useState(null)
  const [billingSummary, setBillingSummary] = useState(null)
  const [backendEnv, setBackendEnv] = useState('prod')
  const [loading, setLoading] = useState(false)
  const [activeQuote, setActiveQuote] = useState(null)
  const [historicalRag, setHistoricalRag] = useState([])
  const [selectedTab, setSelectedTab] = useState('landing')
  const [notifications, setNotifications] = useState([])

  // Clear legacy insecure localStorage key if present
  useEffect(() => {
    localStorage.removeItem('userEmail')
  }, [])

  const [isAuthenticated, setIsAuthenticated] = useState(() => {
    return Boolean(localStorage.getItem('accessToken'))
  })

  const [user, setUser] = useState(() => {
    try {
      const saved = localStorage.getItem('userProfile')
      return saved ? JSON.parse(saved) : null
    } catch {
      return null
    }
  })

  const addNotification = useCallback((message, type = 'success') => {
    const id = Date.now() + Math.random()
    setNotifications(prev => [...prev, { id, message, type }])
    setTimeout(() => {
      setNotifications(prev => prev.filter(n => n.id !== id))
    }, 4500)
  }, [])

  // Listen for 401 unauthorized events from Axios interceptor
  useEffect(() => {
    const handleUnauthorized = () => {
      setIsAuthenticated(false)
      setUser(null)
      setBillingSummary(null)
      setSelectedTab('login')
      addNotification("Session expired. Please sign in again.", "error")
    }
    window.addEventListener('auth:unauthorized', handleUnauthorized)
    return () => window.removeEventListener('auth:unauthorized', handleUnauthorized)
  }, [addNotification])

  const fetchData = useCallback(async (showLoading = false, analyticsDays = null) => {
    if (!localStorage.getItem('accessToken')) return
    if (showLoading) setLoading(true)
    try {
      const [qData, cData, custData, aData, bData, envData, meData] = await Promise.all([
        api.getQuotes(),
        api.getCarriers(),
        api.getCustomers(),
        api.getAnalytics(analyticsDays),
        api.getBillingSummary().catch(() => null),
        api.getEmailEnv().catch(() => ({ env: 'prod' })),
        api.getMe().catch(() => null),
      ])
      setQuotes(qData || [])
      setCarriers(cData || [])
      setCustomers(custData || [])
      setAnalytics(aData)
      if (bData) setBillingSummary(bData)
      if (envData?.env) setBackendEnv(envData.env)
      if (meData) {
        setUser(meData)
        localStorage.setItem('userProfile', JSON.stringify(meData))
      }
    } catch (err) {
      if (err.response?.status !== 401) {
        console.error("Failed to load application data:", err)
        addNotification("Error loading data from server", "error")
      }
    } finally {
      setLoading(false)
    }
  }, [addNotification])

  useEffect(() => {
    const publicTabs = ['landing', 'pricing', 'login', 'register']
    const isPublicPage = publicTabs.includes(selectedTab)

    if (!isAuthenticated || isPublicPage) {
      return
    }

    fetchData(true)
    const interval = setInterval(() => {
      fetchData(false)
    }, 10000)
    return () => clearInterval(interval)
  }, [isAuthenticated, selectedTab, fetchData])

  const selectQuote = async (id) => {
    if (!id) {
      setActiveQuote(null)
      setHistoricalRag([])
      return
    }
    try {
      const [detailedQuote, ragData] = await Promise.all([
        api.getQuote(id),
        api.getHistoricalRag(id)
      ])
      setActiveQuote(detailedQuote)
      setHistoricalRag(ragData || [])
    } catch (err) {
      console.error("Failed to fetch detailed quote info:", err)
      addNotification("Failed to load quote details", "error")
    }
  }

  const handleManualOverride = async (quoteId, toStatus, notes) => {
    try {
      await api.manualOverride(quoteId, toStatus, notes)
      addNotification(`Quote ${quoteId} overridden to stage: ${toStatus}`)
      fetchData()
      if (activeQuote && activeQuote.id === quoteId) {
        selectQuote(quoteId)
      }
    } catch (err) {
      addNotification(err.response?.data?.detail || "Failed to apply manual override", "error")
    }
  }

  const handleApproval = async (quoteId, approved, lostReason = "", competitorInfo = "") => {
    try {
      await api.approveQuote(quoteId, { approved, lost_reason: lostReason, competitor_info: competitorInfo })
      addNotification(approved ? `Quote ${quoteId} APPROVED & booked!` : `Quote ${quoteId} marked as LOST`)
      fetchData()
      if (activeQuote && activeQuote.id === quoteId) {
        selectQuote(quoteId)
      }
    } catch (err) {
      addNotification(err.response?.data?.detail || "Resolution action failed", "error")
    }
  }

  const handleSendMockEmail = async (sender, recipient, subject, body) => {
    try {
      const response = await api.sendMockEmail({ sender, recipient, subject, body })
      if (response.type === "CUSTOMER_INQUIRY") {
        addNotification(`Mock inquiry ingested! Quote ${response.quote_id} created.`)
      } else if (response.type === "CARRIER_BID") {
        addNotification(`Mock bid received! Carrier ${response.carrier} bid $${response.bid_amount}.`)
      }
      fetchData()
      return response
    } catch (err) {
      addNotification(err.response?.data?.detail || "Email simulator error", "error")
      throw err
    }
  }

  const handleFastForwardTimers = async () => {
    try {
      const response = await api.fastForwardTimers()
      addNotification(response.message || "Timers fast-forwarded successfully!")
      fetchData()
      return response
    } catch (err) {
      addNotification(err.response?.data?.detail || "Failed to fast-forward timers", "error")
      throw err
    }
  }

  const handleResetDatabase = async () => {
    try {
      const response = await api.resetSimulatorDatabase()
      addNotification(response.message || "Workspace simulation quotes cleared!")
      fetchData()
      return response
    } catch (err) {
      addNotification(err.response?.data?.detail || "Failed to reset simulation data", "error")
      throw err
    }
  }

  const completeAuthSession = (accessToken, userObj, successMsg) => {
    localStorage.setItem('accessToken', accessToken)
    localStorage.setItem('userProfile', JSON.stringify(userObj))
    setIsAuthenticated(true)
    setUser(userObj)
    addNotification(successMsg, "success")
    setSelectedTab('dashboard')
  }

  const login = async (email, password) => {
    try {
      const response = await api.login({ email, password })
      completeAuthSession(response.access_token, response.user, "Logged in successfully! Welcome back.")
    } catch (err) {
      const errorMsg = err.response?.data?.detail || "Invalid email or password"
      addNotification(errorMsg, "error")
      throw err
    }
  }

  const register = async (name, email, password, organizationName = '') => {
    try {
      const response = await api.register({ name, email, password, organization_name: organizationName })
      addNotification(response.message || "Registration successful! Please check your email to activate.", "success")
      return response
    } catch (err) {
      const errorMsg = err.response?.data?.detail || "Registration failed"
      addNotification(errorMsg, "error")
      throw err
    }
  }

  const acceptInvite = async (token, name, password) => {
    try {
      const response = await api.acceptInvite({ token, name, password })
      completeAuthSession(response.access_token, response.user, "Invitation accepted! Welcome to your Organization workspace.")
      return response
    } catch (err) {
      const errorMsg = err.response?.data?.detail || "Could not accept invitation"
      addNotification(errorMsg, "error")
      throw err
    }
  }

  const loginWithGoogle = async (credentialToken) => {
    try {
      const response = await api.googleSSO({ credential: credentialToken })
      completeAuthSession(response.access_token, response.user, "Logged in with Google successfully!")
    } catch (err) {
      const errorMsg = err.response?.data?.detail || "Google Sign-In failed"
      addNotification(errorMsg, "error")
      throw err
    }
  }

  const logout = () => {
    localStorage.removeItem('accessToken')
    localStorage.removeItem('userProfile')
    localStorage.removeItem('userEmail')
    setIsAuthenticated(false)
    setUser(null)
    setBillingSummary(null)
    setQuotes([])
    setCarriers([])
    setCustomers([])
    setAnalytics(null)
    addNotification("Logged out successfully.", "success")
    setSelectedTab('landing')
  }

  const handleSaveCarrier = async (payload) => {
    try {
      if (editingCarrierId) {
        await api.updateCarrier(editingCarrierId, payload)
        addNotification("Carrier updated successfully!", "success")
      } else {
        await api.createCarrier(payload)
        addNotification("New carrier added successfully!", "success")
      }
      setEditingCarrierId(null)
      fetchData()
      setSelectedTab('connectors_list')
    } catch (err) {
      const errorMsg = err.response?.data?.detail || "Failed to save carrier"
      addNotification(errorMsg, "error")
    }
  }

  const handleDeleteCarrier = async (id) => {
    try {
      await api.deleteCarrier(id)
      addNotification("Carrier deleted successfully!", "success")
      fetchData()
    } catch (err) {
      addNotification(err.response?.data?.detail || "Failed to delete carrier", "error")
    }
  }

  return (
    <AppContext.Provider value={{
      quotes,
      carriers,
      customers,
      editingCarrierId,
      setEditingCarrierId,
      analytics,
      billingSummary,
      backendEnv,
      loading,
      activeQuote,
      historicalRag,
      selectedTab,
      setSelectedTab,
      notifications,
      addNotification,
      fetchData,
      selectQuote,
      handleManualOverride,
      handleApproval,
      handleSendMockEmail,
      handleFastForwardTimers,
      handleResetDatabase,
      isAuthenticated,
      user,
      login,
      register,
      acceptInvite,
      loginWithGoogle,
      logout,
      handleSaveCarrier,
      handleDeleteCarrier
    }}>
      {children}
    </AppContext.Provider>
  )
}

export const useApp = () => useContext(AppContext)
