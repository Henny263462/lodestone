package node

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------- echter Minecraft-Client für die Tests

func dialMinecraft(t *testing.T, port, next int) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	body := &bytesWriter{}
	writeVarInt(body, 767) // 1.21
	writeString(body, "localhost")
	body.data = binary.BigEndian.AppendUint16(body.data, uint16(port))
	writeVarInt(body, next)
	if _, err := conn.Write(packet(0x00, body.data)); err != nil {
		t.Fatal(err)
	}
	return conn, bufio.NewReader(conn)
}

func readClientFrame(t *testing.T, reader *bufio.Reader) (int, []byte) {
	t.Helper()
	length, err := readVarInt(reader)
	if err != nil {
		t.Fatalf("keine Antwort: %v", err)
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(reader, frame); err != nil {
		t.Fatal(err)
	}
	payload := &byteReader{data: frame}
	id, err := readVarInt(payload)
	if err != nil {
		t.Fatal(err)
	}
	return id, frame[payload.pos:]
}

// automatikExpect schickt eine Anfrage an die Node-Schnittstelle und prüft den Status.
func automatikExpect(api *testAPI, want int, method, path string, body any) []byte {
	api.t.Helper()
	status, data := api.do(method, path, body)
	if status != want {
		api.t.Fatalf("%s %s: Status %d, erwartet %d: %s", method, path, status, want, data)
	}
	return data
}

func sleepManager(t *testing.T, rt Runtime) *Manager {
	t.Helper()
	cfg := testConfig(t)
	cfg.SleepMode = "on"
	manager := newTestManager(t, cfg, rt)
	t.Cleanup(manager.auto.closeSleepers)
	return manager
}

// portCheckRuntime merkt sich, ob der Port beim Anlegen des Containers frei war (so, wie Docker ihn binden muss).
type portCheckRuntime struct {
	*fakeRuntime
	port         int
	freeAtCreate atomic.Int32 // 0 = nicht angelegt, 1 = frei, 2 = belegt
}

func (r *portCheckRuntime) Create(ctx context.Context, spec ContainerSpec) (string, error) {
	if portFree(r.port, "tcp") {
		r.freeAtCreate.Store(1)
	} else {
		r.freeAtCreate.Store(2)
	}
	return r.fakeRuntime.Create(ctx, spec)
}

func TestSleepAnswersStatusAndPingAndLoginWakesServer(t *testing.T) {
	rt := &portCheckRuntime{fakeRuntime: newFakeRuntime()}
	manager := sleepManager(t, rt)
	server := createPaper(t, manager, "Schlaf")
	port := sleepPort(server.View().Spec)
	if port == 0 {
		t.Fatal("kein Port")
	}
	rt.port = port

	status, err := manager.SetSleep(server, SleepConfig{Enabled: true, IdleMinutes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Listening || status.Port != port || !status.Supported {
		t.Fatalf("Status: %+v", status)
	}
	if !hasLine(server, "Der Server schläft") {
		t.Error("keine Meldung in der Konsole")
	}

	// Serverliste: Status mit eigener MOTD
	response, _, err := queryStatus(context.Background(), fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	if text := motdText(response.Description); text != sleepMOTD {
		t.Errorf("MOTD %q", text)
	}

	// Ping/Pong mit derselben Zahl
	conn, reader := dialMinecraft(t, port, 1)
	_, _ = conn.Write(packet(0x00, nil))
	if id, _ := readClientFrame(t, reader); id != 0x00 {
		t.Fatalf("Status-Antwort hat ID %d", id)
	}
	payload := binary.BigEndian.AppendUint64(nil, 0x1122334455667788)
	_, _ = conn.Write(packet(0x01, payload))
	id, pong := readClientFrame(t, reader)
	if id != 0x01 || binary.BigEndian.Uint64(pong) != 0x1122334455667788 {
		t.Fatalf("Pong falsch: %d %x", id, pong)
	}

	// Der Server läuft noch nicht
	if server.View().State != StateOffline || rt.freeAtCreate.Load() != 0 {
		t.Fatal("der Server wurde zu früh gestartet")
	}

	// Beitritt: freundliche Meldung, dann startet der Server
	conn, reader = dialMinecraft(t, port, 2)
	login := &bytesWriter{}
	writeString(login, "Steve")
	login.data = append(login.data, make([]byte, 16)...) // UUID
	_, _ = conn.Write(packet(0x00, login.data))
	id, body := readClientFrame(t, reader)
	if id != 0x00 {
		t.Fatalf("Login-Antwort hat ID %d", id)
	}
	text, err := readString(&byteReader{data: body}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var component struct{ Text string }
	if err := json.Unmarshal([]byte(text), &component); err != nil || !strings.Contains(component.Text, "wacht gerade auf") {
		t.Fatalf("Trennmeldung %q", text)
	}
	waitState(t, server, StateRunning)
	if rt.freeAtCreate.Load() != 1 {
		t.Fatal("der Port war beim Anlegen des Containers noch belegt")
	}
	if manager.SleepStatus(server).Listening {
		t.Error("der Listener läuft noch, obwohl der Server läuft")
	}
	events := manager.auto.watch(WatchRequest{}).Events
	if len(events) == 0 || events[len(events)-1].Kind != "wake" || events[len(events)-1].Player != "Steve" {
		t.Fatalf("Ereignisse: %+v", events)
	}

	// Nach dem Stopp schläft er wieder
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateOffline)
	manager.syncSleep()
	if !manager.SleepStatus(server).Listening {
		t.Error("nach dem Stopp wartet der Port nicht wieder auf Spieler")
	}

	// Ausschalten gibt den Port frei
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if !portFree(port, "tcp") {
		t.Error("der Port ist nach dem Ausschalten noch belegt")
	}
}

// runningRuntime meldet bei Inspect einen laufenden Container, wie direkt nach dem Neustart des Daemons, bevor Reconcile
// ihn übernommen hat.
type runningRuntime struct{ *fakeRuntime }

func (runningRuntime) Inspect(context.Context, string) (ContainerState, error) {
	return ContainerState{Exists: true, Running: true}, nil
}

func TestSleepWaitsWhileContainerStillRuns(t *testing.T) {
	cfg := testConfig(t)
	cfg.SleepMode = "on"
	rt := runningRuntime{newFakeRuntime()}
	manager := newTestManager(t, cfg, rt)
	t.Cleanup(manager.auto.closeSleepers)
	server := createPaper(t, manager, "Laeuft")
	status, err := manager.SetSleep(server, SleepConfig{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if status.Listening || !portFree(sleepPort(server.View().Spec), "tcp") {
		t.Fatal("der Listener wurde geöffnet, obwohl der Container noch läuft")
	}
}

func TestSleepIgnoresGarbageAndLegacyPing(t *testing.T) {
	manager := sleepManager(t, newFakeRuntime())
	server := createPaper(t, manager, "Robust")
	port := sleepPort(server.View().Spec)
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, junk := range [][]byte{{0xFE, 0x01}, {0xFF, 0xFF, 0xFF, 0xFF, 0x0F}, []byte("GET / HTTP/1.1\r\n\r\n")} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write(junk)
		_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
		buffer := make([]byte, 16)
		if n, _ := conn.Read(buffer); n != 0 {
			t.Errorf("Antwort auf %q: %q", junk, buffer[:n])
		}
		conn.Close()
	}
	if server.View().State != StateOffline {
		t.Error("Müll hat den Server gestartet")
	}
	// danach funktioniert die Statusabfrage weiter
	if _, _, err := queryStatus(context.Background(), fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Fatal(err)
	}
}

func TestSleepIdleStopsServerAfterMinutes(t *testing.T) {
	manager := sleepManager(t, newFakeRuntime())
	server := createPaper(t, manager, "Leerlauf")
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: true, IdleMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	online := atomic.Int32{}
	manager.auto.players = func(context.Context, *Server) (int, error) { return int(online.Load()), nil }
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateRunning)

	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	manager.checkIdle(ctx, start) // ab jetzt leer
	manager.checkIdle(ctx, start.Add(4*time.Minute))
	if server.View().State != StateRunning {
		t.Fatal("zu früh gestoppt")
	}
	// ein Spieler kommt und geht: die Zeit beginnt von vorn
	online.Store(1)
	manager.checkIdle(ctx, start.Add(4*time.Minute+30*time.Second))
	online.Store(0)
	manager.checkIdle(ctx, start.Add(5*time.Minute))
	manager.checkIdle(ctx, start.Add(9*time.Minute))
	if server.View().State != StateRunning {
		t.Fatal("trotz Spieler gestoppt")
	}
	manager.checkIdle(ctx, start.Add(10*time.Minute))
	waitState(t, server, StateOffline)
	if exit := server.View().LastExit; exit == nil || !exit.Requested {
		t.Errorf("der Stopp zählt als Absturz: %+v", exit)
	}
	status := manager.SleepStatus(server)
	if status.LastSleep != start.Add(10*time.Minute).UnixMilli() {
		t.Errorf("LastSleep %d", status.LastSleep)
	}
	events := manager.auto.watch(WatchRequest{}).Events
	if len(events) == 0 || events[len(events)-1].Kind != "sleep" {
		t.Errorf("Ereignisse: %+v", events)
	}
	manager.syncSleep()
	if !manager.SleepStatus(server).Listening {
		t.Error("nach dem Einschlafen wartet der Port nicht auf Spieler")
	}
}

func TestSleepFailedPlayerQueryDoesNotStop(t *testing.T) {
	manager := sleepManager(t, newFakeRuntime())
	server := createPaper(t, manager, "Stumm")
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: true, IdleMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	manager.auto.players = func(context.Context, *Server) (int, error) { return 0, errors.New("keine Antwort") }
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateRunning)
	start := time.Now()
	for i := 0; i < 5; i++ {
		manager.checkIdle(context.Background(), start.Add(time.Duration(i)*time.Minute))
	}
	if server.View().State != StateRunning {
		t.Fatal("ohne Antwort gestoppt")
	}
}

func TestSleepRejectedWithoutSupportOrPort(t *testing.T) {
	cfg := testConfig(t)
	cfg.SleepMode = "off"
	manager := newTestManager(t, cfg, newFakeRuntime())
	server := createPaper(t, manager, "Container")
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("ohne Unterstützung: %v", err)
	}
	if status := manager.SleepStatus(server); status.Supported || status.Enabled {
		t.Errorf("Status: %+v", status)
	}

	manager.cfg.SleepMode = "on"
	hidden, err := manager.Create(Spec{Name: "Versteckt", Software: "paper", Version: "1.21.8", EULA: true, NoPublish: true})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Installation", func() bool { return hidden.View().Installed })
	if _, err := manager.SetSleep(hidden, SleepConfig{Enabled: true}); !errors.Is(err, errBadRequest) {
		t.Fatalf("ohne Port: %v", err)
	}
	if _, err := manager.SetSleep(server, SleepConfig{Enabled: true, IdleMinutes: 5000}); !errors.Is(err, errBadRequest) {
		t.Fatalf("zu lange Leerlaufzeit: %v", err)
	}
}

func TestSleepSettingSurvivesRestartOfTheDaemon(t *testing.T) {
	cfg := testConfig(t)
	cfg.SleepMode = "on"
	rt := newFakeRuntime()
	first := newTestManager(t, cfg, rt)
	server := createPaper(t, first, "Bleibt")
	if _, err := first.SetSleep(server, SleepConfig{Enabled: true, IdleMinutes: 7}); err != nil {
		t.Fatal(err)
	}
	first.auto.closeSleepers()

	second := newTestManager(t, cfg, rt)
	t.Cleanup(second.auto.closeSleepers)
	again, _ := second.Get(server.ID())
	status := second.SleepStatus(again)
	if !status.Enabled || status.IdleMinutes != 7 || status.Listening {
		t.Fatalf("Status nach Neustart: %+v", status)
	}
	second.syncSleep()
	if !second.SleepStatus(again).Listening {
		t.Error("nach dem Neustart schläft der Server nicht")
	}
}

// ---------------------------------------------------------------- Konsole

func TestObserveJoinLeaveAndPatterns(t *testing.T) {
	var a autoState
	a.watch(WatchRequest{Patterns: map[string][]WatchPattern{
		"s1": {{ID: "p1", Pattern: `Can't keep up!`}, {ID: "kaputt", Pattern: `([`}, {ID: "Falsch!", Pattern: `x`}},
	}})
	lines := []string{
		`[12:00:00] [Server thread/INFO]: Steve joined the game`,
		"\x1b[0m[12:00:01 INFO]: Alex joined the game\x1b[0m",
		`[12:00:02] [Server thread/INFO] [minecraft/MinecraftServer]: Steve left the game`,
		`[12:00:03] [Server thread/INFO]: <Bob> Mallory joined the game`, // Chat, kein Beitritt
		`[12:00:04 INFO]: [connected player] Luna (/127.0.0.1:5555) has connected`,
		`[12:00:05 INFO]: [connected player] Luna (/127.0.0.1:5555) has disconnected`,
		`[lodestone] Steve joined the game`,
		`[12:00:06] [Server thread/WARN]: Can't keep up! Is the server overloaded?`,
	}
	for _, line := range lines {
		a.observe("s1", line)
	}
	a.observe("s2", `[12:00:07] [Server thread/WARN]: Can't keep up! Is the server overloaded?`) // anderer Server, kein Muster
	got := []string{}
	for _, event := range a.watch(WatchRequest{}).Events {
		got = append(got, event.Kind+":"+event.Player+event.Pattern)
	}
	want := "join:Steve join:Alex leave:Steve join:Luna leave:Luna match:p1"
	if strings.Join(got, " ") != want {
		t.Fatalf("Ereignisse %v, erwartet %s", got, want)
	}
}

func TestPatternMatchesAreRateLimitedAndWatchUsesCursor(t *testing.T) {
	var a autoState
	a.watch(WatchRequest{Patterns: map[string][]WatchPattern{"s1": {{ID: "p1", Pattern: `ERROR`}}}})
	for i := 0; i < 20; i++ {
		a.observe("s1", fmt.Sprintf("[12:00:00] [Server thread/ERROR]: ERROR Nummer %d", i))
	}
	first := a.watch(WatchRequest{Patterns: map[string][]WatchPattern{"s1": {{ID: "p1", Pattern: `ERROR`}}}})
	if len(first.Events) != maxPatternMatches {
		t.Fatalf("%d Treffer, erwartet %d", len(first.Events), maxPatternMatches)
	}
	// Mit passendem Boot kommen nur neue Ereignisse
	a.observe("s1", "[12:00:00] [Server thread/INFO]: Steve joined the game")
	next := a.watch(WatchRequest{Boot: first.Boot, After: first.Latest})
	if len(next.Events) != 1 || next.Events[0].Kind != "join" {
		t.Fatalf("neue Ereignisse: %+v", next.Events)
	}
	// Ohne Muster in der Anfrage gibt es keine Treffer mehr
	a.observe("s1", "[12:00:00] [Server thread/ERROR]: ERROR später")
	if later := a.watch(WatchRequest{Boot: next.Boot, After: next.Latest}); len(later.Events) != 0 {
		t.Fatalf("Muster wirkt noch: %+v", later.Events)
	}
	// Anderer Boot (Daemon neu gestartet): alles
	if all := a.watch(WatchRequest{Boot: "anders", After: 999}); len(all.Events) != maxPatternMatches+1 {
		t.Fatalf("bei fremdem Boot %d Ereignisse", len(all.Events))
	}
}

func TestConsoleWatchOverAPI(t *testing.T) {
	api := newTestAPI(t)
	server := createPaper(t, api.manager, "Beobachtet")
	api.manager.syncWatchers()
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateRunning)
	request := WatchRequest{Patterns: map[string][]WatchPattern{server.ID(): {{ID: "gruss", Pattern: `Befehl: say hallo`}}}}
	var response WatchResponse
	_ = json.Unmarshal(automatikExpect(api, 200, "POST", "/api/console-watch", request), &response)
	if err := server.Command("say hallo"); err != nil {
		t.Fatal(err)
	}
	request.Boot, request.After = response.Boot, response.Latest
	waitFor(t, "Treffer", func() bool {
		_ = json.Unmarshal(automatikExpect(api, 200, "POST", "/api/console-watch", request), &response)
		return len(response.Events) > 0
	})
	if event := response.Events[0]; event.Kind != "match" || event.Pattern != "gruss" || event.ServerID != server.ID() || !strings.Contains(event.Line, "say hallo") {
		t.Fatalf("Ereignis: %+v", event)
	}
	// ohne Token kein Zugriff
	if status, _ := api.do("POST", "/api/console-watch", request, "Authorization", "Bearer falsch"); status != 401 {
		t.Errorf("ohne Token: %d", status)
	}
	// unbekannte Felder werden abgelehnt
	automatikExpect(api, 400, "POST", "/api/console-watch", map[string]any{"unbekannt": 1})
}

func TestSleepOverAPI(t *testing.T) {
	api := newTestAPI(t)
	api.manager.cfg.SleepMode = "on"
	t.Cleanup(api.manager.auto.closeSleepers)
	server := createPaper(t, api.manager, "Api")
	var status SleepStatus
	_ = json.Unmarshal(automatikExpect(api, 200, "GET", "/api/servers/"+server.ID()+"/sleep", nil), &status)
	if status.Enabled || status.IdleMinutes != defaultIdleMinutes {
		t.Fatalf("Standard: %+v", status)
	}
	_ = json.Unmarshal(automatikExpect(api, 200, "PUT", "/api/servers/"+server.ID()+"/sleep", SleepConfig{Enabled: true, IdleMinutes: 3}), &status)
	if !status.Enabled || !status.Listening || status.IdleMinutes != 3 {
		t.Fatalf("eingeschaltet: %+v", status)
	}
	automatikExpect(api, 400, "PUT", "/api/servers/"+server.ID()+"/sleep", map[string]any{"enabled": true, "idleMinutes": -1})
	automatikExpect(api, 400, "PUT", "/api/servers/"+server.ID()+"/sleep", map[string]any{"enabled": true, "falsch": 1})
	automatikExpect(api, 404, "GET", "/api/servers/gibtsnicht/sleep", nil)
}

// ---------------------------------------------------------------- Absturzanalyse

func TestAnalyzeCrashForgeSuspectedMod(t *testing.T) {
	report := `---- Minecraft Crash Report ----
// Who set us up the TNT?

Description: Ticking entity

java.lang.NullPointerException: Cannot invoke "Object.toString()"
	at TRANSFORMER/examplemod@1.2.0/com.example.mod.Thing.tick(Thing.java:42)
	at TRANSFORMER/minecraft@1.21.1/net.minecraft.world.level.Level.tick(Level.java:100)

-- Head --
Thread: Server thread
Suspected Mods:
	Example Mod (examplemod), Version: 1.2.0
		Issue tracker URL: https://example.org/issues
		at TRANSFORMER/examplemod@1.2.0/com.example.mod.Thing.tick(Thing.java:42)

Stacktrace:
	at TRANSFORMER/minecraft@1.21.1/net.minecraft.server.MinecraftServer.tick(MinecraftServer.java:1)
`
	result := analyzeCrash(report)
	if !result.Found || len(result.Suspects) == 0 || result.Suspects[0].ModID != "examplemod" || result.Suspects[0].Reason != "suspected" || result.Suspects[0].Name != "Example Mod" {
		t.Fatalf("Verdächtige: %+v", result.Suspects)
	}
	if !strings.Contains(result.Summary, "Example Mod") || !strings.Contains(result.Summary, "examplemod") {
		t.Errorf("Zusammenfassung: %q", result.Summary)
	}
	for _, suspect := range result.Suspects {
		if suspect.ModID == "minecraft" {
			t.Error("minecraft ist kein Verdächtiger")
		}
	}
}

func TestAnalyzeCrashMissingDependencies(t *testing.T) {
	fabric := `[main/ERROR]: Incompatible mods found!
net.fabricmc.loader.impl.FormattedException: Some of your mods are incompatible with the game or each other!
	 - Mod 'Sodium Extra' (sodium-extra) 0.6.0 requires version 0.6.0 or later of mod 'Sodium' (sodium), which is missing!
	 - Mod 'Better Things' (betterthings) 1.0 requires any version of fabric-api, which is missing!`
	result := analyzeCrash(fabric)
	if len(result.Missing) != 2 || result.Missing[0].Requires != "sodium" || result.Missing[0].Mod != "sodium-extra" || result.Missing[1].Requires != "fabric-api" {
		t.Fatalf("Fabric: %+v", result.Missing)
	}
	if !strings.Contains(result.Summary, "„sodium“") {
		t.Errorf("Zusammenfassung: %q", result.Summary)
	}
	forge := `Missing or unsupported mandatory dependencies:
	Mod ID: 'geckolib', Requested by: 'creatures', Expected range: '[4.4,)', Actual version: '[MISSING]'`
	result = analyzeCrash(forge)
	if len(result.Missing) != 1 || result.Missing[0] != (MissingDependency{Mod: "creatures", Requires: "geckolib", Version: "[4.4,)"}) {
		t.Fatalf("Forge: %+v", result.Missing)
	}
}

func TestAnalyzeCrashJavaAndMixin(t *testing.T) {
	java := `Exception in thread "main" java.lang.UnsupportedClassVersionError: net/minecraft/server/Main has been compiled by a more recent version of the Java Runtime (class file version 65.0), this version of the Java Runtime only recognizes class file versions up to 61.0`
	result := analyzeCrash(java)
	if result.Java == nil || result.Java.Required != 21 || result.Java.Current != 17 {
		t.Fatalf("Java: %+v", result.Java)
	}
	if !strings.Contains(result.Summary, "Java 21") {
		t.Errorf("Zusammenfassung: %q", result.Summary)
	}
	mixin := `org.spongepowered.asm.mixin.transformer.throwables.MixinTransformerError: An unexpected critical error was encountered
Caused by: org.spongepowered.asm.mixin.throwables.MixinApplyError: Mixin [coolmod.mixins.json:EntityMixin] from phase [DEFAULT] in config [coolmod.mixins.json] FAILED during APPLY
	at net.minecraft.world.entity.Entity.handler$zza000$coolmod$onTick(Entity.java)`
	result = analyzeCrash(mixin)
	if len(result.Mixins) == 0 || result.Mixins[0] != "coolmod.mixins.json:EntityMixin" || len(result.Suspects) == 0 || result.Suspects[0].ModID != "coolmod" || result.Suspects[0].Reason != "mixin" {
		t.Fatalf("Mixin: %+v %+v", result.Mixins, result.Suspects)
	}
	if !strings.Contains(result.Summary, "Mixin-Fehler") {
		t.Errorf("Zusammenfassung: %q", result.Summary)
	}
}

func TestAnalyzeCrashStacktraceAndNothing(t *testing.T) {
	trace := `java.lang.IllegalStateException: kaputt
	at com.lag.Mod.run(Mod.java:1) ~[lagmod-2.0.jar%23101!/:?]
	at com.lag.Mod.run(Mod.java:2) ~[lagmod-2.0.jar%23101!/:?]
	at com.other.Helper.go(Helper.java:3) ~[helper-1.0.jar%23102!/:?]
	at net.minecraft.Foo.bar(Foo.java:4) ~[minecraft-1.20.1.jar%2399!/:?]`
	result := analyzeCrash(trace)
	if len(result.Suspects) < 2 || result.Suspects[0].ModID != "lagmod" || result.Suspects[0].Reason != "stacktrace" || result.Suspects[1].ModID != "helper" {
		t.Fatalf("Stacktrace: %+v", result.Suspects)
	}
	if empty := analyzeCrash("[12:00:00 INFO]: Done (1s)!"); empty.Found || empty.Summary != "" || empty.Suspects == nil || empty.Missing == nil || empty.Mixins == nil {
		t.Fatalf("ohne Befund: %+v", empty)
	}
}

func TestServerCrashAnalysisReadsLatestReport(t *testing.T) {
	api := newTestAPI(t)
	server := createPaper(t, api.manager, "Absturz")
	if err := server.fs.Mkdir("crash-reports"); err != nil {
		t.Fatal(err)
	}
	report := "Suspected Mods: Bad Mod (badmod), Version: 0.1\n\n"
	if err := server.fs.Write("crash-reports/crash-2026-11-21_12.00.00-server.txt", strings.NewReader(report), 1<<20, true); err != nil {
		t.Fatal(err)
	}
	var result CrashAnalysis
	_ = json.Unmarshal(automatikExpect(api, 200, "GET", "/api/servers/"+server.ID()+"/crash-analysis", nil), &result)
	if !result.Found || result.Report != "crash-reports/crash-2026-11-21_12.00.00-server.txt" || result.Suspects[0].ModID != "badmod" {
		t.Fatalf("Analyse: %+v", result)
	}
	// Ein Bericht von vor dem letzten Start gilt nicht
	if _, text := server.latestCrashReport(time.Now().Add(time.Hour), time.Now()); text != "" {
		t.Error("alter Bericht wurde ausgewertet")
	}
}
