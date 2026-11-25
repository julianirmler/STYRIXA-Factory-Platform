// Minimaler SSE-Parser für fetch-Streams (POST ist mit EventSource nicht möglich).
// Robust gegen Ereignisse über Chunk-Grenzen und \r\n bzw. \r als Zeilenende.

export function createSSEParser(onEvent) {
  let buf = "";
  let event = "";
  let data = [];

  function line(l) {
    if (l === "") {
      if (data.length) onEvent({ event: event || "message", data: data.join("\n") });
      event = "";
      data = [];
      return;
    }
    if (l.startsWith(":")) return; // Kommentar
    const i = l.indexOf(":");
    const field = i < 0 ? l : l.slice(0, i);
    let value = i < 0 ? "" : l.slice(i + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }

  return {
    push(text) {
      buf += text;
      for (;;) {
        const m = /\r\n|\r|\n/.exec(buf);
        if (!m) break;
        // \r am Pufferende könnte zu \r\n gehören → auf nächsten Chunk warten
        if (m[0] === "\r" && m.index === buf.length - 1) break;
        line(buf.slice(0, m.index));
        buf = buf.slice(m.index + m[0].length);
      }
    },
    end() {
      if (buf) line(buf);
      buf = "";
      line("");
    },
  };
}

/** Liest einen fetch-ReadableStream und ruft onEvent je Ereignis. */
export async function readSSE(body, onEvent) {
  const parser = createSSEParser(onEvent);
  const reader = body.getReader();
  const dec = new TextDecoder();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    parser.push(dec.decode(value, { stream: true }));
  }
  parser.push(dec.decode());
  parser.end();
}
