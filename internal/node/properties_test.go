package node

import (
	"strings"
	"testing"
)

func TestParsePropertiesReadsJavaSyntax(t *testing.T) {
	text := strings.Join([]string{
		"#Minecraft server properties",
		"! auch ein Kommentar",
		"",
		"motd=Hallo\\: Welt \\u00e4\\u00f6\\u00fc",
		"  max-players : 20",
		"pvp true",
		"level-seed=",
		"lang=erste \\",
		"    zweite",
		"emoji=\\ud83d\\ude00",
		"tab=a\\tb\\nc",
		"key\\=mit\\:zeichen=wert",
		"doppelt=alt",
		"doppelt=neu",
		"kaputt=\\u12",
	}, "\n") + "\n"
	file := ParseProperties([]byte(text))
	want := map[string]string{
		"motd":            "Hallo: Welt äöü",
		"max-players":     "20",
		"pvp":             "true",
		"level-seed":      "",
		"lang":            "erste zweite",
		"emoji":           "😀",
		"tab":             "a\tb\nc",
		"key=mit:zeichen": "wert",
		"doppelt":         "neu",
		"kaputt":          "u12",
	}
	for key, value := range want {
		if got, ok := file.Get(key); !ok || got != value {
			t.Errorf("%s = %q (%v), erwartet %q", key, got, ok, value)
		}
	}
	entries := file.Entries()
	if len(entries) != len(want) {
		t.Fatalf("%d Einträge, erwartet %d: %v", len(entries), len(want), entries)
	}
	if entries[0].Key != "motd" || entries[len(entries)-2].Key != "doppelt" {
		t.Errorf("Reihenfolge der Datei ging verloren: %v", entries)
	}
	if _, ok := file.Get("#Minecraft server properties"); ok {
		t.Error("Kommentare sind keine Einträge")
	}
	// Ohne Änderung kommt die Datei Byte für Byte wieder heraus
	if string(file.Bytes()) != text {
		t.Errorf("unverändert geschrieben:\n%s\nerwartet:\n%s", file.Bytes(), text)
	}
}

func TestSetPropertiesKeepsCommentsUnknownKeysAndLineEndings(t *testing.T) {
	text := "#Kommentar\r\nmotd=Alt\r\nmein-plugin.wert=42\r\n# noch einer\r\npvp=true\r\n"
	file := ParseProperties([]byte(text))
	if file.Set("pvp", "true") {
		t.Error("gleicher Wert darf nichts ändern")
	}
	if !file.Set("motd", "Neu") || !file.Set("difficulty", "hard") {
		t.Fatal("Set meldet keine Änderung")
	}
	got := string(file.Bytes())
	want := "#Kommentar\r\nmotd=Neu\r\nmein-plugin.wert=42\r\n# noch einer\r\npvp=true\r\ndifficulty=hard\r\n"
	if got != want {
		t.Fatalf("Ergebnis:\n%q\nerwartet:\n%q", got, want)
	}
}

func TestSetPropertiesEscapesAndRoundTrips(t *testing.T) {
	values := []string{
		"Größe: 100% = viel #1 !",
		" führendes Leerzeichen",
		"zwei\nZeilen",
		"Backslash \\ bleibt",
		"Emoji 😀",
		"",
	}
	for _, value := range values {
		file := ParseProperties(nil)
		file.Set("motd", value)
		written := file.Bytes()
		if strings.ContainsAny(string(written[:len(written)-1]), "\n\r") {
			t.Errorf("%q: Zeilenumbruch nicht maskiert: %q", value, written)
		}
		for _, r := range string(written) {
			if r > 0x7e {
				t.Errorf("%q: Zeichen außerhalb von ASCII nicht als \\u geschrieben: %q", value, written)
				break
			}
		}
		again := ParseProperties(written)
		if got, _ := again.Get("motd"); got != value {
			t.Errorf("Rundreise: %q wurde zu %q (geschrieben %q)", value, got, written)
		}
	}
	file := ParseProperties(nil)
	file.Set("schlüssel mit=zeichen", "x")
	if got, ok := ParseProperties(file.Bytes()).Get("schlüssel mit=zeichen"); !ok || got != "x" {
		t.Errorf("Schlüssel mit Sonderzeichen: %q %v (%q)", got, ok, file.Bytes())
	}
}

func TestParsePropertiesHandlesOddInput(t *testing.T) {
	file := ParseProperties([]byte("\ufeffa=1\rb=2\nende=\\"))
	if v, _ := file.Get("a"); v != "1" {
		t.Errorf("BOM oder \\r nicht beachtet: %q", v)
	}
	if v, _ := file.Get("b"); v != "2" {
		t.Errorf("b = %q", v)
	}
	if v, ok := file.Get("ende"); !ok || v != "" {
		t.Errorf("Backslash am Dateiende: %q %v", v, ok)
	}
	if len(ParseProperties(nil).Entries()) != 0 || len(ParseProperties(nil).Bytes()) != 0 {
		t.Error("leere Datei muss leer bleiben")
	}
}
