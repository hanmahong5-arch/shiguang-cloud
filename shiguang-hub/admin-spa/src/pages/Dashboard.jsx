import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { Link } from 'react-router-dom'
import {
  Server, Radio, Users, Crown, Plus, Palette, Key, Activity, RefreshCw, BarChart3,
} from 'lucide-react'
import * as api from '../api'
import { Skeleton, EmptyState, timeAgo } from '../components'

const REFRESH_INTERVAL = 30 // seconds

export default function DashboardPage({ tenant }) {
  const [agents, setAgents] = useState(null)
  const [lines, setLines] = useState(null)
  const [stats, setStats] = useState(null)
  const [countdown, setCountdown] = useState(REFRESH_INTERVAL)
  const [refreshing, setRefreshing] = useState(false)
  const timerRef = useRef(null)

  const loadData = useCallback(async (silent = false) => {
    if (!silent) setRefreshing(true)
    try {
      const [a, l, s] = await Promise.allSettled([api.getAgents(), api.getLines(), api.getStats(7)])
      setAgents(a.status === 'fulfilled' ? a.value : [])
      setLines(l.status === 'fulfilled' ? l.value : [])
      setStats(s.status === 'fulfilled' ? s.value : [])
    } finally {
      setRefreshing(false)
      setCountdown(REFRESH_INTERVAL)
    }
  }, [])

  // Initial load + auto-refresh countdown
  useEffect(() => {
    loadData(true)
    timerRef.current = setInterval(() => {
      setCountdown(prev => {
        if (prev <= 1) {
          loadData(true)
          return REFRESH_INTERVAL
        }
        return prev - 1
      })
    }, 1000)
    return () => clearInterval(timerRef.current)
  }, [loadData])

  const isLoading = agents === null || lines === null
  const onlineAgents = agents?.filter(a => {
    const fresh = Date.now() - new Date(a.last_seen).getTime() < 60000
    return a.status === 'online' && fresh
  }).length ?? 0

  return (
    <div className="page-enter">
      {/* Welcome header */}
      <div className="page-header">
        <div>
          <div className="page-title">
            {tenant ? `Welcome back, ${tenant.name}` : 'Dashboard'}
          </div>
          <div className="page-subtitle">
            Monitor your game server infrastructure at a glance
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 11, color: 'var(--sg-text-tertiary)', fontVariantNumeric: 'tabular-nums' }}>
            {countdown}s
          </span>
          <button
            className="btn btn-ghost"
            onClick={() => loadData()}
            disabled={refreshing}
            title="Refresh now"
            style={{ padding: '6px 8px', minWidth: 0 }}
          >
            <RefreshCw size={14} className={refreshing ? 'spin' : ''} />
          </button>
        </div>
      </div>

      {/* Stat cards */}
      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-icon blue"><Server size={18} /></div>
          {isLoading ? (
            <Skeleton variant="stat" />
          ) : (
            <>
              <div className="stat-value">{lines.length}<span style={{ fontSize: 14, color: 'var(--sg-text-tertiary)', fontWeight: 400 }}> / {tenant?.max_lines ?? '?'}</span></div>
              <div className="stat-label">Server Lines</div>
            </>
          )}
        </div>

        <div className="stat-card">
          <div className="stat-icon green"><Radio size={18} /></div>
          {isLoading ? (
            <Skeleton variant="stat" />
          ) : (
            <>
              <div className="stat-value">{onlineAgents}<span style={{ fontSize: 14, color: 'var(--sg-text-tertiary)', fontWeight: 400 }}> / {agents.length}</span></div>
              <div className="stat-label">Agents Online</div>
            </>
          )}
        </div>

        <div className="stat-card">
          <div className="stat-icon amber"><Users size={18} /></div>
          <div className="stat-value">{tenant?.max_players ?? 0}</div>
          <div className="stat-label">Max Players</div>
        </div>

        <div className="stat-card">
          <div className="stat-icon purple"><Crown size={18} /></div>
          <div className="stat-value" style={{ fontSize: 22, textTransform: 'uppercase' }}>{tenant?.plan ?? '\u2014'}</div>
          <div className="stat-label">Current Plan</div>
        </div>
      </div>

      {/* 7-day stats chart */}
      <div className="card">
        <div className="card-header">
          <span className="card-title"><BarChart3 size={14} style={{ marginRight: 6, verticalAlign: -2 }} />7-Day Activity</span>
        </div>
        {stats === null ? (
          <Skeleton variant="row" count={3} />
        ) : stats.length === 0 ? (
          <EmptyState
            icon={<BarChart3 />}
            title="No stats yet"
            description="Stats appear after agents report heartbeats."
          />
        ) : (
          <StatsChart data={stats} />
        )}
      </div>

      {/* Agent status overview */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Gate Agent Status</span>
          <Link to="/agents" className="btn btn-ghost" style={{ fontSize: 12 }}>
            View All
          </Link>
        </div>

        {isLoading ? (
          <Skeleton variant="row" count={2} />
        ) : agents.length === 0 ? (
          <EmptyState
            icon={<Radio />}
            title="No agents connected"
            description="Deploy shiguang-agent on your game server and configure the agent token to get started."
          />
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Hostname</th>
                  <th>Public IP</th>
                  <th>Version</th>
                  <th>Last Seen</th>
                </tr>
              </thead>
              <tbody>
                {agents.slice(0, 5).map(a => {
                  const ms = Date.now() - new Date(a.last_seen).getTime()
                  const stale = ms > 60000
                  return (
                    <tr key={a.id}>
                      <td>
                        <span className={`status-dot ${stale ? 'stale' : a.status === 'online' ? 'online' : 'offline'}`} />
                      </td>
                      <td style={{ fontWeight: 500 }}>{a.hostname}</td>
                      <td className="mono" style={{ color: 'var(--sg-text-secondary)' }}>{a.public_ip || '\u2014'}</td>
                      <td><span className="badge badge-neutral">{a.version}</span></td>
                      <td style={{ color: stale ? 'var(--sg-danger)' : 'var(--sg-text-tertiary)' }}>
                        {timeAgo(a.last_seen)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Quick actions */}
      <div className="card">
        <div className="card-header">
          <span className="card-title">Quick Actions</span>
        </div>
        <div className="quick-actions">
          <Link to="/servers" className="quick-action">
            <Plus size={18} />
            Add Server Line
          </Link>
          <Link to="/branding" className="quick-action">
            <Palette size={18} />
            Configure Branding
          </Link>
          <Link to="/codes" className="quick-action">
            <Key size={18} />
            Create Invite Code
          </Link>
          <Link to="/agents" className="quick-action">
            <Activity size={18} />
            Monitor Agents
          </Link>
        </div>
      </div>

      {/* Tenant info */}
      {tenant && (
        <div className="card">
          <div className="card-header">
            <span className="card-title">Account Details</span>
          </div>
          <div className="agent-card-meta">
            <div className="agent-meta-item">
              <span className="agent-meta-label">Tenant ID</span>
              <span className="agent-meta-value mono" style={{ fontSize: 11 }}>{tenant.id}</span>
            </div>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Slug</span>
              <span className="agent-meta-value">{tenant.slug}</span>
            </div>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Email</span>
              <span className="agent-meta-value">{tenant.email}</span>
            </div>
            <div className="agent-meta-item">
              <span className="agent-meta-label">Registered</span>
              <span className="agent-meta-value">{new Date(tenant.created_at).toLocaleDateString()}</span>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * StatsChart —— 7 日活动 SVG 折线图（纯原生，无图表库）。
 *
 * 设计要点：
 *   - 三条 polyline（Logins / Peak / New Accounts），共享同一 X 轴（日期）
 *   - 双 Y 轴在逻辑上不画，全部归一化到 0..maxY（Logins 与 Peak 共享；NewAccounts 独立线）
 *     -> 避免交互复杂度；tooltip 中显示原始数值
 *   - 网格线：4 条水平、N 条垂直（按天），暗灰色
 *   - hover：鼠标移动时捕获最近一天的 x 索引，显示竖线 + 三数据点高亮 + 浮层
 *   - Area fill：Logins 折线下方浅色渐变，提升可读性
 *   - 响应式：固定 viewBox，通过 preserveAspectRatio 等比缩放
 */
function StatsChart({ data }) {
  const sorted = useMemo(() => [...data].sort((a, b) => a.date.localeCompare(b.date)), [data])

  // SVG 画布尺寸（逻辑单位）
  const W = 640
  const H = 200
  const PAD_L = 36
  const PAD_R = 16
  const PAD_T = 14
  const PAD_B = 28
  const plotW = W - PAD_L - PAD_R
  const plotH = H - PAD_T - PAD_B

  // 数据域
  const maxLogins = Math.max(1, ...sorted.map(d => d.total_logins))
  const maxPeak = Math.max(1, ...sorted.map(d => d.peak_online))
  const maxNew = Math.max(1, ...sorted.map(d => d.new_accounts))
  // 上方留 10% 空白避免折线顶到边框
  const yMax = Math.ceil(Math.max(maxLogins, maxPeak) * 1.1)

  const xStep = sorted.length > 1 ? plotW / (sorted.length - 1) : 0
  const xOf = (i) => PAD_L + i * xStep
  const yOf = (v, max = yMax) => PAD_T + plotH - (v / max) * plotH

  const loginsPts = sorted.map((d, i) => `${xOf(i)},${yOf(d.total_logins)}`).join(' ')
  const peakPts = sorted.map((d, i) => `${xOf(i)},${yOf(d.peak_online)}`).join(' ')
  const newPts = sorted.map((d, i) => `${xOf(i)},${yOf(d.new_accounts, maxNew * 1.1)}`).join(' ')

  // Area path for logins —— 线下渐变填充
  const areaPath = sorted.length > 0
    ? `M${xOf(0)},${yOf(0)} ` +
      sorted.map((d, i) => `L${xOf(i)},${yOf(d.total_logins)}`).join(' ') +
      ` L${xOf(sorted.length - 1)},${yOf(0)} Z`
    : ''

  // hover 状态
  const [hoverIdx, setHoverIdx] = useState(null)

  const onMouseMove = (e) => {
    const rect = e.currentTarget.getBoundingClientRect()
    // 把鼠标 x 映射回 SVG 逻辑坐标
    const mx = ((e.clientX - rect.left) / rect.width) * W
    const rel = (mx - PAD_L) / xStep
    const idx = Math.max(0, Math.min(sorted.length - 1, Math.round(rel)))
    setHoverIdx(idx)
  }

  // Y 轴刻度 (4 条水平网格)
  const yTicks = [0, 0.25, 0.5, 0.75, 1].map(f => ({
    v: Math.round(yMax * f),
    y: PAD_T + plotH - f * plotH,
  }))

  return (
    <div style={{ padding: '16px 20px' }}>
      {/* Legend */}
      <div style={{ display: 'flex', gap: 18, fontSize: 11, color: 'var(--sg-text-tertiary)', marginBottom: 10 }}>
        <LegendItem color="var(--sg-accent)" label="Logins" />
        <LegendItem color="var(--sg-success)" label="Peak Online" />
        <LegendItem color="var(--sg-warning)" label="New Accounts" />
      </div>

      {/* SVG chart */}
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="xMidYMid meet"
        style={{ width: '100%', height: 'auto', display: 'block', overflow: 'visible' }}
        onMouseMove={onMouseMove}
        onMouseLeave={() => setHoverIdx(null)}
      >
        <defs>
          <linearGradient id="stats-area-gradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--sg-accent)" stopOpacity="0.32" />
            <stop offset="100%" stopColor="var(--sg-accent)" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* Horizontal grid lines + Y ticks */}
        {yTicks.map((t, i) => (
          <g key={i}>
            <line
              x1={PAD_L} y1={t.y} x2={W - PAD_R} y2={t.y}
              stroke="rgba(255,255,255,0.06)"
              strokeWidth="1"
              strokeDasharray={i === 0 ? '' : '2 3'}
            />
            <text
              x={PAD_L - 6} y={t.y + 3}
              textAnchor="end"
              fontSize="9"
              fill="var(--sg-text-tertiary)"
              style={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {t.v}
            </text>
          </g>
        ))}

        {/* Area fill under Logins */}
        {sorted.length > 0 && (
          <path d={areaPath} fill="url(#stats-area-gradient)" />
        )}

        {/* Polylines */}
        {sorted.length > 0 && (
          <>
            <polyline points={loginsPts} fill="none" stroke="var(--sg-accent)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
            <polyline points={peakPts} fill="none" stroke="var(--sg-success)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
            <polyline points={newPts} fill="none" stroke="var(--sg-warning)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" strokeDasharray="4 3" />
          </>
        )}

        {/* X-axis date labels */}
        {sorted.map((d, i) => (
          <text
            key={d.date}
            x={xOf(i)} y={H - 8}
            textAnchor="middle"
            fontSize="9"
            fill="var(--sg-text-tertiary)"
            style={{ fontVariantNumeric: 'tabular-nums' }}
          >
            {d.date.slice(5)}
          </text>
        ))}

        {/* Hover cursor + focus dots */}
        {hoverIdx !== null && sorted[hoverIdx] && (
          <g>
            <line
              x1={xOf(hoverIdx)} y1={PAD_T}
              x2={xOf(hoverIdx)} y2={PAD_T + plotH}
              stroke="rgba(255,255,255,0.18)"
              strokeWidth="1"
              strokeDasharray="3 3"
            />
            <circle cx={xOf(hoverIdx)} cy={yOf(sorted[hoverIdx].total_logins)} r="3.5" fill="var(--sg-accent)" stroke="var(--sg-bg)" strokeWidth="1.5" />
            <circle cx={xOf(hoverIdx)} cy={yOf(sorted[hoverIdx].peak_online)} r="3.5" fill="var(--sg-success)" stroke="var(--sg-bg)" strokeWidth="1.5" />
            <circle cx={xOf(hoverIdx)} cy={yOf(sorted[hoverIdx].new_accounts, maxNew * 1.1)} r="3.5" fill="var(--sg-warning)" stroke="var(--sg-bg)" strokeWidth="1.5" />
          </g>
        )}
      </svg>

      {/* Tooltip panel (absolute-positioned below svg, not floating over chart) */}
      {hoverIdx !== null && sorted[hoverIdx] && (
        <div
          style={{
            marginTop: 8,
            padding: '8px 12px',
            background: 'rgba(16, 22, 36, 0.85)',
            border: '1px solid var(--sg-border)',
            borderRadius: 'var(--sg-radius-sm)',
            fontSize: 11,
            color: 'var(--sg-text-secondary)',
            display: 'flex',
            gap: 16,
            fontVariantNumeric: 'tabular-nums',
          }}
        >
          <span style={{ color: 'var(--sg-text)' }}>{sorted[hoverIdx].date}</span>
          <span><span style={{ color: 'var(--sg-accent)' }}>●</span> Logins <strong>{sorted[hoverIdx].total_logins}</strong></span>
          <span><span style={{ color: 'var(--sg-success)' }}>●</span> Peak <strong>{sorted[hoverIdx].peak_online}</strong></span>
          <span><span style={{ color: 'var(--sg-warning)' }}>●</span> New <strong>{sorted[hoverIdx].new_accounts}</strong></span>
        </div>
      )}

      {/* Summary */}
      <div style={{ display: 'flex', justifyContent: 'space-around', marginTop: 12, fontSize: 11, color: 'var(--sg-text-secondary)' }}>
        <span>Total logins: <strong>{sorted.reduce((s, d) => s + d.total_logins, 0)}</strong></span>
        <span>Peak online: <strong>{Math.max(0, ...sorted.map(d => d.peak_online))}</strong></span>
        <span>New accounts: <strong>{sorted.reduce((s, d) => s + d.new_accounts, 0)}</strong></span>
      </div>
    </div>
  )
}

/** LegendItem —— 图例一项（色块 + 标签） */
function LegendItem({ color, label }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
      <span style={{ display: 'inline-block', width: 10, height: 10, borderRadius: 2, background: color }} />
      {label}
    </span>
  )
}
