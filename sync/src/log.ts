// A JSON-lines logger, one object per line on stdout, like the API's slog output. Never log a
// ticket, a token or a document's text.

export type Level = "debug" | "info" | "warn" | "error";

const order: Record<Level, number> = { debug: 0, info: 1, warn: 2, error: 3 };

export type Fields = Record<string, unknown>;

export interface Logger {
  debug(msg: string, fields?: Fields): void;
  info(msg: string, fields?: Fields): void;
  warn(msg: string, fields?: Fields): void;
  error(msg: string, fields?: Fields): void;
}

export function createLogger(level: Level, write: (line: string) => void = (l) => process.stdout.write(l + "\n")): Logger {
  const at = (l: Level) => (msg: string, fields: Fields = {}) => {
    if (order[l] < order[level]) return;
    const out: Fields = { time: new Date().toISOString(), level: l.toUpperCase(), msg };
    for (const [k, v] of Object.entries(fields)) {
      out[k] = v instanceof Error ? v.message : v;
    }
    write(JSON.stringify(out));
  };
  return { debug: at("debug"), info: at("info"), warn: at("warn"), error: at("error") };
}

/** A logger that drops everything, for tests. */
export const silentLogger: Logger = { debug() {}, info() {}, warn() {}, error() {} };
