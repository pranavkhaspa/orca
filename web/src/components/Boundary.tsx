import { Component, type ErrorInfo, type ReactNode } from 'react'

// A render error in an optional panel must not take the verdict with it.
//
// The globe is the riskiest component in the app: it is a WebGL context, a
// third-party renderer and a large lazy chunk, and on an old Android browser or
// a locked-down desktop any of those can fail. When it does, the user still
// needs the one thing they came for, so the boundary swaps the child for a
// fallback and leaves the rest of the page standing.
interface Props {
  fallback: ReactNode
  label: string
  children: ReactNode
}

interface State {
  failed: boolean
}

export class Boundary extends Component<Props, State> {
  state: State = { failed: false }

  static getDerivedStateFromError(): State {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Kept as a console warning rather than swallowed: if the globe is failing
    // for real users, the evidence should be somewhere a developer can find it.
    console.warn(`[orca] ${this.props.label} failed, using fallback:`, error.message, info.componentStack)
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children
  }
}
