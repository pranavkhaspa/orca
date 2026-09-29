// The mark is an anchor over two waves — the same drawing as the favicon, kept
// as a component so the two cannot drift apart.
export function AnchorMark() {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true">
      <path
        d="M16 5v11"
        stroke="currentColor"
        strokeWidth="2.6"
        strokeLinecap="round"
        fill="none"
      />
      <circle cx="16" cy="6" r="2.8" fill="currentColor" />
      <path
        d="M5 18c3.6 3.4 6.8 3.4 11 0 4.2-3.4 7.4-3.4 11 0"
        stroke="currentColor"
        strokeWidth="2.6"
        fill="none"
        strokeLinecap="round"
      />
      <path
        d="M5 24c3.6 3.4 6.8 3.4 11 0 4.2-3.4 7.4-3.4 11 0"
        stroke="currentColor"
        strokeWidth="2.6"
        fill="none"
        strokeLinecap="round"
        opacity=".6"
      />
    </svg>
  )
}
