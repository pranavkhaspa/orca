import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AnchorMark } from './AnchorMark'
import {
  CloseIcon,
  GlobeIcon,
  HelpIcon,
  LayersIcon,
  MenuIcon,
  ShieldIcon,
  SignalIcon,
  SourceIcon,
} from './Icons'
import type { Health, Meta } from '../types'

// The navigation bar.
//
// This replaced a plain title row, which was doing two jobs badly: identifying
// the product and hiding three unrelated controls in the corner. A user who has
// just been told NO-GO has a verdict, a map, a set of hazard checks and a source
// list on one long page, and no way to move between them. On a phone that page
// is several screens tall. So this is a real nav rail with scroll-spy, not a
// decorative header.
//
// Two decisions worth stating, because they look like over-engineering until
// you have used them:
//
// The section links only appear once there is a result. Before a question is
// asked they would scroll to nothing, and a nav item that does nothing is worse
// than no nav item. This also keeps the first screen clean, which is the state
// that matters for a first-time visitor.
//
// The health chip is a chip, not a status line. In a safety tool "is this thing
// actually looking at the sea right now, or is it reading a snapshot from last
// Tuesday" is a question the user should be able to answer without asking, and
// it has to be legible at a glance from the far end of a phone.

// Section order mirrors reading order down the results column.
// Section ids are fixed and drive the scroll-spy and the anchor targets, so only
// the visible label is localized. The id is never user-facing and must not be
// translated, or a translated id would break every anchor that points at it.
const SECTION_IDS: { id: string; key: 'navVerdict' | 'navZone' | 'navSources'; Icon: typeof ShieldIcon }[] = [
  { id: 'verdict', key: 'navVerdict', Icon: ShieldIcon },
  { id: 'zone', key: 'navZone', Icon: LayersIcon },
  { id: 'sources', key: 'navSources', Icon: SourceIcon },
]

interface Props {
  t: {
    title: string
    tagline: string
    language: string
    navVerdict: string
    navZone: string
    navSources: string
    resultSections: string
    openMenu: string
    closeMenu: string
  }
  c: { tourCta: string }
  meta: Meta | null
  health: Health | null
  lang: string
  onLang: (l: string) => void
  onTour: () => void
  hasResults: boolean
}

export function TopBar({ t, c, meta, health, lang, onLang, onTour, hasResults }: Props) {
  // Which section is on screen. Starts on the first one so the rail has an
  // active state immediately rather than flickering in as the user scrolls.
  const sections = useMemo(
    () => SECTION_IDS.map((s) => ({ ...s, label: t[s.key] })),
    [t],
  )

  const [active, setActive] = useState(() => SECTION_IDS[0]?.id ?? '')
  const [open, setOpen] = useState(false)
  const [scrolled, setScrolled] = useState(false)
  const railRef = useRef<HTMLDivElement>(null)

  // Close the mobile sheet whenever a section is chosen, so the result is not
  // hidden behind the sheet that was just dismissed to reach it.
  const go = useCallback((id: string) => {
    setOpen(false)
    document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [])

  useEffect(() => {
    if (!hasResults) {
      setActive(SECTION_IDS[0]?.id ?? '')
      return
    }

    // Scroll-spy by geometry rather than by IntersectionObserver ratios.
    //
    // The ratio approach was tried first and is wrong in the case that matters
    // most: Sources is the last panel, so at the bottom of the page it can never
    // reach the top of the viewport, and a top-anchored observer never reports
    // it. The rail then said "Verdict" while the user was looking at the source
    // list, which is worse than having no rail at all.
    //
    // Instead: the active section is the last one whose top has passed the bar.
    // This is a single well-defined rule that behaves the same at the top, in
    // the middle and pinned to the bottom, and it costs one read of two rects
    // per frame.
    const measure = () => {
      const bar = railRef.current?.getBoundingClientRect().bottom ?? 0
      let current = SECTION_IDS[0]?.id ?? ''
      for (const s of SECTION_IDS) {
        const el = document.getElementById(s.id)
        if (!el) continue
        if (el.getBoundingClientRect().top <= bar + 2) current = s.id
      }
      // The final section is usually shorter than the viewport, so its top can
      // never pass the bar and the rule above leaves the rail naming the
      // previous section while the reader is looking at the last one. When the
      // document is essentially at its end and the last section is on screen,
      // the last section is what is being read.
      //
      // The tolerance is a screenful rather than two pixels because the page
      // grows while the answer streams in: a click on the last section scrolls
      // to the maximum that was valid at that moment, and the text that arrives
      // afterwards pushes the true maximum further down. Landing exactly on the
      // end is not something the user can be held to.
      const last = SECTION_IDS[SECTION_IDS.length - 1]
      const el = last && document.getElementById(last.id)
      if (el) {
        const max = document.documentElement.scrollHeight - window.innerHeight
        if (window.scrollY >= max - 96 && el.getBoundingClientRect().top < window.innerHeight) {
          current = last.id
        }
      }
      setActive(current)
    }

    measure()
    window.addEventListener('scroll', measure, { passive: true })
    window.addEventListener('resize', measure)
    // Streaming the answer changes the document height without any scroll or
    // resize, which left the rail naming a stale section until the user happened
    // to scroll. Observing the body catches that, and it is cheap because the
    // callback is one rect read per layout pass.
    const ro = new ResizeObserver(measure)
    ro.observe(document.body)
    return () => {
      window.removeEventListener('scroll', measure)
      window.removeEventListener('resize', measure)
      ro.disconnect()
    }
  }, [hasResults])

  // Condense on scroll. The bar is opaque enough to read at the top of the page
  // and gains a border and a tighter rhythm once content is behind it, which is
  // the difference between a header that floats and one that sits on the work.
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  // Escape closes the sheet, and a resize past the breakpoint closes it too —
  // otherwise the open state persists invisibly on a wide window.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    const onResize = () => window.innerWidth > 860 && setOpen(false)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', onResize)
    return () => {
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onResize)
    }
  }, [open])

  const languages = meta?.languages ?? [{ code: 'en', native: 'English' }]

  return (
    <header className={`top${scrolled ? ' scrolled' : ''}`} ref={railRef}>
      <div className="top-inner">
        <div className="brand">
          {/* Only the mark is the button. The wordmark beside it is the page's
              one <h1>, and a heading inside a button is both invalid nesting and
              a worse outline for screen readers than a plain heading next to a
              labelled control. */}
          <button
            className="logo"
            onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
            aria-label={t.title}
          >
            <AnchorMark />
          </button>
          <div className="brand-text">
            <div className="brand-row">
              <h1 className="wordmark">{t.title}</h1>
              <span className="brand-tag">SIH26176</span>
            </div>
            <p className="tagline">{t.tagline}</p>
          </div>
        </div>

        {hasResults ? (
          <>
            <nav className="rail" aria-label={t.resultSections}>
              {sections.map((s) => (
                <button
                  key={s.id}
                  className={`rail-link${active === s.id ? ' on' : ''}`}
                  aria-current={active === s.id ? 'true' : undefined}
                  onClick={() => go(s.id)}
                >
                  <s.Icon className="rail-ico" />
                  {s.label}
                </button>
              ))}
            </nav>

            <button
              className="icon-btn menu-btn"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              aria-controls="top-sheet"
              aria-label={open ? t.closeMenu : t.openMenu}
            >
              {open ? <CloseIcon /> : <MenuIcon />}
            </button>
          </>
        ) : null}

        <div
          className={`top-right${hasResults ? ' in-sheet' : ''}${open ? ' open' : ''}`}
          id="top-sheet"
        >
          {health ? <HealthChip health={health} /> : null}

          <div className="lang-row">
            <GlobeIcon className="lang-ico" />
            <label className="lang-pick">
              <span className="sr-only">{t.language}</span>
              <select
                value={lang}
                onChange={(e) => onLang(e.target.value)}
                aria-label={t.language}
              >
                {languages.map((l) => (
                  <option key={l.code} value={l.code}>
                    {l.native}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <button className="btn ghost tour-btn" onClick={onTour}>
            <HelpIcon className="btn-ico" />
            <span className="tour-label">{c.tourCta}</span>
          </button>
        </div>
      </div>
    </header>
  )
}

// The health chip.
//
// Kept deliberately small and factual: a dot, a word, and the detail in the
// title attribute. It is not a status dashboard, and it must never claim more
// than it knows — in offline mode it says "offline", because a green light over
// baked data would be the single most misleading thing this interface could do.
function HealthChip({ health }: { health: Health }) {
  const detail = health.offline
    ? 'ORCA_OFFLINE is set: every value comes from the baked snapshot'
    : `snapshot ${health.snapshot_age} · ${health.cache_entries} cached · ${health.places} places`
  return (
    <span className={`health${health.offline ? ' offline' : ''}`} title={detail}>
      <SignalIcon className="health-ico" />
      {health.offline ? 'offline' : health.status}
    </span>
  )
}
