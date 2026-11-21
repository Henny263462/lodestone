// Demo: liefert die gebaute Oberfläche (web/out) mit einem kleinen Schein-Backend voller Beispieldaten aus.
// Es gibt keine echten Server; Aktionen verändern nur den Zustand im Speicher. Start: npm run demo
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { WebSocketServer } from "ws";

const root = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "out");
const port = Number(process.env.PORT ?? 3100);
const now = () => Date.now();
const MIN = 60_000;
const HOUR = 60 * MIN;

if (!fs.existsSync(path.join(root, "index.html"))) {
  console.error("Die Oberfläche ist noch nicht gebaut. Erst: npm run build");
  process.exit(1);
}

// ---------------------------------------------------------------- Beispieldaten

const user = { id: "u1", username: "admin", isAdmin: true, disabled: false, createdAt: now() - 40 * 24 * HOUR };
const users = [
  user,
  { id: "u2", username: "anna", isAdmin: false, disabled: false, createdAt: now() - 12 * 24 * HOUR },
  { id: "u3", username: "ben", isAdmin: false, disabled: false, createdAt: now() - 3 * 24 * HOUR },
];
const nodes = [
  {
    id: "n1", name: "home", url: "http://node:8480", publicHost: "mc.example.org", online: true, version: "0.1.0", serverCount: 6,
    system: { dockerVersion: "29.6.2", os: "Ubuntu 24.04.4 LTS", architecture: "x86_64", cpus: 8, memoryBytes: 16 * 1024 ** 3 },
    memoryAvailable: 5.2 * 1024 ** 3, memoryCommitted: 9.5 * 1024 ** 3,
  },
  { id: "n2", name: "hetzner-1", url: "https://node.example.com:8480", publicHost: "mc2.example.org", online: false, serverCount: 0, error: "der Node ist nicht erreichbar (hetzner-1): connection refused" },
];

let seq = 100;
const id = () => (seq++).toString(16).padStart(12, "0");

function ports(software, extra = []) {
  return [...extra];
}

function makeServer(projectId, name, software, version, memoryMB, state, extra = {}) {
  const sid = extra.id ?? id();
  const role = extra.role ?? "standalone";
  const running = state === "running";
  const stats = running ? { cpuPercent: 8 + Math.random() * 40, memoryBytes: memoryMB * 1024 ** 2 * (0.55 + Math.random() * 0.3), memoryLimit: memoryMB * 1024 ** 2, netRx: 4_000_000, netTx: 9_000_000 } : undefined;
  const hostPort = 28000 + (seq % 90);
  const server = {
    id: sid, projectId, nodeId: "n1", name, slug: name.toLowerCase().replace(/[^a-z0-9]+/g, "-"), role, software,
    inTry: extra.inTry ?? false, tryPosition: extra.tryPosition ?? 0, forcedHost: extra.forcedHost ?? "", voicechat: extra.voicechat ?? false,
    groupId: extra.groupId, nodeName: "home", publicHost: "mc.example.org", online: true,
    address: role === "backend" ? undefined : `mc.example.org:${hostPort}`, voiceAddress: extra.voiceAddress,
    view: {
      id: sid, name, software, version, memoryMB, cpu: 0, eula: true, backupKeep: 5, installed: true, state,
      ports: role === "backend" ? [] : ports(software, [{ container: 25565, host: hostPort, protocol: "tcp" }]),
      stats, startedAt: running ? now() - (extra.uptimeH ?? 5) * HOUR : undefined,
    },
  };
  return server;
}

const proxyId = id();
const survivalId = id();
const lobby1Id = id();
const lobby2Id = id();
const minigamesGroupId = id();
const lobbyGroupId = id();

const projects = [
  {
    id: "p1", name: "Survival-Netz", kind: "network", description: "Hauptnetzwerk mit Lobby, Survival und Creative", ownerId: "u1", createdAt: now() - 30 * 24 * HOUR,
    myRole: "owner", voicechat: true,
    members: [
      { projectId: "p1", userId: "u1", username: "admin", role: "owner" },
      { projectId: "p1", userId: "u2", username: "anna", role: "admin" },
      { projectId: "p1", userId: "u3", username: "ben", role: "viewer" },
    ],
    servers: [
      makeServer("p1", "Proxy", "velocity", "3.5.1", 512, "running", { id: proxyId, role: "proxy", voicechat: true, voiceAddress: "mc.example.org:28000", uptimeH: 71 }),
      makeServer("p1", "Lobby-1", "paper", "1.21.4", 1024, "running", { id: lobby1Id, role: "backend", inTry: true, tryPosition: 0, groupId: lobbyGroupId, voicechat: true, uptimeH: 26 }),
      makeServer("p1", "Lobby-2", "paper", "1.21.4", 1024, "running", { id: lobby2Id, role: "backend", inTry: true, tryPosition: 1, groupId: lobbyGroupId, voicechat: true, uptimeH: 2 }),
      makeServer("p1", "Survival", "paper", "1.21.4", 4096, "running", { id: survivalId, role: "backend", inTry: false, tryPosition: 2, forcedHost: "survival.example.org", voicechat: true, uptimeH: 48 }),
      makeServer("p1", "Creative", "fabric", "1.21.4", 2048, "offline", { role: "backend", inTry: false, tryPosition: 3, voicechat: true }),
    ],
    groups: [
      {
        id: lobbyGroupId, projectId: "p1", name: "Lobby", slug: "lobby", software: "paper", version: "1.21.4", memoryMB: 1024, minInstances: 1, maxInstances: 4, playersPerInstance: 20,
        idleMinutes: 5, inTry: true, dynamic: true, plugins: ["luckperms", "essentialsx"], instances: [lobby1Id, lobby2Id], active: 2, players: 27,
      },
      {
        id: minigamesGroupId, projectId: "p1", name: "Minigames", slug: "minigames", software: "purpur", version: "1.21.4", memoryMB: 2048, minInstances: 0, maxInstances: 6, playersPerInstance: 12,
        idleMinutes: 10, inTry: false, dynamic: true, plugins: [], instances: [], active: 0, players: 0,
        note: "Zu wenig Arbeitsspeicher auf dem Node (13926 von 14212 MB für Server vergeben)",
      },
    ],
    problems: [],
  },
  {
    id: "p2", name: "Test-Server", kind: "server", description: "Zum Ausprobieren von Plugins", ownerId: "u1", createdAt: now() - 6 * 24 * HOUR,
    myRole: "owner", voicechat: false,
    members: [{ projectId: "p2", userId: "u1", username: "admin", role: "owner" }],
    servers: [makeServer("p2", "Test", "paper", "1.21.4", 2048, "running", { uptimeH: 9 })],
    groups: [],
  },
];

projects[0].settings = { statusPublic: true, statusSlug: "survival-netz", motd: "<gradient:#09add3:#22c55e>Survival-Netz</gradient>\n<gray>Neu: Voicechat!" };
projects[1].settings = { statusPublic: false, statusSlug: "", motd: "" };
projects[0].servers.forEach((server) => { server.autoRestart = server.role !== "proxy"; });
projects[1].servers.forEach((server) => { server.autoRestart = false; });
projects[0].servers[4].modpack = { projectId: "P7dR8mSH", slug: "fabulously-optimized", title: "Fabulously Optimized", versionId: "v9", versionNumber: "10.0.0", minecraft: "1.21.4" };
projects[0].servers[3].publicAddress = "survival.playit.gg";

const findServer = (sid) => {
  for (const project of projects) {
    const server = project.servers.find((entry) => entry.id === sid);
    if (server) return { project, server };
  }
  return null;
};

function refreshGroups(project) {
  for (const group of project.groups) {
    const members = project.servers.filter((server) => server.groupId === group.id);
    group.instances = members.map((server) => server.id);
    group.active = members.filter((server) => server.view?.state === "running" || server.view?.state === "starting").length;
  }
}

// ---------------------------------------------------------------- Messreihen

const ranges = { "1h": HOUR, "6h": 6 * HOUR, "24h": 24 * HOUR, "7d": 7 * 24 * HOUR };

function hash(text) {
  let h = 2166136261;
  for (const ch of text) h = Math.imul(h ^ ch.charCodeAt(0), 16777619);
  return (h >>> 0) / 2 ** 32;
}

/** Tageskurve mit Abendspitze und Rauschen, je Server leicht anders. */
function players(serverId, time, scale) {
  const hour = new Date(time).getHours() + new Date(time).getMinutes() / 60;
  const curve = 0.25 + 0.75 * Math.exp(-((hour - 20.5) ** 2) / 14) + 0.25 * Math.exp(-((hour - 14) ** 2) / 20);
  const noise = 0.85 + 0.3 * hash(serverId + Math.floor(time / (5 * MIN)));
  return Math.max(0, Math.round(scale * curve * noise));
}

function series(rangeName, serverIds, scale = 14, memoryMB = 2048, startedHoursAgo = 30) {
  const duration = ranges[rangeName] ?? ranges["6h"];
  const count = 120;
  const start = now() - duration;
  const width = duration / count;
  const times = [];
  const cpu = [];
  const memory = [];
  const playerSeries = [];
  let peakPlayers = 0, sumPlayers = 0, peakCpu = 0, sumCpu = 0, peakMemory = 0, samples = 0;
  for (let i = 0; i < count; i++) {
    const t = start + i * width + width / 2;
    times.push(t);
    if (t < now() - startedHoursAgo * HOUR) {
      cpu.push(null); memory.push(null); playerSeries.push(null);
      continue;
    }
    const p = players(serverIds[0], t, scale);
    const c = Math.min(95, 6 + p * 2.2 + 8 * hash(serverIds[0] + "c" + i));
    const age = (t - (now() - startedHoursAgo * HOUR)) / HOUR;
    const m = memoryMB * 1024 ** 2 * Math.min(0.9, 0.38 + age * 0.012 + 0.02 * hash(serverIds[0] + "m" + i));
    cpu.push(c); memory.push(m); playerSeries.push(p);
    peakPlayers = Math.max(peakPlayers, p); sumPlayers += p; peakCpu = Math.max(peakCpu, c); sumCpu += c; peakMemory = Math.max(peakMemory, m); samples++;
  }
  return { range: rangeName, times, cpu, memory, players: playerSeries, peakPlayers, avgPlayers: samples ? sumPlayers / samples : 0, avgCpu: samples ? sumCpu / samples : 0, peakCpu, peakMemory, samples };
}

const names = ["Alex", "Steve", "Notch", "Herobrine", "Dream", "Technoblade", "Kiwi", "Luna", "Mika", "Finn", "Jonas", "Lea", "Tom", "Nora", "Ole", "Sam"];

// ---------------------------------------------------------------- Dateien

const files = {
  "": [
    { name: "plugins", dir: true, size: 0, modified: now() - 2 * HOUR, mode: "drwxr-xr-x" },
    { name: "world", dir: true, size: 0, modified: now() - 5 * MIN, mode: "drwxr-xr-x" },
    { name: "config", dir: true, size: 0, modified: now() - 26 * HOUR, mode: "drwxr-xr-x" },
    { name: "logs", dir: true, size: 0, modified: now() - 1 * MIN, mode: "drwxr-xr-x" },
    { name: "server.properties", dir: false, size: 1280, modified: now() - 3 * 24 * HOUR, mode: "-rw-r--r--" },
    { name: "bukkit.yml", dir: false, size: 980, modified: now() - 3 * 24 * HOUR, mode: "-rw-r--r--" },
    { name: "eula.txt", dir: false, size: 12, modified: now() - 3 * 24 * HOUR, mode: "-rw-r--r--" },
    { name: "server.jar", dir: false, size: 52_000_000, modified: now() - 3 * 24 * HOUR, mode: "-rw-r--r--" },
  ],
  plugins: [
    { name: "LuckPerms-Bukkit-5.4.150.jar", dir: false, size: 1_600_000, modified: now() - 3 * 24 * HOUR, mode: "-rw-r--r--" },
    { name: "voicechat-bukkit-2.6.27.jar", dir: false, size: 2_100_000, modified: now() - 2 * 24 * HOUR, mode: "-rw-r--r--" },
  ],
};
const fileTexts = {
  "server.properties": "#Minecraft server properties\nmotd=Willkommen im Survival-Netz\nonline-mode=false\nserver-port=25565\nmax-players=20\nview-distance=10\ndifficulty=normal\nenable-rcon=false\n",
  "bukkit.yml": "settings:\n  allow-end: true\n  warn-on-overload: true\n  permissions-file: permissions.yml\nspawn-limits:\n  monsters: 70\n  animals: 10\n",
  "eula.txt": "eula=true\n",
  "velocity.toml": "bind = '0.0.0.0:25565'\nmotd = '<#09add3>Survival-Netz'\nplayer-info-forwarding-mode = 'modern'\n\n[servers]\nlobby-1 = 'lodestone-000000000066:25565'\nlobby-2 = 'lodestone-000000000067:25565'\nsurvival = 'lodestone-000000000068:25565'\ntry = ['lobby-1', 'lobby-2']\n\n[forced-hosts]\n'survival.example.org' = ['survival']\n",
};

// ---------------------------------------------------------------- Anfragen

function send(res, status, body) {
  const text = body === undefined ? "" : JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" });
  res.end(text);
}

function readBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => {
      const text = Buffer.concat(chunks).toString();
      try {
        resolve(text ? JSON.parse(text) : {});
      } catch {
        resolve({});
      }
    });
  });
}


// ---------------------------------------------------------------- Neue Bereiche (Zeitpläne, Ereignisse, Protokoll ...)

let schedules = [
  { id: "sc1", projectId: "p1", serverId: survivalId, serverName: "Survival", name: "Nächtlicher Neustart", steps: [{ action: "backup" }, { action: "restart" }], kind: "daily", everyMinutes: 0, atMinute: 4 * 60, weekdays: 127, warnMinutes: 5, enabled: true, lastRun: now() - 8 * HOUR, lastStatus: "ok", lastMessage: "2 Schritte ausgeführt", nextRun: now() + 16 * HOUR },
  { id: "sc2", projectId: "p1", serverId: lobby1Id, serverName: "Lobby-1", name: "Hinweis zur vollen Stunde", steps: [{ action: "command", value: "say Vergiss nicht, abzustimmen!" }], kind: "interval", everyMinutes: 60, atMinute: 0, weekdays: 127, warnMinutes: 0, enabled: true, lastRun: now() - 25 * MIN, lastStatus: "ok", lastMessage: "1 Schritt ausgeführt", nextRun: now() + 35 * MIN },
  { id: "sc3", projectId: "p1", serverId: survivalId, serverName: "Survival", name: "Wochenend-Backup", steps: [{ action: "backup" }], kind: "weekly", everyMinutes: 0, atMinute: 3 * 60, weekdays: 0b1100000, warnMinutes: 0, enabled: false, lastRun: 0, lastStatus: "", lastMessage: "", nextRun: 0 },
];
const events = [
  { id: 9, time: now() - 6 * MIN, serverId: survivalId, kind: "backup", level: "info", message: "Backup von „Survival“ ist fertig (208 MB)" },
  { id: 8, time: now() - 2 * HOUR, serverId: lobby2Id, kind: "start", level: "info", message: "„Lobby-2“ wurde gestartet" },
  { id: 7, time: now() - 5 * HOUR, serverId: survivalId, kind: "crash", level: "error", message: "„Survival“ ist abgestürzt (Exit-Code 137). Auto-Restart startet den Server neu." },
  { id: 6, time: now() - 5 * HOUR + 40_000, serverId: survivalId, kind: "restart", level: "warn", message: "„Survival“ wird automatisch neu gestartet (Versuch 1 von 3)" },
  { id: 5, time: now() - 9 * HOUR, kind: "lowmemory", level: "warn", message: "Auf dem Node „home“ sind nur noch 1380 MB Arbeitsspeicher frei." },
  { id: 4, time: now() - 26 * HOUR, kind: "schedule", level: "info", message: "Zeitplan „Nächtlicher Neustart“ ist durchgelaufen" },
];
let webhooks = [
  { id: "w1", name: "Discord #server", url: "https://discord.com/api/webhooks/…/•••", kind: "discord", events: ["crash", "restart", "lowmemory"], enabled: true, lastStatus: "ok" },
];
const audit = [
  ["POST /servers/{id}/power", "Survival", 200, 2], ["PUT /projects/{id}/motd", "Survival-Netz", 200, 40], ["POST /projects/{id}/schedules", "Survival-Netz", 201, 130],
  ["POST /me/keys", "", 201, 300], ["POST /projects/{id}/servers", "Survival-Netz", 201, 700], ["PATCH /users/{id}", "anna", 200, 1500], ["POST /auth/login", "", 200, 2200],
].map(([action, target, status, minutes], index) => ({ id: 100 - index, time: now() - minutes * MIN, userId: "u1", username: index % 3 === 2 ? "anna" : "admin", action, target, status, ip: index % 2 ? "192.168.1.20" : "203.0.113.7" }));
let keys = [{ id: "k1", name: "Skript Backup", prefix: "lds_8fK3", createdAt: now() - 6 * 24 * HOUR, lastUsed: now() - 3 * HOUR, expiresAt: 0 }];
const sessions = [
  { id: "s1", createdAt: now() - 2 * HOUR, expiresAt: now() + 29 * 24 * HOUR, userAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/130", ip: "192.168.1.20", current: true },
  { id: "s2", createdAt: now() - 4 * 24 * HOUR, expiresAt: now() + 26 * 24 * HOUR, userAgent: "Mozilla/5.0 (iPhone) Safari/604", ip: "203.0.113.7", current: false },
];
let totpEnabled = false;
const me = () => ({ ...user, totpEnabled, recoveryCodes: totpEnabled ? 8 : 0 });
const playerLists = {
  whitelist: [{ uuid: "1", name: "Alex" }, { uuid: "2", name: "Luna" }, { uuid: "3", name: "Mika" }],
  ops: [{ uuid: "1", name: "Alex", level: 4 }],
  banned: [{ uuid: "9", name: "Griefer99", reason: "Griefing", created: "2026-10-20" }],
  bannedIps: [{ ip: "198.51.100.23", reason: "Spam" }],
};
let playit = { configured: false, running: false, image: "ghcr.io/playit-cloud/playit-agent:0.15", logs: [] };

const synced = { applied: ["Proxy", "Lobby-1", "Lobby-2", "Survival"], pending: [], failed: [] };

async function api(req, res, url) {
  const parts = url.pathname.replace(/^\/api\//, "").split("/").filter(Boolean);
  const method = req.method;
  const body = ["POST", "PUT", "PATCH"].includes(method) ? await readBody(req) : {};
  if (weltenDemo(res, parts, method, body)) return;
  const [a, b, c, d] = parts;
  if (rechteApi(res, method, body, a, b, c, d)) return;
  if (sftpDemo(res, method, body, a, b, c, d)) return;
  if (backupsDemo(res, parts, method, body, url)) return;
  if (anmeldungDemo(res, method, body, parts)) return;
  if (serveropsApi(res, parts, method, body)) return;
  const automatik = automatikDemo(method, parts, body);
  if (automatik) return send(res, automatik.status, automatik.body);
  if (inhalteApi(res, url, method, body, parts)) return;

  if (a === "auth") {
    if (b === "status") return send(res, 200, { setupRequired: false, version: "0.1.0-demo", user, timezone: "Europe/Berlin" });
    if (b === "totp") return send(res, 200, user);
    if (b === "logout") return send(res, 204);
    return send(res, 200, user);
  }
  if (a === "me") {
    if (!b) return send(res, 200, me());
    if (b === "password") return send(res, 204);
    if (b === "keys") {
      if (method === "POST") {
        const key = { id: id(), name: body.name || "Schlüssel", prefix: "lds_Demo", createdAt: now(), lastUsed: 0, expiresAt: body.days ? now() + body.days * 24 * HOUR : 0 };
        keys.push(key);
        return send(res, 201, { ...key, key: "lds_DemoSchluesselNurImDemoGueltig0123456789" });
      }
      if (c) { keys = keys.filter((key) => key.id !== c); return send(res, 204); }
      return send(res, 200, keys);
    }
    if (b === "sessions") return send(res, c ? 204 : 200, c ? undefined : sessions);
    if (b === "totp") {
      if (c === "setup") return send(res, 200, { secret: "JBSWY3DPEHPK3PXP", uri: "otpauth://totp/Lodestone:admin?secret=JBSWY3DPEHPK3PXP&issuer=Lodestone" });
      if (c === "enable") { totpEnabled = true; return send(res, 200, { recoveryCodes: ["a1b2-c3d4", "e5f6-a7b8", "c9d0-e1f2", "a3b4-c5d6", "e7f8-a9b0", "c1d2-e3f4", "a5b6-c7d8", "e9f0-a1b2"] }); }
      if (c === "disable") { totpEnabled = false; return send(res, 204); }
    }
    return send(res, 204);
  }
  if (a === "audit") {
    const who = url.searchParams.get("user");
    return send(res, 200, audit.filter((entry) => !who || entry.username.includes(who)));
  }
  if (a === "public" && b === "status") {
    if (c !== "survival-netz") return send(res, 404, { error: "nicht gefunden" });
    return send(res, 200, { name: "Survival-Netz", description: "Hauptnetzwerk mit Lobby, Survival und Creative", online: true, address: "mc.example.org:28000", players: players("proxy", now(), 38), maxPlayers: 500, version: "Velocity 3.5.1", servers: [{ name: "Lobby", online: true, players: 12, maxPlayers: 40 }, { name: "Survival", online: true, players: 9, maxPlayers: 20 }, { name: "Creative", online: false, players: 0 }], updated: now() });
  }
  if (a === "schedules") {
    const schedule = schedules.find((entry) => entry.id === b);
    if (!schedule) return send(res, 404, { error: "nicht gefunden" });
    if (method === "DELETE") { schedules = schedules.filter((entry) => entry !== schedule); return send(res, 204); }
    if (c === "run") { schedule.lastRun = now(); schedule.lastStatus = "ok"; schedule.lastMessage = "von Hand ausgeführt"; return send(res, 200, schedule); }
    Object.assign(schedule, body);
    return send(res, 200, schedule);
  }
  if (a === "webhooks") {
    const hook = webhooks.find((entry) => entry.id === b);
    if (!hook) return send(res, 404, { error: "nicht gefunden" });
    if (method === "DELETE") { webhooks = webhooks.filter((entry) => entry !== hook); return send(res, 204); }
    if (c === "test") return send(res, 200, { ok: true, status: "204 No Content" });
    Object.assign(hook, body);
    return send(res, 200, hook);
  }
  if (a === "users") return send(res, method === "GET" ? 200 : 204, method === "GET" ? users : undefined);
  const linked = nodeLinkDemo(method, parts, body);
  if (linked) return send(res, linked.status, linked.body);
  if (a === "nodes" && c === "playit") {
    if (method === "PUT") playit = { configured: true, running: true, image: playit.image, logs: ["[playit] agent started", "[playit] tunnel ready: survival.playit.gg"] };
    else if (method === "DELETE") playit = { configured: false, running: false, image: playit.image, logs: [] };
    return send(res, method === "GET" ? 200 : 202, playit);
  }
  if (a === "nodes") return send(res, method === "GET" ? 200 : 201, method === "GET" ? nodes : nodes[0]);
  if (a === "software") {
    if (b && c === "versions") {
      return send(res, 200, b === "velocity" ? ["3.5.1", "3.4.0", "4.3.1"] : b === "fabric" ? ["1.21.4", "1.21.3", "1.20.6"] : b === "neoforge" ? ["1.21.4", "1.21.3", "1.21.1", "1.20.4"] : b === "forge" ? ["26.3", "1.21.1", "1.20.1", "1.16.5", "1.12.2"] : ["26.3", "26.2", "1.21.11", "1.21.10", "1.21.4"]);
    }
    return send(res, 200, [
      { id: "paper", name: "Paper", kind: "server", extensions: "plugins", defaultPort: 25565 },
      { id: "purpur", name: "Purpur", kind: "server", extensions: "plugins", defaultPort: 25565 },
      { id: "folia", name: "Folia", kind: "server", extensions: "plugins", defaultPort: 25565 },
      { id: "fabric", name: "Fabric", kind: "server", extensions: "mods", defaultPort: 25565 },
      { id: "neoforge", name: "NeoForge", kind: "server", extensions: "mods", defaultPort: 25565 },
      { id: "forge", name: "Forge", kind: "server", extensions: "mods", defaultPort: 25565 },
      { id: "vanilla", name: "Vanilla", kind: "server", extensions: "none", defaultPort: 25565 },
      { id: "velocity", name: "Velocity", kind: "proxy", extensions: "plugins", defaultPort: 25565 },
    ]);
  }

  if (a === "projects") {
    if (!b) {
      if (method === "POST") {
        const project = { id: id(), name: body.name || "Neu", kind: body.kind || "server", description: body.description || "", ownerId: "u1", createdAt: now(), myRole: "owner", voicechat: false, members: [{ projectId: "x", userId: "u1", username: "admin", role: "owner" }], servers: [], groups: [], problems: [] };
        projects.push(project);
        return send(res, 201, project);
      }
      projects.forEach(refreshGroups);
      return send(res, 200, projects);
    }
    const project = projects.find((entry) => entry.id === b);
    if (!project) return send(res, 404, { error: "nicht gefunden" });
    refreshGroups(project);
    if (!c) {
      if (method === "DELETE") { projects.splice(projects.indexOf(project), 1); return send(res, 204); }
      if (method === "PATCH") Object.assign(project, { name: body.name ?? project.name, description: body.description ?? project.description });
      return send(res, 200, project);
    }
    if (c === "metrics") {
      const ids = project.servers.map((server) => server.id);
      const range = url.searchParams.get("range") ?? "6h";
      const s = series(range, ids.length ? [ids[0]] : ["x"], project.kind === "network" ? 38 : 9, 4096, 70);
      return send(res, 200, { series: s, players: project.kind === "network" ? 38 : 9 });
    }
    if (c === "servers" && method === "POST") {
      const server = makeServer(project.id, body.name, body.software, body.version, body.memoryMB, "installing", { role: project.kind === "network" ? (body.software === "velocity" ? "proxy" : "backend") : "standalone", inTry: true });
      project.servers.push(server);
      setTimeout(() => { server.view.state = "offline"; }, 4000);
      return send(res, 201, server);
    }
    if (c === "events") return send(res, 200, events);
    if (c === "webhooks") {
      if (method === "POST") { const hook = { id: id(), name: body.name, url: body.url, kind: body.kind, events: body.events ?? [], enabled: true, lastStatus: "" }; webhooks.push(hook); return send(res, 201, hook); }
      return send(res, 200, webhooks);
    }
    if (c === "schedules") {
      if (method === "POST") {
        const target = project.servers.find((server) => server.id === body.serverId);
        const schedule = { id: id(), projectId: project.id, serverName: target?.name ?? "", lastRun: 0, lastStatus: "", lastMessage: "", nextRun: now() + HOUR, enabled: true, everyMinutes: 0, atMinute: 0, weekdays: 127, warnMinutes: 0, ...body };
        schedules.push(schedule);
        return send(res, 201, schedule);
      }
      return send(res, 200, schedules.filter((entry) => entry.projectId === project.id));
    }
    if (c === "status") {
      project.settings = { ...project.settings, statusPublic: !!body.public, statusSlug: body.slug || project.settings?.statusSlug || project.name.toLowerCase().replace(/[^a-z0-9]+/g, "-") };
      return send(res, 200, project.settings);
    }
    if (c === "motd") { project.settings = { ...project.settings, motd: body.motd ?? "" }; return send(res, 200, synced); }
    if (c === "network") return send(res, 200, { ...synced, warnings: [] });
    if (c === "voicechat") {
      project.voicechat = !!body.enabled;
      project.servers.forEach((server) => { server.voicechat = !!body.enabled; });
      return send(res, 200, synced);
    }
    if (c === "groups" && method === "POST") {
      const group = { id: id(), projectId: project.id, name: body.name, slug: String(body.name).toLowerCase(), software: body.software, version: body.version, memoryMB: body.memoryMB, minInstances: body.minInstances, maxInstances: body.maxInstances, playersPerInstance: body.playersPerInstance, idleMinutes: body.idleMinutes, inTry: body.inTry, dynamic: body.dynamic, plugins: body.plugins ?? [], instances: [], active: 0, players: 0 };
      project.groups.push(group);
      return send(res, 201, group);
    }
    if (c === "members") return send(res, d ? 204 : 200, d ? undefined : project.members);
    return send(res, 204);
  }

  if (a === "groups") {
    const project = projects.find((entry) => entry.groups.some((group) => group.id === b));
    const group = project?.groups.find((entry) => entry.id === b);
    if (!group) return send(res, 404, { error: "nicht gefunden" });
    if (c === "instances") {
      const number = project.servers.filter((server) => server.groupId === group.id).length + 1;
      const server = makeServer(project.id, `${group.name}-${number}`, group.software, group.version, group.memoryMB, "installing", { role: "backend", inTry: group.inTry, groupId: group.id });
      project.servers.push(server);
      setTimeout(() => { server.view.state = "running"; server.view.startedAt = now(); }, 5000);
      return send(res, 201, server);
    }
    if (method === "DELETE") {
      project.groups.splice(project.groups.indexOf(group), 1);
      project.servers = project.servers.filter((server) => server.groupId !== group.id);
      return send(res, 204);
    }
    Object.assign(group, body);
    return send(res, 200, group);
  }

  if (a === "servers") {
    const found = findServer(b);
    if (!found) return send(res, 404, { error: "nicht gefunden" });
    const { project, server } = found;
    if (!c) {
      if (method === "DELETE") { project.servers.splice(project.servers.indexOf(server), 1); return send(res, 204); }
      if (method === "PATCH") return send(res, 200, server);
      return send(res, 200, { server, myRole: "owner", myPermissions: allPermissions });
    }
    if (c === "power") {
      const action = body.action;
      if (action === "start") { server.view.state = "starting"; setTimeout(() => { server.view.state = "running"; server.view.startedAt = now(); server.view.stats = { cpuPercent: 22, memoryBytes: server.view.memoryMB * 1024 ** 2 * 0.5, memoryLimit: server.view.memoryMB * 1024 ** 2, netRx: 0, netTx: 0 }; }, 2500); }
      else if (action === "stop" || action === "kill") { server.view.state = "stopping"; setTimeout(() => { server.view.state = "offline"; server.view.stats = undefined; }, 1800); }
      else if (action === "restart") { server.view.state = "stopping"; setTimeout(() => { server.view.state = "starting"; setTimeout(() => { server.view.state = "running"; server.view.startedAt = now(); }, 2000); }, 1500); }
      return send(res, 202, server.view);
    }
    if (c === "command") return send(res, 204);
    if (c === "auto-restart") { server.autoRestart = !!body.enabled; return send(res, 200, server); }
    if (c === "public-address") { server.publicAddress = body.address || undefined; return send(res, 200, server); }
    if (c === "reinstall") return send(res, 202, server.view);
    if (c === "player-lists") return send(res, 200, playerLists);
    if (c === "player-action") {
      const name = body.name;
      const edit = (list, add) => {
        if (add && !list.some((entry) => entry.name === name)) list.push({ uuid: id(), name, reason: body.reason });
        if (!add) list.splice(0, list.length, ...list.filter((entry) => entry.name !== name));
      };
      if (body.action === "whitelist-add") edit(playerLists.whitelist, true);
      if (body.action === "whitelist-remove") edit(playerLists.whitelist, false);
      if (body.action === "op") edit(playerLists.ops, true);
      if (body.action === "deop") edit(playerLists.ops, false);
      if (body.action === "ban") edit(playerLists.banned, true);
      if (body.action === "pardon") edit(playerLists.banned, false);
      if (body.action === "ban-ip") playerLists.bannedIps.push({ ip: name, reason: body.reason });
      if (body.action === "pardon-ip") playerLists.bannedIps = playerLists.bannedIps.filter((entry) => entry.ip !== name);
      return send(res, 200, { ok: true });
    }
    if (c === "modpack") return send(res, 200, { state: "done", done: 148, total: 148, name: "Fabulously Optimized", version: "10.0.0", minecraft: "1.21.4", loader: "fabric" });
    if (c === "logs") return send(res, 200, []);
    if (c === "metrics") {
      const range = url.searchParams.get("range") ?? "6h";
      const scale = server.role === "proxy" ? 38 : server.name.startsWith("Survival") ? 16 : 9;
      const live = server.view.state === "running" ? players(server.id, now(), scale) : 0;
      return send(res, 200, { series: series(range, [server.id], scale, server.view.memoryMB, server.view.startedAt ? (now() - server.view.startedAt) / HOUR : 0.2), players: live, maxPlayers: server.role === "proxy" ? 500 : 20, names: names.slice(0, Math.min(live, 8)) });
    }
    if (c === "players") {
      if (server.view.state !== "running") return send(res, 409, { error: "der Server läuft nicht" });
      const online = players(server.id, now(), server.role === "proxy" ? 38 : 9);
      return send(res, 200, { online, max: server.role === "proxy" ? 500 : 20, names: server.role === "proxy" ? [] : names.slice(0, Math.min(online, 10)), version: server.software === "velocity" ? "Velocity 1.7.2-26.2" : "Paper 1.21.4", motd: "Willkommen im Survival-Netz", latencyMs: 4 });
    }
    if (c === "player-history") {
      const list = names.map((name, index) => ({ name, firstSeen: now() - (20 - index) * 24 * HOUR, lastSeen: now() - index * 37 * MIN, seconds: Math.round((60 - index * 3) * 3600 * hash(name)), online: index < 4 }));
      return send(res, 200, { players: list, online: 4, max: 20 });
    }
    if (c === "files") {
      if (d === "content") {
        const file = url.searchParams.get("path") ?? "";
        res.writeHead(200, { "Content-Type": "text/plain; charset=utf-8" });
        return res.end(fileTexts[file] ?? fileTexts[file.split("/").pop()] ?? "# leer\n");
      }
      if (d) return send(res, 204);
      const dir = (url.searchParams.get("path") ?? "").replace(/^\/+|\/+$/g, "");
      return send(res, 200, files[dir] ?? []);
    }
    if (c === "addons") {
      if (d === "check") return send(res, 200, [
        { file: "LuckPerms-Bukkit-5.4.150.jar", dir: "plugins", name: "LuckPerms", current: "5.4.150", latest: "5.5.0", latestFile: "LuckPerms-Bukkit-5.5.0.jar", known: true, updateAvailable: true },
        { file: "voicechat-bukkit-2.6.27.jar", dir: "plugins", name: "Simple Voice Chat", current: "2.6.27", known: true, updateAvailable: false },
        { file: "Eigenbau.jar", dir: "plugins", name: "Eigenbau", current: "1.0", known: false, updateAvailable: false },
      ]);
      if (d === "update") return send(res, 200, [{ file: "LuckPerms-Bukkit-5.4.150.jar", updated: true, to: "5.5.0" }]);
      if (d === "details") return send(res, 200, [
        { file: "LuckPerms-Bukkit-5.4.150.jar", dir: "plugins", known: true, projectId: "Vebnzrzj", slug: "luckperms", title: "LuckPerms", description: "Ein Rechtesystem für Minecraft-Server.", projectType: "plugin", link: "https://modrinth.com/plugin/luckperms", downloads: 12_400_000, sourceUrl: "https://github.com/LuckPerms/LuckPerms", issuesUrl: "https://github.com/LuckPerms/LuckPerms/issues", license: "MIT", versionId: "v1", versionNumber: "5.4.150", versionName: "v5.4.150", versionType: "release", published: "2026-08-12T10:00:00Z" },
        { file: "voicechat-bukkit-2.6.27.jar", dir: "plugins", known: true, projectId: "9eGKb6K1", slug: "simple-voice-chat", title: "Simple Voice Chat", description: "Proximity-Voicechat in Minecraft.", projectType: "plugin", link: "https://modrinth.com/plugin/simple-voice-chat", downloads: 25_000_000, license: "Custom", versionId: "v2", versionNumber: "2.6.27", versionName: "2.6.27", versionType: "release", published: "2026-09-30T10:00:00Z" },
        { file: "Eigenbau.jar", dir: "plugins", known: false },
      ]);
      if (d === "switch") return send(res, 200, { file: "LuckPerms-Bukkit-5.5.0.jar", from: "5.4.150", to: body.versionId });
      return send(res, 200, [
        { file: "LuckPerms-Bukkit-5.4.150.jar", dir: "plugins", size: 1_600_000, modified: now() - 3 * 24 * HOUR, enabled: true, name: "LuckPerms", version: "5.4.150", kind: "paper", sha1: "a" },
        { file: "voicechat-bukkit-2.6.27.jar", dir: "plugins", size: 2_100_000, modified: now() - 2 * 24 * HOUR, enabled: true, name: "Simple Voice Chat", version: "2.6.27", kind: "paper", sha1: "b" },
        { file: "Eigenbau.jar", dir: "plugins", size: 90_000, modified: now() - 24 * HOUR, enabled: false, name: "Eigenbau", version: "1.0", kind: "unknown", sha1: "c" },
      ]);
    }
    if (c === "modrinth") {
      if (d === "search") return send(res, 200, { total_hits: 3, hits: [
        { project_id: "Vebnzrzj", slug: "luckperms", title: "LuckPerms", description: "Ein Rechtesystem für Minecraft-Server.", author: "Luck", downloads: 12_400_000, icon_url: "", categories: [] },
        { project_id: "hXiIvTyT", slug: "essentialsx", title: "EssentialsX", description: "Die Essentials für Server: Homes, Warps, Kits und mehr.", author: "EssentialsX", downloads: 8_100_000, icon_url: "", categories: [] },
        { project_id: "9eGKb6K1", slug: "simple-voice-chat", title: "Simple Voice Chat", description: "Proximity-Voicechat in Minecraft.", author: "henkelmax", downloads: 25_000_000, icon_url: "", categories: [] },
      ] });
      if (d === "install") return send(res, 200, { installed: ["Beispiel.jar"] });
      return send(res, 200, { project: { id: "Vebnzrzj", title: "LuckPerms", description: "Ein Rechtesystem.", icon_url: "", downloads: 1, slug: "luckperms" }, versions: [{ id: "v1", name: "v5.5.0", versionNumber: "5.5.0", type: "release", published: new Date().toISOString(), gameVersions: ["1.21.4"] }] });
    }
  }
  return send(res, 404, { error: "unbekannte Adresse (Demo)" });
}

// ---------------------------------------------------------------- Dateien der Oberfläche

const types = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2", ".txt": "text/plain" };

function staticFile(res, pathname) {
  const clean = path.normalize(decodeURIComponent(pathname)).replace(/^([/\\])+/, "");
  const candidates = [clean || "index.html", clean + ".html", path.join(clean, "index.html")];
  for (const candidate of candidates) {
    const file = path.join(root, candidate);
    if (!file.startsWith(root)) continue;
    if (fs.existsSync(file) && fs.statSync(file).isFile()) {
      res.writeHead(200, { "Content-Type": types[path.extname(file)] ?? "application/octet-stream", "Cache-Control": "no-cache" });
      return fs.createReadStream(file).pipe(res);
    }
  }
  const missing = path.join(root, "404.html");
  res.writeHead(404, { "Content-Type": "text/html; charset=utf-8" });
  res.end(fs.existsSync(missing) ? fs.readFileSync(missing) : "nicht gefunden");
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  if (url.pathname.startsWith("/api/")) {
    api(req, res, url).catch((error) => send(res, 500, { error: String(error) }));
    return;
  }
  staticFile(res, url.pathname);
});

// ---------------------------------------------------------------- Schein-Konsole

const sockets = new WebSocketServer({ noServer: true });
server.on("upgrade", (req, socket, head) => {
  const match = /^\/api\/servers\/([^/]+)\/ws/.exec(new URL(req.url, "http://x").pathname);
  if (!match) return socket.destroy();
  sockets.handleUpgrade(req, socket, head, (ws) => sockets.emit("connection", ws, match[1]));
});

const esc = "\u001b[";
const history = (name) => [
  `[lodestone] Server gestartet`,
  `[08:00:11] [main/INFO]: Starting minecraft server version 1.21.4`,
  `[08:00:12] [Server thread/INFO]: Loading properties`,
  `[08:00:13] [Server thread/INFO]: Default game type: SURVIVAL`,
  `[08:00:14] [Server thread/WARN]: ${esc}33mDie Konfiguration ist veraltet${esc}0m`,
  `[08:00:15] [Server thread/INFO]: Preparing level "world"`,
  `[08:00:16] [Server thread/INFO]: Done (3.412s)! For help, type "help"`,
  `[08:02:41] [Server thread/INFO]: Alex joined the game`,
  `[08:03:02] [Server thread/INFO]: <Alex> hallo zusammen`,
  `[08:05:44] [Server thread/INFO]: Luna joined the game`,
  `[08:06:10] [Server thread/ERROR]: ${esc}31mCould not pass event PlayerMoveEvent to ${name}${esc}0m`,
];

sockets.on("connection", (ws, serverId) => {
  const found = findServer(serverId);
  const lines = history(found?.server.name ?? "Server").map((text, index) => ({ seq: index + 1, time: now() - (20 - index) * 1000, text }));
  let seqNo = lines.length;
  ws.send(JSON.stringify({ type: "hello", state: found?.server.view.state ?? "offline", lines, stats: found?.server.view.stats }));
  const timer = setInterval(() => {
    if (ws.readyState !== 1) return;
    const stats = found?.server.view.stats;
    if (stats) ws.send(JSON.stringify({ type: "stats", stats: { ...stats, cpuPercent: 8 + Math.random() * 40 } }));
    if (Math.random() < 0.5) ws.send(JSON.stringify({ type: "line", line: { seq: ++seqNo, time: now(), text: `[${new Date().toLocaleTimeString("de-DE")}] [Server thread/INFO]: ${names[Math.floor(Math.random() * names.length)]} hat sich bewegt` } }));
  }, 2000);
  ws.on("message", (data) => {
    try {
      const message = JSON.parse(data.toString());
      if (message.type === "command") ws.send(JSON.stringify({ type: "line", line: { seq: ++seqNo, time: now(), text: `> ${message.data}\n[Server thread/INFO]: (Demo) Befehl empfangen` } }));
    } catch {}
  });
  ws.on("close", () => clearInterval(timer));
});

server.listen(port, "127.0.0.1", () => console.log(`Lodestone-Demo läuft auf http://localhost:${port}`));

// --- rechte
// Einzelrechte pro Mitglied (Spiegel von internal/panel/rechte.go). Die Demo ist immer Eigentümer; „ben“ hat angepasste Rechte.

const permissionCatalog = [
  ["console.read", "Konsole", "Konsole lesen", "Sieht die Ausgabe der Konsole und das Protokoll des Servers.", "viewer"],
  ["console.command", "Konsole", "Befehle senden", "Schickt Befehle an die Konsole, zum Beispiel „op“ oder „stop“.", "operator"],
  ["power", "Konsole", "Starten und Stoppen", "Startet, stoppt und startet Server neu, auch Instanzen einer Gruppe.", "operator"],
  ["files.read", "Dateien", "Dateien ansehen", "Öffnet und lädt Dateien des Servers herunter, auch Welten, Datapacks und server.properties.", "operator"],
  ["files.write", "Dateien", "Dateien ändern", "Bearbeitet, lädt hoch, benennt um und löscht Dateien. Auch Plugins/Mods hochladen, abschalten und löschen läuft darüber.", "operator"],
  ["sftp", "Dateien", "SFTP-Zugang", "Darf per SFTP auf die Dateien zugreifen, zusammen mit „Dateien ansehen“. Schreiben geht nur mit „Dateien ändern“.", "operator"],
  ["backups.read", "Backups", "Backups ansehen", "Sieht die Liste der Backups. Herunterladen braucht zusätzlich „Dateien ansehen“.", "viewer"],
  ["backups.create", "Backups", "Backups erstellen", "Erstellt neue Backups und lädt sie zum Backup-Ziel hoch.", "operator"],
  ["backups.restore", "Backups", "Backups wiederherstellen", "Ersetzt alle Dateien des Servers durch ein Backup.", "operator"],
  ["backups.delete", "Backups", "Backups löschen", "Löscht Backups unwiderruflich und hält sie fest oder entsperrt sie.", "operator"],
  ["addons", "Server", "Plugins und Mods", "Installiert, aktualisiert und wechselt Plugins/Mods über Modrinth und die anderen Quellen. Bausteine wie BlueMap brauchen zusätzlich „Einstellungen“.", "operator"],
  ["players", "Server", "Spieler verwalten", "Sieht Whitelist, Operatoren und Banns und kickt, bannt oder whitelistet Spieler.", "operator"],
  ["settings", "Server", "Einstellungen", "Ändert Name, Arbeitsspeicher, Startzeile, Version, Auto-Restart, Schlafmodus, Speicherlimit, Spieleinstellungen, Welten, Backup-Ausschlüsse, öffentliche Adresse, Projektname, Statusseite, Backup-Ziel und Discord-Bot.", "admin"],
  ["servers", "Server", "Server anlegen und löschen", "Legt neue Server an, kopiert sie, zieht sie auf einen anderen Node um und löscht Server samt Welten und Backups.", "admin"],
  ["schedules", "Projekt", "Zeitpläne", "Legt Zeitpläne an, ändert und löscht sie. Schritte gehen nur mit den passenden Rechten (Befehle, Starten, Backups).", "admin"],
  ["webhooks", "Projekt", "Webhooks", "Verwaltet Webhooks, etwa für Discord, und Konsolen-Auslöser.", "admin"],
  ["members", "Projekt", "Mitglieder", "Fügt Mitglieder hinzu, entfernt sie und vergibt Rollen und Rechte – nie mehr, als man selbst hat.", "admin"],
  ["network", "Projekt", "Netzwerk", "Ändert Reihenfolge, Forced Hosts, MOTD, Voicechat, Tunnel und Gruppen (mit „Server anlegen und löschen“) des Netzwerks.", "admin"],
].map(([id, group, label, description, role]) => ({ id, group, label, description, role }));

const demoRanks = { viewer: 1, operator: 2, admin: 3, owner: 4 };
const roleTemplate = (role) => permissionCatalog.filter((info) => demoRanks[role] >= demoRanks[info.role]).map((info) => info.id);
const allPermissions = roleTemplate("owner");

function decorateMember(member) {
  if (!member.permissions) Object.assign(member, { permissions: roleTemplate(member.role), customPermissions: false });
  return member;
}

const ben = projects[0].members.find((member) => member.username === "ben");
if (ben) Object.assign(ben, { permissions: ["console.read", "backups.read", "files.read", "power"], customPermissions: true });

function rechteApi(res, method, body, a, b, c, d) {
  // Projekte und Server tragen die Rechte des Demo-Benutzers (Eigentümer: alles)
  for (const project of projects) {
    project.myPermissions ??= allPermissions;
    project.members?.forEach(decorateMember);
  }
  if (a === "permissions" && method === "GET") {
    return send(res, 200, { permissions: permissionCatalog, roles: Object.fromEntries(Object.keys(demoRanks).map((role) => [role, roleTemplate(role)])) }), true;
  }
  if (a === "servers" && b && !c && method === "GET") {
    const found = findServer(b);
    if (!found) return false;
    return send(res, 200, { server: found.server, myRole: "owner", myPermissions: allPermissions }), true;
  }
  if (a === "projects" && b && c === "members" && d && method === "PUT") {
    const project = projects.find((entry) => entry.id === b);
    if (!project) return false;
    const role = body.role in demoRanks ? body.role : "viewer";
    const custom = Array.isArray(body.permissions) && role !== "owner" ? permissionCatalog.map((info) => info.id).filter((id) => body.permissions.includes(id)) : null;
    const template = roleTemplate(role);
    const same = !custom || (custom.length === template.length && custom.every((id) => template.includes(id)));
    let member = project.members.find((entry) => entry.userId === d);
    if (!member) {
      member = { projectId: project.id, userId: d, username: users.find((entry) => entry.id === d)?.username ?? d, role };
      project.members.push(member);
    }
    Object.assign(member, { role, permissions: same ? template : custom, customPermissions: !same });
    return send(res, 200, project.members), true;
  }
  return false;
}

// --- sftp

let sftpAccount = {
  passwordSet: true,
  passwordLogin: true,
  keys: [{ id: "sk1", name: "Laptop", publicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDemoDemoDemoDemoDemoDemoDemoDemoDemoDemo", fingerprint: "SHA256:k3Yp0Demo9mQz1vT4bXcR7nLwE2sFhJ8uA6oG5iKdNc", createdAt: now() - 12 * 24 * HOUR, lastUsed: now() - 2 * HOUR }],
};
const sftpReadOnly = {};

function sftpDemo(res, method, body, a, b, c, d) {
  if (a === "me" && b === "sftp") {
    if (!c) return send(res, 200, sftpAccount), true;
    if (c === "password") {
      if (method === "PUT" && String(body.password ?? "").length < 8) return send(res, 400, { error: "Das SFTP-Passwort ist ungültig: das Passwort braucht mindestens 8 Zeichen." }), true;
      sftpAccount = { ...sftpAccount, passwordSet: method === "PUT" };
      return send(res, 200, sftpAccount), true;
    }
    if (c === "password-login") {
      sftpAccount = { ...sftpAccount, passwordLogin: !!body.enabled };
      return send(res, 200, sftpAccount), true;
    }
    if (c === "keys" && method === "POST") {
      const text = String(body.publicKey ?? "").trim();
      if (!/^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp\d+) [A-Za-z0-9+/=]+/.test(text)) return send(res, 400, { error: "Das ist kein öffentlicher SSH-Schlüssel. Füge den Inhalt der .pub-Datei ein (beginnt mit ssh-ed25519, ecdsa-sha2-… oder ssh-rsa)." }), true;
      const key = { id: id(), name: body.name || text.split(" ")[2] || "SSH-Schlüssel", publicKey: text.split(" ").slice(0, 2).join(" "), fingerprint: "SHA256:Demo" + Math.random().toString(36).slice(2, 12), createdAt: now(), lastUsed: 0 };
      sftpAccount = { ...sftpAccount, keys: [...sftpAccount.keys, key] };
      return send(res, 201, key), true;
    }
    if (c === "keys" && d && method === "DELETE") {
      sftpAccount = { ...sftpAccount, keys: sftpAccount.keys.filter((key) => key.id !== d) };
      return send(res, 204), true;
    }
    return false;
  }
  if (a === "servers" && c === "sftp") {
    if (!findServer(b)) return send(res, 404, { error: "nicht gefunden" }), true;
    if (method === "PUT") {
      sftpReadOnly[b] = !!body.operatorsReadOnly;
      return send(res, 200, { operatorsReadOnly: sftpReadOnly[b] }), true;
    }
    return send(res, 200, {
      enabled: true,
      host: "mc.example.org",
      port: 2022,
      username: `${user.username}.${b.slice(0, 8)}`,
      fingerprint: "SHA256:Qm9vdERlbW9Ib3N0S2V5RmluZ2VycHJpbnQxMjM0NTY",
      readOnly: false,
      operatorsReadOnly: !!sftpReadOnly[b],
      canConfigure: true,
      passwordReady: sftpAccount.passwordSet && sftpAccount.passwordLogin,
      keyCount: sftpAccount.keys.length,
    }), true;
  }
  return false;
}

// --- backups
// Backups mit externem Ziel: Liste (lokal/extern, Status, Festhalten), Einstellungen pro Server und Backup-Ziel pro Projekt.

const demoBackups = new Map();
const demoBackupSettings = new Map();
const demoTargets = new Map([["p1", { configured: true, endpoint: "https://s3.eu-central-1.amazonaws.com", region: "eu-central-1", bucket: "lodestone-backups", prefix: "survival/", accessKey: "AKIADEMO", pathStyle: true, hasSecret: true, updatedAt: now() - 5 * 24 * HOUR }]]);

function backupsOf(serverId) {
  if (!demoBackups.has(serverId)) {
    demoBackups.set(serverId, [
      { name: "20261029-080135-vor-update.tar.gz", label: "vor-update", size: 217_245_440, created: now() - 3 * HOUR, status: "done", local: true, remote: true, upload: "done", locked: true },
      { name: "20261028-030000-nachts.tar.gz", label: "nachts", size: 201_000_000, created: now() - 29 * HOUR, status: "done", local: true, remote: true, upload: "done", locked: false },
      { name: "20261027-030000-nachts.tar.gz", label: "nachts", size: 198_000_000, created: now() - 53 * HOUR, status: "done", local: true, remote: false, upload: "failed", error: "Das Backup-Ziel verweigert den Zugriff. Prüfe Access Key, Secret und die Rechte am Bucket.", locked: false },
      { name: "20261020-030000-nachts.tar.gz", label: "nachts", size: 190_000_000, created: now() - 9 * 24 * HOUR, status: "done", local: false, remote: true, upload: "done", locked: false },
    ]);
  }
  return demoBackups.get(serverId);
}

// Lässt ein Backup nach kurzer Zeit fertig werden und auf Wunsch „hochladen“.
function finishDemoBackup(backup) {
  setTimeout(() => {
    Object.assign(backup, { local: true, size: 205_000_000, status: backup.upload === "running" ? "running" : "done" });
    if (backup.upload !== "running") return;
    backup.progress = 40;
    setTimeout(() => Object.assign(backup, { status: "done", upload: "done", remote: true, progress: undefined }), 3000);
  }, 2500);
}

function backupsDemo(res, parts, method, body, url) {
  const [a, b, c, d, e] = parts;
  const reply = (status, data) => {
    send(res, status, data);
    return true;
  };
  if (a === "projects" && c === "backup-target") {
    if (d === "test") return reply(200, { ok: true });
    if (method === "DELETE") {
      demoTargets.delete(b);
      return reply(204);
    }
    if (method === "PUT") {
      const old = demoTargets.get(b);
      const prefix = String(body.prefix ?? "").replace(/^\/+|\/+$/g, "");
      const target = { configured: true, endpoint: body.endpoint, region: body.region || "us-east-1", bucket: body.bucket, prefix: prefix ? prefix + "/" : "", accessKey: body.accessKey, pathStyle: !!body.pathStyle, hasSecret: !!body.secretKey || !!old?.hasSecret, updatedAt: now() };
      if (!target.hasSecret) return reply(400, { error: "Gib das Secret an." });
      demoTargets.set(b, target);
      return reply(200, target);
    }
    return reply(200, demoTargets.get(b) ?? { configured: false, endpoint: "", region: "", bucket: "", prefix: "", accessKey: "", pathStyle: true, hasSecret: false });
  }
  if (a !== "servers" || (c !== "backups" && c !== "backup-settings")) return false;
  const found = findServer(b);
  if (!found) return reply(404, { error: "nicht gefunden" });
  const targetConfigured = demoTargets.has(found.project.id);
  if (c === "backup-settings") {
    if (method === "PUT") demoBackupSettings.set(b, { exclude: (body.exclude ?? []).filter(Boolean), remoteKeep: Number(body.remoteKeep) || 7 });
    return reply(200, { ...(demoBackupSettings.get(b) ?? { exclude: ["dynmap/**", "logs/**"], remoteKeep: 7 }), targetConfigured });
  }
  const list = backupsOf(b);
  if (!d) {
    if (method !== "POST") return reply(200, list);
    const label = String(body.label ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40);
    const stamp = new Date().toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
    const backup = { name: `${stamp}${label ? "-" + label : ""}.tar.gz`, label: label || undefined, size: 0, created: now(), status: "running", local: false, remote: false, upload: body.remote && targetConfigured ? "running" : undefined, locked: false };
    list.unshift(backup);
    finishDemoBackup(backup);
    return reply(202, backup);
  }
  const backup = list.find((entry) => entry.name === d);
  if (!backup) return reply(404, { error: "nicht gefunden" });
  if (e === "lock") {
    backup.locked = !!body.locked;
    return reply(204);
  }
  if (e === "upload") {
    Object.assign(backup, { status: "running", upload: "running", progress: 10, error: undefined });
    setTimeout(() => Object.assign(backup, { status: "done", upload: "done", remote: true, progress: undefined }), 3000);
    return reply(202);
  }
  if (e === "restore" || e === "restore-remote") return reply(202, found.server.view);
  if (e === "download") return reply(404, { error: "In der Demo gibt es keine Archive zum Herunterladen." });
  if (method === "DELETE") {
    if (backup.locked) return reply(409, { error: "Das Backup ist festgehalten. Entsperre es zuerst, um es zu löschen." });
    const where = url.searchParams.get("where");
    if (where !== "remote") backup.local = false;
    if (where !== "local") Object.assign(backup, { remote: false, upload: undefined });
    if (!backup.local && !backup.remote) list.splice(list.indexOf(backup), 1);
    return reply(204);
  }
  return reply(404, { error: "unbekannte Adresse (Demo)" });
}

// --- anmeldung (OIDC und Passkeys)
// Die Demo prüft keine Passkeys und spricht mit keinem Anbieter: Zeremonien liefern gültig aussehende Optionen
// (der Browser fragt dann wirklich nach einem Passkey), und jede Antwort wird angenommen.

let demoPasskeys = [{ id: "pk1", name: "MacBook (Touch ID)", createdAt: now() - 12 * 24 * HOUR, lastUsed: now() - 5 * HOUR }];
let demoOidcLink = null;

function demoChallenge() {
  return Buffer.from(Array.from({ length: 32 }, () => Math.floor(Math.random() * 256))).toString("base64url");
}

function demoRedirect(res, location) {
  res.writeHead(302, { Location: location, "Cache-Control": "no-store" });
  res.end();
}

function anmeldungDemo(res, method, body, parts) {
  const [a, b, c, d] = parts;
  const rp = { id: "localhost", name: "Lodestone" };
  if (a === "auth" && b === "status") {
    send(res, 200, { setupRequired: false, version: "0.1.0-demo", user, timezone: "Europe/Berlin", passwordLogin: true, passkeys: true, oidc: { name: "Authentik" } });
    return true;
  }
  if (a === "auth" && b === "oidc") {
    if (c === "start") demoRedirect(res, "/");
    else if (c === "callback") {
      demoOidcLink = { issuer: "https://auth.example.org/application/o/lodestone/", email: "admin@example.org", autoCreated: false, createdAt: now(), lastLogin: 0 };
      demoRedirect(res, "/account/?oidc=linked");
    } else send(res, 404, { error: "unbekannte Adresse (Demo)" });
    return true;
  }
  if (a === "auth" && b === "passkey") {
    const begin = c === "begin" || (c === "second" && d === "begin");
    if (begin) {
      const allowCredentials = c === "second" ? [{ type: "public-key", id: Buffer.from("demo-passkey").toString("base64url") }] : [];
      send(res, 200, { flow: id(), options: { publicKey: { challenge: demoChallenge(), rpId: rp.id, timeout: 300000, userVerification: c === "second" ? "preferred" : "required", allowCredentials } } });
    } else send(res, 200, user);
    return true;
  }
  if (a === "me" && b === "passkeys") {
    if (method === "GET") send(res, 200, demoPasskeys);
    else if (c === "begin") {
      send(res, 200, {
        flow: id(),
        options: {
          publicKey: {
            challenge: demoChallenge(), rp, user: { id: Buffer.from(user.id).toString("base64url"), name: user.username, displayName: user.username },
            pubKeyCredParams: [{ type: "public-key", alg: -7 }, { type: "public-key", alg: -257 }], timeout: 300000, attestation: "none",
            authenticatorSelection: { residentKey: "preferred", requireResidentKey: false, userVerification: "preferred" }, excludeCredentials: [],
          },
        },
      });
    } else if (c === "finish") {
      const key = { id: id(), name: body.name || "Passkey", createdAt: now(), lastUsed: 0 };
      demoPasskeys = [key, ...demoPasskeys];
      send(res, 201, key);
    } else if (method === "PATCH") {
      demoPasskeys = demoPasskeys.map((key) => (key.id === c ? { ...key, name: body.name || key.name } : key));
      send(res, 204);
    } else if (method === "DELETE") {
      demoPasskeys = demoPasskeys.filter((key) => key.id !== c);
      send(res, 204);
    } else send(res, 404, { error: "unbekannte Adresse (Demo)" });
    return true;
  }
  if (a === "me" && b === "oidc") {
    if (c === "link") send(res, 200, { url: "/api/auth/oidc/callback?demo=1" });
    else if (method === "DELETE") {
      demoOidcLink = null;
      send(res, 204);
    } else send(res, 200, { configured: true, name: "Authentik", link: demoOidcLink ?? undefined, hasPassword: true, passwordLogin: true, firstPasswordUntil: 0 });
    return true;
  }
  if (a === "users" && c === "passkeys" && method === "DELETE") {
    send(res, 204);
    return true;
  }
  return false;
}

// --- serverops: Speicherplatz, Kopie und Umzug (Schein-Abläufe mit Fortschritt)

const GB = 1024 ** 3;
const serverops = { ops: [], limits: { [survivalId]: 20 * 1024 }, measured: now() - 3 * MIN };
// Ein zweiter, erreichbarer Node, damit sich der Umzug in der Demo ausprobieren lässt
nodes.push({
  id: "n3", name: "hetzner-2", url: "https://node2.example.com:8480", publicHost: "mc3.example.org", online: true, version: "0.1.0", serverCount: 0,
  system: { dockerVersion: "29.6.2", os: "Debian 13", architecture: "x86_64", cpus: 16, memoryBytes: 64 * GB },
  memoryAvailable: 58 * GB, memoryCommitted: 0,
});

function demoSize(serverId) {
  return Math.round(serverId === survivalId ? 18.6 * GB : (0.2 + hash(serverId) * 3) * GB);
}

function demoStorage(serverId) {
  const limitMB = serverops.limits[serverId] ?? 0;
  const size = demoSize(serverId);
  return { bytes: size, files: Math.round(size / 180_000), backups: Math.round(size * 2.4), measuredAt: serverops.measured, limitMB, blocked: limitMB > 0 && size >= limitMB * 1024 ** 2, measuring: false, free: 61 * GB, total: 240 * GB };
}

function demoOp(project, serverId, kind, label, total, finish) {
  const op = { id: id(), projectId: project.id, serverId, kind, state: "running", label, step: kind === "clone" ? "Dateien werden kopiert" : "Server werden gestoppt", done: 0, total, message: "", warnings: [], cancelRequested: false, createdAt: now(), updatedAt: now() };
  serverops.ops.unshift(op);
  const started = now();
  const timer = setInterval(() => {
    op.updatedAt = now();
    if (op.cancelRequested) {
      clearInterval(timer);
      Object.assign(op, { state: "canceled", step: "", message: "Abgebrochen. Alle Server sind unverändert auf dem alten Node." });
      return;
    }
    const fraction = Math.min(1, (now() - started) / (kind === "clone" ? 3000 : 9000));
    op.done = Math.round(total * fraction);
    if (kind === "migrate" && fraction > 0.1) op.step = `„${project.servers[0]?.name ?? "Server"}“ wird übertragen (1 von ${project.servers.length})`;
    if (fraction >= 1) {
      clearInterval(timer);
      Object.assign(op, { state: "done", step: "" }, finish());
    }
  }, 500);
  return op;
}

function serveropsApi(res, parts, method, body) {
  const [a, b, c, d] = parts;
  if (a === "servers" && (c === "storage" || c === "clone")) {
    const found = findServer(b);
    if (!found) return send(res, 404, { error: "nicht gefunden" }), true;
    const { project, server } = found;
    if (c === "storage") {
      if (method === "PUT") serverops.limits[b] = Number(body.limitMB) || 0;
      if (d === "measure") serverops.measured = now();
      return send(res, d === "measure" ? 202 : 200, demoStorage(b)), true;
    }
    if (server.role === "proxy") return send(res, 409, { error: "Ein Netzwerk hat genau einen Proxy, er lässt sich deshalb nicht kopieren." }), true;
    const name = String(body.name ?? "").trim() || `${server.name} Kopie`;
    const op = demoOp(project, server.id, "clone", `„${server.name}“ → „${name}“`, demoSize(server.id), () => {
      let target = project;
      if (project.kind === "server") {
        target = { ...project, id: id(), name, description: `Kopie von „${server.name}“`, createdAt: now(), servers: [], groups: [], settings: { statusPublic: false, statusSlug: "", motd: "" } };
        projects.push(target);
      }
      const copy = makeServer(target.id, name, server.software, server.view.version, server.view.memoryMB, "offline", { role: server.role, inTry: false, tryPosition: target.servers.length });
      target.servers.push(copy);
      return {
        resultServerId: copy.id,
        message: project.kind === "network" ? `„${name}“ ist angelegt. Sie ist noch nicht im Try-Ablauf des Proxys: Schalte sie im Tab „Netzwerk“ ein, wenn sie fertig eingerichtet ist.` : `„${name}“ ist im neuen Projekt „${name}“ angelegt, weil ein Server-Projekt genau einen Server hat.`,
      };
    });
    return send(res, 202, op), true;
  }
  if (a === "projects" && (c === "migrate" || c === "server-ops")) {
    const project = projects.find((entry) => entry.id === b);
    if (!project) return send(res, 404, { error: "nicht gefunden" }), true;
    if (c === "server-ops") return send(res, 200, serverops.ops.filter((op) => op.projectId === project.id).slice(0, 10)), true;
    const target = nodes.find((entry) => entry.id === body.nodeId);
    if (!target || !target.online) return send(res, 502, { error: "Der Ziel-Node ist nicht erreichbar. Prüfe, ob er läuft." }), true;
    if (project.groups.length > 0) return send(res, 409, { error: "Netzwerke mit Gruppen können noch nicht umziehen, weil der Autoscaler laufend Instanzen anlegt und löscht. Lösche die Gruppen vorher und lege sie danach neu an." }), true;
    if (serverops.ops.some((op) => op.projectId === project.id && op.state === "running")) return send(res, 409, { error: "Im Projekt läuft schon eine Kopie oder ein Umzug. Warte, bis er fertig ist." }), true;
    const total = project.servers.reduce((sum, server) => sum + demoSize(server.id), 0);
    const source = project.servers[0]?.nodeName ?? "home";
    const op = demoOp(project, project.servers[0]?.id, "migrate", `„${project.name}“: „${source}“ → „${target.name}“`, total, () => {
      const warnings = project.servers.filter((server) => server.publicAddress).map((server) => `„${server.name}“ hatte die öffentliche Adresse „${server.publicAddress}“. Sie gehörte zur alten Maschine und wurde entfernt. Lege auf „${target.name}“ einen neuen playit-Tunnel an.`);
      for (const server of project.servers) {
        Object.assign(server, { nodeId: target.id, nodeName: target.name, publicHost: target.publicHost, publicAddress: undefined });
        if (server.address) server.address = `${target.publicHost}:25565`;
      }
      return { message: `Umzug auf „${target.name}“ abgeschlossen.`, warnings };
    });
    return send(res, 202, op), true;
  }
  if (a === "server-ops") {
    const op = serverops.ops.find((entry) => entry.id === b);
    if (!op) return send(res, 404, { error: "nicht gefunden" }), true;
    if (c === "cancel") {
      if (op.state !== "running") return send(res, 409, { error: "Der Vorgang ist schon beendet." }), true;
      op.cancelRequested = true;
      return send(res, 202, op), true;
    }
    return send(res, 200, op), true;
  }
  if (a === "nodes" && c === "storage") {
    const node = nodes.find((entry) => entry.id === b);
    if (!node) return send(res, 404, { error: "nicht gefunden" }), true;
    if (!node.online) return send(res, 200, { online: false, error: node.error, free: 0, total: 0, used: 0 }), true;
    const servers = projects.flatMap((project) => project.servers.filter((server) => server.nodeId === node.id).map((server) => ({ ...demoStorage(server.id), id: server.id, name: server.name, projectId: project.id, projectName: project.name })));
    const used = servers.reduce((sum, server) => sum + server.bytes + server.backups, 0);
    return send(res, 200, { online: true, free: node.id === "n1" ? 61 * GB : 410 * GB, total: node.id === "n1" ? 240 * GB : 480 * GB, used, servers }), true;
  }
  return false;
}

// --- welten
// Welten, server.properties, Datapacks und die eigene Ordnung der Projekte. weltenDemo gibt true zurück, wenn es die
// Anfrage beantwortet hat.

let projectPrefs = [
  { projectId: "p1", favorite: true, position: 0, tags: ["Öffentlich"] },
  { projectId: "p2", favorite: false, position: 0, tags: ["Test"] },
];
const demoProperties = new Map();
const demoWorlds = new Map();
const demoDatapacks = new Map();

function propertiesOf(server) {
  if (!demoProperties.has(server.id)) {
    const entries = fileTexts["server.properties"]
      .split("\n")
      .filter((line) => line && !line.startsWith("#"))
      .map((line) => ({ key: line.slice(0, line.indexOf("=")), value: line.slice(line.indexOf("=") + 1) }));
    entries.push({ key: "level-name", value: "world" }, { key: "level-seed", value: "-4172144997902289642" }, { key: "pvp", value: "true" }, { key: "simulation-distance", value: "8" }, { key: "luckperms-sync", value: "true" });
    demoProperties.set(server.id, entries);
  }
  return demoProperties.get(server.id);
}

function worldsOf(server) {
  if (!demoWorlds.has(server.id)) {
    demoWorlds.set(server.id, [
      { name: "world", kind: "overworld", main: true, size: 212_000_000, modified: now() - 4 * MIN, datapacks: 2 },
      { name: "world_nether", kind: "nether", main: false, size: 38_000_000, modified: now() - 2 * HOUR, datapacks: 0 },
      { name: "world_the_end", kind: "end", main: false, size: 6_400_000, modified: now() - 26 * HOUR, datapacks: 0 },
    ]);
  }
  return demoWorlds.get(server.id);
}

function datapacksOf(serverId, world) {
  const key = `${serverId}/${world}`;
  if (!demoDatapacks.has(key)) {
    demoDatapacks.set(key, world === "world" ? [
      { name: "Terralith_1.21.zip", dir: false, size: 2_300_000, modified: now() - 3 * 24 * HOUR },
      { name: "mehr-rezepte", dir: true, size: 0, modified: now() - 6 * 24 * HOUR },
    ] : []);
  }
  return demoDatapacks.get(key);
}

function lockedOf(server) {
  const locked = { "server-port": "Lodestone verbindet den Port im Container (25565) mit einem Port der Maschine. Welchen, stellst du unter Einstellungen ein." };
  if (server.role === "backend") locked["online-mode"] = "Im Netzwerk prüft der Velocity-Proxy die Konten. Die Autokonfiguration setzt online-mode hier deshalb auf false; ein anderer Wert würde den Beitritt über den Proxy verhindern.";
  return locked;
}

function weltenDemo(res, parts, method, body) {
  const [a, b, c, d, e, f] = parts.map((part) => decodeURIComponent(part));
  if (a === "me" && b === "project-prefs") {
    if (method === "GET") return send(res, 200, projectPrefs), true;
    let pref = projectPrefs.find((entry) => entry.projectId === c);
    if (!pref) projectPrefs.push((pref = { projectId: c, favorite: false, position: 0, tags: [] }));
    if (typeof body.favorite === "boolean") pref.favorite = body.favorite;
    if (Array.isArray(body.tags)) pref.tags = [...new Set(body.tags.map((tag) => String(tag).trim()).filter(Boolean))].slice(0, 8);
    return send(res, 200, pref), true;
  }
  if (a === "me" && b === "project-order") {
    (body.projectIds ?? []).forEach((projectId, index) => {
      let pref = projectPrefs.find((entry) => entry.projectId === projectId);
      if (!pref) projectPrefs.push((pref = { projectId, favorite: false, position: 0, tags: [] }));
      pref.position = index + 1;
    });
    return send(res, 200, projectPrefs), true;
  }
  if (a === "projects" && c === "live-players") {
    const project = projects.find((entry) => entry.id === b);
    if (!project) return send(res, 404, { error: "nicht gefunden" }), true;
    return send(res, 200, Object.fromEntries(project.servers.map((server) => [server.id, server.view.state === "running" ? (server.role === "proxy" ? 38 : Math.round(hash(server.id) * 20)) : 0]))), true;
  }
  if (a !== "servers" || (c !== "properties" && c !== "worlds")) return false;
  const found = findServer(b);
  if (!found) return send(res, 404, { error: "nicht gefunden" }), true;
  const { server } = found;
  if (server.role === "proxy") return send(res, 400, { error: "Velocity hat keine Welten und keine server.properties; seine Einstellungen stehen in velocity.toml (unter Dateien)" }), true;

  if (c === "properties") {
    const entries = propertiesOf(server);
    if (method === "PUT") {
      const locked = lockedOf(server);
      for (const [key, value] of Object.entries(body.values ?? {})) {
        if (locked[key]) return send(res, 400, { error: `${key} lässt sich hier nicht ändern. ${locked[key]}` }), true;
        const entry = entries.find((item) => item.key === key);
        if (entry) entry.value = String(value);
        else entries.push({ key, value: String(value) });
      }
    }
    return send(res, 200, { exists: true, entries, locked: lockedOf(server) }), true;
  }

  const worlds = worldsOf(server);
  const level = propertiesOf(server).find((entry) => entry.key === "level-name")?.value ?? "world";
  const seed = propertiesOf(server).find((entry) => entry.key === "level-seed")?.value ?? "";
  if (!d) return send(res, 200, { levelName: level, seed, worlds }), true;
  const world = worlds.find((entry) => entry.name === d);
  const offline = server.view.state === "offline";
  if (e === "upload") {
    if (!offline) return send(res, 409, { error: "das ist im aktuellen Zustand des Servers nicht möglich: Stoppe den Server, bevor du die Welt änderst" }), true;
    if (!world) worlds.push({ name: d, kind: "other", main: d === level, size: 48_000_000, modified: now(), datapacks: 0 });
    else Object.assign(world, { size: 48_000_000, modified: now() });
    return send(res, 201, { world: d }), true;
  }
  if (!world) return send(res, 404, { error: `die Welt „${d}“ gibt es nicht` }), true;
  if (e === "download") {
    res.writeHead(200, { "Content-Type": "application/zip", "Content-Disposition": `attachment; filename="${d}.zip"` });
    res.end("Demo: hier käme das Zip der Welt.");
    return true;
  }
  if (e === "reset") {
    if (!offline) return send(res, 409, { error: "das ist im aktuellen Zustand des Servers nicht möglich: Stoppe den Server, bevor du die Welt änderst" }), true;
    const targets = [d, ...(body.dimensions && world.main ? [`${d}_nether`, `${d}_the_end`] : [])];
    if (typeof body.seed === "string") propertiesOf(server).find((entry) => entry.key === "level-seed").value = body.seed;
    server.view.state = "restoring";
    setTimeout(() => {
      demoWorlds.set(server.id, worlds.filter((entry) => !targets.includes(entry.name)));
      server.view.state = "offline";
    }, 2500);
    return send(res, 202, server.view), true;
  }
  if (e === "datapacks") {
    const packs = datapacksOf(server.id, d);
    if (method === "GET") return send(res, 200, packs), true;
    if (method === "POST") {
      packs.push({ name: `Demo-Pack-${packs.length + 1}.zip`, dir: false, size: 180_000, modified: now() });
      world.datapacks = packs.length;
      return send(res, 201, { name: packs[packs.length - 1].name }), true;
    }
    if (method === "DELETE") {
      const index = packs.findIndex((pack) => pack.name === f);
      if (index < 0) return send(res, 404, { error: `das Datapack „${f}“ gibt es nicht` }), true;
      packs.splice(index, 1);
      world.datapacks = packs.length;
      return send(res, 204), true;
    }
  }
  return send(res, 404, { error: "unbekannte Adresse (Demo)" }), true;
}

// --- automatik: Schlafmodus, Konsolen-Auslöser, Discord-Bot, Absturzanalyse (wird oben in api() zuerst gefragt)

const sleepState = new Map(); // Server-ID → { enabled, idleMinutes, lastWake, lastSleep }
let triggers = [
  { id: "t1", projectId: projects[0].id, serverId: "", name: "Lag erkannt", pattern: "Can't keep up!", level: "warn", scheduleId: "", enabled: true, lastMatch: now() - 3 * HOUR, lastLine: "[Server thread/WARN]: Can't keep up! Is the server overloaded? Running 2034ms behind", createdAt: now() - 5 * 24 * HOUR },
];
const discordBots = new Map(); // Projekt-ID → Einstellungen (ohne Token)
events.unshift(
  { id: 12, time: now() - 2 * MIN, serverId: projects[0].servers[0]?.id, kind: "player_join", level: "info", message: "Luna hat Proxy betreten." },
  { id: 11, time: now() - 4 * MIN, serverId: projects[0].servers[0]?.id, kind: "wake", level: "info", message: "Proxy wacht auf, weil Luna beitreten möchte." },
  { id: 10, time: now() - 3 * HOUR, serverId: projects[0].servers[0]?.id, kind: "console_match", level: "warn", message: "Konsolen-Auslöser „Lag erkannt“ auf Survival: [Server thread/WARN]: Can't keep up! Running 2034ms behind" },
);

function automatikDemo(method, parts, body) {
  const reply = (status, data) => ({ status, body: data });
  const [a, b, c, d] = parts;
  if (a === "servers" && (c === "sleep" || c === "crash-analysis")) {
    const found = findServer(b);
    if (!found) return reply(404, { error: "nicht gefunden" });
    const { server } = found;
    if (c === "crash-analysis") {
      const crashed = server.view.state === "offline" && server.software !== "velocity";
      return reply(200, crashed
        ? { found: true, report: "crash-reports/crash-2026-11-21_08.06.10-server.txt", suspects: [{ modId: "examplemod", name: "Example Mod", reason: "suspected" }], missing: [], mixins: [], summary: "Verdächtige Mod laut Absturzbericht: „Example Mod“ (examplemod)." }
        : { found: false, suspects: [], missing: [], mixins: [] });
    }
    const allowed = server.role !== "backend" && !server.groupId;
    const state = sleepState.get(server.id) ?? { enabled: false, idleMinutes: 10, lastWake: now() - 50 * MIN, lastSleep: now() - 3 * HOUR };
    if (method === "PUT") {
      if (body.enabled && !allowed) return reply(400, { error: "Backend-Server hinter dem Proxy erreichen Spieler nicht direkt, ein Beitritt kann sie also nicht wecken. Schalte den Schlafmodus beim Proxy ein." });
      Object.assign(state, { enabled: !!body.enabled, idleMinutes: body.idleMinutes || 10 });
      sleepState.set(server.id, state);
    }
    const port = server.view.ports?.find((entry) => entry.protocol === "tcp")?.host;
    return reply(200, {
      ...state, supported: true, allowed, port,
      listening: state.enabled && server.view.state === "offline",
      reason: allowed ? undefined : server.groupId ? "Instanzen einer Gruppe startet und stoppt der Autoscaler, der Schlafmodus gilt hier nicht." : "Backend-Server hinter dem Proxy erreichen Spieler nicht direkt, ein Beitritt kann sie also nicht wecken. Schalte den Schlafmodus beim Proxy ein: Schläft er ein, stoppt Lodestone auch die Backend-Server, und wacht er auf, startet es sie mit.",
    });
  }
  if (a === "projects" && c === "console-triggers") {
    if (method === "POST") {
      if (!body.name || !body.pattern) return reply(400, { error: "der Name braucht 1 bis 48 Zeichen" });
      const trigger = { id: id(), projectId: b, serverId: body.serverId ?? "", name: body.name, pattern: body.pattern, level: body.level ?? "info", scheduleId: body.scheduleId ?? "", enabled: true, lastMatch: 0, lastLine: "", createdAt: now() };
      triggers.push(trigger);
      return reply(201, trigger);
    }
    return reply(200, triggers.filter((entry) => entry.projectId === b));
  }
  if (a === "console-triggers") {
    const trigger = triggers.find((entry) => entry.id === b);
    if (!trigger) return reply(404, { error: "nicht gefunden" });
    if (method === "DELETE") {
      triggers = triggers.filter((entry) => entry.id !== b);
      return reply(204);
    }
    Object.assign(trigger, body);
    return reply(200, trigger);
  }
  if (a === "projects" && c === "discord") {
    const path = `/api/public/discord/${b}/interactions`;
    const empty = { configured: false, applicationId: "", publicKey: "", hasToken: false, rules: [], enabled: false, registeredAt: 0, interactionsPath: path };
    const bot = discordBots.get(b);
    if (d === "register") {
      if (!bot?.hasToken) return reply(409, { error: "trag zuerst Application-ID, Public Key und Bot-Token ein" });
      bot.registeredAt = now();
      return reply(200, bot);
    }
    if (method === "DELETE") {
      discordBots.delete(b);
      return reply(204);
    }
    if (method === "PUT") {
      const next = { ...(bot ?? empty), configured: true, applicationId: body.applicationId, publicKey: body.publicKey, rules: body.rules ?? [], enabled: !!body.enabled };
      if (body.botToken) next.hasToken = true;
      discordBots.set(b, next);
      return reply(200, next);
    }
    return reply(200, bot ?? empty);
  }
  return null;
}

// --- inhalte
// Weitere Quellen (Hangar, Spigot, CurseForge) und Bausteine (Geyser, BlueMap). Liefert true, wenn die Anfrage erledigt ist.

const demoSourceHits = {
  hangar: [
    { id: "31", slug: "ViaVersion", title: "ViaVersion", description: "Lässt neuere Clients auf ältere Server.", author: "ViaVersion", downloads: 2_400_000, link: "https://hangar.papermc.io/ViaVersion/ViaVersion", installable: true },
    { id: "12", slug: "ViaBackwards", title: "ViaBackwards", description: "Lässt ältere Clients auf neuere Server.", author: "ViaVersion", downloads: 1_100_000, link: "https://hangar.papermc.io/ViaVersion/ViaBackwards", installable: true },
  ],
  spigot: [
    { id: "100", title: "Gutes Plugin", description: "Kann was.", downloads: 50_000, link: "https://www.spigotmc.org/resources/100/", installable: true },
    { id: "9089", title: "EssentialsX", description: "Die Essentials für Spigot und Paper.", downloads: 5_900_000, link: "https://www.spigotmc.org/resources/9089/", installable: false, note: "Das Plugin liegt nicht auf Spigot, sondern auf einer anderen Seite. Lade es dort herunter." },
  ],
  curseforge: [
    { id: "11", title: "Freier Mod", description: "Darf über Lodestone geladen werden.", author: "Autorin", downloads: 1_000_000, link: "https://www.curseforge.com/minecraft/mc-mods/frei", installable: true },
    { id: "22", title: "Gesperrter Mod", description: "Nur über curseforge.com.", downloads: 5000, installable: false, note: "Der Autor erlaubt keine Downloads über andere Programme. Lade die Datei auf curseforge.com herunter und dann hier hoch." },
  ],
};
const demoBlocks = new Map();
let demoNetworkGeyser = false;

function inhalteApi(res, url, method, body, parts) {
  const [a, b, c, d, e] = parts;
  if (a === "projects" && c === "blocks") {
    if (method === "PUT") demoNetworkGeyser = !!body.enabled;
    send(res, 200, [{ id: "geyser", name: "Geyser + Floodgate", managed: "network", available: true, enabled: demoNetworkGeyser, address: demoNetworkGeyser ? "mc.example.org:19132" : undefined,
      description: "Bedrock-Spieler (Handy, Konsole, Windows) kommen über den Proxy ins Netzwerk. Geyser und Floodgate laufen auf dem Proxy, Floodgate auf allen Servern dahinter, mit gemeinsamem Schlüssel." }]);
    return true;
  }
  if (a !== "servers") return false;
  const found = findServer(b);
  if (!found) return false;
  const { project, server } = found;
  if (c === "blocks") {
    const state = demoBlocks.get(server.id) ?? { geyser: false, bluemap: false };
    if (method === "PUT") state[d] = !!body.enabled;
    demoBlocks.set(server.id, state);
    const network = project.kind === "network";
    send(res, 200, [
      { id: "geyser", name: "Geyser + Floodgate", description: "Bedrock-Spieler (Handy, Konsole, Windows) können mitspielen, ohne ein Java-Konto zu haben.", available: !network && ["paper", "purpur", "velocity"].includes(server.software),
        enabled: network ? demoNetworkGeyser : state.geyser, managed: network ? "network" : undefined, reason: network ? "Im Netzwerk schaltest du Geyser im Projekt unter „Netzwerk“ ein." : undefined, address: state.geyser ? "mc.example.org:19132" : undefined },
      { id: "bluemap", name: "BlueMap", description: "Eine 3D-Karte der Welt im Browser. Lodestone veröffentlicht den Port der Karte und erlaubt BlueMap, die nötigen Minecraft-Dateien von Mojang zu laden.", available: server.software !== "velocity", enabled: state.bluemap, url: state.bluemap ? "http://mc.example.org:28100/" : undefined },
    ]);
    return true;
  }
  if (c !== "sources") return false;
  const mods = ["fabric", "neoforge", "forge"].includes(server.software);
  if (!d) {
    send(res, 200, [
      { id: "modrinth", name: "Modrinth", available: server.software !== "vanilla" },
      { id: "hangar", name: "Hangar", available: !mods, reason: mods ? "Hangar hat nur Plugins für Paper, Purpur, Folia und Velocity." : undefined },
      { id: "spigot", name: "Spigot", available: ["paper", "purpur"].includes(server.software), reason: ["paper", "purpur"].includes(server.software) ? undefined : "Spigot-Plugins laufen nur auf Paper und Purpur." },
      { id: "curseforge", name: "CurseForge", available: mods, reason: mods ? undefined : "Über CurseForge lädt Lodestone nur Mods für Fabric, Forge und NeoForge." },
    ]);
    return true;
  }
  const hits = demoSourceHits[d] ?? [];
  if (e === "search") {
    const query = (url.searchParams.get("q") ?? "").toLowerCase();
    send(res, 200, { hits: hits.filter((hit) => !query || hit.title.toLowerCase().includes(query)) });
    return true;
  }
  if (e === "project") {
    const hit = hits.find((entry) => entry.id === parts[5]) ?? hits[0];
    send(res, 200, { project: hit, versions: [
      { id: "v2", name: "Release", versionNumber: "2.1", type: "release", published: new Date().toISOString(), gameVersions: ["1.21"], downloadable: hit.installable, reason: hit.note },
      { id: "v1", name: "Release", versionNumber: "2.0", type: "release", published: new Date(now() - 30 * 24 * HOUR).toISOString(), gameVersions: ["1.21"], downloadable: false, reason: "Diese Version liegt nicht auf Hangar, sondern auf einer anderen Seite. Lade sie dort herunter." },
    ] });
    return true;
  }
  if (e === "install") {
    const hit = hits.find((entry) => entry.id === body.project);
    if (!hit?.installable) send(res, 409, { error: hit?.note ?? "das Projekt gibt es nicht (mehr)" });
    else send(res, 200, { installed: [`${hit.title.replace(/[^A-Za-z0-9]+/g, "_")}-2.1.jar`] });
    return true;
  }
  return false;
}

// --- nodelink
// Nodes, die sich selbst zum Panel verbinden: ein verbundener Beispiel-Node, Join-Tokens und der Verbindungszustand.

nodes.push({
  id: "n3", name: "zuhause-nat", publicHost: "mc3.example.org", online: true, linked: true, version: "0.3.0", serverCount: 0,
  system: { dockerVersion: "29.6.2", os: "Debian 13", architecture: "aarch64", cpus: 4, memoryBytes: 8 * 1024 ** 3 },
  memoryAvailable: 6.1 * 1024 ** 3, memoryCommitted: 0,
});
const nodeLinks = [{ nodeId: "n3", connected: true, connectedAt: now() - 3 * HOUR, remote: "198.51.100.40", version: "0.3.0", insecure: false, joinedAt: now() - 3 * 24 * HOUR }];

function demoJoinToken(node) {
  const expiresAt = now() + 24 * HOUR;
  const link = nodeLinks.find((entry) => entry.nodeId === node.id);
  if (link) link.joinExpiresAt = expiresAt;
  else nodeLinks.push({ nodeId: node.id, connected: false, insecure: false, joinExpiresAt: expiresAt });
  return { node: { id: node.id, name: node.name, publicHost: node.publicHost, online: false, serverCount: 0, linked: true }, token: "ldj_" + "0123456789abcdef".repeat(4), expiresAt };
}

/** Antwortet auf die Routen der Node-Verbindung oder liefert null. */
function nodeLinkDemo(method, parts, body) {
  const [a, b, c] = parts;
  if (a !== "nodes") return null;
  if (b === "link" && method === "GET") return { status: 200, body: nodeLinks };
  if (b === "link" && method === "POST") {
    if (!body.name?.trim()) return { status: 400, body: { error: "der Name braucht 1 bis 48 Zeichen" } };
    if (nodes.some((node) => node.name.toLowerCase() === body.name.trim().toLowerCase())) return { status: 409, body: { error: "gibt es schon: der Name ist vergeben" } };
    const node = { id: id(), name: body.name.trim(), publicHost: body.publicHost ?? "", online: false, linked: true, serverCount: 0, error: "der Node ist nicht erreichbar: der Node ist gerade nicht mit dem Panel verbunden" };
    nodes.push(node);
    return { status: 201, body: demoJoinToken(node) };
  }
  if (b && c === "join-token" && method === "POST") {
    const node = nodes.find((entry) => entry.id === b);
    if (!node) return { status: 404, body: { error: "nicht gefunden" } };
    if (!node.linked) return { status: 400, body: { error: "dieser Node wird direkt über seine Adresse angesprochen und braucht kein Join-Token" } };
    return { status: 200, body: demoJoinToken(node) };
  }
  if (b && !c && method === "DELETE") {
    const index = nodes.findIndex((entry) => entry.id === b);
    if (index >= 0 && nodes[index].serverCount === 0) nodes.splice(index, 1);
    return { status: 204 };
  }
  return null;
}
