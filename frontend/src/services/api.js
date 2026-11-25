import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8000' : window.location.origin)
const normalizedBaseURL = (API_URL === '/' || API_URL === '') ? '/api' : `${API_URL}/api`

const api = axios.create({
  baseURL: normalizedBaseURL,
  headers: {
    'Content-Type': 'application/json'
  }
})

// Attach signed JWT Bearer token from session storage
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('accessToken')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Clear stale tokens on 401
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && error.response.status === 401) {
      localStorage.removeItem('accessToken')
      localStorage.removeItem('userProfile')
      window.dispatchEvent(new Event('auth:unauthorized'))
    }
    return Promise.reject(error)
  }
)

export const getQuotes = () => api.get('/quotes').then(res => res.data)
export const getQuote = (id) => api.get(`/quotes/${id}`).then(res => res.data)
export const createQuote = (payload) => api.post('/quotes', payload).then(res => res.data)
export const approveQuote = (id, payload) => api.post(`/quotes/${id}/approve`, payload).then(res => res.data)
export const manualOverride = (id, to_status, notes) => api.post(`/quotes/${id}/manual-override`, { to_status, notes }).then(res => res.data)
export const getHistoricalRag = (id) => api.get(`/quotes/${id}/historical-rag`).then(res => res.data)
export const getQuoteDocumentUrl = (id, type) => api.get(`/quotes/${id}/document-url`, { params: { type } }).then(res => res.data)

export const getCarriers = () => api.get('/carriers').then(res => res.data)
export const createCarrier = (payload) => api.post('/carriers', payload).then(res => res.data)
export const updateCarrier = (id, payload) => api.put(`/carriers/${id}`, payload).then(res => res.data)
export const deleteCarrier = (id) => api.delete(`/carriers/${id}`).then(res => res.data)

export const getCustomers = () => api.get('/customers').then(res => res.data)
export const createCustomer = (payload) => api.post('/customers', payload).then(res => res.data)

export const getAnalytics = (days) => api.get('/analytics', { params: days ? { days } : {} }).then(res => res.data)

export const sendMockEmail = (payload) => api.post('/simulator/send-mock-email', payload).then(res => res.data)
export const fastForwardTimers = () => api.post('/simulator/fast-forward').then(res => res.data)
export const resetSimulatorDatabase = () => api.post('/simulator/reset-database').then(res => res.data)
export const getSimulatorLogs = () => api.get('/simulator/logs').then(res => res.data)
export const getMailpitMessages = () => api.get('/simulator/mailpit-messages').then(res => res.data)

export const getEmailCredentials = () => api.get('/email-credentials').then(res => res.data)
export const getEmailEnv = () => api.get('/email-credentials/env').then(res => res.data)
export const saveEmailCredentials = (payload) => api.post('/email-credentials', payload).then(res => res.data)
export const deleteEmailCredentials = () => api.delete('/email-credentials').then(res => res.data)
export const testEmailCredentials = (payload) => api.post('/email-credentials/test', payload).then(res => res.data)
export const testExistingEmailCredentials = () => api.post('/email-credentials/test-existing').then(res => res.data)

// Auth
export const googleSSO = (payload) => api.post('/auth/google-sso', payload).then(res => res.data)
export const register = (payload) => api.post('/auth/register', payload).then(res => res.data)
export const login = (payload) => api.post('/auth/login', payload).then(res => res.data)
export const forgotPassword = (payload) => api.post('/auth/forgot-password', payload).then(res => res.data)
export const resetPassword = (payload) => api.post('/auth/reset-password', payload).then(res => res.data)
export const acceptInvite = (payload) => api.post('/auth/accept-invite', payload).then(res => res.data)
export const getMe = () => api.get('/auth/me').then(res => res.data)

// Stripe Option A Billing
export const getBillingPlans = () => api.get('/billing/plans').then(res => res.data)
export const getBillingSummary = () => api.get('/billing/summary').then(res => res.data)
export const createCheckoutSession = (payload) => api.post('/billing/checkout', payload).then(res => res.data)
export const createPortalSession = () => api.post('/billing/portal').then(res => res.data)

// Enterprise Organization Management
export const getOrganization = () => api.get('/organization').then(res => res.data)
export const updateOrganization = (payload) => api.put('/organization', payload).then(res => res.data)
export const getOrganizationMembers = () => api.get('/organization/members').then(res => res.data)
export const inviteOrganizationMember = (payload) => api.post('/organization/invites', payload).then(res => res.data)
export const revokeOrganizationInvite = (inviteId) => api.delete(`/organization/invites/${inviteId}`).then(res => res.data)
export const removeOrganizationMember = (userId) => api.delete(`/organization/members/${userId}`).then(res => res.data)

export default api
