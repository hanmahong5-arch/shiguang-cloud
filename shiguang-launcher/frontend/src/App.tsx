import { useEffect, useMemo, useRef, useState, useCallback } from 'react'
import {
  Login,
  Register,
  Logout,
  ChangePassword,
  ResetPassword,
  FetchLauncherConfig,
  StartPatch,
  LaunchGame,
  GetPrefs,
  SetClientPath,
  SetControlURL,
  GetBrand,
  SetServerCode,
  ClearServerCode,
  GetVersion,
} from '../wailsjs/go/main/App'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import { useToast } from './components/Toast'
import { useConfirm } from './components/ConfirmDialog'
import { Spinner } from './components/Spinner'
import { FormField } from './components/FormField'
import {
  validateAccount,
  validatePassword,
  validateNewPassword,
  validateEmail,
  validateServerCode,
  validateHttpUrl,
  validateClientPath,
  type Validation,
} from './utils/validators'
import { NetworkBanner } from './components/NetworkBanner'

/**
 * formatBytes —— 字节数友好显示（B / KB / MB / GB）。
 * 保留 1 位小数，边界统一用 1024；用于补丁进度详情面板。
 */
function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

/**
 * formatEta —— 把剩余秒数转为“HH:MM:SS / MM:SS”人读字符串。
 * ETA <= 0 或 Infinity 时返回占位符 "—"。
 */
function formatEta(sec: number): string {
  if (!Number.isFinite(sec) || sec <= 0) return '—'
  const s = Math.floor(sec % 60)
  const m = Math.floor((sec / 60) % 60)
  const h = Math.floor(sec / 3600)
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

interface ServerLine {
  id: string
  name: string
  auth_port: number
  game_args: string
  client_path: string
}
interface LauncherConfig {
  public_gate_ip: string
  patch_manifest_url: string
  news_url: string
  servers: ServerLine[]
}

interface BrandConfig {
  server_code: string
  server_name: string
  logo_url: string
  bg_url: string
  accent_color: string
  text_color: string
  control_url: string
  gate_ip: string
  news_url: string
  servers: { id: string; name: string; auth_port: number; game_args: string }[]
}

type Screen = 'activate' | 'login' | 'main' | 'settings'
type LoginTab = 'login' | 'register' | 'change' | 'reset'

// Apply brand colors to CSS variables so the entire UI theme adapts.
function applyBrand(b: BrandConfig) {
  const root = document.documentElement
  if (b.accent_color) root.style.setProperty('--color-primary', b.accent_color)
  if (b.text_color) root.style.setProperty('--color-text', b.text_color)
  if (b.server_name) document.title = b.server_name + ' Launcher'
}

function App() {
  const toast = useToast()
  const confirm = useConfirm()
  const [brand, setBrand] = useState<BrandConfig | null>(null)
  const [screen, setScreen] = useState<Screen>('activate') // start at activation
  const [loggedUser, setLoggedUser] = useState<string>('')
  const [selectedServer, setSelectedServer] = useState<string>('5.8')
  const [config, setConfig] = useState<LauncherConfig | null>(null)

  /**
   * 优雅退出：先征求确认，再调用 backend Logout。
   * 无论 Logout 成功失败都清空会话并回登录页 —— 本地态必须先于远端失败回退。
   */
  const handleLogout = useCallback(async () => {
    const ok = await confirm({
      title: '退出登录？',
      message: `将结束当前会话 (${loggedUser} @ ${selectedServer})。\n任何未完成的补丁或游戏启动不会受影响。`,
      confirmLabel: '退出',
      danger: true,
    })
    if (!ok) return
    try {
      await Logout()
      toast.success('已退出登录')
    } catch (e: any) {
      // 后端 Logout 失败不拦截本地清理，只给一个警告
      toast.warning(`后端注销失败，已强制清理本地会话：${e?.message || e}`)
    }
    setLoggedUser('')
    setScreen('login')
  }, [confirm, toast, loggedUser, selectedServer])

  // On mount: check if brand is already cached
  useEffect(() => {
    GetBrand()
      .then((b: BrandConfig | null) => {
        if (b && b.server_code) {
          setBrand(b)
          applyBrand(b)
          setScreen('login') // skip activation
        }
      })
      .catch(() => {})

    // Listen for brand updates (from async refresh or SetServerCode)
    EventsOn('brand:loaded', (b: BrandConfig) => {
      setBrand(b)
      applyBrand(b)
    })
    EventsOn('brand:cleared', () => {
      setBrand(null)
      setScreen('activate')
    })
    return () => {
      EventsOff('brand:loaded')
      EventsOff('brand:cleared')
    }
  }, [])

  // Once brand is set, fetch launcher config
  useEffect(() => {
    if (brand && screen !== 'activate') {
      FetchLauncherConfig()
        .then((c: LauncherConfig) => setConfig(c))
        .catch(() => {})
    }
  }, [brand, screen])

  const titleText = brand?.server_name
    ? `${brand.server_name} Launcher`
    : '拾光登录器 · Shiguang Launcher'

  return (
    <div className="app">
      <div className="title-bar">
        <div className="title">{titleText}</div>
      </div>
      <NetworkBanner />
      <div className="content">
        {screen === 'activate' && (
          <ActivationScreen
            onActivated={(b: BrandConfig) => {
              setBrand(b)
              applyBrand(b)
              setScreen('login')
            }}
          />
        )}
        {screen === 'login' && (
          <LoginScreen
            config={config}
            brand={brand}
            onSuccess={(user: string, server: string) => {
              setLoggedUser(user)
              setSelectedServer(server)
              setScreen('main')
            }}
          />
        )}
        {screen === 'main' && (
          <MainScreen
            config={config}
            selectedServer={selectedServer}
            onSelectServer={setSelectedServer}
          />
        )}
        {screen === 'settings' && <SettingsScreen brand={brand} />}
      </div>
      {screen !== 'login' && screen !== 'activate' && (
        <div className="nav-bar">
          <button
            className={`nav-btn ${screen === 'main' ? 'active' : ''}`}
            onClick={() => setScreen('main')}
          >
            主页
          </button>
          <button
            className={`nav-btn ${screen === 'settings' ? 'active' : ''}`}
            onClick={() => setScreen('settings')}
          >
            设置
          </button>
          <div className="spacer" />
          <div className="user-info">
            {loggedUser} @ {selectedServer} ·{' '}
            <button
              className="btn-secondary"
              style={{ padding: '4px 10px', fontSize: 11 }}
              onClick={handleLogout}
            >
              退出登录
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ================================================================
// Activation screen — first-time server code input (white-label entry)
// ================================================================
function ActivationScreen({ onActivated }: { onActivated: (b: BrandConfig) => void }) {
  const toast = useToast()
  const [code, setCode] = useState('')
  const [touched, setTouched] = useState(false)
  const [busy, setBusy] = useState(false)

  // 实时校验：仅在 blur 或尝试提交后显示错误（避免首次打字即红）
  const validation = touched ? validateServerCode(code) : null

  const submit = async () => {
    setTouched(true)
    const v = validateServerCode(code)
    if (!v.ok) {
      toast.warning(v.message || '服务器代码无效')
      return
    }
    setBusy(true)
    try {
      const brand = await SetServerCode(code.trim().toUpperCase())
      toast.success(brand.server_name ? `已连接至 ${brand.server_name}` : '服务器已连接')
      onActivated(brand)
    } catch (e: any) {
      const msg = e?.message || String(e)
      toast.error(`连接失败：${msg}`)
      // 失败不清空输入，允许玩家修正一两位错字
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <div className="login-card">
        <h2>连接游戏服务器</h2>
        <div style={{ textAlign: 'center', fontSize: 'var(--fs-body)', color: 'var(--color-text-dim)', marginBottom: 20, lineHeight: 1.5 }}>
          输入您的运营商提供的服务器邀请码。
          <br />
          登录器将自动完成品牌主题与连接配置。
        </div>
        <FormField
          label="服务器代码"
          required
          value={code}
          onChange={(e) => setCode(e.target.value)}
          onBlur={() => setTouched(true)}
          placeholder="例如 JUEZHAN"
          style={{ textTransform: 'uppercase', letterSpacing: 3, textAlign: 'center', fontSize: 18 }}
          onKeyDown={(e) => e.key === 'Enter' && submit()}
          validation={validation}
          hint="3–16 位大写字母、数字或连字符"
          disabled={busy}
        />
        <button
          className="btn-primary"
          disabled={busy || !code.trim()}
          onClick={submit}
        >
          {busy ? (
            <>
              <Spinner size={14} />
              连接中…
            </>
          ) : (
            '连接服务器'
          )}
        </button>
        <div style={{ textAlign: 'center', fontSize: 'var(--fs-caption)', color: 'var(--color-text-muted)', marginTop: 16, lineHeight: 1.5 }}>
          没有邀请码？请联系您的服务器管理员获取。
          <br />
          连接后可随时在「设置」中切换到其它服务器。
        </div>
      </div>
    </div>
  )
}

// ================================================================
// Login screen — 4 tabs: 登录 / 注册 / 改密 / 找回
// ================================================================
function LoginScreen({
  config,
  brand,
  onSuccess,
}: {
  config: LauncherConfig | null
  brand: BrandConfig | null
  onSuccess: (user: string, server: string) => void
}) {
  const toast = useToast()
  const [tab, setTab] = useState<LoginTab>('login')
  const [server, setServer] = useState('5.8')
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [newPw2, setNewPw2] = useState('') // 确认新密码
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [ok, setOk] = useState('')

  // “已触碰”标记：仅当用户与字段交互后才显示校验红框
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const mark = (k: string) => () => setTouched((s) => ({ ...s, [k]: true }))

  // 密码框 ref：登录失败时自动聚焦回来
  const passwordRef = useRef<HTMLInputElement>(null)

  // 服务器列表从 config 拿，缺省兜底
  const servers = useMemoServers(config)

  // ------- 每个字段的校验结果（按 tab 条件性应用） -------
  const vAccount = touched.name ? validateAccount(name) : null
  const vPassword = touched.password ? validatePassword(password) : null
  const vOldPw = touched.oldPw ? validatePassword(oldPw) : null
  const vNewPw = touched.newPw ? validateNewPassword(newPw, oldPw) : null
  const vNewPw2 = touched.newPw2
    ? newPw === newPw2
      ? { ok: true }
      : { ok: false, message: '两次输入的新密码不一致' }
    : null
  const vEmail = touched.email ? validateEmail(email) : null

  /**
   * 提交前的强校验：不依赖 touched，一次性把所有字段校验一遍，
   * 拿到第一个失败项就 toast 并打回。同时把 touched 全部打开，
   * 让红框展示给玩家看到问题字段。
   */
  const validateTab = (t: LoginTab): Validation => {
    const checks: [string, Validation][] = []
    checks.push(['name', validateAccount(name)])
    if (t === 'login' || t === 'register') checks.push(['password', validatePassword(password)])
    if (t === 'register') checks.push(['email', validateEmail(email)])
    if (t === 'change') {
      checks.push(['oldPw', validatePassword(oldPw)])
      checks.push(['newPw', validateNewPassword(newPw, oldPw)])
      checks.push([
        'newPw2',
        newPw === newPw2 ? { ok: true } : { ok: false, message: '两次输入的新密码不一致' },
      ])
    }
    if (t === 'reset') checks.push(['email', validateEmail(email)])

    const opened: Record<string, boolean> = {}
    checks.forEach(([k]) => (opened[k] = true))
    setTouched((s) => ({ ...s, ...opened }))

    for (const [, v] of checks) if (!v.ok) return v
    return { ok: true }
  }

  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    setError('')
    setOk('')
    try {
      await fn()
    } catch (e: any) {
      const msg = e?.message || String(e)
      setError(msg)
      // 登录失败的恢复：保留账号，清空密码，自动聚焦密码框
      if (tab === 'login') {
        setPassword('')
        setTimeout(() => passwordRef.current?.focus(), 0)
      }
    } finally {
      setBusy(false)
    }
  }

  const submit = () => {
    if (busy) return
    const v = validateTab(tab)
    if (!v.ok) {
      setError(v.message || '请检查输入')
      return
    }
    setError('')
    setOk('')
    if (tab === 'login') {
      run(async () => {
        await Login(server, name, password)
        onSuccess(name, server)
      })
    } else if (tab === 'register') {
      run(async () => {
        await Register(server, name, password, email)
        setOk('注册成功，即将切换到登录页')
        toast.success('注册成功')
        // 预填到登录 tab，玩家只需输入密码即可
        setTimeout(() => {
          setTab('login')
          setPassword('')
          setEmail('')
          setTouched({})
          setTimeout(() => passwordRef.current?.focus(), 0)
        }, 900)
      })
    } else if (tab === 'change') {
      run(async () => {
        await ChangePassword(server, name, oldPw, newPw)
        setOk('密码已修改，请使用新密码登录')
        toast.success('密码修改成功')
        setOldPw('')
        setNewPw('')
        setNewPw2('')
        setTouched({})
      })
    } else if (tab === 'reset') {
      run(async () => {
        const reset = await ResetPassword(server, name, email)
        setOk(`新密码：${reset}（已发送至注册邮箱，请妥善保存）`)
        toast.success('密码已重置')
      })
    }
  }

  const buttonLabel = {
    login: '进入游戏',
    register: '创建账号',
    change: '修改密码',
    reset: '找回密码',
  }[tab]

  return (
    <div className="login-wrap">
      <div className="login-card">
        <h2>{brand?.server_name ? `欢迎回到 ${brand.server_name}` : '欢迎回到艾欧'}</h2>
        <div className="tabs">
          {(['login', 'register', 'change', 'reset'] as LoginTab[]).map((t) => (
            <button
              key={t}
              className={tab === t ? 'active' : ''}
              onClick={() => {
                if (busy) return
                setTab(t)
                setError('')
                setOk('')
              }}
              disabled={busy}
            >
              {{ login: '登录', register: '注册', change: '改密', reset: '找回' }[t]}
            </button>
          ))}
        </div>

        <div className="field">
          <label htmlFor="login-server">服务器</label>
          <select
            id="login-server"
            value={server}
            onChange={(e) => setServer(e.target.value)}
            disabled={busy}
          >
            {servers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>

        <FormField
          label="账号"
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={mark('name')}
          validation={vAccount}
          hint={tab === 'register' ? '将用作您的游戏账号，注册后不可修改' : undefined}
          disabled={busy}
          onKeyDown={(e) => e.key === 'Enter' && submit()}
        />

        {(tab === 'login' || tab === 'register') && (
          <FormField
            ref={passwordRef}
            label="密码"
            required
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onBlur={mark('password')}
            validation={vPassword}
            hint={tab === 'register' ? '6–64 位，不含空白字符' : undefined}
            disabled={busy}
            onKeyDown={(e) => e.key === 'Enter' && submit()}
          />
        )}

        {tab === 'register' && (
          <FormField
            label="邮箱"
            required
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            onBlur={mark('email')}
            validation={vEmail}
            hint="用于找回密码，不会用于任何营销邮件"
            disabled={busy}
            onKeyDown={(e) => e.key === 'Enter' && submit()}
          />
        )}

        {tab === 'change' && (
          <>
            <FormField
              label="原密码"
              required
              type="password"
              value={oldPw}
              onChange={(e) => setOldPw(e.target.value)}
              onBlur={mark('oldPw')}
              validation={vOldPw}
              disabled={busy}
            />
            <FormField
              label="新密码"
              required
              type="password"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
              onBlur={mark('newPw')}
              validation={vNewPw}
              hint="至少 6 位，且与原密码不同"
              disabled={busy}
            />
            <FormField
              label="确认新密码"
              required
              type="password"
              value={newPw2}
              onChange={(e) => setNewPw2(e.target.value)}
              onBlur={mark('newPw2')}
              validation={vNewPw2}
              disabled={busy}
              onKeyDown={(e) => e.key === 'Enter' && submit()}
            />
          </>
        )}

        {tab === 'reset' && (
          <FormField
            label="注册邮箱"
            required
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            onBlur={mark('email')}
            validation={vEmail}
            hint="系统将重置密码并发送到此邮箱"
            disabled={busy}
            onKeyDown={(e) => e.key === 'Enter' && submit()}
          />
        )}

        <button className="btn-primary" disabled={busy || !name} onClick={submit}>
          {busy ? (
            <>
              <Spinner size={14} />
              处理中…
            </>
          ) : (
            buttonLabel
          )}
        </button>

        {error && <div className="error-msg">{error}</div>}
        {ok && <div className="ok-msg">{ok}</div>}
      </div>
    </div>
  )
}

/**
 * useMemoServers —— 从 LauncherConfig 推导服务器下拉项，附一套兜底。
 * 抽出为纯函数 hook 仅为了 LoginScreen 的可读性。
 */
function useMemoServers(config: LauncherConfig | null) {
  if (config?.servers && config.servers.length > 0) return config.servers
  return [
    { id: '5.8', name: 'AionCore 5.8', auth_port: 2108, game_args: '', client_path: '' },
    { id: '4.8', name: 'Beyond 4.8', auth_port: 2107, game_args: '', client_path: '' },
  ] as ServerLine[]
}

function MainScreen({
  config,
  selectedServer,
  onSelectServer,
}: {
  config: LauncherConfig | null
  selectedServer: string
  onSelectServer: (s: string) => void
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const [onlineCount, setOnlineCount] = useState<Record<string, number>>({})
  const [patchPhase, setPatchPhase] = useState<string>('')
  const [patchProgress, setPatchProgress] = useState<{ done: number; total: number; file: string }>({
    done: 0,
    total: 0,
    file: '',
  })
  // 速率统计：EMA 平滑 done/dt，避免瞬时抖动。
  // sampleRef 记录上一帧的 (time, done) 以算增量；speedRef 为 EMA bytes/s。
  const sampleRef = useRef<{ t: number; done: number } | null>(null)
  const [speedBps, setSpeedBps] = useState(0)
  const [error, setError] = useState('')
  const [launching, setLaunching] = useState(false)

  useEffect(() => {
    EventsOn('control:online_count', (raw: string) => {
      try {
        setOnlineCount(JSON.parse(raw))
      } catch {}
    })
    EventsOn('patch:progress', (p: any) => {
      setPatchPhase(p.phase)
      setPatchProgress({ done: p.done, total: p.total, file: p.file })

      // 仅在下载阶段更新速率（校验阶段 done 线性增长但不代表网络速度）
      if (p.phase === 'downloading') {
        const now = Date.now()
        const prev = sampleRef.current
        if (prev) {
          const dt = (now - prev.t) / 1000
          const dDone = p.done - prev.done
          if (dt > 0.05 && dDone >= 0) {
            const inst = dDone / dt
            // EMA α=0.3：过滤网络抖动
            setSpeedBps((cur) => (cur === 0 ? inst : cur * 0.7 + inst * 0.3))
          }
        }
        sampleRef.current = { t: now, done: p.done }
      } else {
        // 退出下载阶段清零采样，下次重新基线
        sampleRef.current = null
      }
    })
    EventsOn('patch:complete', () => {
      setPatchPhase('complete')
      setSpeedBps(0)
      sampleRef.current = null
      toast.success('补丁完成，已是最新版本')
    })
    EventsOn('patch:error', (p: any) => {
      const msg = p.error || 'patch failed'
      setError(msg)
      setPatchPhase('error')
      setSpeedBps(0)
      sampleRef.current = null
      toast.error(`补丁失败：${msg}`)
    })
    return () => {
      EventsOff('control:online_count')
      EventsOff('patch:progress')
      EventsOff('patch:complete')
      EventsOff('patch:error')
    }
  }, [toast])

  const pct =
    patchProgress.total > 0 ? Math.floor((patchProgress.done * 100) / patchProgress.total) : 0
  const remaining = Math.max(0, patchProgress.total - patchProgress.done)
  const etaSec = patchPhase === 'downloading' && speedBps > 0 ? remaining / speedBps : 0

  const startPatch = useCallback(async () => {
    setError('')
    try {
      await StartPatch(selectedServer)
    } catch (e: any) {
      const msg = e?.message || String(e)
      setError(msg)
      toast.error(`补丁启动失败：${msg}`)
    }
  }, [selectedServer, toast])

  /**
   * 当前选择服务器的客户端路径是否已配置。
   * 这是"启动游戏"的硬性前置条件 —— 未配置就启动必定失败。
   */
  const selectedClientPath = useMemo(() => {
    return config?.servers.find((s) => s.id === selectedServer)?.client_path || ''
  }, [config, selectedServer])

  const launch = useCallback(async () => {
    setError('')

    // 前置检查 1：服务器必须存在
    const cur = config?.servers.find((s) => s.id === selectedServer)
    if (!cur) {
      toast.error('请选择一个有效服务器')
      return
    }

    // 前置检查 2：客户端路径必须配置
    if (!selectedClientPath) {
      const ok = await confirm({
        title: '尚未配置客户端路径',
        message: `${cur.name} 的客户端路径为空。\n是否前往「设置」配置后再启动？`,
        confirmLabel: '前往设置',
      })
      if (ok) {
        // 通过 error 借道提示用户在 nav-bar 切"设置"（父组件管理 screen state，
        // 此处只能发一条信号）
        toast.info('请在底部导航栏选择「设置」配置客户端路径')
      }
      return
    }

    // 前置检查 3：若尚未 fetch 过 manifest，建议先补丁
    if (patchPhase === '' || patchPhase === 'error') {
      const ok = await confirm({
        title: '未检查补丁',
        message: '尚未进行补丁检查，直接启动游戏可能提示版本不一致。\n建议先执行补丁检查。',
        confirmLabel: '仍然启动',
        cancelLabel: '取消',
      })
      if (!ok) return
    }

    setLaunching(true)
    try {
      await LaunchGame(selectedServer)
      toast.success('游戏已启动')
    } catch (e: any) {
      const msg = e?.message || String(e)
      setError(msg)
      toast.error(`启动失败：${msg}`)
    } finally {
      setLaunching(false)
    }
  }, [config, selectedServer, selectedClientPath, patchPhase, toast, confirm])

  return (
    <div className="main-wrap">
      <div className="news-panel">
        <div className="news-header">游戏资讯</div>
        {config?.news_url ? (
          <iframe src={config.news_url} title="news" />
        ) : (
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#6b7380',
            }}
          >
            （未配置公告页）
          </div>
        )}
      </div>

      <div className="side-panel">
        <div className="server-picker">
          <label>选择服务器</label>
          <div className="choices">
            {(config?.servers || []).map((s) => (
              <div
                key={s.id}
                className={`choice ${s.id === selectedServer ? 'active' : ''}`}
                onClick={() => onSelectServer(s.id)}
              >
                {s.name}
              </div>
            ))}
          </div>
        </div>

        <div className="online-badge">
          <span>在线人数</span>
          <span style={{ color: '#6aa8ff', fontWeight: 500 }}>
            {onlineCount[selectedServer] ?? '—'}
          </span>
        </div>

        <div className="action-panel">
          <div style={{ fontSize: 'var(--fs-body-sm)', color: 'var(--color-text-dim)' }}>
            {patchPhase === '' && '准备就绪'}
            {patchPhase === 'fetching_manifest' && '正在获取清单…'}
            {patchPhase === 'verifying' && '正在校验文件…'}
            {patchPhase === 'downloading' && `下载中：${patchProgress.file}`}
            {patchPhase === 'up_to_date' && '已是最新版本'}
            {patchPhase === 'complete' && '补丁完成'}
            {patchPhase === 'error' && '补丁失败'}
          </div>

          {(patchPhase === 'downloading' || patchPhase === 'verifying') && (
            <>
              <div className="progress-bar">
                <div className="fill" style={{ width: `${pct}%` }} />
                <div className="label">{pct}%</div>
              </div>
              <div className="progress-meta">
                <span>
                  {formatBytes(patchProgress.done)} / {formatBytes(patchProgress.total)}
                </span>
                {patchPhase === 'downloading' && (
                  <>
                    <span className="progress-sep">·</span>
                    <span>{formatBytes(speedBps)}/s</span>
                    <span className="progress-sep">·</span>
                    <span>剩余 {formatEta(etaSec)}</span>
                  </>
                )}
              </div>
            </>
          )}

          {/* 补丁失败的优雅回退：给一条清晰的错误说明 + 重试按钮 */}
          {patchPhase === 'error' && error && (
            <div className="retry-row">
              <span className="retry-msg">{error}</span>
              <button className="retry-btn" onClick={startPatch}>
                重试
              </button>
            </div>
          )}

          <div className="buttons">
            <button
              className="btn-secondary"
              onClick={startPatch}
              disabled={
                launching ||
                patchPhase === 'fetching_manifest' ||
                patchPhase === 'verifying' ||
                patchPhase === 'downloading'
              }
            >
              {patchPhase === 'downloading' || patchPhase === 'verifying' ? (
                <>
                  <Spinner size={13} />
                  {patchPhase === 'downloading' ? '下载中' : '校验中'}
                </>
              ) : (
                '检查/下载补丁'
              )}
            </button>
            <button className="btn-primary" onClick={launch} disabled={launching}>
              {launching ? (
                <>
                  <Spinner size={14} />
                  启动中…
                </>
              ) : (
                '开始游戏'
              )}
            </button>
          </div>

          {/* 启动游戏相关错误单独展示，便于玩家定位 */}
          {error && patchPhase !== 'error' && <div className="error-msg">{error}</div>}
        </div>
      </div>
    </div>
  )
}

function SettingsScreen({ brand }: { brand: BrandConfig | null }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [controlURL, setCtrlURL] = useState('')
  const [controlURLInitial, setControlURLInitial] = useState('')
  const [controlURLTouched, setControlURLTouched] = useState(false)
  const [savingURL, setSavingURL] = useState(false)
  const [paths, setPaths] = useState<Record<string, string>>({})
  const [version, setVersion] = useState<{
    version: string
    build_time: string
    go_version: string
    platform: string
  } | null>(null)
  // 每个服务器路径的防抖保存 timer
  const pathTimers = useRef<Record<string, number>>({})

  useEffect(() => {
    GetPrefs()
      .then((p: any) => {
        setCtrlURL(p.control_url || '')
        setControlURLInitial(p.control_url || '')
        setPaths(p.client_paths || {})
      })
      .catch((e: any) => {
        toast.error(`读取配置失败：${e?.message || e}`)
      })
    GetVersion()
      .then((v: any) => setVersion(v))
      .catch(() => {
        // 版本查询失败不影响主流程，静默
      })
    // 组件卸载时清理未完成的 debounce
    return () => {
      Object.values(pathTimers.current).forEach((h) => window.clearTimeout(h))
      pathTimers.current = {}
    }
  }, [toast])

  const urlValidation = controlURLTouched ? validateHttpUrl(controlURL) : null
  const urlDirty = controlURL !== controlURLInitial
  const canSaveURL = urlDirty && !savingURL && validateHttpUrl(controlURL).ok

  const saveURL = async () => {
    setControlURLTouched(true)
    const v = validateHttpUrl(controlURL)
    if (!v.ok) {
      toast.warning(v.message || 'URL 无效')
      return
    }
    setSavingURL(true)
    try {
      await SetControlURL(controlURL)
      setControlURLInitial(controlURL)
      toast.success('控制中心地址已保存')
    } catch (e: any) {
      toast.error(`保存失败：${e?.message || e}`)
    } finally {
      setSavingURL(false)
    }
  }

  const resetURL = () => {
    setCtrlURL(controlURLInitial)
    setControlURLTouched(false)
  }

  /**
   * 客户端路径保存：用户输入后 600ms 无新输入才触发后端调用。
   * 避免每打一个字符就 flush 到 backend + Toast 刷屏。
   */
  const savePathDebounced = (server: string, path: string) => {
    setPaths((p) => ({ ...p, [server]: path }))
    const prev = pathTimers.current[server]
    if (prev !== undefined) window.clearTimeout(prev)
    pathTimers.current[server] = window.setTimeout(async () => {
      delete pathTimers.current[server]
      const v = validateClientPath(path)
      if (!v.ok) {
        // 空值允许（视作清除），非空才报错
        if (path.trim() !== '') {
          toast.warning(`${server}: ${v.message}`)
        }
        return
      }
      try {
        await SetClientPath(server, path)
        toast.success(`${server} 客户端路径已保存`)
      } catch (e: any) {
        toast.error(`${server} 保存失败：${e?.message || e}`)
      }
    }, 600)
  }

  const handleSwitchServer = async () => {
    const current = brand?.server_name || brand?.server_code || '未连接'
    const ok = await confirm({
      title: '切换服务器？',
      message: `将断开与「${current}」的连接并返回激活页。\n您的账号凭据保留在本地不会丢失，可随时重新连接。`,
      confirmLabel: '切换',
      danger: true,
    })
    if (!ok) return
    try {
      await ClearServerCode()
      toast.success('已断开，请输入新服务器代码')
    } catch (e: any) {
      toast.error(`切换失败：${e?.message || e}`)
    }
  }

  return (
    <div className="settings-wrap">
      <h2>设置</h2>

      <div className="settings-section">
        <h3>控制中心</h3>
        <div className="settings-field">
          <label htmlFor="settings-url">URL</label>
          <input
            id="settings-url"
            value={controlURL}
            onChange={(e) => {
              setCtrlURL(e.target.value)
              setControlURLTouched(true)
            }}
            onBlur={() => setControlURLTouched(true)}
            placeholder="http://127.0.0.1:10443"
            aria-invalid={urlValidation && !urlValidation.ok ? true : undefined}
          />
          {urlDirty && !savingURL && (
            <button
              className="btn-secondary"
              onClick={resetURL}
              style={{ padding: '8px 12px' }}
              title="放弃未保存的修改"
            >
              撤销
            </button>
          )}
          <button
            className="btn-secondary"
            onClick={saveURL}
            disabled={!canSaveURL}
            style={{ padding: '8px 14px' }}
          >
            {savingURL ? (
              <>
                <Spinner size={12} />
                保存中
              </>
            ) : (
              '保存'
            )}
          </button>
        </div>
        {urlValidation && !urlValidation.ok && (
          <div className="field-error" style={{ marginLeft: 132 }}>
            {urlValidation.message}
          </div>
        )}
        <div className="field-hint" style={{ marginLeft: 132, marginTop: 6 }}>
          控制中心负责签发会话 token，通常为 http://127.0.0.1:10443
        </div>
      </div>

      <div className="settings-section">
        <h3>客户端路径</h3>
        {['5.8', '4.8'].map((s) => (
          <div key={s} className="settings-field">
            <label htmlFor={`settings-path-${s}`}>{s}</label>
            <input
              id={`settings-path-${s}`}
              value={paths[s] || ''}
              onChange={(e) => savePathDebounced(s, e.target.value)}
              placeholder={
                s === '5.8' ? 'D:\\Aion\\5.8 （含 bin64/Aion.bin）' : 'D:\\Aion\\4.8 （含 bin32/Aion.bin）'
              }
            />
          </div>
        ))}
        <div className="field-hint" style={{ marginTop: 8 }}>
          提示：5.8 客户端目录需包含 <code>bin64/Aion.bin</code>；
          4.8 客户端目录需包含 <code>bin32/Aion.bin</code>。修改后 0.6 秒自动保存。
        </div>
      </div>

      <div className="settings-section">
        <h3>服务器连接</h3>
        <div className="settings-field">
          <label>当前服务器</label>
          <input value={brand?.server_name || brand?.server_code || '未连接'} disabled />
          <button
            className="btn-secondary"
            style={{ color: 'var(--color-danger)' }}
            onClick={handleSwitchServer}
          >
            切换服务器
          </button>
        </div>
        <div className="field-hint" style={{ marginLeft: 132, marginTop: 6 }}>
          切换将清除品牌主题，需重新输入邀请码连接。
        </div>
      </div>

      {/* About —— 启动器版本信息（调试 / 升级判定用） */}
      <div className="settings-section">
        <h3>关于</h3>
        <div className="about-grid">
          <div className="about-row">
            <span className="about-label">版本</span>
            <span className="about-value mono">{version?.version ?? '—'}</span>
          </div>
          {version?.build_time && (
            <div className="about-row">
              <span className="about-label">构建时间</span>
              <span className="about-value mono">{version.build_time}</span>
            </div>
          )}
          <div className="about-row">
            <span className="about-label">Go 版本</span>
            <span className="about-value mono">{version?.go_version ?? '—'}</span>
          </div>
          <div className="about-row">
            <span className="about-label">平台</span>
            <span className="about-value mono">{version?.platform ?? '—'}</span>
          </div>
        </div>
        <div className="field-hint" style={{ marginTop: 8 }}>
          如遇异常请截图此信息并提供给管理员；会同时注明服务器代码与 Request ID。
        </div>
      </div>
    </div>
  )
}

export default App
