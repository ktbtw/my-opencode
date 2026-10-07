type Envelope = {
  type: string
  payload: Record<string, unknown>
  requestID: string
}

// Bound the extra latency and memory, keep the first character immediate, and
// flush before every structural event so tools and terminal states stay ordered.
export class RelayDeltaBatcher {
  private pending?: Envelope
  private timer?: ReturnType<typeof setTimeout>
  private key = ""
  private sentAt = 0

  constructor(private readonly deliver: (envelope: Envelope) => boolean) {}

  send(envelope: Envelope): boolean {
    if (envelope.type !== "task.delta" || typeof envelope.payload.content !== "string") {
      this.flush()
      this.key = ""
      return this.deliver(envelope)
    }
    const key = JSON.stringify([envelope.payload.task_id, envelope.payload.session_id, envelope.payload.field])
    if (key !== this.key || (!this.pending && Date.now() - this.sentAt >= 40)) {
      this.flush()
      this.key = key
      this.sentAt = Date.now()
      return this.deliver(envelope)
    }
    if (this.pending) {
      this.pending.payload.content = String(this.pending.payload.content) + envelope.payload.content
    } else {
      this.pending = { ...envelope, payload: { ...envelope.payload } }
    }
    if (String(this.pending.payload.content).length >= 16_384) return this.flush()
    this.timer ??= setTimeout(() => this.flush(), Math.max(0, 40 - (Date.now() - this.sentAt)))
    return true
  }

  flush(): boolean {
    clearTimeout(this.timer)
    this.timer = undefined
    const pending = this.pending
    this.pending = undefined
    if (!pending) return true
    this.sentAt = Date.now()
    return this.deliver(pending)
  }
}
