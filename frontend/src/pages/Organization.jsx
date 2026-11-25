import React, { useState, useEffect, useCallback } from 'react'
import { useApp } from '../context/AppContext'
import * as api from '../services/api'
import { Building2, Users, UserPlus, Trash2, Lock, Clock, Save, Mail } from 'lucide-react'

const Organization = () => {
  const { user, addNotification, setSelectedTab, fetchData } = useApp()
  const [orgInfo, setOrgInfo] = useState(null)
  const [members, setMembers] = useState([])
  const [invites, setInvites] = useState([])
  const [multiSeatUnlocked, setMultiSeatUnlocked] = useState(false)

  const [orgName, setOrgName] = useState('')
  const [r1Mins, setR1Mins] = useState(120)
  const [r2Mins, setR2Mins] = useState(30)
  const [savingSettings, setSavingSettings] = useState(false)

  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('broker')
  const [inviting, setInviting] = useState(false)

  const loadOrg = useCallback(async () => {
    try {
      const [orgRes, memRes] = await Promise.all([
        api.getOrganization(),
        api.getOrganizationMembers()
      ])
      setOrgInfo(orgRes.organization)
      setMultiSeatUnlocked(Boolean(orgRes.multi_seat_unlocked))
      setOrgName(orgRes.organization?.name || '')
      setR1Mins(orgRes.organization?.round1_duration_minutes || 120)
      setR2Mins(orgRes.organization?.round2_duration_minutes || 30)
      setMembers(memRes.members || [])
      setInvites(memRes.pending_invites || [])
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to load organization details', 'error')
    }
  }, [addNotification])

  useEffect(() => {
    loadOrg()
  }, [loadOrg])

  const handleSaveSettings = async (e) => {
    e.preventDefault()
    setSavingSettings(true)
    try {
      await api.updateOrganization({
        name: orgName,
        round1_duration_minutes: parseInt(r1Mins, 10),
        round2_duration_minutes: parseInt(r2Mins, 10)
      })
      addNotification('Organization settings saved!', 'success')
      loadOrg()
      fetchData(false)
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to update settings', 'error')
    } finally {
      setSavingSettings(false)
    }
  }

  const handleInvite = async (e) => {
    e.preventDefault()
    setInviting(true)
    try {
      await api.inviteOrganizationMember({ email: inviteEmail, role: inviteRole })
      addNotification(`Invitation sent to ${inviteEmail}`, 'success')
      setInviteEmail('')
      loadOrg()
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to invite teammate', 'error')
    } finally {
      setInviting(false)
    }
  }

  const handleRevokeInvite = async (id) => {
    try {
      await api.revokeOrganizationInvite(id)
      addNotification('Invitation revoked', 'success')
      loadOrg()
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to revoke invite', 'error')
    }
  }

  const handleRemoveMember = async (id) => {
    if (!window.confirm('Remove this team member from the organization?')) return
    try {
      await api.removeOrganizationMember(id)
      addNotification('Team member removed', 'success')
      loadOrg()
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to remove member', 'error')
    }
  }

  const isAdmin = user?.role === 'owner' || user?.role === 'admin'

  return (
    <div className="space-y-8 max-w-6xl mx-auto">
      <form onSubmit={handleSaveSettings} className="glass-card rounded-2xl p-6 border border-white/10 bg-surface-container-low/30 space-y-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-xl bg-primary/15 border border-primary/30 flex items-center justify-center text-primary">
              <Building2 size={20} />
            </div>
            <div>
              <h2 className="text-lg font-bold text-on-surface">Organization &amp; Bidding Timers</h2>
              <p className="text-xs text-on-surface-variant">Configure brokerage workspace name and carrier RFQ bidding windows.</p>
            </div>
          </div>
          <span className="px-3 py-1 rounded-full text-xs font-bold uppercase bg-primary/15 text-primary border border-primary/20">
            {orgInfo?.plan_tier || 'free'} tier
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="space-y-1.5">
            <label className="text-[11px] font-bold uppercase text-on-surface-variant">Workspace / Brokerage Name</label>
            <input
              type="text"
              value={orgName}
              onChange={(e) => setOrgName(e.target.value)}
              disabled={!isAdmin}
              className="w-full bg-black/30 border border-white/10 rounded-xl px-3.5 py-2.5 text-sm text-on-surface"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-[11px] font-bold uppercase text-on-surface-variant flex items-center gap-1">
              <Clock size={12} /> Round 1 Bidding Window (mins)
            </label>
            <input
              type="number"
              min="1"
              max="1440"
              value={r1Mins}
              onChange={(e) => setR1Mins(e.target.value)}
              disabled={!isAdmin}
              className="w-full bg-black/30 border border-white/10 rounded-xl px-3.5 py-2.5 text-sm text-on-surface"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-[11px] font-bold uppercase text-on-surface-variant flex items-center gap-1">
              <Clock size={12} /> Round 2 Re-Bid Window (mins)
            </label>
            <input
              type="number"
              min="1"
              max="720"
              value={r2Mins}
              onChange={(e) => setR2Mins(e.target.value)}
              disabled={!isAdmin}
              className="w-full bg-black/30 border border-white/10 rounded-xl px-3.5 py-2.5 text-sm text-on-surface"
            />
          </div>
        </div>

        {isAdmin && (
          <div className="flex justify-end">
            <button
              type="submit"
              disabled={savingSettings}
              className="px-5 py-2.5 rounded-xl bg-primary text-on-primary font-bold text-xs flex items-center gap-2 hover:brightness-110 disabled:opacity-50"
            >
              <Save size={14} /> {savingSettings ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
        )}
      </form>

      {!multiSeatUnlocked ? (
        <div className="glass-card rounded-2xl p-8 border border-primary/30 bg-primary/5 flex flex-col md:flex-row items-center justify-between gap-6">
          <div className="flex items-start gap-4">
            <div className="w-12 h-12 rounded-xl bg-primary/20 text-primary flex items-center justify-center shrink-0">
              <Lock size={22} />
            </div>
            <div>
              <h3 className="text-lg font-bold text-on-surface">Multi-Seat Organizations — Enterprise Exclusive</h3>
              <p className="text-sm text-on-surface-variant mt-1 max-w-xl">
                Upgrade to the <strong>Enterprise / 3PL ($799/mo)</strong> tier to invite up to 25 dispatchers, pricing analysts, and accounting staff into a shared Organization workspace with role-based permissions.
              </p>
            </div>
          </div>
          <button
            onClick={() => setSelectedTab('subscription')}
            className="px-6 py-3 rounded-xl bg-primary text-on-primary font-bold text-xs hover:brightness-110 shrink-0"
          >
            Upgrade to Enterprise
          </button>
        </div>
      ) : (
        isAdmin && (
          <form onSubmit={handleInvite} className="glass-card rounded-2xl p-6 border border-white/10 bg-surface-container-low/30 space-y-4">
            <h3 className="text-base font-bold text-on-surface flex items-center gap-2">
              <UserPlus size={18} className="text-secondary" /> Invite Team Member ({members.length} / {orgInfo?.seat_limit || 25} seats used)
            </h3>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <input
                type="email"
                required
                placeholder="teammate@brokerage.com"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                className="bg-black/30 border border-white/10 rounded-xl px-4 py-2.5 text-sm text-on-surface"
              />
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value)}
                className="bg-black/30 border border-white/10 rounded-xl px-4 py-2.5 text-sm text-on-surface"
              >
                <option value="broker">Broker (Quotes &amp; Carriers)</option>
                <option value="accounting">Accounting (Freight Ledger)</option>
                <option value="admin">Admin (Team &amp; Billing)</option>
                <option value="viewer">Viewer (Read-only)</option>
              </select>
              <button
                type="submit"
                disabled={inviting}
                className="px-5 py-2.5 rounded-xl bg-secondary text-on-secondary font-bold text-xs hover:brightness-110 disabled:opacity-50"
              >
                {inviting ? 'Sending Invite...' : 'Send Organization Invite'}
              </button>
            </div>
          </form>
        )
      )}

      <div className="glass-card rounded-2xl overflow-hidden border border-white/10 bg-surface-container-low/20">
        <div className="p-5 border-b border-white/5 flex items-center justify-between">
          <h3 className="text-base font-bold text-on-surface flex items-center gap-2">
            <Users size={18} className="text-primary" /> Workspace Members ({members.length})
          </h3>
        </div>
        <table className="w-full text-left border-collapse">
          <thead>
            <tr className="border-b border-white/5 text-[10px] uppercase font-bold text-on-surface-variant">
              <th className="px-6 py-3">Name</th>
              <th className="px-6 py-3">Email</th>
              <th className="px-6 py-3">Role</th>
              <th className="px-6 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/5 text-sm">
            {members.map((m) => (
              <tr key={m.id}>
                <td className="px-6 py-4 font-semibold text-on-surface">{m.name}</td>
                <td className="px-6 py-4 text-on-surface-variant font-mono text-xs">{m.email}</td>
                <td className="px-6 py-4">
                  <span className="px-2.5 py-0.5 rounded-full text-[11px] font-bold uppercase bg-primary/15 text-primary">{m.role}</span>
                </td>
                <td className="px-6 py-4 text-right">
                  {isAdmin && m.role !== 'owner' && m.id !== user?.id && (
                    <button onClick={() => handleRemoveMember(m.id)} className="text-error hover:bg-error/10 p-1.5 rounded-lg">
                      <Trash2 size={14} />
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {invites.length > 0 && (
        <div className="glass-card rounded-2xl overflow-hidden border border-white/10 bg-surface-container-low/20">
          <div className="p-5 border-b border-white/5">
            <h3 className="text-base font-bold text-on-surface flex items-center gap-2">
              <Mail size={16} className="text-tertiary" /> Pending Invitations ({invites.length})
            </h3>
          </div>
          <div className="divide-y divide-white/5">
            {invites.map((inv) => (
              <div key={inv.id} className="px-6 py-3.5 flex items-center justify-between text-xs">
                <div>
                  <span className="font-mono text-on-surface font-semibold">{inv.email}</span>
                  <span className="ml-3 px-2 py-0.5 rounded bg-white/5 text-on-surface-variant uppercase font-bold">{inv.role}</span>
                </div>
                {isAdmin && (
                  <button onClick={() => handleRevokeInvite(inv.id)} className="text-error hover:underline font-semibold">
                    Revoke
                  </button>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

export default Organization
