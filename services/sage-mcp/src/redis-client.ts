/**
 * Minimal Redis RESP2 client for the sage-mcp stdio server.
 * Supports PUBLISH and SUBSCRIBE. No external dependencies.
 */

import net from "node:net";

type MessageHandler = (channel: string, message: string) => void;

interface PendingReply {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
}

export class RedisClient {
  private socket: net.Socket | null = null;
  private buffer = "";
  private pendingReplies: PendingReply[] = [];
  private subscriptions = new Map<string, MessageHandler[]>();
  private connected = false;

  constructor(
    private readonly host: string,
    private readonly port: number,
    private readonly password?: string,
  ) {}

  async connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const sock = net.createConnection({ host: this.host, port: this.port });
      sock.setEncoding("utf8");
      sock.on("connect", async () => {
        this.socket = sock;
        this.connected = true;
        if (this.password) await this.send("AUTH", this.password);
        resolve();
      });
      sock.on("data", (chunk: string) => this.handleData(chunk));
      sock.on("error", (err) => { if (!this.connected) reject(err); });
      sock.on("close", () => { this.connected = false; });
    });
  }

  async publish(channel: string, message: string): Promise<void> {
    await this.send("PUBLISH", channel, message);
  }

  async subscribe(channel: string, handler: MessageHandler): Promise<void> {
    const existing = this.subscriptions.get(channel);
    if (existing) { existing.push(handler); return; }
    this.subscriptions.set(channel, [handler]);
    await this.send("SUBSCRIBE", channel);
  }

  close(): void {
    this.socket?.destroy();
    this.connected = false;
  }

  private send(...args: string[]): Promise<unknown> {
    return new Promise((resolve, reject) => {
      if (!this.socket) { reject(new Error("Not connected")); return; }
      this.pendingReplies.push({ resolve, reject });
      let cmd = `*${args.length}\r\n`;
      for (const arg of args) {
        cmd += `$${Buffer.byteLength(arg)}\r\n${arg}\r\n`;
      }
      this.socket.write(cmd);
    });
  }

  private handleData(chunk: string): void {
    this.buffer += chunk;
    while (this.buffer.length > 0) {
      const result = this.parseResp(this.buffer);
      if (result === null) break;
      this.buffer = this.buffer.slice(result.consumed);
      this.dispatchReply(result.value);
    }
  }

  private dispatchReply(value: unknown): void {
    if (Array.isArray(value)) {
      const [kind, channel, payload] = value as string[];
      if (kind === "message" && typeof channel === "string" && typeof payload === "string") {
        const handlers = this.subscriptions.get(channel);
        if (handlers) for (const h of handlers) h(channel, payload);
        return;
      }
    }
    const pending = this.pendingReplies.shift();
    if (pending) pending.resolve(value);
  }

  private parseResp(buf: string): { value: unknown; consumed: number } | null {
    if (buf.length === 0) return null;
    const type = buf[0];
    const end = buf.indexOf("\r\n");
    if (end === -1) return null;
    if (type === "+") return { value: buf.slice(1, end), consumed: end + 2 };
    if (type === "-") return { value: new Error(buf.slice(1, end)), consumed: end + 2 };
    if (type === ":") return { value: parseInt(buf.slice(1, end), 10), consumed: end + 2 };
    if (type === "$") {
      const len = parseInt(buf.slice(1, end), 10);
      if (len === -1) return { value: null, consumed: end + 2 };
      const start = end + 2;
      if (buf.length < start + len + 2) return null;
      return { value: buf.slice(start, start + len), consumed: start + len + 2 };
    }
    if (type === "*") {
      const count = parseInt(buf.slice(1, end), 10);
      if (count === -1) return { value: null, consumed: end + 2 };
      let offset = end + 2;
      const arr: unknown[] = [];
      for (let i = 0; i < count; i++) {
        const item = this.parseResp(buf.slice(offset));
        if (item === null) return null;
        arr.push(item.value);
        offset += item.consumed;
      }
      return { value: arr, consumed: offset };
    }
    return null;
  }
}
