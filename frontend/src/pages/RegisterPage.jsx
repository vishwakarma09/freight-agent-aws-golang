import React, { useState, useEffect } from 'react'
import { useApp } from '../context/AppContext'
import { Zap, ArrowRight, User, Mail, Lock, Building2 } from 'lucide-react'

const RegisterPage = () => {
  const { register, acceptInvite, setSelectedTab } = useApp()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [organizationName, setOrganizationName] = useState('')
  const [password, setPassword] = useState('')
  const [inviteToken, setInviteToken] = useState('')
  const [error, setError] = useState('')
  const [isSubmitted, setIsSubmitted] = useState(false)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const tok = params.get('invite_token')
    if (tok) {
      setInviteToken(tok)
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [])

  const handleSubmit = async (e) => {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      if (inviteToken) {
        await acceptInvite(inviteToken, name, password)
      } else {
        await register(name, email, password, organizationName)
        setIsSubmitted(true)
      }
    } catch (err) {
      setError(err.response?.data?.detail || "Registration failed. Please try again.")
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen text-on-surface bg-background mesh-gradient relative flex flex-col justify-between">
      <div className="absolute inset-0 bg-grid-pattern opacity-10 pointer-events-none"></div>

      <nav className="w-full border-b border-white/5 bg-surface/80 backdrop-blur-xl relative z-50">
        <div className="flex justify-between items-center h-20 px-8 max-w-7xl mx-auto">
          <div onClick={() => setSelectedTab('landing')} className="flex items-center gap-3 cursor-pointer">
            <div className="w-10 h-10 bg-primary/20 rounded-lg flex items-center justify-center border border-primary/30">
              <Zap className="text-primary" size={20} />
            </div>
            <div>
              <span className="font-bold text-xl text-primary tracking-tight">Dispatch</span>
              <p className="text-[9px] uppercase tracking-widest text-secondary font-bold -mt-0.5">Enterprise Logistics</p>
            </div>
          </div>

          <button onClick={() => setSelectedTab('landing')} className="text-sm font-medium text-on-surface-variant hover:text-primary transition-colors">
            Back to Home
          </button>
        </div>
      </nav>

      <div className="flex-1 flex items-center justify-center p-8 relative z-10 my-12">
        <div className="w-full max-w-md glass-panel rounded-2xl overflow-hidden border border-white/10 bg-surface-container-low/40 backdrop-blur-xl p-8 relative">
          {isSubmitted ? (
            <div className="text-center py-4 relative z-10">
              <div className="w-16 h-16 bg-primary/10 rounded-full flex items-center justify-center border border-primary/20 mx-auto mb-6">
                <Mail className="text-primary" size={28} />
              </div>
              <h3 className="text-xl font-bold text-on-surface mb-3">Check your email</h3>
              <p className="text-sm text-on-surface-variant mb-8 leading-relaxed">
                We sent a 24-hour activation link to <strong className="text-on-surface">{email}</strong>.
                Click the link in your inbox to activate your Dispatch account.
              </p>

              <button
                onClick={() => setSelectedTab('login')}
                className="w-full py-3 rounded-xl bg-primary text-on-primary font-bold text-sm hover:brightness-110 transition-all neon-glow-primary"
              >
                Go to Sign In
              </button>
            </div>
          ) : (
            <>
              <div className="text-center mb-8">
                <h2 className="text-2xl font-bold text-on-surface">
                  {inviteToken ? 'Accept Organization Invite' : 'Create Account'}
                </h2>
                <p className="text-sm text-on-surface-variant mt-2">
                  {inviteToken
                    ? 'Set your name and password to join your brokerage team.'
                    : 'Deploy your autonomous freight quote & bidding workspace.'}
                </p>
              </div>

              {error && (
                <div className="mb-6 p-3 rounded-lg bg-red-500/15 text-red-400 border border-red-500/20 text-xs text-center">
                  {error}
                </div>
              )}

              <form onSubmit={handleSubmit} className="space-y-5">
                <div className="space-y-2">
                  <label className="text-xs uppercase font-bold tracking-wider text-on-surface-variant">Full Name</label>
                  <div className="relative flex items-center">
                    <User className="absolute left-4 text-on-surface-variant/40" size={18} />
                    <input
                      type="text"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder="Alex Rivera"
                      className="w-full bg-surface border border-white/10 rounded-xl pl-12 pr-4 py-3 text-sm text-on-surface focus:border-primary outline-none"
                      required
                      disabled={loading}
                    />
                  </div>
                </div>

                {!inviteToken && (
                  <>
                    <div className="space-y-2">
                      <label className="text-xs uppercase font-bold tracking-wider text-on-surface-variant">Brokerage / Company Name</label>
                      <div className="relative flex items-center">
                        <Building2 className="absolute left-4 text-on-surface-variant/40" size={18} />
                        <input
                          type="text"
                          value={organizationName}
                          onChange={(e) => setOrganizationName(e.target.value)}
                          placeholder="Northstar Freight Logistics"
                          className="w-full bg-surface border border-white/10 rounded-xl pl-12 pr-4 py-3 text-sm text-on-surface focus:border-primary outline-none"
                          disabled={loading}
                        />
                      </div>
                    </div>

                    <div className="space-y-2">
                      <label className="text-xs uppercase font-bold tracking-wider text-on-surface-variant">Work Email Address</label>
                      <div className="relative flex items-center">
                        <Mail className="absolute left-4 text-on-surface-variant/40" size={18} />
                        <input
                          type="email"
                          value={email}
                          onChange={(e) => setEmail(e.target.value)}
                          placeholder="broker@company.com"
                          className="w-full bg-surface border border-white/10 rounded-xl pl-12 pr-4 py-3 text-sm text-on-surface focus:border-primary outline-none"
                          required
                          disabled={loading}
                        />
                      </div>
                    </div>
                  </>
                )}

                <div className="space-y-2">
                  <label className="text-xs uppercase font-bold tracking-wider text-on-surface-variant">Password</label>
                  <div className="relative flex items-center">
                    <Lock className="absolute left-4 text-on-surface-variant/40" size={18} />
                    <input
                      type="password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      placeholder="Min 10 characters with a number"
                      minLength={10}
                      className="w-full bg-surface border border-white/10 rounded-xl pl-12 pr-4 py-3 text-sm text-on-surface focus:border-primary outline-none"
                      required
                      disabled={loading}
                    />
                  </div>
                </div>

                <button
                  type="submit"
                  disabled={loading}
                  className="w-full py-3 rounded-xl bg-primary text-on-primary font-bold text-sm hover:brightness-110 transition-all neon-glow-primary flex items-center justify-center gap-2 disabled:opacity-50"
                >
                  {loading ? "Processing..." : inviteToken ? "Join Organization" : "Create Free Sandbox"} <ArrowRight size={16} />
                </button>
              </form>

              <div className="mt-8 text-center text-sm text-on-surface-variant">
                Already have an account?{' '}
                <button onClick={() => setSelectedTab('login')} className="text-primary hover:underline font-semibold">
                  Sign in
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      <footer className="py-8 text-center text-xs text-on-surface-variant/50 border-t border-white/5 max-w-7xl mx-auto w-full">
        &copy; {new Date().getFullYear()} Dispatch Inc. All rights reserved. Secured with AES-256-GCM / AWS KMS.
      </footer>
    </div>
  )
}

export default RegisterPage
