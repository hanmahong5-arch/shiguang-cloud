/**
 * Spinner
 * ------------------------------------------------------------------
 * 轻量旋转加载指示器，CSS 动画驱动。用于按钮内部与异步状态。
 * 尊重 prefers-reduced-motion：动画关闭但仍保留可视圆环。
 */
interface Props {
  /** 像素尺寸，默认 14 */
  size?: number
  /** 额外 CSS 类名 */
  className?: string
  /** 无障碍：屏幕阅读器标签 */
  label?: string
}

export function Spinner({ size = 14, className = '', label = '加载中' }: Props) {
  return (
    <span
      className={`spinner ${className}`}
      style={{ width: size, height: size, borderWidth: Math.max(2, Math.round(size / 7)) }}
      role="status"
      aria-label={label}
    />
  )
}
