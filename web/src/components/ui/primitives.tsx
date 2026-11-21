"use client";

import Link from "next/link";
import { Check, ChevronDown, ChevronRight, Loader2, Search } from "lucide-react";
import { createPortal } from "react-dom";
import {
  Fragment,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ComponentType,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import { t } from "@/lib/i18n";

export function cx(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

/**
 * Hält ein Element nach dem Schließen noch kurz im Baum, damit es ausblenden kann. `leaving` ist während dieser Zeit wahr;
 * die Dauer passt zu den *-out-Animationen in tailwind.config.ts.
 */
export function usePresence(open: boolean, duration = 140): { mounted: boolean; leaving: boolean } {
  const [mounted, setMounted] = useState(open);
  useEffect(() => {
    if (open) {
      setMounted(true);
      return;
    }
    const timer = setTimeout(() => setMounted(false), duration);
    return () => clearTimeout(timer);
  }, [open, duration]);
  return { mounted: open || mounted, leaving: !open && mounted };
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md" | "lg" | "icon" | "icon-sm";
  loading?: boolean;
};

// Knöpfe sind kleine Blöcke: helle Oberkante, dunkle Unterkante; beim Drücken sinken sie ein (wie im Spielmenü)
const buttonVariants = {
  primary: "bg-accent font-display text-accent-ink text-pixel-shadow shadow-bevel hover:brightness-110 active:shadow-bevel-pressed",
  secondary: "border border-black/40 bg-raised font-display text-ink text-pixel-shadow shadow-bevel hover:brightness-125 active:shadow-bevel-pressed",
  ghost: "text-muted hover:bg-white/[0.06] hover:text-ink",
  danger: "border border-bad/50 bg-bad/20 font-display text-bad shadow-bevel hover:bg-bad/30 active:shadow-bevel-pressed",
};

const buttonSizes = {
  sm: "h-8 px-3 text-xs",
  md: "h-9 px-4 text-sm",
  lg: "h-11 px-5 text-sm",
  icon: "h-10 w-10",
  "icon-sm": "h-8 w-8",
};

export function Button({ variant = "secondary", size = "md", loading, className, children, disabled, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      disabled={disabled || loading}
      className={cx(
        "inline-flex select-none items-center justify-center gap-2 rounded-md font-medium tracking-wide transition duration-100 ease-out active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50 disabled:active:translate-y-0",
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
    >
      {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      {children}
    </button>
  );
}

export function Card({ className, children, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div {...rest} className={cx("rounded-xl border border-white/[0.07] bg-surface shadow-panel", className)}>
      {children}
    </div>
  );
}

export function CardHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-white/[0.06] px-5 py-4">
      <div className="min-w-0">
        <h2 className="font-display text-sm font-medium tracking-wide text-ink">{title}</h2>
        {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
      </div>
      {action}
    </div>
  );
}

type Tone = "neutral" | "good" | "warn" | "bad" | "info" | "accent";

const tones: Record<Tone, string> = {
  neutral: "border-white/[0.1] bg-white/[0.05] text-muted",
  good: "border-good/30 bg-good/10 text-good",
  warn: "border-warn/30 bg-warn/10 text-warn",
  bad: "border-bad/30 bg-bad/10 text-bad",
  info: "border-info/30 bg-info/10 text-info",
  accent: "border-accent/30 bg-accent/10 text-accent",
};

export function Badge({ tone = "neutral", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span className={cx("inline-flex items-center gap-1.5 whitespace-nowrap rounded-sm border px-1.5 py-0.5 text-[10.5px] font-medium", tones[tone], className)}>
      {children}
    </span>
  );
}

/** Zustandspunkt: ein kleines Quadrat, wie eine Lampe im Spiel. */
export function Dot({ tone }: { tone: "good" | "bad" | "warn" | "neutral" | "info" }) {
  const colors = { good: "bg-good", bad: "bg-bad", warn: "bg-warn", neutral: "bg-muted/70", info: "bg-info" };
  return <span aria-hidden className={cx("inline-block h-1.5 w-1.5 shrink-0", colors[tone])} />;
}

export function Switch({ checked, onChange, disabled, label }: { checked: boolean; onChange: (value: boolean) => void; disabled?: boolean; label?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        "relative h-6 w-11 shrink-0 rounded-md border shadow-sunken transition duration-100 disabled:opacity-50",
        checked ? "border-black/40 bg-accent" : "border-white/[0.1] bg-black/30",
      )}
    >
      <span
        className={cx(
          "absolute top-0.5 h-[18px] w-[18px] rounded-sm bg-ink shadow-bevel transition-[left] duration-150 ease-snappy",
          checked ? "left-[21px]" : "left-0.5",
        )}
      />
    </button>
  );
}

export interface Crumb {
  label: string;
  href?: string;
}

/** Pfad über dem Seitentitel: zeigt, wo man ist, und führt eine Ebene zurück. */
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label={t("Pfad")} className="mb-2 flex min-w-0 items-center gap-1 text-xs text-muted">
      {items.map((item, index) => (
        <Fragment key={`${item.label}-${index}`}>
          {index > 0 && <ChevronRight className="h-3 w-3 shrink-0 text-muted/60" />}
          {item.href ? (
            <Link href={item.href} className="truncate transition hover:text-ink">
              {item.label}
            </Link>
          ) : (
            <span className="truncate text-ink/80">{item.label}</span>
          )}
        </Fragment>
      ))}
    </nav>
  );
}

export function PageHeader({
  title,
  description,
  action,
  crumbs,
  icon,
  meta,
}: {
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  /** Pfad über dem Titel, z. B. Übersicht → Survival */
  crumbs?: Crumb[];
  /** Bild links vom Titel (BlockIcon, PixelAvatar) */
  icon?: ReactNode;
  /** Zeile mit Abzeichen unter dem Titel (Zustand, Software, Adresse) */
  meta?: ReactNode;
}) {
  return (
    <div className="mb-6">
      {crumbs && crumbs.length > 0 && <Breadcrumbs items={crumbs} />}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-4">
          {icon}
          <div className="min-w-0">
            <h1 className="truncate font-display text-2xl font-medium tracking-wide text-pixel-shadow">{title}</h1>
            {description && <p className="mt-1 text-[13px] text-muted">{description}</p>}
            {meta && <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted">{meta}</div>}
          </div>
        </div>
        {action && <div className="flex flex-wrap items-center gap-2">{action}</div>}
      </div>
    </div>
  );
}

export interface PageTab {
  id: string;
  label: string;
  href: string;
  icon?: ReactNode;
  count?: number;
}

/**
 * Bereiche einer Seite (Projekt, Server) als Leiste mit Links. Der aktive Bereich steht in der Adresse, damit Neuladen,
 * Zurück und geteilte Links an derselben Stelle landen. Für Umschalter innerhalb einer Karte gibt es `Tabs`.
 */
export function PageTabs({ items, value }: { items: PageTab[]; value: string }) {
  return (
    <nav aria-label={t("Bereiche")} className="no-scrollbar -mx-4 mb-6 flex gap-1 overflow-x-auto border-b border-white/[0.07] px-4 lg:mx-0 lg:px-0">
      {items.map((item) => {
        const active = item.id === value;
        return (
          <Link
            key={item.id}
            href={item.href}
            aria-current={active ? "page" : undefined}
            className={cx(
              "relative flex shrink-0 items-center gap-2 whitespace-nowrap px-3 pb-3 pt-2 font-display text-[13px] tracking-wide transition duration-100",
              active ? "text-ink" : "text-muted hover:text-ink",
            )}
          >
            {item.icon && <span className={cx("shrink-0", active ? "text-accent" : "text-muted")}>{item.icon}</span>}
            {item.label}
            {item.count !== undefined && item.count > 0 && (
              <span className="rounded-sm bg-white/[0.07] px-1.5 text-[10.5px] tabular-nums text-muted">{item.count}</span>
            )}
            {active && <span aria-hidden className="absolute inset-x-2 -bottom-px h-[3px] bg-accent" />}
          </Link>
        );
      })}
    </nav>
  );
}

export type StatTone = "green" | "blue" | "violet" | "amber" | "red" | "neutral";

// Kennzahlen: dunkle Karte, die Farbe tragen nur das Symbol und der Streifen links
const statTones: Record<StatTone, { bar: string; icon: string; value: string }> = {
  green: { bar: "bg-good", icon: "border-good/30 bg-good/10 text-good", value: "text-ink" },
  blue: { bar: "bg-info", icon: "border-info/30 bg-info/10 text-info", value: "text-ink" },
  violet: { bar: "bg-accent", icon: "border-accent/30 bg-accent/10 text-accent", value: "text-ink" },
  amber: { bar: "bg-warn", icon: "border-warn/30 bg-warn/10 text-warn", value: "text-ink" },
  red: { bar: "bg-bad", icon: "border-bad/30 bg-bad/10 text-bad", value: "text-bad" },
  neutral: { bar: "bg-white/[0.12]", icon: "border-white/[0.1] bg-white/[0.05] text-muted", value: "text-ink" },
};

export function StatCard({
  label,
  value,
  hint,
  icon,
  tone = "neutral",
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  icon: ReactNode;
  tone?: StatTone;
}) {
  const style = statTones[tone];
  return (
    <div className="relative overflow-hidden rounded-xl border border-white/[0.07] bg-surface p-5 shadow-panel">
      <span aria-hidden className={cx("absolute inset-y-0 left-0 w-[3px]", style.bar)} />
      <div className="flex items-center justify-between gap-3">
        <p className="text-[13px] text-muted">{label}</p>
        <span className={cx("grid h-8 w-8 place-items-center rounded-md border", style.icon)}>{icon}</span>
      </div>
      <p className={cx("mt-3 font-display text-[28px] font-normal leading-none tracking-wide tabular-nums", style.value)}>{value}</p>
      {hint && <p className="mt-2.5 truncate text-xs text-muted">{hint}</p>}
    </div>
  );
}

export function EmptyState({ icon, title, text, action }: { icon: ReactNode; title: string; text?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center px-6 py-14 text-center">
      <div className="mb-3 rounded-md border border-white/[0.08] bg-black/20 p-3 text-muted shadow-sunken">{icon}</div>
      <p className="font-display text-sm font-medium tracking-wide">{title}</p>
      {text && <p className="mt-1 max-w-sm text-xs text-muted">{text}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

export function Spinner() {
  return <Loader2 className="h-5 w-5 animate-spin text-muted" />;
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (value: T) => void; items: { id: T; label: string; count?: number }[] }) {
  const list = useRef<HTMLDivElement>(null);
  const [marker, setMarker] = useState<{ left: number; width: number; animate: boolean } | null>(null);

  // Der Marker gleitet unter den aktiven Tab. Beim ersten Messen springt er ohne Animation an seinen Platz.
  useLayoutEffect(() => {
    const measure = () => {
      const button = list.current?.querySelector<HTMLElement>('[aria-selected="true"]');
      if (!button) return setMarker(null);
      setMarker((previous) => ({ left: button.offsetLeft, width: button.offsetWidth, animate: previous !== null }));
    };
    measure();
    const observer = new ResizeObserver(measure);
    if (list.current) observer.observe(list.current);
    return () => observer.disconnect();
  }, [value, items.length]);

  // Auf schmalen Bildschirmen scrollt die Leiste seitlich, statt die Seite zu verbreitern
  return (
    <div className="no-scrollbar max-w-full overflow-x-auto">
      <div ref={list} role="tablist" className="relative inline-flex rounded-md border border-white/[0.08] bg-black/25 p-0.5 shadow-sunken">
        {marker && (
          <span
            aria-hidden
            className={cx("absolute inset-y-0.5 rounded-sm bg-raised shadow-bevel", marker.animate && "transition-[left,width] duration-200 ease-snappy")}
            style={{ left: marker.left, width: marker.width }}
          />
        )}
        {items.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={value === item.id}
            onClick={() => onChange(item.id)}
            className={cx(
              "relative whitespace-nowrap rounded-sm px-3 py-1.5 text-xs font-medium transition duration-150",
              value === item.id ? "text-ink" : "text-muted hover:text-ink",
              !marker && value === item.id && "bg-raised shadow-bevel",
            )}
          >
            {item.label}
            {item.count !== undefined && <span className="ml-1.5 text-muted">{item.count}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

export interface Section<T extends string> {
  id: T;
  label: string;
  icon?: ComponentType<{ className?: string }>;
}

/**
 * Unternavigation innerhalb eines Bereichs mit vielen Karten (Einstellungen, Konto): ab `lg` eine Liste links im Stil der
 * Seitenleiste, darunter eine Auswahl. Es ist immer nur ein Abschnitt zu sehen. Den gewählten Abschnitt hält die Seite in
 * der Adresse (`useUrlState`). Bereiche einer Seite gehören dagegen in `PageTabs`.
 */
export function SectionLayout<T extends string>({
  sections,
  value,
  onChange,
  label,
  children,
}: {
  sections: Section<T>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
  children: ReactNode;
}) {
  // Ein einzelner Abschnitt braucht keine Navigation
  if (sections.length < 2) return <div className="min-w-0 space-y-5">{children}</div>;
  return (
    <div className="grid gap-4 lg:grid-cols-[208px_minmax(0,1fr)] lg:gap-8">
      <nav aria-label={label} className="min-w-0 lg:sticky lg:top-6 lg:self-start">
        <Select
          className="lg:hidden"
          aria-label={label}
          value={value}
          onChange={(next) => onChange(next as T)}
          options={sections.map((section) => ({ value: section.id, label: section.label }))}
        />
        <div className="hidden space-y-px lg:block">
          {sections.map((section) => {
            const selected = section.id === value;
            return (
              <button
                key={section.id}
                type="button"
                aria-current={selected ? "page" : undefined}
                onClick={() => onChange(section.id)}
                className={cx(
                  "relative flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-[13px] transition duration-100",
                  selected ? "bg-white/[0.08] text-ink" : "text-muted hover:bg-white/[0.04] hover:text-ink",
                )}
              >
                {selected && <span aria-hidden className="absolute inset-y-1.5 left-0 w-[3px] bg-accent" />}
                {section.icon && <section.icon className={cx("h-4 w-4 shrink-0", selected ? "text-accent" : "text-muted")} />}
                <span className="truncate">{section.label}</span>
              </button>
            );
          })}
        </div>
      </nav>
      {/* Neuer Schlüssel pro Abschnitt: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={value} className="min-w-0 animate-fade-in space-y-5 motion-reduce:animate-none">
        {children}
      </div>
    </div>
  );
}

/** Balken wie die Erfahrungsleiste: ein vertiefter Kanal, darin die gefüllte Fläche. */
export function Meter({ value, max, tone = "accent" }: { value: number; max: number; tone?: "accent" | "good" | "warn" | "bad" }) {
  const percent = max <= 0 ? 0 : Math.min(100, Math.max(0, (value / max) * 100));
  const colors = { accent: "bg-accent", good: "bg-good", warn: "bg-warn", bad: "bg-bad" };
  return (
    <div className="h-2 overflow-hidden rounded-sm border border-black/40 bg-black/30">
      <div className={cx("h-full transition-[width,background-color] duration-500 ease-snappy", colors[tone])} style={{ width: `${percent}%` }} />
    </div>
  );
}

/** Farbe nach Auslastung: grün, ab 75 % gelb, ab 90 % rot. */
export function loadTone(percent: number): "good" | "warn" | "bad" {
  return percent >= 90 ? "bad" : percent >= 75 ? "warn" : "good";
}

export function SearchInput({ className, ...rest }: Omit<InputHTMLAttributes<HTMLInputElement>, "type">) {
  return (
    <div
      className={cx(
        "flex items-center gap-2.5 rounded-md border border-white/[0.1] bg-black/25 px-3 shadow-sunken transition focus-within:border-accent/70 focus-within:ring-2 focus-within:ring-accent/20",
        className,
      )}
    >
      <Search className="h-4 w-4 shrink-0 text-muted" />
      <input
        {...rest}
        type="search"
        className="h-10 w-full bg-transparent text-sm text-ink outline-hidden placeholder:text-muted/70 [&::-webkit-search-cancel-button]:hidden"
      />
    </div>
  );
}

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

type SelectPlacement = {
  left: number;
  width: number;
  maxHeight: number;
  top?: number;
  bottom?: number;
};

/**
 * Auswahlfeld mit eigener Liste im Panel-Stil. Ein natives <select> zeichnet die aufgeklappte Liste im
 * Systemstil des Browsers, der sich nicht gestalten lässt; deshalb übernimmt diese Komponente Anzeige und Liste.
 */
export function Select({
  value,
  onChange,
  options,
  disabled,
  placeholder,
  className,
  size = "md",
  "aria-label": ariaLabel,
}: {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  disabled?: boolean;
  placeholder?: string;
  className?: string;
  size?: "sm" | "md";
  "aria-label"?: string;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [placement, setPlacement] = useState<SelectPlacement | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const listId = useId();

  const selectedIndex = options.findIndex((option) => option.value === value);
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;

  const firstEnabled = useCallback(() => options.findIndex((option) => !option.disabled), [options]);
  const lastEnabled = useCallback(() => {
    for (let index = options.length - 1; index >= 0; index--) if (!options[index].disabled) return index;
    return -1;
  }, [options]);

  const move = useCallback(
    (start: number, delta: number) => {
      if (options.length === 0) return;
      let index = start;
      for (let step = 0; step < options.length; step++) {
        index = (index + delta + options.length) % options.length;
        if (!options[index].disabled) {
          setActive(index);
          return;
        }
      }
    },
    [options],
  );

  function show(index: number) {
    if (disabled) return;
    setActive(index >= 0 && !options[index]?.disabled ? index : firstEnabled());
    setOpen(true);
  }

  function choose(index: number) {
    const option = options[index];
    if (!option || option.disabled) return;
    onChange(option.value);
    setOpen(false);
    triggerRef.current?.focus();
  }

  useEffect(() => {
    if (!open) return;
    const down = (event: PointerEvent) => {
      const target = event.target as Node;
      if (triggerRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      setOpen(false);
    };
    window.addEventListener("pointerdown", down, true);
    return () => window.removeEventListener("pointerdown", down, true);
  }, [open]);

  useEffect(() => {
    if (!open || active < 0) return;
    menuRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  useLayoutEffect(() => {
    if (!open) return;
    const update = () => {
      const rect = triggerRef.current?.getBoundingClientRect();
      if (!rect) return;
      const gap = 4;
      const below = window.innerHeight - rect.bottom - gap - 8;
      const above = rect.top - gap - 8;
      // Nach oben klappen, wenn unten kaum Platz ist und oben mehr.
      const placeAbove = below < 160 && above > below;
      const maxHeight = Math.max(120, Math.min(288, placeAbove ? above : below));
      const width = Math.min(rect.width, window.innerWidth - 16);
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
      setPlacement(
        placeAbove
          ? { left, width, maxHeight, bottom: window.innerHeight - rect.top + gap }
          : { left, width, maxHeight, top: rect.bottom + gap },
      );
    };
    const reposition = (event: Event) => {
      // Scrollt die Liste selbst, bleibt die Position; nur äußere Scrollbewegungen verschieben das Feld.
      if (menuRef.current?.contains(event.target as Node)) return;
      update();
    };
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [open]);

  function onKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>) {
    if (disabled) return;
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        if (!open) show(selectedIndex >= 0 ? selectedIndex : firstEnabled());
        else move(active >= 0 ? active : firstEnabled(), 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        if (!open) show(selectedIndex >= 0 ? selectedIndex : lastEnabled());
        else move(active >= 0 ? active : lastEnabled(), -1);
        break;
      case "Home":
        if (!open) return;
        event.preventDefault();
        setActive(firstEnabled());
        break;
      case "End":
        if (!open) return;
        event.preventDefault();
        setActive(lastEnabled());
        break;
      case "Enter":
      case " ":
        event.preventDefault();
        if (open) choose(active);
        else show(selectedIndex >= 0 ? selectedIndex : firstEnabled());
        break;
      case "Escape":
        if (!open) return;
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        break;
      case "Tab":
        setOpen(false);
        break;
      default: {
        // Tippen springt zur nächsten Option mit diesem Anfangsbuchstaben.
        if (event.key.length !== 1 || event.ctrlKey || event.metaKey || event.altKey) return;
        const needle = event.key.toLowerCase();
        let index = open && active >= 0 ? active : -1;
        for (let step = 0; step < options.length; step++) {
          index = (index + 1) % options.length;
          const option = options[index];
          if (!option.disabled && option.label.toLowerCase().startsWith(needle)) {
            if (!open) show(index);
            else setActive(index);
            return;
          }
        }
      }
    }
  }

  return (
    <div className={cx("relative", className)}>
      <button
        ref={triggerRef}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open && active >= 0 ? `${listId}-${active}` : undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => (open ? setOpen(false) : show(selectedIndex >= 0 ? selectedIndex : firstEnabled()))}
        onKeyDown={onKeyDown}
        className={cx(
          "field flex items-center justify-between gap-2 text-left",
          size === "sm" && "h-8 py-0 text-xs",
          open && "border-accent/70 bg-black/35 ring-2 ring-accent/20",
        )}
      >
        <span className={cx("min-w-0 flex-1 truncate", !selected && "text-muted/70")}>{selected?.label ?? placeholder ?? t("Auswählen …")}</span>
        <ChevronDown className={cx("h-3.5 w-3.5 shrink-0 text-muted transition-transform duration-100", open && "rotate-180")} />
      </button>
      {open &&
        placement &&
        createPortal(
          <div
            ref={menuRef}
            id={listId}
            role="listbox"
            aria-label={ariaLabel}
            style={{ left: placement.left, width: placement.width, maxHeight: placement.maxHeight, top: placement.top, bottom: placement.bottom }}
            className="fixed z-[70] animate-fade-in overflow-y-auto rounded-md border border-black/50 bg-raised p-1 shadow-panel"
          >
            {options.map((option, index) => (
              <button
                key={option.value}
                id={`${listId}-${index}`}
                data-index={index}
                type="button"
                role="option"
                aria-selected={option.value === value}
                disabled={option.disabled}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(index)}
                onMouseEnter={() => {
                  if (!option.disabled) setActive(index);
                }}
                className={cx(
                  "flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-[13px] transition duration-100",
                  index === active ? "bg-white/[0.08] text-ink" : "text-ink/90",
                  option.disabled && "cursor-not-allowed opacity-40",
                )}
              >
                <span className="min-w-0 flex-1 truncate">{option.label}</span>
                {option.value === value && <Check className="h-3.5 w-3.5 shrink-0 text-accent" />}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </div>
  );
}

export function Checkbox({ checked, onChange, label }: { checked: boolean; onChange: (value: boolean) => void; label?: string }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={label}
      onClick={(event) => {
        event.stopPropagation();
        onChange(!checked);
      }}
      className={cx(
        "grid h-4 w-4 shrink-0 place-items-center rounded-sm border transition duration-100 active:scale-90",
        checked ? "border-black/40 bg-accent text-accent-ink" : "border-white/[0.2] bg-black/25 hover:border-white/40",
      )}
    >
      {checked && <Check className="h-3 w-3" strokeWidth={3} />}
    </button>
  );
}

export interface MenuItem {
  label: string;
  icon?: ReactNode;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  hint?: string;
}

/** Kleines Aufklappmenü (⋯) für Zeilenaktionen. */
export function RowMenu({ items, label = t("Aktionen") }: { items: (MenuItem | "separator")[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const { mounted, leaving } = usePresence(open, 120);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => event.key === "Escape" && setOpen(false);
    window.addEventListener("mousedown", close);
    window.addEventListener("keydown", escape);
    return () => {
      window.removeEventListener("mousedown", close);
      window.removeEventListener("keydown", escape);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative" onClick={(event) => event.stopPropagation()}>
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="grid h-8 w-8 place-items-center rounded-md text-muted transition duration-100 hover:bg-white/[0.06] hover:text-ink active:scale-90 active:bg-white/[0.1]"
      >
        <span className="text-lg leading-none tracking-widest">···</span>
      </button>
      {mounted && (
        <div
          role="menu"
          className={cx(
            "absolute right-0 top-9 z-30 min-w-[210px] origin-top-right rounded-md border border-black/50 bg-raised p-1 shadow-panel",
            leaving ? "pointer-events-none animate-pop-out" : "animate-pop-in",
          )}
        >
          {items.map((item, index) =>
            item === "separator" ? (
              <div key={`separator-${index}`} className="my-1 h-px bg-white/[0.07]" />
            ) : (
              <button
                key={item.label}
                role="menuitem"
                type="button"
                disabled={item.disabled}
                title={item.hint}
                onClick={() => {
                  setOpen(false);
                  item.onClick();
                }}
                className={cx(
                  "flex w-full items-center gap-2.5 rounded-sm px-2.5 py-2 text-left text-[13px] transition duration-100 active:bg-white/[0.1] disabled:cursor-not-allowed disabled:opacity-40",
                  item.danger ? "text-bad hover:bg-bad/10" : "text-ink hover:bg-white/[0.07]",
                )}
              >
                {item.icon && <span className="text-muted">{item.icon}</span>}
                {item.label}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}
