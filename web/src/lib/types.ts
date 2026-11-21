export type Role = "viewer" | "operator" | "admin" | "owner";
export const roleRank: Record<Role, number> = { viewer: 1, operator: 2, admin: 3, owner: 4 };
export const roleLabel: Record<Role, string> = { viewer: "Betrachter", operator: "Operator", admin: "Admin", owner: "Eigentümer" };

export interface User {
  id: string;
  username: string;
  isAdmin: boolean;
  disabled: boolean;
  createdAt: number;
}

export interface AuthStatus {
  setupRequired: boolean;
  version: string;
  user?: User;
}

export interface NodeInfo {
  id: string;
  name: string;
  url?: string;
  publicHost: string;
  online: boolean;
  error?: string;
  version?: string;
  serverCount: number;
  memoryAvailable?: number;
  memoryCommitted?: number;
  system?: { dockerVersion: string; os: string; architecture: string; cpus: number; memoryBytes: number };
}

export type ServerState = "installing" | "offline" | "starting" | "running" | "stopping" | "restoring";

export interface Stats {
  cpuPercent: number;
  memoryBytes: number;
  memoryLimit: number;
  netRx: number;
  netTx: number;
}

export interface PortMapping {
  container: number;
  host: number;
  protocol: "tcp" | "udp";
}

export interface NodeView {
  id: string;
  name: string;
  software: string;
  version: string;
  memoryMB: number;
  cpu: number;
  ports: PortMapping[] | null;
  env?: Record<string, string>;
  startup?: string;
  image?: string;
  eula: boolean;
  noPublish?: boolean;
  backupKeep: number;
  installed: boolean;
  state: ServerState;
  lastError?: string;
  stats?: Stats;
  startedAt?: number;
  autoconfigPending?: boolean;
}

export type ServerRole = "standalone" | "proxy" | "backend";

export interface Server {
  id: string;
  projectId: string;
  nodeId: string;
  name: string;
  slug: string;
  role: ServerRole;
  software: string;
  inTry: boolean;
  tryPosition: number;
  forcedHost: string;
  voicechat: boolean;
  groupId?: string;
  nodeName: string;
  publicHost: string;
  online: boolean;
  address?: string;
  voiceAddress?: string;
  publicAddress?: string;
  publicVoiceAddress?: string;
  modpack?: ModpackInfo;
  autoRestart: boolean;
  view: NodeView | null;
}

export interface ModpackInfo {
  projectId: string;
  slug: string;
  title: string;
  iconUrl?: string;
  versionId: string;
  versionNumber: string;
  minecraft: string;
}

export interface ModpackStatus {
  state: "idle" | "running" | "done" | "failed";
  phase?: string;
  done: number;
  total: number;
  name?: string;
  version?: string;
  minecraft?: string;
  loader?: string;
  error?: string;
}

export interface ModpackVersion {
  id: string;
  name: string;
  versionNumber: string;
  type: string;
  published: string;
  gameVersions: string[];
}

export interface PlayitTunnel {
  id: string;
  name: string;
  tunnelType: string;
  portType: string;
  localIp: string;
  localPort: number;
  publicAddress: string;
  enabled: boolean;
  createdAt?: number;
}

export interface PlayitStatus {
  configured: boolean;
  running: boolean;
  image: string;
  logs: string[];
  error?: string;
  since?: number;
  tunnels?: PlayitTunnel[];
}

export interface AddonDetail {
  file: string;
  dir: string;
  known: boolean;
  projectId?: string;
  slug?: string;
  title?: string;
  description?: string;
  iconUrl?: string;
  projectType?: string;
  link?: string;
  downloads?: number;
  sourceUrl?: string;
  issuesUrl?: string;
  license?: string;
  versionId?: string;
  versionNumber?: string;
  versionName?: string;
  versionType?: string;
  published?: string;
  versionLink?: string;
}

export interface Member {
  projectId: string;
  userId: string;
  username: string;
  role: Role;
}

export interface Project {
  id: string;
  name: string;
  kind: "server" | "network";
  description: string;
  ownerId: string;
  createdAt: number;
  myRole: Role;
  voicechat: boolean;
  members?: Member[];
  servers: Server[];
  groups: Group[];
  settings?: ProjectSettings;
  problems?: string[];
}

export interface ProjectSettings {
  statusPublic: boolean;
  statusSlug: string;
  motd: string;
}

export interface Group {
  id: string;
  projectId: string;
  name: string;
  slug: string;
  software: string;
  version: string;
  memoryMB: number;
  minInstances: number;
  maxInstances: number;
  playersPerInstance: number;
  idleMinutes: number;
  inTry: boolean;
  dynamic: boolean;
  plugins: string[];
  instances: string[];
  active: number;
  players: number;
  note?: string;
}

export type Range = "1h" | "6h" | "24h" | "7d";

export interface Series {
  range: Range;
  times: number[];
  cpu: (number | null)[];
  memory: (number | null)[];
  players: (number | null)[];
  peakPlayers: number;
  avgPlayers: number;
  avgCpu: number;
  peakCpu: number;
  peakMemory: number;
  samples: number;
}

export interface Metrics {
  series: Series;
  players: number;
  maxPlayers?: number;
  names?: string[];
}

export interface SeenPlayer {
  name: string;
  firstSeen: number;
  lastSeen: number;
  seconds: number;
  online: boolean;
}

export interface SoftwareInfo {
  id: string;
  name: string;
  kind: "server" | "proxy";
  extensions: "plugins" | "mods" | "none";
  defaultPort: number;
}

export interface FileEntry {
  name: string;
  dir: boolean;
  symlink?: boolean;
  size: number;
  modified: number;
  mode: string;
}

export interface Addon {
  file: string;
  dir: string;
  size: number;
  modified: number;
  enabled: boolean;
  name: string;
  id?: string;
  version: string;
  kind: string;
  sha1: string;
}

export interface PlayerInfo {
  online: number;
  max: number;
  names: string[];
  version: string;
  motd: string;
  latencyMs: number;
}

export interface SyncResult {
  applied: string[];
  pending: string[];
  failed: string[];
  warnings?: string[];
}

export interface ModrinthHit {
  project_id: string;
  slug: string;
  title: string;
  description: string;
  author: string;
  downloads: number;
  icon_url: string;
}

export interface UpdateEntry {
  file: string;
  dir: string;
  name: string;
  current: string;
  latest?: string;
  latestFile?: string;
  known: boolean;
  updateAvailable: boolean;
}

export interface LogLine {
  seq: number;
  time: number;
  text: string;
}

export interface Me extends User {
  totpEnabled: boolean;
  recoveryCodes: number;
}

export type StepAction = "backup" | "command" | "restart" | "start" | "stop" | "wait";

export interface ScheduleStep {
  action: StepAction;
  value?: string;
}

export interface Schedule {
  id: string;
  projectId: string;
  serverId: string;
  serverName: string;
  name: string;
  steps: ScheduleStep[];
  kind: "interval" | "daily" | "weekly";
  everyMinutes: number;
  atMinute: number;
  weekdays: number;
  warnMinutes: number;
  enabled: boolean;
  lastRun: number;
  lastStatus: "" | "ok" | "failed" | "skipped";
  lastMessage: string;
  nextRun: number;
}

export interface ProjectEvent {
  id: number;
  time: number;
  projectId: string;
  serverId?: string;
  kind: string;
  level: "info" | "warn" | "error";
  message: string;
}

export interface Webhook {
  id: string;
  name: string;
  url: string;
  kind: "discord" | "generic";
  events: string[];
  enabled: boolean;
  lastStatus: string;
}

export interface AuditEntry {
  id: number;
  time: number;
  userId?: string;
  username: string;
  action: string;
  target?: string;
  status: number;
  ip?: string;
}

export interface ApiKeyInfo {
  id: string;
  name: string;
  prefix: string;
  createdAt: number;
  lastUsed: number;
  expiresAt: number;
}

export interface SessionInfo {
  id: string;
  createdAt: number;
  expiresAt: number;
  userAgent: string;
  ip: string;
  current: boolean;
}

export interface ListedPlayer {
  uuid: string;
  name: string;
  level?: number;
  reason?: string;
  created?: string;
  source?: string;
  expires?: string;
}

export interface PlayerLists {
  whitelist: ListedPlayer[];
  ops: ListedPlayer[];
  banned: ListedPlayer[];
  bannedIps: { ip: string; reason?: string; created?: string }[];
}

export interface PublicStatus {
  name: string;
  description?: string;
  online: boolean;
  address?: string;
  players: number;
  maxPlayers?: number;
  version?: string;
  servers: { name: string; online: boolean; players: number; maxPlayers?: number }[];
  updated: number;
}

// --- rechte
// Einzelrechte pro Projektmitglied. Die Rollen sind Vorlagen; die echte Prüfung macht das Backend (internal/panel/rechte.go).

export type Permission =
  | "console.read"
  | "console.command"
  | "power"
  | "files.read"
  | "files.write"
  | "sftp"
  | "backups.read"
  | "backups.create"
  | "backups.restore"
  | "backups.delete"
  | "addons"
  | "players"
  | "settings"
  | "servers"
  | "schedules"
  | "webhooks"
  | "members"
  | "network";

export interface PermissionInfo {
  id: Permission;
  group: string;
  label: string;
  description: string;
  /** Kleinste Rolle, deren Vorlage das Recht enthält. */
  role: Role;
}

export interface PermissionCatalog {
  permissions: PermissionInfo[];
  roles: Record<Role, Permission[]>;
}

// Ergänzt Project und Member (Interface-Zusammenführung), damit alles zu den Rechten an einer Stelle steht.
export interface Project {
  myPermissions: Permission[];
}

export interface Member {
  permissions: Permission[];
  customPermissions: boolean;
}

/** Antwort von GET /servers/{id}. */
export interface ServerAccess {
  server: Server;
  myRole: Role;
  myPermissions: Permission[];
}

/** can sagt, ob alle genannten Rechte in der Liste stehen. */
export function can(permissions: Permission[] | undefined, ...needed: Permission[]): boolean {
  return needed.every((permission) => permissions?.includes(permission) ?? false);
}

// --- sftp

export interface SftpKey {
  id: string;
  name: string;
  publicKey: string;
  fingerprint: string;
  createdAt: number;
  lastUsed: number;
}

/** SFTP-Zugang des angemeldeten Benutzers (GET /me/sftp). Der Passwort-Hash kommt nie an. */
export interface SftpAccount {
  passwordSet: boolean;
  passwordLogin: boolean;
  keys: SftpKey[];
}

/** Karte „SFTP“ im Dateien-Tab (GET /servers/{id}/sftp). */
export interface ServerSftp {
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  fingerprint: string;
  readOnly: boolean;
  operatorsReadOnly: boolean;
  canConfigure: boolean;
  passwordReady: boolean;
  keyCount: number;
}

// --- backups
export type BackupState = "running" | "done" | "failed";

/** Ein Backup, wie das Panel es aus Node (lokal) und Backup-Ziel (extern) zusammensetzt. */
export interface ServerBackup {
  name: string;
  label?: string;
  size: number;
  created: number;
  status: BackupState;
  local: boolean;
  remote: boolean;
  upload?: BackupState;
  progress?: number;
  error?: string;
  locked: boolean;
  otherTarget?: boolean;
}

export interface BackupSettings {
  exclude: string[];
  remoteKeep: number;
  targetConfigured: boolean;
}

/** Externes, S3-kompatibles Backup-Ziel eines Projekts. Das Secret kommt nie zurück, nur hasSecret. */
export interface BackupTarget {
  configured: boolean;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  accessKey: string;
  pathStyle: boolean;
  hasSecret: boolean;
  updatedAt?: number;
}

// Ergänzt ScheduleStep: Ein Backup-Schritt kann zusätzlich zum Backup-Ziel hochladen.
export interface ScheduleStep {
  remote?: boolean;
}

// --- anmeldung

/** Ergänzt AuthStatus (Zusammenführung der Schnittstelle): welche Anmeldewege das Panel anbietet. */
export interface AuthStatus {
  /** false, wenn LODESTONE_OIDC_ONLY die Passwort-Anmeldung abschaltet */
  passwordLogin?: boolean;
  passkeys?: boolean;
  oidc?: { name: string };
}

export type SecondFactor = "totp" | "passkey";

export interface LoginStep {
  needsTotp?: boolean;
  ticket?: string;
  secondFactor?: SecondFactor[];
}

export interface Passkey {
  id: string;
  name: string;
  createdAt: number;
  lastUsed: number;
}

export interface OidcLink {
  issuer: string;
  email: string;
  autoCreated: boolean;
  createdAt: number;
  lastLogin: number;
}

export interface OidcAccount {
  configured: boolean;
  name?: string;
  link?: OidcLink;
  hasPassword: boolean;
  passwordLogin: boolean;
  /** Bis wann (Unix-ms) ein Konto ohne Passwort ein erstes festlegen darf (kurz nach der Anmeldung); 0 = nicht. */
  firstPasswordUntil: number;
}

interface PasskeyDescriptor {
  type: string;
  id: string;
  transports?: string[];
}

/** Optionen für navigator.credentials.create, wie das Panel sie schickt (Binärfelder als base64url). */
export interface PasskeyCreationOptions {
  publicKey: {
    challenge: string;
    rp: { id?: string; name: string };
    user: { id: string; name: string; displayName: string };
    excludeCredentials?: PasskeyDescriptor[];
    [key: string]: unknown;
  };
}

/** Optionen für navigator.credentials.get (Binärfelder als base64url). */
export interface PasskeyAssertionOptions {
  publicKey: {
    challenge: string;
    rpId?: string;
    allowCredentials?: PasskeyDescriptor[];
    [key: string]: unknown;
  };
}

export interface PasskeyCeremony<T> {
  flow: string;
  options: T;
}

// --- serverops: Speicherplatz, Kopie und Umzug

export interface DiskUsage {
  bytes: number;
  files: number;
  backups: number;
  measuredAt: number;
}

export interface StorageInfo extends DiskUsage {
  limitMB: number;
  blocked: boolean;
  measuring: boolean;
  free: number;
  total: number;
}

export interface NodeStorageServer {
  id: string;
  name: string;
  projectId: string;
  projectName: string;
  bytes: number;
  backups: number;
  limitMB: number;
  measuredAt: number;
}

export interface NodeStorage {
  online: boolean;
  error?: string;
  free: number;
  total: number;
  used: number;
  servers?: NodeStorageServer[];
}

export type ServerOpState = "running" | "done" | "failed" | "canceled";

export interface ServerOp {
  id: string;
  projectId: string;
  serverId?: string;
  kind: "clone" | "migrate";
  state: ServerOpState;
  label: string;
  step: string;
  done: number;
  total: number;
  message: string;
  warnings: string[];
  sourceNodeId?: string;
  targetNodeId?: string;
  resultServerId?: string;
  cancelRequested: boolean;
  createdAt: number;
  updatedAt: number;
}

// --- welten

export interface ServerProperty {
  key: string;
  value: string;
}

export interface ServerProperties {
  exists: boolean;
  entries: ServerProperty[];
  /** Schlüssel, die Lodestone selbst verwaltet, mit dem Grund. */
  locked: Record<string, string>;
}

export type WorldKind = "overworld" | "nether" | "end" | "other";

export interface World {
  name: string;
  kind: WorldKind;
  main: boolean;
  size: number;
  modified: number;
  datapacks: number;
}

export interface WorldList {
  levelName: string;
  seed: string;
  worlds: World[];
}

export interface Datapack {
  name: string;
  dir: boolean;
  size: number;
  modified: number;
}

/** Eigene Ordnung eines Projekts (pro Benutzer). position 0 = keine eigene Reihenfolge. */
export interface ProjectPref {
  projectId: string;
  favorite: boolean;
  position: number;
  tags: string[];
}

// --- automatik: Schlafmodus, Konsolen-Auslöser, Discord-Bot, Absturzanalyse

export interface SleepStatus {
  enabled: boolean;
  idleMinutes: number;
  /** false: Der Node kann die Ports der Maschine nicht selbst belegen (läuft im Container). */
  supported: boolean;
  /** Der Server schläft, der Node wartet auf seinem Port auf Spieler. */
  listening: boolean;
  port?: number;
  idleSince?: number;
  lastWake?: number;
  lastSleep?: number;
  problem?: string;
  /** false für Backend-Server hinter dem Proxy und Gruppen-Instanzen. */
  allowed: boolean;
  reason?: string;
}

export type TriggerLevel = "info" | "warn" | "error";

export interface ConsoleTrigger {
  id: string;
  projectId: string;
  /** leer = alle Server des Projekts */
  serverId: string;
  name: string;
  pattern: string;
  level: TriggerLevel;
  /** leer = kein Zeitplan */
  scheduleId: string;
  enabled: boolean;
  lastMatch: number;
  lastLine: string;
  createdAt: number;
}

export type DiscordAction = "status" | "players" | "start" | "stop" | "restart";

export interface DiscordRule {
  kind: "role" | "user";
  id: string;
  label?: string;
  actions: DiscordAction[];
}

export interface DiscordConfig {
  configured: boolean;
  applicationId: string;
  publicKey: string;
  hasToken: boolean;
  rules: DiscordRule[];
  enabled: boolean;
  registeredAt: number;
  interactionsPath: string;
}

export interface CrashAnalysis {
  found: boolean;
  report?: string;
  suspects: { modId: string; name?: string; reason: "suspected" | "stacktrace" | "mixin" }[];
  missing: { mod: string; requires: string; version?: string }[];
  java?: { required: number; current?: number };
  mixins: string[];
  summary?: string;
}

// --- inhalte
// Weitere Quellen für Plugins und Mods (Hangar, Spigot, CurseForge) und Bausteine (Geyser, BlueMap).

export type SourceId = "modrinth" | "hangar" | "spigot" | "curseforge";

export interface SourceInfo {
  id: SourceId;
  name: string;
  available: boolean;
  reason?: string;
}

export interface SourceHit {
  id: string;
  slug?: string;
  title: string;
  description: string;
  author?: string;
  downloads: number;
  iconUrl?: string;
  link?: string;
  installable: boolean;
  note?: string;
}

export interface SourceVersion {
  id: string;
  name: string;
  versionNumber: string;
  type: string;
  published: string;
  gameVersions: string[];
  downloadable: boolean;
  reason?: string;
}

export type BlockId = "geyser" | "bluemap";

export interface BlockInfo {
  id: BlockId;
  name: string;
  description: string;
  available: boolean;
  reason?: string;
  enabled: boolean;
  managed?: "network";
  address?: string;
  url?: string;
  note?: string;
}

// Ergänzungen bestehender Antworten (TypeScript führt die Schnittstellen zusammen)
export interface AddonDetail {
  source?: Exclude<SourceId, "modrinth">;
}

export interface UpdateEntry {
  source?: Exclude<SourceId, "modrinth">;
}

// --- nodelink
// Nodes, die sich selbst zum Panel verbinden (Websocket-Tunnel statt direkter Adresse).

export interface NodeInfo {
  /** Der Node verbindet sich selbst zum Panel und hat deshalb keine Adresse. */
  linked?: boolean;
}

/** Verbindungszustand eines solchen Nodes (GET /nodes/link, nur Administratoren). */
export interface NodeLink {
  nodeId: string;
  connected: boolean;
  connectedAt?: number;
  remote?: string;
  version?: string;
  /** Unverschlüsselte Verbindung (ws:// statt wss://). */
  insecure: boolean;
  joinedAt?: number;
  /** Gesetzt, solange ein Join-Token offen ist. */
  joinExpiresAt?: number;
}

/** Einmaliges Join-Token (POST /nodes/link, POST /nodes/{id}/join-token). Es erscheint nur in dieser Antwort. */
export interface JoinToken {
  node: NodeInfo;
  token: string;
  expiresAt: number;
}
