// Prüft die Übersetzung der Oberfläche (Aufruf in web/: node scripts/i18n-check.mjs).
//
// - jeder Text in t("…")/msg("…")/tFor(…, "…") steht in src/lib/messages/en.ts (das prüft auch tsc über den Schlüsseltyp),
// - en.ts enthält keine Einträge, die niemand mehr benutzt,
// - Platzhalter wie {name} sind im deutschen Schlüssel und in der Übersetzung dieselben (auch in den Pluralformen),
// - sichtbarer Text in JSX (Text zwischen Tags, title/label/placeholder/… als fester String) läuft über t().
//
// Den Quelltext liest der SWC-Parser, den Next.js mitbringt: TypeScript 7 hat keine JavaScript-Schnittstelle zum Parsen mehr.
// Beendet sich mit Code 1, wenn etwas davon nicht stimmt.
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const webDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(path.join(webDir, "package.json"));
const swc = require("next/dist/build/swc");
await swc.loadBindings();
const srcDir = path.join(webDir, "src");

// Wörter, die auf Deutsch und Englisch gleich sind und deshalb ohne t() stehen dürfen.
const neutral = new Set([
  "Server", "Backups", "Plugins", "Mods", "Node", "Nodes", "Proxy", "Backend", "Webhook", "Online", "Offline", "Log", "Docker", "Host",
  "Port", "Token", "Seed", "Hardcore", "PvP", "Nether", "End", "Endpoint", "Bucket", "Region", "Access Key", "Secret", "Public Key",
  "Interactions Endpoint URL", "Link", "Laptop", "Browser", "QR-Code", "Passkeys", "Survival", "Lobby", "Datapacks", "Esc", "Strg K",
  "Voicechat", "Simple Voice Chat", "Minecraft-EULA", "EULA", "Lodestone", "Status", "Info", "MOTD", "RAM", "CPU", "TPS", "Velocity",
  "Discord", "SFTP", "MB", "playit.gg", "home", "admin", "world", "A Velocity Server", "Can't keep up!", "luckperms, essentialsx",
  // Technische Werte (Adressen, Pfade, Befehle, Beispiele) sind in jeder Sprache gleich.
  "http://node.example.org:8480", "mc.example.org", "mc2.example.org", "hetzner-1", "https://panel.example.org",
  "https://s3.eu-central-1.amazonaws.com", "eu-central-1", "lodestone-backups", "*.log", "&gt;", "network_mode: host",
  "LODESTONE_NODE_SLEEP_MODE=on", "/api/public/status/", "exec java -Xms{{MEMORY}}M -Xmx{{MEMORY}}M -jar {{JAR}} nogui",
  "ssh-ed25519 AAAAC3Nza… name@host", "playit.gg/account/agents", "ms", "autoconfig.json", "#!/bin/sh",
]);
const visibleAttrs = new Set(["title", "description", "label", "placeholder", "aria-label", "text", "confirm", "hint", "alt"]);

function walk(dir, files = []) {
  for (const name of fs.readdirSync(dir).sort()) {
    const full = path.join(dir, name);
    if (fs.statSync(full).isDirectory()) walk(full, files);
    else if (/\.tsx?$/.test(name)) files.push(full);
  }
  return files;
}

// Liest eine Datei als SWC-Baum. `line(pos)` rechnet eine Position (1-basierter Byte-Versatz von SWC) in eine Zeile um.
async function parse(file) {
  const text = fs.readFileSync(file, "utf8");
  const bytes = Buffer.from(text, "utf8");
  const ast = await swc.parse(text, { filename: file, isModule: true });
  const tree = typeof ast === "string" ? JSON.parse(ast) : ast;
  const line = (pos) => {
    let count = 1;
    for (let i = 0; i < pos - 1 && i < bytes.length; i++) if (bytes[i] === 10) count++;
    return count;
  };
  return { tree, line };
}

// Besucht jeden Knoten (Objekt mit `type`) des Baums.
function visit(node, fn) {
  if (Array.isArray(node)) {
    for (const child of node) visit(child, fn);
    return;
  }
  if (!node || typeof node !== "object") return;
  if (typeof node.type === "string") fn(node);
  for (const key of Object.keys(node)) {
    if (key === "span") continue;
    const value = node[key];
    if (value && typeof value === "object") visit(value, fn);
  }
}

// Fester Text eines Ausdrucks ("…" oder `…` ohne Platzhalter), sonst undefined.
function literal(node) {
  if (!node) return undefined;
  if (node.type === "StringLiteral") return node.value;
  if (node.type === "TemplateLiteral" && node.expressions.length === 0) return node.quasis[0]?.cooked ?? node.quasis[0]?.raw;
  return undefined;
}

// Objektliteral einer exportierten Konstante als Map lesen (Werte: String oder { one, other }).
async function readObject(file, name) {
  const { tree } = await parse(file);
  const result = new Map();
  visit(tree, (node) => {
    if (node.type !== "VariableDeclarator" || node.id?.value !== name || !node.init) return;
    let init = node.init;
    while (init.type === "TsSatisfiesExpression" || init.type === "TsAsExpression" || init.type === "TsConstAssertion") init = init.expression;
    if (init.type !== "ObjectExpression") return;
    for (const prop of init.properties) {
      if (prop.type !== "KeyValueProperty") continue;
      const key = prop.key.value;
      const text = literal(prop.value);
      if (text !== undefined) result.set(key, text);
      else if (prop.value.type === "ObjectExpression") {
        const forms = {};
        for (const form of prop.value.properties) if (form.type === "KeyValueProperty") forms[form.key.value] = literal(form.value);
        result.set(key, forms);
      }
    }
  });
  return result;
}

const placeholders = (text) => [...new Set([...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort().join(",");

const problems = [];
const en = await readObject(path.join(srcDir, "lib/messages/en.ts"), "en");
const pluralsDe = await readObject(path.join(srcDir, "lib/messages/plurals.ts"), "pluralsDe");
const pluralsEn = await readObject(path.join(srcDir, "lib/messages/plurals.ts"), "pluralsEn");

const used = new Set();
for (const file of walk(srcDir)) {
  const rel = path.relative(srcDir, file).split(path.sep).join("/");
  if (rel.startsWith("lib/messages/")) continue;
  const { tree, line } = await parse(file);
  const where = (node) => `${rel}:${line(node.span.start)}`;
  visit(tree, (node) => {
    if (node.type === "CallExpression" && node.callee?.type === "Identifier" && ["t", "msg", "tFor"].includes(node.callee.value)) {
      const arg = node.arguments[node.callee.value === "tFor" ? 1 : 0]?.expression;
      const text = literal(arg);
      if (text !== undefined) {
        used.add(text);
        if (!en.has(text)) problems.push(`${where(node)}: Übersetzung fehlt: ${JSON.stringify(text)}`);
      }
    }
    if (node.type === "JSXText") {
      const text = node.value.replace(/\s+/g, " ").trim();
      if (/[A-Za-zÄÖÜäöüß]{2,}/.test(text) && !neutral.has(text)) problems.push(`${where(node)}: Text ohne t(): ${JSON.stringify(text)}`);
    }
    if (node.type === "JSXAttribute" && node.value?.type === "StringLiteral") {
      const name = node.name?.value ?? node.name?.name?.value;
      if (!visibleAttrs.has(name)) return;
      const text = node.value.value.trim();
      if (/[A-Za-zÄÖÜäöüß]{2,}/.test(text) && !neutral.has(text)) problems.push(`${where(node)}: ${name} ohne t(): ${JSON.stringify(text)}`);
    }
  });
}

for (const [key, value] of en) {
  if (!used.has(key)) problems.push(`en.ts: wird nicht mehr benutzt: ${JSON.stringify(key)}`);
  if (typeof value === "string" && placeholders(key) !== placeholders(value)) problems.push(`en.ts: Platzhalter passen nicht: ${JSON.stringify(key)} → ${JSON.stringify(value)}`);
}
for (const [key, forms] of pluralsDe) {
  const english = pluralsEn.get(key);
  if (!english) continue; // fehlt der Schlüssel, meldet es tsc
  const expected = placeholders(forms.other);
  for (const form of [forms.one, english.one, english.other]) {
    if (placeholders(form) !== expected) problems.push(`plurals.ts: Platzhalter passen nicht bei ${key}: ${JSON.stringify(form)}`);
  }
}

if (problems.length) {
  console.error(problems.join("\n"));
  console.error(`\n${problems.length} Problem(e) in der Übersetzung.`);
  process.exit(1);
}
console.log(`Übersetzung vollständig: ${en.size} Texte, ${pluralsDe.size} Pluralformen.`);
