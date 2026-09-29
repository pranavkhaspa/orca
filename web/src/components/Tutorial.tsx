import { useEffect, useRef, useState } from 'react'
import type { Copy } from '../tour'

// The tour is a real dialog: it traps nothing by accident, it can be dismissed
// three ways (the button, the backdrop, Escape), and it moves focus into itself
// and back out again. A modal that swallows keyboard focus is a trap for anyone
// not using a mouse, and this product's users are not all using a mouse.
export function Tutorial({ c, onClose }: { c: Copy; onClose: () => void }) {
  const [i, setI] = useState(0)
  const panel = useRef<HTMLDivElement>(null)
  const lastStep = i >= c.tourSteps.length - 1

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    panel.current?.focus()
    return () => prev?.focus?.()
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === 'ArrowRight' && i < c.tourSteps.length - 1) setI((n) => n + 1)
      if (e.key === 'ArrowLeft' && i > 0) setI((n) => n - 1)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [i, c.tourSteps.length, onClose])

  const step = c.tourSteps[i] ?? c.tourSteps[0]
  if (!step) return null

  return (
    <div
      className="tour-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        className="tour"
        role="dialog"
        aria-modal="true"
        aria-labelledby="tour-title"
        ref={panel}
        tabIndex={-1}
      >
        <h2 id="tour-title">{c.tourTitle}</h2>
        <div className="tour-step" aria-live="polite">
          <h3>
            {i + 1}/{c.tourSteps.length} · {step.title}
          </h3>
          <p>{step.body}</p>
        </div>

        <div className="tour-dots" role="tablist" aria-label={c.tourTitle}>
          {c.tourSteps.map((s, n) => (
            <button
              key={s.title}
              className={`tour-dot ${n === i ? 'on' : ''}`}
              role="tab"
              aria-selected={n === i}
              aria-label={s.title}
              onClick={() => setI(n)}
            />
          ))}
        </div>

        <div className="tour-actions">
          <button className="btn ghost" onClick={onClose}>
            {c.tourSkip}
          </button>
          <span className="spacer" />
          {i > 0 ? (
            <button className="btn" onClick={() => setI((n) => n - 1)}>
              {c.tourBack}
            </button>
          ) : null}
          <button
            className="btn primary"
            onClick={() => (lastStep ? onClose() : setI((n) => n + 1))}
          >
            {lastStep ? c.tourDone : c.tourNext}
          </button>
        </div>
      </div>
    </div>
  )
}
