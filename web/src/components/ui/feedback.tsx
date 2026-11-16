"use client";

import { AlertTriangle, CheckCircle2, X } from "lucide-react";
import { createPortal } from "react-dom";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type Context, type ReactNode } from "react";
import { ApiError } from "@/lib/api";
import { Button, cx, usePresence } from "./primitives";

// ---------------------------------------------------------------- Meldungen

interface Toast {
  id: number;
  tone: "good" | "bad";
  text: string;
  leaving?: boolean;
}

const TOAST_TIME = 4500;
const TOAST_EXIT = 160;

interface ToastApi {
  success: (text: string) => void;
  error: (text: string) => void;
  /** Führt eine Aktion aus und zeigt Erfolg oder die Fehlermeldung an. Gibt zurück, ob sie funktioniert hat. */
  run: <T,>(action: () => Promise<T>, success?: string) => Promise<boolean>;
}

// Der Kontext hängt an globalThis. Dadurch teilen sich alle Kopien dieses Moduls denselben Kontext, auch wenn ein
// offener Tab nach einem Update gemischte Dateien alter und neuer Versionen geladen hat.
const CONTEXT_KEY = "__lodestoneToastContext";
const holder = globalThis as unknown as Record<string, Context<ToastApi | null> | undefined>;
const ToastContext = (holder[CONTEXT_KEY] ??= createContext<ToastApi | null>(null));

/** Notlösung ohne Anzeige: die Seite soll weiter funktionieren, auch wenn der Provider fehlt. */
const fallback: ToastApi = {
  success: (text) => console.info(text),
  error: (text) => console.error(text),
  run: async (action, success) => {
    try {
      await action();
      if (success) console.info(success);
      return true;
    } catch (error) {
      console.error(error);
      return false;
    }
  },
};

export function useToast(): ToastApi {
  return useContext(ToastContext) ?? fallback;
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  // Erst ausblenden, dann entfernen
  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.map((toast) => (toast.id === id ? { ...toast, leaving: true } : toast)));
    setTimeout(() => setToasts((current) => current.filter((toast) => toast.id !== id)), TOAST_EXIT);
  }, []);

  const push = useCallback(
    (tone: Toast["tone"], text: string) => {
      const id = Date.now() + Math.random();
      setToasts((current) => [...current, { id, tone, text }]);
      setTimeout(() => dismiss(id), TOAST_TIME);
    },
    [dismiss],
  );

  const api = useMemo<ToastApi>(
    () => ({
      success: (text) => push("good", text),
      error: (text) => push("bad", text),
      run: async (action, success) => {
        try {
          await action();
          if (success) push("good", success);
          return true;
        } catch (error) {
          push("bad", error instanceof ApiError || error instanceof Error ? error.message : "Unbekannter Fehler");
          return false;
        }
      },
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed bottom-5 right-5 z-[60] flex w-80 flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            role={toast.tone === "bad" ? "alert" : "status"}
            onClick={() => dismiss(toast.id)}
            title="Schließen"
            className={cx(
              "pointer-events-auto relative flex cursor-pointer items-start gap-3 overflow-hidden rounded-md border border-black/50 bg-raised py-3 pl-5 pr-4 text-sm shadow-panel",
              toast.leaving ? "pointer-events-none animate-toast-out" : "animate-toast-in",
            )}
          >
            <span aria-hidden className={cx("absolute inset-y-0 left-0 w-[3px]", toast.tone === "good" ? "bg-good" : "bg-bad")} />
            {toast.tone === "good" ? (
              <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-good" />
            ) : (
              <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-bad" />
            )}
            <span className="flex-1">{toast.text}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

// ---------------------------------------------------------------- Dialoge

export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  wide,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
}) {
  const { mounted, leaving } = usePresence(open);
  // Beim Ausblenden den letzten Inhalt zeigen: Die Seite hat die Daten dafür oft schon geleert
  const shown = useRef({ title, description, children, footer });
  if (open) shown.current = { title, description, children, footer };

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => event.key === "Escape" && onClose();
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onClose]);

  if (!mounted) return null;
  const content = shown.current;
  // Über ein Portal direkt unter body, damit weder Animationen noch Scroll-Container die Position verschieben
  return createPortal(
    <div className={cx("fixed inset-0 z-50 flex items-center justify-center p-4", leaving && "pointer-events-none")}>
      <div className={cx("absolute inset-0 bg-black/70", leaving ? "animate-overlay-out" : "animate-overlay-in")} onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={content.title}
        className={cx(
          "relative max-h-[calc(100vh-2rem)] w-full overflow-y-auto rounded-xl border border-black/50 bg-surface shadow-panel",
          wide ? "max-w-2xl" : "max-w-md",
          leaving ? "animate-pop-out" : "animate-pop-in",
        )}
      >
        <div className="flex items-start justify-between gap-4 border-b border-white/[0.06] px-5 py-4">
          <div>
            <h3 className="font-display text-base font-medium tracking-wide">{content.title}</h3>
            {content.description && <p className="mt-0.5 text-xs text-muted">{content.description}</p>}
          </div>
          <button onClick={onClose} className="rounded-md p-1 text-muted transition duration-100 hover:bg-white/[0.06] hover:text-ink active:scale-90" aria-label="Schließen">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="space-y-4 px-5 py-4">{content.children}</div>
        {content.footer && <div className="flex justify-end gap-2 border-t border-white/[0.06] bg-black/15 px-5 py-3">{content.footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  text,
  confirm = "Bestätigen",
  danger,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void> | void;
  title: string;
  text: string;
  confirm?: string;
  danger?: boolean;
}) {
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant={danger ? "danger" : "primary"}
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await onConfirm();
              } finally {
                setBusy(false);
                onClose();
              }
            }}
          >
            {confirm}
          </Button>
        </>
      }
    >
      <p className="text-sm text-muted">{text}</p>
    </Dialog>
  );
}
