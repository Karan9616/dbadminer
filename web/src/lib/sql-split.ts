/** Split SQL into statements, respecting quotes and comments. */
export function splitStatements(sql: string): string[] {
  const parts: string[] = [];
  let start = 0;
  let i = 0;
  const s = sql;

  const push = (end: number) => {
    const part = s.slice(start, end).trim();
    if (part) parts.push(part);
  };

  while (i < s.length) {
    const c = s[i];
    if (c === "-" && s[i + 1] === "-") {
      i += 2;
      while (i < s.length && s[i] !== "\n") i++;
      continue;
    }
    if (c === "#") {
      i++;
      while (i < s.length && s[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && s[i + 1] === "*") {
      const end = s.indexOf("*/", i + 2);
      if (end < 0) break;
      i = end + 2;
      continue;
    }
    if (c === "'" || c === '"' || c === "`") {
      const quote = c;
      i++;
      while (i < s.length) {
        if (s[i] === quote) {
          if (s[i + 1] === quote) {
            i += 2;
            continue;
          }
          i++;
          break;
        }
        i++;
      }
      continue;
    }
    if (c === "$") {
      let j = i + 1;
      while (j < s.length && /[A-Za-z0-9_]/.test(s[j]!)) j++;
      if (j < s.length && s[j] === "$") {
        const tag = s.slice(i, j + 1);
        const rest = s.indexOf(tag, j + 1);
        if (rest >= 0) {
          i = rest + tag.length;
          continue;
        }
      }
    }
    if (c === ";") {
      push(i);
      start = i + 1;
      i++;
      continue;
    }
    i++;
  }
  push(s.length);
  return parts;
}
