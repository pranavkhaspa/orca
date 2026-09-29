// Inline SVG icons.
//
// Every icon is inlined rather than pulled from an icon package: the whole set
// is under 2 kB, it tree-shakes to nothing, and there is no font or sprite
// request to fail. On a fishing boat's connection a missing icon font is a row
// of empty boxes, which is the kind of detail that makes software feel broken.

interface IconProps {
  className?: string
}

const base = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

export function AnchorIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <circle cx="12" cy="5" r="2.4" />
      <path d="M12 7.4V21" />
      <path d="M7 12H4.6a7.4 7.4 0 0 0 14.8 0H17" />
      <path d="M7 12h10" />
    </svg>
  )
}

export function CheckIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} strokeWidth={2.6} aria-hidden="true">
      <path d="m4.5 12.5 5 5 10-11" />
    </svg>
  )
}

export function AlertIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} strokeWidth={2.4} aria-hidden="true">
      <path d="M12 3.6 2.6 20h18.8z" />
      <path d="M12 9.5v4.6" />
      <circle cx="12" cy="17.3" r="0.6" fill="currentColor" />
    </svg>
  )
}

export function StopIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} strokeWidth={2.4} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M8.5 8.5h7v7h-7z" fill="currentColor" stroke="none" />
    </svg>
  )
}

export function RuleIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} strokeWidth={2.2} aria-hidden="true">
      <path d="M12 3.2 5 6v6c0 4.2 3 7.6 7 8.8 4-1.2 7-4.6 7-8.8V6z" />
      <path d="m9 12 2.2 2.2L15.4 10" />
    </svg>
  )
}

export function GlobeIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3.2 12h17.6" />
      <path d="M12 3a15 15 0 0 1 0 18 15 15 0 0 1 0-18z" />
    </svg>
  )
}

export function InfoIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5.4" />
      <circle cx="12" cy="7.9" r="0.7" fill="currentColor" />
    </svg>
  )
}

export function HelpIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M9.4 9.3a2.7 2.7 0 1 1 3.6 2.5c-.7.3-1 .8-1 1.5v.4" />
      <circle cx="12" cy="16.6" r="0.7" fill="currentColor" />
    </svg>
  )
}

export function CompassIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="m15.5 8.5-2 5.2-5.2 2 2-5.2z" />
    </svg>
  )
}

export function PinIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M12 21s7-6.1 7-11a7 7 0 1 0-14 0c0 4.9 7 11 7 11z" />
      <circle cx="12" cy="10" r="2.6" />
    </svg>
  )
}

export function WaveIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M2 8.5c2.5 2.4 4.7 2.4 7.2 0s4.7-2.4 7.2 0 4.7 2.4 5.6 1.2" />
      <path d="M2 15c2.5 2.4 4.7 2.4 7.2 0s4.7-2.4 7.2 0 4.7 2.4 5.6 1.2" opacity=".6" />
    </svg>
  )
}

export function CloudIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M7 17.5h9.4a3.9 3.9 0 0 0 .3-7.8 5.6 5.6 0 0 0-10.7-1A3.9 3.9 0 0 0 7 17.5z" />
    </svg>
  )
}

export function GovernmentIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M3 9.6 12 4l9 5.6" />
      <path d="M5 9.6V19M9.5 9.6V19M14.5 9.6V19M19 9.6V19" />
      <path d="M3 19h18" />
    </svg>
  )
}

export function ScaleIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M12 4v16" />
      <path d="M6 20h12" />
      <path d="M4 8h16" />
      <path d="M4 8 2 14h4zM20 8l-2 6h4z" />
    </svg>
  )
}

export function PenIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M4 20h4l10-10-4-4L4 16z" />
      <path d="m14.5 5.5 4 4" />
    </svg>
  )
}

export function SpeakerIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M11 5 6.5 9H3v6h3.5L11 19z" />
      <path d="M15.5 9.2a4 4 0 0 1 0 5.6" />
      <path d="M18.2 6.6a8 8 0 0 1 0 10.8" opacity=".55" />
    </svg>
  )
}

export function ShieldIcon({ className }: IconProps) {
  return (
    <svg {...base} className={className} aria-hidden="true">
      <path d="M12 3.2 5 6v6c0 4.2 3 7.6 7 8.8 4-1.2 7-4.6 7-8.8V6z" />
    </svg>
  )
}

export const VERDICT_ICON = {
  go: CheckIcon,
  caution: AlertIcon,
  'no-go': StopIcon,
} as const

export const AGENT_ICON = {
  planner: CompassIcon,
  geo: PinIcon,
  ocean: WaveIcon,
  weather: CloudIcon,
  incois: GovernmentIcon,
  domain: ScaleIcon,
  narrator: PenIcon,
} as const
