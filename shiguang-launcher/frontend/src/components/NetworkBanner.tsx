/**
 * NetworkBanner
 * ------------------------------------------------------------------
 * 顶部全宽横幅：控制中心不可达时自动展示，恢复后自动隐藏。
 *
 * 信号来源：
 *   1. Go 侧 FetchLauncherConfig 重试全部失败后 emit "control:offline" 事件
 *   2. 恢复：Go 侧 FetchLauncherConfig 成功后 emit "control:online" 事件
 *      （注意：当前 Go 侧仅 emit offline，我们同时在 React 层做轮询探测）
 *   3. 前端备用探测：如果 banner 已展示，每 15 秒尝试一次 FetchLauncherConfig，
 *      成功则自动隐藏
 *
 * 设计考量：
 *   - 不打断用户操作（banner 非模态、不阻挡点击）
 *   - 黄色警告色 + 简短文案 + 手动刷新按钮
 *   - 首次展示时 toast.warning 辅助提醒
 */
import { useEffect, useRef, useState } from 'react'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { FetchLauncherConfig } from '../../wailsjs/go/main/App'

export function NetworkBanner() {
  const [offline, setOffline] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const [checking, setChecking] = useState(false)
  const retryTimer = useRef<number | null>(null)

  useEffect(() => {
    // 监听 Go 侧离线事件
    EventsOn('control:offline', (p: any) => {
      setOffline(true)
      setErrorMsg(p?.error || '控制中心不可达')
    })
    EventsOn('control:online', () => {
      setOffline(false)
      setErrorMsg('')
    })
    return () => {
      EventsOff('control:offline')
      EventsOff('control:online')
      if (retryTimer.current !== null) {
        window.clearInterval(retryTimer.current)
      }
    }
  }, [])

  // 横幅可见时每 15 秒自动探测恢复
  useEffect(() => {
    if (!offline) {
      if (retryTimer.current !== null) {
        window.clearInterval(retryTimer.current)
        retryTimer.current = null
      }
      return
    }
    retryTimer.current = window.setInterval(() => {
      FetchLauncherConfig()
        .then(() => {
          setOffline(false)
          setErrorMsg('')
        })
        .catch(() => {
          // 仍然离线，不做任何事
        })
    }, 15000)
    return () => {
      if (retryTimer.current !== null) {
        window.clearInterval(retryTimer.current)
        retryTimer.current = null
      }
    }
  }, [offline])

  const manualRetry = async () => {
    setChecking(true)
    try {
      await FetchLauncherConfig()
      setOffline(false)
      setErrorMsg('')
    } catch {
      // 仍然离线
    } finally {
      setChecking(false)
    }
  }

  if (!offline) return null

  return (
    <div className="network-banner" role="alert">
      <span className="network-banner-icon" aria-hidden="true">
        ⚠
      </span>
      <span className="network-banner-msg">
        控制中心连接中断
        {errorMsg && <span className="network-banner-detail"> — {errorMsg}</span>}
      </span>
      <button
        className="network-banner-btn"
        onClick={manualRetry}
        disabled={checking}
      >
        {checking ? '检测中…' : '重试'}
      </button>
    </div>
  )
}
