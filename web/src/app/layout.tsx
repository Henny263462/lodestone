import type { Metadata, Viewport } from "next";
import { Inter, Pixelify_Sans } from "next/font/google";
import type { ReactNode } from "react";
import { ToastProvider } from "@/components/ui/feedback";
import { I18nProvider, LanguageRoot } from "@/lib/i18n-provider";
import "./globals.css";

// Die Schriften werden beim Bauen heruntergeladen und mit der Oberfläche ausgeliefert: kein Abruf bei Google im Betrieb
const pixel = Pixelify_Sans({ subsets: ["latin"], variable: "--font-pixel", display: "swap" });
const text = Inter({ subsets: ["latin"], variable: "--font-text", display: "swap" });

export const metadata: Metadata = {
  title: { default: "Lodestone", template: "%s · Lodestone" },
  description: "Hosting-Panel für Minecraft-Server und -Netzwerke",
};

export const viewport: Viewport = {
  themeColor: "#161618",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="de" className={`${pixel.variable} ${text.variable}`} suppressHydrationWarning>
      <head>
        {/* Setzt Schrift und Akzentfarbe vor dem ersten Zeichnen (siehe lib/appearance.ts) */}
        {/* eslint-disable-next-line @next/next/no-sync-scripts */}
        <script src="/appearance.js" />
      </head>
      <body>
        <I18nProvider>
          <ToastProvider>
            <LanguageRoot>{children}</LanguageRoot>
          </ToastProvider>
        </I18nProvider>
      </body>
    </html>
  );
}
