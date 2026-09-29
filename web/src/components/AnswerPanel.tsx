import { useEffect, useState } from 'react'
import { voiceTag } from '../i18n'
import type { T } from '../i18n'

// Text-to-speech uses the browser's built-in engine: no API key, no audio bytes
// over the wire, and it works while the backend is in offline mode. The speech is
// generated from the same localized string the user is reading, so the two can
// never disagree.
export function AnswerPanel({
  answer,
  lang,
  t,
}: {
  answer: string
  lang: string
  t: T
}) {
  const [speaking, setSpeaking] = useState(false)
  const [supported, setSupported] = useState(false)

  useEffect(() => {
    setSupported(typeof window !== 'undefined' && 'speechSynthesis' in window)
  }, [])

  useEffect(() => {
    return () => {
      if (typeof window !== 'undefined' && 'speechSynthesis' in window) {
        window.speechSynthesis.cancel()
      }
    }
  }, [])

  const speak = () => {
    if (!supported) return
    const synth = window.speechSynthesis
    if (speaking) {
      synth.cancel()
      setSpeaking(false)
      return
    }
    // Strip markdown emphasis so the engine does not read asterisks aloud.
    const plain = answer
      .replace(/\*\*/g, '')
      .replace(/^#+\s*/gm, '')
      .replace(/`/g, '')
    const u = new SpeechSynthesisUtterance(plain)
    u.lang = voiceTag(lang)
    // Prefer a local voice: a remote voice would need network and would defeat
    // the point of an offline-capable demo.
    const voices = synth.getVoices()
    const match =
      voices.find((v) => v.lang === u.lang && v.localService) ??
      voices.find((v) => v.lang === u.lang) ??
      voices.find((v) => v.lang.startsWith(lang))
    if (match) u.voice = match
    u.onend = () => setSpeaking(false)
    u.onerror = () => setSpeaking(false)
    synth.speak(u)
    setSpeaking(true)
  }

  return (
    <section className="panel answer">
      <div className="answer-head">
        <h2>{t.answer}</h2>
        {supported ? (
          <button className="btn ghost" onClick={speak} aria-pressed={speaking}>
            {speaking ? `⏹ ${t.stop}` : `🔊 ${t.speak}`}
          </button>
        ) : null}
      </div>
      {answer.split(/\n{2,}/).map((block, i) => {
        const heading = block.startsWith('**') && block.endsWith('**')
        const text = block.replace(/\*\*/g, '')
        if (heading) return <h3 key={i}>{text}</h3>
        return <p key={i}>{text}</p>
      })}
    </section>
  )
}
