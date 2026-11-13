// Keeps the documentation true to the repository: a page, link, source path, make target, metric, decision,
// diagram or schema that no longer matches the code fails here, in the same change that broke it.
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { decisions } from "@/components/docs/decisions-data";
import { components, connections, groups, lifecycle } from "@/components/docs/diagrams/architecture-data";
import { controlTables, deliveryTables, referencedTables, type Column, type Table } from "@/components/docs/diagrams/schema-data";
import { concepts, outboxPath, traceLinks } from "@/components/docs/diagrams/small-data";
import { states, transitions } from "@/components/docs/diagrams/states-data";
import { docLinks, guideLinks } from "@/components/docs/nav";
import { slugify } from "@/components/docs/prose";

const consoleDir = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const repo = resolve(consoleDir, "..");
const guideDir = join(consoleDir, "src/app/docs/(guide)");

function walk(dir: string, match: (file: string) => boolean): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return walk(p, match);
    return match(p) ? [p] : [];
  });
}

const pages = walk(guideDir, (f) => f.endsWith("page.mdx")).map((file) => {
  const route = "/docs" + relative(guideDir, dirname(file)).replace(/^\.?$/, "").replace(/^(?=.)/, "/");
  return { file, route, text: readFileSync(file, "utf8") };
});

const pageText = (route: string) => pages.find((p) => p.route === route)?.text ?? "";

// headingIds are the anchors a page's ## and ### headings get, as prose.tsx computes them.
function headingIds(text: string): string[] {
  return [...text.matchAll(/^#{2,3} (.+)$/gm)].map((m) => slugify(m[1].replace(/`/g, "")));
}

describe("navigation", () => {
  it("lists every guide page exactly once, and every entry has its page", () => {
    const routes = pages.map((p) => p.route).sort();
    expect(routes).toEqual(guideLinks.map((l) => l.href).sort());
    for (const l of docLinks.filter((d) => d.fullPage)) {
      expect(existsSync(join(consoleDir, "src/app", l.href, "page.tsx")), l.href).toBe(true);
    }
  });

  it("titles every page as its navigation entry does", () => {
    for (const link of guideLinks) {
      const text = pageText(link.href);
      expect(text, link.href).toContain(`title: "${link.title}"`);
      expect(text, link.href).toMatch(new RegExp(`^# ${link.title}$`, "m"));
    }
  });
});

describe("links", () => {
  it("points every internal link at a page, and every anchor at a heading", () => {
    const known = new Set(docLinks.map((l) => l.href));
    for (const page of pages) {
      for (const m of page.text.matchAll(/["(](\/docs[^"()#\s]*)(?:#([a-z0-9-]+))?/g)) {
        const [, path, anchor] = m;
        expect(known.has(path), `${page.route} links ${path}`).toBe(true);
        if (anchor) expect(headingIds(pageText(path)), `${page.route} links ${path}#${anchor}`).toContain(anchor);
      }
    }
  });

  it("names only repository paths that exist, folders with a trailing slash", () => {
    const paths: string[] = [];
    for (const page of pages) {
      for (const m of page.text.matchAll(/<Source path="([^"]+)"/g)) paths.push(m[1]);
      for (const m of page.text.matchAll(/<Sources paths=\{\[([^\]]*)\]\}/g)) paths.push(...[...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]));
      for (const m of page.text.matchAll(/github\.com\/bete7512\/hookbeam\/(?:blob|tree)\/main\/([^\s)"]+)/g)) paths.push(m[1]);
    }
    paths.push(...components.flatMap((c) => c.code ?? []), ...lifecycle.flatMap((s) => s.code));
    expect(paths.length).toBeGreaterThan(0);
    for (const p of paths) {
      const full = join(repo, p);
      expect(existsSync(full), `${p} does not exist`).toBe(true);
      expect(statSync(full).isDirectory(), `${p}: folders end with "/", files do not`).toBe(p.endsWith("/"));
    }
  });

  it("shows only console files that exist in a CodeFile", () => {
    for (const page of pages) {
      for (const m of page.text.matchAll(/<CodeFile path="([^"]+)"/g)) {
        expect(existsSync(join(consoleDir, m[1])), `${page.route}: ${m[1]}`).toBe(true);
      }
    }
  });

  it("runs only make targets the Makefile has", () => {
    const makefile = readFileSync(join(repo, "Makefile"), "utf8");
    const targets = new Set([...makefile.matchAll(/^([a-z][a-z0-9-]*):/gm)].map((m) => m[1]));
    for (const page of pages) {
      // Only code mentions a target: fenced blocks and inline code, never prose such as "make the".
      const code = [...page.text.matchAll(/```[\s\S]*?```|`[^`\n]+`/g)].map((m) => m[0]).join("\n");
      for (const m of code.matchAll(/\bmake ([a-z][a-z0-9-]*)/g)) {
        expect(targets.has(m[1]), `${page.route}: make ${m[1]}`).toBe(true);
      }
    }
  });

  it("names only metrics the code records", () => {
    const sources = ["pkg/telemetry", "api/services", "delivery/services"].flatMap((d) => walk(join(repo, d), (f) => f.endsWith(".go"))).map((f) => readFileSync(f, "utf8"));
    for (const page of pages) {
      for (const m of page.text.matchAll(/`(hookbeam\.[a-z_]+(?:\.[a-z_]+)+)`/g)) {
        expect(
          sources.some((s) => s.includes(`"${m[1]}"`)),
          `${page.route}: ${m[1]}`,
        ).toBe(true);
      }
    }
  });
});

describe("decisions", () => {
  it("lists every record in docs/decisions with its own title", () => {
    const dir = join(repo, "docs/decisions");
    const files = readdirSync(dir).filter((f) => /^\d{4}-.*\.md$/.test(f)).sort();
    expect(decisions.map((d) => d.file)).toEqual(files);
    for (const d of decisions) {
      const heading = readFileSync(join(dir, d.file), "utf8").split("\n")[0];
      expect(heading, d.file).toBe(`# ${d.number}. ${d.title}`);
    }
  });
});

describe("diagrams", () => {
  const unique = (ids: string[]) => expect(new Set(ids).size, ids.join(",")).toBe(ids.length);

  it("connect only parts that exist", () => {
    const parts = new Set(components.map((c) => c.id));
    unique(components.map((c) => c.id));
    unique(connections.map((c) => c.id));
    for (const c of connections) expect(parts.has(c.source) && parts.has(c.target), c.id).toBe(true);
    for (const c of components) if (c.group) expect(groups.some((g) => g.id === c.group), c.id).toBe(true);

    const edges = new Set(connections.map((c) => c.id));
    for (const s of lifecycle) {
      for (const n of s.nodes) expect(parts.has(n), `${s.title}: ${n}`).toBe(true);
      for (const e of s.edges) expect(edges.has(e), `${s.title}: ${e}`).toBe(true);
    }

    const stateIds = new Set(states.map((s) => s.id));
    unique(transitions.map((t) => t.id));
    for (const t of transitions) expect(stateIds.has(t.source) && stateIds.has(t.target), t.id).toBe(true);

    for (const d of [concepts, traceLinks, outboxPath]) {
      const cards = new Set(d.cards.map((c) => c.id));
      unique(d.cards.map((c) => c.id));
      for (const l of d.links) expect(cards.has(l.source) && cards.has(l.target), l.id).toBe(true);
    }
  });

  it("names every delivery state the database allows", () => {
    const baseline = readFileSync(join(repo, "delivery/migrations/0001_baseline.go"), "utf8");
    const allowed = /state\s+TEXT\s+NOT NULL CHECK \(state IN \(([^)]*)\)\)/.exec(baseline)?.[1] ?? "";
    const dbStates = [...allowed.matchAll(/'([a-z_]+)'/g)].map((m) => m[1]).sort();
    expect(states.filter((s) => s.id !== "start").map((s) => s.id).sort()).toEqual(dbStates);
  });
});

// The schema as the migrations leave it: up functions in file order, CREATE TABLE and ALTER TABLE applied.
type Schema = Map<string, Map<string, Column>>;

function splitTopLevel(body: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let cur = "";
  for (const ch of body) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) {
      parts.push(cur.trim());
      cur = "";
      continue;
    }
    cur += ch;
  }
  if (cur.trim()) parts.push(cur.trim());
  return parts;
}

function normalizeType(t: string): string {
  const lower = t.toLowerCase();
  return lower === "integer" ? "int" : lower === "boolean" ? "bool" : lower;
}

function parseColumn(def: string): Column | null {
  const m = /^(\w+)\s+([A-Za-z]+(?:\(\d+\))?(?:\[\])?)(.*)$/s.exec(def);
  if (!m) return null;
  const [, name, type, rest] = m;
  const column: Column = { name, type: normalizeType(type) };
  const pk = /PRIMARY KEY/.test(rest);
  if (pk) column.pk = true;
  if (!pk && !/NOT NULL/.test(rest)) column.nullable = true;
  const ref = /REFERENCES (\w+) \((\w+)\)/.exec(rest);
  if (ref) column.fk = `${ref[1]}.${ref[2]}`;
  return column;
}

function readSchema(plane: "api" | "delivery"): { tables: Schema; indexes: Set<string> } {
  const dir = join(repo, plane, "migrations");
  const files = readdirSync(dir)
    .filter((f) => /^\d{4}_.*\.go$/.test(f))
    .sort();
  const tables: Schema = new Map();
  const indexes = new Set<string>();
  for (const f of files) {
    const src = readFileSync(join(dir, f), "utf8");
    const up = /func \(s \*service\) up_\w+\([^)]*\) error \{([\s\S]*?)\n\}\n/.exec(src);
    if (!up) throw new Error(`${f}: no up function`);
    const sql = [...up[1].matchAll(/`([^`]*)`/g)].map((m) => m[1].replace(/--.*$/gm, "")).join(";\n");
    for (const raw of sql.split(";")) {
      const stmt = raw.trim();
      const create = /^CREATE TABLE (\w+) \(([\s\S]*)\)$/.exec(stmt);
      if (create) {
        const columns = new Map<string, Column>();
        for (const item of splitTopLevel(create[2])) {
          const pk = /^PRIMARY KEY \(([^)]*)\)/.exec(item);
          if (pk) {
            for (const name of pk[1].split(",").map((s) => s.trim())) {
              const c = columns.get(name);
              if (c) {
                c.pk = true;
                delete c.nullable;
              }
            }
            continue;
          }
          if (/^(UNIQUE|CONSTRAINT|CHECK|FOREIGN KEY)\b/.test(item)) continue;
          const c = parseColumn(item);
          if (c) columns.set(c.name, c);
        }
        tables.set(create[1], columns);
        continue;
      }
      const add = /^ALTER TABLE (\w+) ADD COLUMN ([\s\S]+)$/.exec(stmt);
      if (add) {
        const c = parseColumn(add[2]);
        if (c) tables.get(add[1])?.set(c.name, c);
        continue;
      }
      const notNull = /^ALTER TABLE (\w+) ALTER COLUMN (\w+) SET NOT NULL$/.exec(stmt);
      if (notNull) {
        const c = tables.get(notNull[1])?.get(notNull[2]);
        if (c) delete c.nullable;
        continue;
      }
      const drop = /^ALTER TABLE (\w+) DROP COLUMN (\w+)/.exec(stmt);
      if (drop) {
        tables.get(drop[1])?.delete(drop[2]);
        continue;
      }
      const constraint = /^ALTER TABLE \w+ ADD CONSTRAINT (\w+)/.exec(stmt);
      if (constraint) indexes.add(constraint[1]);
      for (const m of stmt.matchAll(/CREATE (?:UNIQUE )?INDEX (?:CONCURRENTLY )?(?:IF NOT EXISTS )?(\w+)/g)) indexes.add(m[1]);
    }
  }
  return { tables, indexes };
}

function comparable(c: Column) {
  return { name: c.name, type: c.type, nullable: Boolean(c.nullable), pk: Boolean(c.pk), fk: c.fk ?? null };
}

function sameSchema(drawn: Table[], plane: "api" | "delivery") {
  const { tables, indexes } = readSchema(plane);
  expect(drawn.map((t) => t.name).sort(), `${plane}: tables`).toEqual([...tables.keys()].sort());
  for (const t of drawn) {
    const real = tables.get(t.name);
    if (!real) continue;
    const want = [...real.values()].map(comparable).sort((a, b) => a.name.localeCompare(b.name));
    const got = t.columns.map(comparable).sort((a, b) => a.name.localeCompare(b.name));
    expect(got, `${plane}.${t.name}: columns (update src/components/docs/diagrams/schema-data.ts)`).toEqual(want);
    for (const i of t.indexes ?? []) expect(indexes.has(i.name), `${plane}.${t.name}: index ${i.name}`).toBe(true);
  }
}

describe("data model", () => {
  it("draws the data plane exactly as its migrations build it", () => sameSchema(deliveryTables, "delivery"));
  it("draws the control plane exactly as its migrations build it", () => sameSchema(controlTables, "api"));

  it("refers only to tables it lists as referenced", () => {
    for (const t of controlTables) {
      for (const c of t.columns) {
        if (!c.ref) continue;
        const [owner, table] = c.ref.split(".");
        const ref = referencedTables.find((r) => r.owner === owner);
        expect(ref?.tables, `${t.name}.${c.name}`).toContain(table);
        if (owner === "delivery") expect(deliveryTables.some((d) => d.name === table), c.ref).toBe(true);
      }
    }
  });
});
