// Prüft die Übersetzung der Oberfläche (Aufruf in web/: node scripts/i18n-check.mjs).
//
// - jeder Text in t("…")/msg("…")/tFor(…, "…") steht in src/lib/messages/en.ts (das prüft auch tsc über den Schlüsseltyp),
// - en.ts enthält keine Einträge, die niemand mehr benutzt,
// - Platzhalter wie {name} sind im deutschen Schlüssel und in der Übersetzung dieselben (auch in den Pluralformen),
// - sichtbarer Text in JSX (Text zwischen Tags, title/label/placeholder/… als fester String) läuft über t().
//
// Beendet sich mit Code 1, wenn etwas davon nicht stimmt.
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const webDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(path.join(webDir, "package.json"));
const ts = require("typescript");
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
  "ssh-ed25519 AAAAC3Nza… name@host",
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

function parse(file) {
  const text = fs.readFileSync(file, "utf8");
  return ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
}

// Objektliteral einer exportierten Konstante als Map lesen (Werte: String oder { one, other }).
function readObject(file, name) {
  const sf = parse(file);
  const result = new Map();
  const visit = (node) => {
    if (ts.isVariableDeclaration(node) && node.name.getText(sf) === name && node.initializer) {
      let init = node.initializer;
      while (ts.isSatisfiesExpression?.(init) || ts.isAsExpression(init)) init = init.expression;
      for (const prop of init.properties) {
        const key = prop.name.text ?? prop.name.getText(sf);
        const value = prop.initializer;
        if (ts.isStringLiteral(value) || ts.isNoSubstitutionTemplateLiteral(value)) result.set(key, value.text);
        else if (ts.isObjectLiteralExpression(value)) {
          const forms = {};
          for (const form of value.properties) forms[form.name.getText(sf)] = form.initializer.text;
          result.set(key, forms);
        }
      }
    }
    node.forEachChild(visit);
  };
  visit(sf);
  return result;
}

const placeholders = (text) => [...new Set([...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort().join(",");

const problems = [];
const en = readObject(path.join(srcDir, "lib/messages/en.ts"), "en");
const pluralsDe = readObject(path.join(srcDir, "lib/messages/plurals.ts"), "pluralsDe");
const pluralsEn = readObject(path.join(srcDir, "lib/messages/plurals.ts"), "pluralsEn");

const used = new Set();
for (const file of walk(srcDir)) {
  const rel = path.relative(srcDir, file).split(path.sep).join("/");
  if (rel.startsWith("lib/messages/")) continue;
  const sf = parse(file);
  const where = (node) => `${rel}:${sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1}`;
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && ["t", "msg", "tFor"].includes(node.expression.text)) {
      const arg = node.arguments[node.expression.text === "tFor" ? 1 : 0];
      if (arg && (ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg))) {
        used.add(arg.text);
        if (!en.has(arg.text)) problems.push(`${where(node)}: Übersetzung fehlt: ${JSON.stringify(arg.text)}`);
      }
    }
    if (ts.isJsxText(node)) {
      const text = node.text.replace(/\s+/g, " ").trim();
      if (/[A-Za-zÄÖÜäöüß]{2,}/.test(text) && !neutral.has(text)) problems.push(`${where(node)}: Text ohne t(): ${JSON.stringify(text)}`);
    }
    if (ts.isJsxAttribute(node) && node.initializer && ts.isStringLiteral(node.initializer) && visibleAttrs.has(node.name.getText(sf))) {
      const text = node.initializer.text.trim();
      if (/[A-Za-zÄÖÜäöüß]{2,}/.test(text) && !neutral.has(text)) problems.push(`${where(node)}: ${node.name.getText(sf)} ohne t(): ${JSON.stringify(text)}`);
    }
    node.forEachChild(visit);
  };
  visit(sf);
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
