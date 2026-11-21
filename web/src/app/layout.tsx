import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { ToastProvider } from "@/components/ui/feedback";
import { I18nProvider, LanguageRoot } from "@/lib/i18n-provider";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "Lodestone", template: "%s · Lodestone" },
  description: "Hosting-Panel für Minecraft-Server und -Netzwerke",
};

export const viewport: Viewport = {
  themeColor: "#111111",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="de">
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
