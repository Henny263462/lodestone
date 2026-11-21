"use client";

import { useEffect, useSyncExternalStore } from "react";
import { mutate } from "swr";
import { apiBase } from "./api";

// Live-Aktualisierung: Das Panel meldet über GET /api/live (Server-Sent Events), *dass* sich etwas geändert hat. Die
// Oberfläche lädt dann genau die betroffenen Abfragen neu; die Daten selbst kommen wie immer über useApi und dessen
// Rechteprüfung. Die Abfrage-Intervalle bleiben als Rückfall, falls der Kanal einmal nicht steht.

export type LiveTopic = "server" | "players" | "backups" | "events" | "ops" | "project" | "node" | "resync";

export interface LiveUpdate {
  topic: LiveTopic;
  projectId?: string;
  serverId?: string;
  nodeId?: string;
  area?: string;
}

/** Liegt key unter prefix (gleich, Unterpfad oder mit Suchparametern)? */
function under(key: string, prefix: string): boolean {
  return key === prefix || key.startsWith(`${prefix}/`) || key.startsWith(`${prefix}?`);
}

/** Sagt, ob eine Meldung den SWR-Schlüssel (Pfad ohne /api) betrifft. */
export function affects(update: LiveUpdate, key: string): boolean {
  const server = update.serverId ? `/servers/${update.serverId}` : null;
  const project = update.projectId ? `/projects/${update.projectId}` : null;
  switch (update.topic) {
    case "resync":
      return true;
    case "server":
      // Zustand, Einstellungen oder ein Bereich des Servers; Listen zeigen den Zustand mit an
      return (!!server && under(key, server)) || (!!project && key === project) || key === "/projects";
    case "players":
      return (!!server && key.startsWith(`${server}/player`)) || (!!project && under(key, `${project}/live-players`));
    case "backups":
      return !!server && key.startsWith(`${server}/backup`);
    case "events":
      return (!!project && under(key, `${project}/events`)) || under(key, "/events/recent");
    case "ops":
      return (!!project && (under(key, `${project}/server-ops`) || key === project)) || key === "/projects";
    case "project":
      return (!!project && under(key, project)) || key === "/projects";
    case "node":
      // Ein Node fällt aus oder kommt wieder: Nodes, Projekte und Server zeigen das an
      return under(key, "/nodes") || key === "/projects" || /^\/(projects|servers)\/[^/?]+$/.test(key);
  }
}

// ---------------------------------------------------------------- Verbindungszustand

let connected = false;
const listeners = new Set<() => void>();

function setConnected(value: boolean) {
  if (connected === value) return;
  connected = value;
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Steht der Live-Kanal gerade? */
export function useLiveConnected(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => connected,
    () => false,
  );
}

/**
 * Abfrage-Intervall mit Live-Kanal als Hauptweg: steht er, reicht ein langes Intervall als Rückfall (`live`),
 * sonst gilt das kurze (`fallback`), wie vor dem Live-Kanal.
 */
export function usePoll(live: number, fallback: number): number {
  return useLiveConnected() ? live : fallback;
}

// ---------------------------------------------------------------- Verbindung

const COALESCE_MS = 250; // Meldungen kurz sammeln, damit eine Welle nur ein Neuladen auslöst
const RETRY_MS = 30_000; // nach einer Ablehnung (z. B. zu viele Tabs) erst später neu versuchen
const HIDDEN_CLOSE_MS = 60_000; // versteckte Tabs geben ihre Verbindung nach einer Minute frei

/** Hält den Live-Kanal offen, solange die Oberfläche angemeldet ist. Einmal in der Shell einbinden. */
export function useLiveUpdates(): void {
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    let source: EventSource | null = null;
    let pending: LiveUpdate[] = [];
    let flushTimer: ReturnType<typeof setTimeout> | undefined;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let hiddenTimer: ReturnType<typeof setTimeout> | undefined;
    let opened = false;
    let stopped = false;

    const flush = () => {
      flushTimer = undefined;
      const list = pending;
      pending = [];
      if (list.length === 0) return;
      void mutate((key) => typeof key === "string" && list.some((update) => affects(update, key)));
    };
    const queue = (update: LiveUpdate) => {
      pending.push(update);
      flushTimer ??= setTimeout(flush, COALESCE_MS);
    };

    const close = () => {
      source?.close();
      source = null;
      setConnected(false);
    };

    const connect = () => {
      if (stopped || source) return;
      clearTimeout(retryTimer);
      const next = new EventSource(`${apiBase()}/api/live`, { withCredentials: true });
      source = next;
      next.onopen = () => {
        setConnected(true);
        // Nach einer Unterbrechung kann etwas verpasst worden sein: alles Sichtbare einmal neu laden
        if (opened) queue({ topic: "resync" });
        opened = true;
      };
      next.onmessage = (event) => {
        try {
          const update = JSON.parse(event.data) as LiveUpdate;
          if (update.topic) queue(update);
        } catch {
          // Unlesbare Meldung: ignorieren, das Intervall deckt es ab
        }
      };
      // Das Panel beendet den Kanal, wenn die Anmeldung nicht mehr gilt; die nächste Abfrage führt zur Anmeldung
      next.addEventListener("end", () => {
        close();
        queue({ topic: "resync" });
      });
      next.onerror = () => {
        setConnected(false);
        // Offen bleibende Fehler versucht der Browser selbst neu; nach einer Ablehnung (401, 429) ist er geschlossen
        if (next.readyState === EventSource.CLOSED && source === next) {
          source = null;
          retryTimer = setTimeout(connect, RETRY_MS);
        }
      };
    };

    const visibility = () => {
      clearTimeout(hiddenTimer);
      if (document.visibilityState === "hidden") {
        hiddenTimer = setTimeout(close, HIDDEN_CLOSE_MS);
      } else if (!source) {
        connect();
      }
    };

    connect();
    document.addEventListener("visibilitychange", visibility);
    return () => {
      stopped = true;
      document.removeEventListener("visibilitychange", visibility);
      clearTimeout(flushTimer);
      clearTimeout(retryTimer);
      clearTimeout(hiddenTimer);
      close();
    };
  }, []);
}
