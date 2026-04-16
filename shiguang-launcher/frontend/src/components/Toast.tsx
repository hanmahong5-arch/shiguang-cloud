/**
 * Toast
 * ------------------------------------------------------------------
 * 轻量级通知系统：Context + Portal 浮层，替代现有零散的 error-msg / ok-msg。
 *
 * 设计原则：
 *   - 零运行时依赖：不引入 react-hot-toast / sonner 等库，保持 launcher 体积
 *   - 语义化 4 类：success / error / warning / info，对应配色与图标文字
 *   - 自动消失：默认 4 秒；error 类 6 秒（给玩家看清）；可显式 dismiss
 *   - 动画：CSS transform + opacity 淡入上滑，退出反向；respect prefers-reduced-motion
 *   - 最大同时展示数：5 条，超出自动踢掉最早的（FIFO），防屏幕淹没
 *   - 幂等：相同文案在 500ms 内的重复请求被合并
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  ReactNode,
} from 'react'

export type ToastKind = 'success' | 'error' | 'warning' | 'info'

interface Toast {
  id: number
  kind: ToastKind
  message: string
  createdAt: number
}

interface ToastContextValue {
  show: (kind: ToastKind, message: string, opts?: { ttlMs?: number }) => void
  success: (message: string, opts?: { ttlMs?: number }) => void
  error: (message: string, opts?: { ttlMs?: number }) => void
  warning: (message: string, opts?: { ttlMs?: number }) => void
  info: (message: string, opts?: { ttlMs?: number }) => void
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

const MAX_TOASTS = 5
const DEFAULT_TTL: Record<ToastKind, number> = {
  success: 4000,
  info: 4000,
  warning: 5000,
  error: 6000,
}
const DEDUPE_WINDOW_MS = 500

/**
 * useToast —— 在组件内消费 Toast 能力。
 * 必须在 <ToastProvider> 下使用，否则抛错提示配置问题。
 */
export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) {
    throw new Error('useToast must be used within <ToastProvider>')
  }
  return ctx
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const idRef = useRef(0)
  // 保存每条 toast 的 setTimeout 句柄，dismiss 时清理避免竞态
  const timersRef = useRef<Map<number, number>>(new Map())

  const dismiss = useCallback((id: number) => {
    const t = timersRef.current.get(id)
    if (t !== undefined) {
      window.clearTimeout(t)
      timersRef.current.delete(id)
    }
    setToasts((prev) => prev.filter((x) => x.id !== id))
  }, [])

  const show = useCallback(
    (kind: ToastKind, message: string, opts?: { ttlMs?: number }) => {
      if (!message) return
      setToasts((prev) => {
        // 去重：相同 kind + message 且在 dedupe 窗口内则跳过
        const now = Date.now()
        const dup = prev.find(
          (t) => t.kind === kind && t.message === message && now - t.createdAt < DEDUPE_WINDOW_MS
        )
        if (dup) return prev

        const id = ++idRef.current
        const toast: Toast = { id, kind, message, createdAt: now }
        const ttl = opts?.ttlMs ?? DEFAULT_TTL[kind]
        if (ttl > 0) {
          const handle = window.setTimeout(() => dismiss(id), ttl)
          timersRef.current.set(id, handle)
        }

        // FIFO 裁剪
        const next = [...prev, toast]
        if (next.length > MAX_TOASTS) {
          const drop = next.shift()!
          const h = timersRef.current.get(drop.id)
          if (h !== undefined) {
            window.clearTimeout(h)
            timersRef.current.delete(drop.id)
          }
        }
        return next
      })
    },
    [dismiss]
  )

  // 组件卸载时清理所有计时器，防止内存泄漏
  useEffect(() => {
    return () => {
      timersRef.current.forEach((h) => window.clearTimeout(h))
      timersRef.current.clear()
    }
  }, [])

  const value = useMemo<ToastContextValue>(
    () => ({
      show,
      success: (m, o) => show('success', m, o),
      error: (m, o) => show('error', m, o),
      warning: (m, o) => show('warning', m, o),
      info: (m, o) => show('info', m, o),
      dismiss,
    }),
    [show, dismiss]
  )

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-viewport" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`}>
            <span className="toast-icon" aria-hidden="true">
              {iconFor(t.kind)}
            </span>
            <span className="toast-message">{t.message}</span>
            <button
              className="toast-close"
              onClick={() => dismiss(t.id)}
              aria-label="关闭"
              type="button"
            >
              ×
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function iconFor(kind: ToastKind): string {
  switch (kind) {
    case 'success':
      return '✓'
    case 'error':
      return '×'
    case 'warning':
      return '!'
    case 'info':
    default:
      return 'i'
  }
}
