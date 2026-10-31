import { acceptCompletion, autocompletion, type Completion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { PostgreSQL, MariaSQL, MySQL, SQLite, sql, type SQLNamespace } from "@codemirror/lang-sql";
import { bracketMatching, defaultHighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { EditorState, Compartment, Prec, type Extension } from "@codemirror/state";
import { oneDarkHighlightStyle } from "@codemirror/theme-one-dark";
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers, placeholder } from "@codemirror/view";
import { api, type TableInfo } from "./api";

export interface EditorContext {
  dialect: string;
  tables: TableInfo[];
  dark: boolean;
}

export interface EditorHandle {
  setText(text: string): void;
  setContext(ctx: EditorContext): void;
  runnable(): string;
  focus(): void;
  destroy(): void;
}

const columnCache = new Map<string, Completion[]>();

function dialectOf(name: string) {
  if (name === "mysql") return MySQL;
  if (name === "mariadb") return MariaSQL;
  if (name === "sqlite") return SQLite;
  return PostgreSQL;
}

function unquote(ident: string): string {
  return ident.replace(/^[`"']|[`"']$/g, "");
}

function schemaOf(tables: TableInfo[]): SQLNamespace {
  const schemaNames = new Set(tables.map((t) => t.schema));
  const schema: Record<string, SQLNamespace> = {};
  for (const table of tables) {
    const bucket = schema[table.schema];
    const next: Record<string, readonly string[]> =
      bucket && !Array.isArray(bucket) ? (bucket as Record<string, readonly string[]>) : {};
    next[table.name] = [];
    schema[table.schema] = next;
    if (!schemaNames.has(table.name)) schema[table.name] = [];
  }
  return schema;
}

function findTable(tables: TableInfo[], parts: string[]): TableInfo | undefined {
  if (parts.length >= 2) {
    const schema = parts[parts.length - 2];
    const name = parts[parts.length - 1];
    return tables.find((t) => t.schema === schema && t.name === name);
  }
  if (parts.length === 1) {
    const matches = tables.filter((t) => t.name === parts[0]);
    return matches.length === 1 ? matches[0] : undefined;
  }
  return undefined;
}

function columnSource(getTables: () => TableInfo[]) {
  return async (ctx: CompletionContext): Promise<CompletionResult | null> => {
    const match = ctx.matchBefore(/[\w."`]+$/);
    if (!match || !match.text.includes(".")) return null;
    const dot = match.text.lastIndexOf(".");
    const qual = match.text.slice(0, dot);
    const prefix = match.text.slice(dot + 1);
    const parts = qual.split(".").map(unquote).filter(Boolean);
    const table = findTable(getTables(), parts);
    if (!table) return null;
    const key = `${table.schema}.${table.name}`;
    let options = columnCache.get(key);
    if (!options) {
      const detail = await api.table(table.schema, table.name);
      options = detail.columns.map((column) => ({
        label: column.name,
        type: "property",
        detail: column.type,
      }));
      columnCache.set(key, options);
    }
    const filtered = prefix
      ? options.filter((option) => option.label.toLowerCase().startsWith(prefix.toLowerCase()))
      : options;
    return { from: match.from + dot + 1, options: filtered, validFor: /^[\w"]*$/ };
  };
}

function editorTheme(dark: boolean): Extension {
  return [
    syntaxHighlighting(dark ? oneDarkHighlightStyle : defaultHighlightStyle),
    EditorView.theme(
      {
        "&": {
          height: "100%",
          fontSize: "12.5px",
          backgroundColor: "transparent",
          color: "var(--foreground)",
        },
        ".cm-scroller": {
          fontFamily: '"JetBrains Mono Variable", ui-monospace, monospace',
          lineHeight: "1.5",
        },
        "&.cm-focused": { outline: "none" },
        ".cm-gutters": {
          backgroundColor: "transparent",
          color: "var(--muted-foreground)",
          border: "none",
        },
        ".cm-activeLine": { backgroundColor: "color-mix(in oklch, var(--foreground) 5%, transparent)" },
        ".cm-activeLineGutter": { backgroundColor: "transparent" },
        ".cm-content": { padding: "8px 0" },
        ".cm-tooltip.cm-tooltip-autocomplete": {
          backgroundColor: "var(--popover)",
          color: "var(--popover-foreground)",
          border: "1px solid var(--border)",
          borderRadius: "8px",
        },
        ".cm-tooltip-autocomplete ul li[aria-selected]": {
          backgroundColor: "var(--secondary)",
          color: "var(--foreground)",
        },
      },
      { dark },
    ),
  ];
}

function language(ctx: EditorContext, getTables: () => TableInfo[]): Extension {
  return [
    sql({
      dialect: dialectOf(ctx.dialect),
      schema: schemaOf(ctx.tables),
      upperCaseKeywords: true,
    }),
    EditorState.languageData.of(() => [{ autocomplete: columnSource(getTables) }]),
  ];
}

export function createSqlEditor(
  parent: HTMLElement,
  opts: EditorContext & {
    text: string;
    onChange: (text: string) => void;
    onRun: (sql: string) => void;
  },
): EditorHandle {
  const lang = new Compartment();
  const theme = new Compartment();
  let tables = opts.tables;
  let signature = "";

  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: opts.text,
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        history(),
        drawSelection(),
        bracketMatching(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        autocompletion({ activateOnTyping: true }),
        keymap.of([{ key: "Tab", run: acceptCompletion }, ...searchKeymap, ...defaultKeymap, ...historyKeymap]),
        placeholder("SELECT * FROM …    Ctrl+Enter runs the statement, or the selection"),
        EditorView.lineWrapping,
        lang.of(language(opts, () => tables)),
        theme.of(editorTheme(opts.dark)),
        Prec.high(
          keymap.of([
            {
              key: "Mod-Enter",
              run(v) {
                opts.onRun(selectedOrAll(v));
                return true;
              },
            },
          ]),
        ),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) opts.onChange(update.state.doc.toString());
        }),
      ],
    }),
  });

  function selectedOrAll(v: EditorView): string {
    const sel = v.state.selection.main;
    return sel.empty ? v.state.doc.toString() : v.state.sliceDoc(sel.from, sel.to);
  }

  const handle: EditorHandle = {
    setText(text) {
      const current = view.state.doc.toString();
      if (current === text) return;
      view.dispatch({ changes: { from: 0, to: current.length, insert: text } });
    },
    setContext(ctx) {
      tables = ctx.tables;
      const next = `${ctx.dark}|${ctx.dialect}|${ctx.tables.map((t) => `${t.schema}.${t.name}`).join(",")}`;
      if (next === signature) return;
      signature = next;
      view.dispatch({
        effects: [lang.reconfigure(language(ctx, () => tables)), theme.reconfigure(editorTheme(ctx.dark))],
      });
    },
    runnable: () => selectedOrAll(view),
    focus: () => view.focus(),
    destroy: () => view.destroy(),
  };
  handle.setContext(opts);
  return handle;
}
