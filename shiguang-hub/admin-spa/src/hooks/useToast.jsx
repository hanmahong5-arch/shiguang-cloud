import { useState, useCallback, createContext, useContext } from 'react'

const ToastContext = createContext(null)
let nextId = 0

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])

  const dismiss = useCallback((id) => {
    // Mark toast as exiting for CSS animation
    setToasts(prev => prev.map(t => t.id === id ? { ...t, exiting: true } : t))
    // Remove after animation completes
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 250)
  }, [])

  const push = useCallback((type, message, duration = 4000) => {
    const id = ++nextId
    setToasts(prev => [...prev, { id, type, message, exiting: false }])
    if (duration > 0) {
      setTimeout(() => dismiss(id), duration)
    }
    return id
  }, [dismiss])

  const toast = {
    success: (msg) => push('success', msg),
    error:   (msg) => push('error', msg, 6000),
    info:    (msg) => push('info', msg),
  }

  return (
    <ToastContext.Provider value={toast}>
      {children}
      {toasts}
    </ToastContext.Provider>
  )
}

// Access both the toast function and raw toasts for rendering
export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}

// Separate provider that also exposes toasts array for rendering
export function ToastProviderWithRender({ children }) {
  const [toasts, setToasts] = useState([])

  const dismiss = useCallback((id) => {
    setToasts(prev => prev.map(t => t.id === id ? { ...t, exiting: true } : t))
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 250)
  }, [])

  const push = useCallback((type, message, duration = 4000) => {
    const id = ++nextId
    setToasts(prev => [...prev, { id, type, message, exiting: false }])
    if (duration > 0) {
      setTimeout(() => dismiss(id), duration)
    }
    return id
  }, [dismiss])

  const toast = {
    success: (msg) => push('success', msg),
    error:   (msg) => push('error', msg, 6000),
    info:    (msg) => push('info', msg),
    toasts,
    dismiss,
  }

  return (
    <ToastContext.Provider value={toast}>
      {children}
    </ToastContext.Provider>
  )
}
