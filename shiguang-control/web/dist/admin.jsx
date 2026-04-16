// Shiguang Admin SPA — single-file React (babel-transpiled in-browser).
// Intentionally minimal: no Vite build, no npm deps beyond React from CDN.
// This keeps the operator workflow simple: drop admin.jsx in web/dist, reload.

const { useState, useEffect } = React;

const API_BASE = ''; // same origin as control server

// ---------- API helper ----------
async function api(path, opts = {}) {
  const token = localStorage.getItem('shiguang_admin_token');
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch(API_BASE + path, { ...opts, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `HTTP ${res.status}`);
  }
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) return res.json();
  return res.text();
}

// ---------- Login ----------
function Login({ onLogin }) {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e) => {
    e?.preventDefault();
    setBusy(true);
    setError('');
    try {
      const r = await api('/api/admin/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      });
      if (r && r.token) {
        localStorage.setItem('shiguang_admin_token', r.token);
        onLogin();
      } else {
        setError('无 token 返回');
      }
    } catch (e) {
      setError(e.message || '登录失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <h2>拾光控制台</h2>
        <div className="sub">Shiguang Admin Console</div>
        <div className="field">
          <label>用户名</label>
          <input value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="field">
          <label>密码</label>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <button className="primary" disabled={busy}>
          {busy ? '登录中…' : '登录'}
        </button>
        {error && <div className="error-msg">{error}</div>}
      </form>
    </div>
  );
}

// ---------- Dashboard ----------
function Dashboard() {
  const [status, setStatus] = useState(null);
  const [error, setError] = useState('');

  const refresh = async () => {
    try {
      const r = await api('/api/admin/status');
      setStatus(r);
      setError('');
    } catch (e) {
      setError(e.message);
    }
  };

  useEffect(() => {
    refresh();
    const t = setInterval(refresh, 5000);
    return () => clearInterval(t);
  }, []);

  if (error) return <div className="error-msg">{error}</div>;
  if (!status) return <div className="loading">加载中…</div>;

  const gates = status.gates || {};
  return (
    <>
      <h1>仪表盘</h1>
      <div className="subtitle">实时服务状态 · 每 5 秒自动刷新</div>

      <div className="stats">
        <div className="stat-box">
          <div className="label">登录器在线</div>
          <div className="value">{status.launcher_online ?? 0}</div>
          <div className="hint">累计服务 {status.launcher_served ?? 0} 次</div>
        </div>
        {Object.entries(gates).map(([name, s]) => (
          <div className="stat-box" key={name}>
            <div className="label">{name}</div>
            {s.error ? (
              <>
                <div className="value" style={{ color: 'var(--danger)' }}>离线</div>
                <div className="hint">{s.error}</div>
              </>
            ) : (
              <>
                <div className="value">{sumRoutes(s.routes)}</div>
                <div className="hint">
                  已屏蔽 IP {s.ban_count ?? 0} · 活跃 IP {s.live_ips ?? 0} · uptime {s.uptime}
                </div>
              </>
            )}
          </div>
        ))}
      </div>

      <div className="card">
        <h3>路由明细</h3>
        <table>
          <thead>
            <tr>
              <th>网关</th>
              <th>路由</th>
              <th>已接受</th>
              <th>被拒</th>
              <th>活跃</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(gates).flatMap(([name, s]) =>
              (s.routes || []).map((r) => (
                <tr key={name + r.name}>
                  <td>{name}</td>
                  <td>{r.name}</td>
                  <td>{r.accepted}</td>
                  <td>{r.rejected}</td>
                  <td>{r.active}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}

function sumRoutes(routes) {
  if (!routes) return 0;
  return routes.reduce((a, r) => a + (r.active || 0), 0);
}

// ---------- Bans ----------
function Bans() {
  const [data, setData] = useState({});
  const [error, setError] = useState('');
  const [newIP, setNewIP] = useState('');
  const [newReason, setNewReason] = useState('');
  const [newDurationHours, setNewDurationHours] = useState('');

  const refresh = async () => {
    try {
      const r = await api('/api/admin/banlist');
      setData(r);
      setError('');
    } catch (e) {
      setError(e.message);
    }
  };
  useEffect(() => {
    refresh();
  }, []);

  const doBan = async () => {
    const ms = newDurationHours ? parseInt(newDurationHours) * 3600 * 1000 : 0;
    try {
      await api('/api/admin/ban', {
        method: 'POST',
        body: JSON.stringify({ ip: newIP, reason: newReason, duration_ms: ms }),
      });
      setNewIP('');
      setNewReason('');
      setNewDurationHours('');
      refresh();
    } catch (e) {
      setError(e.message);
    }
  };
  const doUnban = async (ip, gate) => {
    try {
      await api('/api/admin/unban', {
        method: 'POST',
        body: JSON.stringify({ ip, gate }),
      });
      refresh();
    } catch (e) {
      setError(e.message);
    }
  };

  return (
    <>
      <h1>IP 封禁</h1>
      <div className="subtitle">封禁会传播到所有配置的网关实例</div>

      <div className="card">
        <h3>添加封禁</h3>
        <div className="row">
          <input placeholder="IP 地址" value={newIP} onChange={(e) => setNewIP(e.target.value)} />
          <input placeholder="原因" value={newReason} onChange={(e) => setNewReason(e.target.value)} />
          <input
            placeholder="时长(小时, 留空=永久)"
            value={newDurationHours}
            onChange={(e) => setNewDurationHours(e.target.value)}
          />
          <button className="primary" onClick={doBan} disabled={!newIP}>
            封禁
          </button>
        </div>
        {error && <div className="error-msg">{error}</div>}
      </div>

      {Object.entries(data).map(([gateName, list]) => (
        <div className="card" key={gateName}>
          <h3>{gateName}</h3>
          {Array.isArray(list) && list.length > 0 ? (
            <table>
              <thead>
                <tr>
                  <th>IP</th>
                  <th>原因</th>
                  <th>封禁时间</th>
                  <th>到期</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.map((b) => (
                  <tr key={b.ip}>
                    <td>{b.ip}</td>
                    <td>{b.reason}</td>
                    <td>{new Date(b.banned_at).toLocaleString()}</td>
                    <td>
                      {b.expires_at && b.expires_at !== '0001-01-01T00:00:00Z'
                        ? new Date(b.expires_at).toLocaleString()
                        : '永久'}
                    </td>
                    <td>
                      <button className="danger" onClick={() => doUnban(b.ip, gateName)}>
                        解封
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div style={{ color: 'var(--text-dim)', padding: '12px 0' }}>
              {list && list.error ? list.error : '无封禁记录'}
            </div>
          )}
        </div>
      ))}
    </>
  );
}

// ---------- LauncherConfig ----------
function LauncherConfig() {
  const [cfg, setCfg] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch('/api/launcher/config')
      .then((r) => r.json())
      .then(setCfg)
      .catch((e) => setError(e.message));
  }, []);

  if (error) return <div className="error-msg">{error}</div>;
  if (!cfg) return <div className="loading">加载中…</div>;

  return (
    <>
      <h1>登录器配置</h1>
      <div className="subtitle">当前下发给玩家的配置（只读，修改需编辑 control.yaml）</div>

      <div className="card">
        <h3>基础参数</h3>
        <pre>{JSON.stringify(cfg, null, 2)}</pre>
      </div>
    </>
  );
}

// ---------- Shell ----------
function Shell({ onLogout }) {
  const [page, setPage] = useState('dashboard');
  return (
    <div className="layout">
      <div className="sidebar">
        <div className="logo">拾光控制台</div>
        <div className="nav">
          {[
            { id: 'dashboard', label: '仪表盘' },
            { id: 'bans', label: 'IP 封禁' },
            { id: 'launcher', label: '登录器配置' },
          ].map((n) => (
            <div
              key={n.id}
              className={`nav-item ${page === n.id ? 'active' : ''}`}
              onClick={() => setPage(n.id)}
            >
              {n.label}
            </div>
          ))}
          <div
            className="nav-item"
            onClick={() => {
              localStorage.removeItem('shiguang_admin_token');
              onLogout();
            }}
          >
            退出登录
          </div>
        </div>
      </div>
      <div className="main">
        {page === 'dashboard' && <Dashboard />}
        {page === 'bans' && <Bans />}
        {page === 'launcher' && <LauncherConfig />}
      </div>
    </div>
  );
}

// ---------- Root ----------
function App() {
  const [loggedIn, setLoggedIn] = useState(!!localStorage.getItem('shiguang_admin_token'));
  return loggedIn ? <Shell onLogout={() => setLoggedIn(false)} /> : <Login onLogin={() => setLoggedIn(true)} />;
}

ReactDOM.createRoot(document.getElementById('app')).render(<App />);
