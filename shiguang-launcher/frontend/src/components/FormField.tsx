/**
 * FormField
 * ------------------------------------------------------------------
 * 统一封装的表单字段：label + input + hint/error + required 标记。
 *
 * 用法：
 *   <FormField
 *     label="账号"
 *     required
 *     value={name}
 *     onChange={e => setName(e.target.value)}
 *     validation={touched ? validateAccount(name) : null}
 *     hint="4–32 位字母、数字、下划线或连字符"
 *     onBlur={() => setTouched(true)}
 *   />
 *
 * 约定：
 *   - `validation === null` 表示未触发校验（初次渲染、用户未交互），不显示错误
 *   - `validation.ok === true` 时不显示错误，退回展示 hint
 *   - `validation.ok === false` 时以 field-invalid 标识并覆盖 hint
 *   - aria-invalid / aria-describedby 按 WAI-ARIA 1.2 正确连线
 */
import { forwardRef, InputHTMLAttributes, ReactNode, useId } from 'react'
import type { Validation } from '../utils/validators'

type Props = InputHTMLAttributes<HTMLInputElement> & {
  label: ReactNode
  hint?: ReactNode
  validation?: Validation | null
  required?: boolean
}

export const FormField = forwardRef<HTMLInputElement, Props>(function FormField(
  { label, hint, validation, required, id: idProp, className, ...rest },
  ref
) {
  const auto = useId()
  const id = idProp || auto
  const invalid = !!(validation && !validation.ok)
  const describedBy = invalid ? `${id}-err` : hint ? `${id}-hint` : undefined

  return (
    <div className={`field ${invalid ? 'field-invalid' : ''} ${className ?? ''}`}>
      <label htmlFor={id}>
        {label}
        {required && (
          <span className="field-required" aria-label="必填">
            *
          </span>
        )}
      </label>
      <input
        id={id}
        ref={ref}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        autoComplete="off"
        spellCheck={false}
        {...rest}
      />
      {invalid ? (
        <span className="field-error" id={`${id}-err`} role="alert">
          {validation?.message}
        </span>
      ) : hint ? (
        <span className="field-hint" id={`${id}-hint`}>
          {hint}
        </span>
      ) : null}
    </div>
  )
})
