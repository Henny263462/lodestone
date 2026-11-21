package panel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

func init() {
	opBeat = 15 * time.Millisecond
	opPoll = 5 * time.Millisecond
}

// opsNode erweitert die Node-Attrappe um Speicherplatz, Kopie, Export und Import. Alles andere reicht sie an die
// gewöhnliche Attrappe weiter.
type opsNode struct {
	base *fakeNode

	mu          sync.Mutex
	bytes       map[string]int64
	limits      map[string]int
	free, total int64
	clones      []node.CloneRequest
	imports     []string
	exportMode  string // "" = vollständig, "break" = bricht mitten ab, "hang" = wartet auf den Abbruch
	importFail  bool
	exporting   chan struct{}
}

func newOpsNode(t *testing.T) (*opsNode, *httptest.Server) {
	base, baseServer := newFakeNode(t)
	o := &opsNode{base: base, bytes: map[string]int64{}, limits: map[string]int{}, free: 100 << 30, total: 200 << 30, exporting: make(chan struct{}, 4)}
	target, _ := url.Parse(baseServer.URL)
	mux := http.NewServeMux()
	mux.Handle("/", httputil.NewSingleHostReverseProxy(target))
	usage := func(id string) node.DiskUsage {
		size, ok := o.bytes[id]
		if !ok {
			size = 1 << 20
		}
		return node.DiskUsage{Bytes: size, Files: 3, MeasuredAt: time.Now().UnixMilli()}
	}
	mux.HandleFunc("GET /api/storage", func(w http.ResponseWriter, _ *http.Request) {
		base.mu.Lock()
		o.mu.Lock()
		result := node.NodeStorage{Free: o.free, Total: o.total, Servers: []node.ServerStorage{}}
		for id := range base.servers {
			result.Servers = append(result.Servers, node.ServerStorage{ID: id, DiskUsage: usage(id), LimitMB: o.limits[id]})
		}
		o.mu.Unlock()
		base.mu.Unlock()
		writeJSON(w, 200, result)
	})
	info := func(id string) node.StorageInfo {
		o.mu.Lock()
		defer o.mu.Unlock()
		return node.StorageInfo{DiskUsage: usage(id), LimitMB: o.limits[id], Free: o.free, Total: o.total}
	}
	mux.HandleFunc("GET /api/servers/{id}/storage", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, info(r.PathValue("id"))) })
	mux.HandleFunc("POST /api/servers/{id}/storage/measure", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 202, info(r.PathValue("id"))) })
	mux.HandleFunc("PUT /api/servers/{id}/storage", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ LimitMB int }
		_ = json.NewDecoder(r.Body).Decode(&body)
		o.mu.Lock()
		o.limits[r.PathValue("id")] = body.LimitMB
		o.mu.Unlock()
		writeJSON(w, 200, info(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/servers/{id}/clone", func(w http.ResponseWriter, r *http.Request) {
		var request node.CloneRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		base.mu.Lock()
		defer base.mu.Unlock()
		source, ok := base.servers[r.PathValue("id")]
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "nicht gefunden"})
			return
		}
		clone := source
		clone.ID, clone.Name, clone.State, clone.Autoconfig = request.ID, request.Name, node.StateOffline, nil
		clone.Ports = nil
		for i, port := range source.Ports {
			clone.Ports = append(clone.Ports, node.PortMapping{Container: port.Container, Protocol: port.Protocol, Host: 30000 + len(base.servers) + i})
		}
		base.servers[clone.ID] = clone
		o.mu.Lock()
		o.clones = append(o.clones, request)
		o.mu.Unlock()
		writeJSON(w, 201, clone)
	})
	mux.HandleFunc("GET /api/servers/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		base.mu.Lock()
		view, ok := base.servers[r.PathValue("id")]
		base.mu.Unlock()
		if !ok || view.State != node.StateOffline {
			writeJSON(w, 409, map[string]string{"error": "Der Server muss für den Umzug ausgeschaltet sein"})
			return
		}
		o.mu.Lock()
		mode := o.exportMode
		o.mu.Unlock()
		w.WriteHeader(200)
		gz := gzip.NewWriter(w)
		archive := tar.NewWriter(gz)
		spec, _ := json.Marshal(view.Spec)
		_ = archive.WriteHeader(&tar.Header{Name: "spec.json", Mode: 0o600, Size: int64(len(spec)), Typeflag: tar.TypeReg})
		_, _ = archive.Write(spec)
		world := strings.Repeat("w", 64<<10)
		_ = archive.WriteHeader(&tar.Header{Name: "data/world/level.dat", Mode: 0o644, Size: int64(len(world)), Typeflag: tar.TypeReg})
		_, _ = archive.Write([]byte(world))
		if mode != "" {
			_ = archive.Flush()
			_ = gz.Flush()
			w.(http.Flusher).Flush()
			o.exporting <- struct{}{}
			if mode == "hang" {
				<-r.Context().Done()
			}
			panic(http.ErrAbortHandler)
		}
		_ = archive.WriteHeader(&tar.Header{Name: ".lodestone-export-complete", Mode: 0o600, Typeflag: tar.TypeReg})
		_ = archive.Close()
		_ = gz.Close()
	})
	mux.HandleFunc("POST /api/servers/import", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		failing := o.importFail
		o.mu.Unlock()
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "kein gzip"})
			return
		}
		archive := tar.NewReader(gz)
		header, err := archive.Next()
		if err != nil || header.Name != "spec.json" {
			writeJSON(w, 400, map[string]string{"error": "ohne Beschreibung"})
			return
		}
		var spec node.Spec
		_ = json.NewDecoder(archive).Decode(&spec)
		if failing {
			writeJSON(w, 500, map[string]string{"error": "interner Fehler: die Platte ist voll"})
			return
		}
		complete := false
		for {
			header, err := archive.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "der Umzugs-Strom ist beschädigt"})
				return
			}
			complete = header.Name == ".lodestone-export-complete"
		}
		if !complete {
			writeJSON(w, 400, map[string]string{"error": "die Übertragung ist unvollständig"})
			return
		}
		for i := range spec.Ports {
			spec.Ports[i].Host = 29000 + i
		}
		view := node.View{Spec: spec, State: node.StateOffline}
		base.mu.Lock()
		base.servers[spec.ID] = view
		base.mu.Unlock()
		o.mu.Lock()
		o.imports = append(o.imports, spec.ID)
		o.mu.Unlock()
		writeJSON(w, 201, view)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return o, server
}

func (o *opsNode) has(id string) bool {
	o.base.mu.Lock()
	defer o.base.mu.Unlock()
	_, ok := o.base.servers[id]
	return ok
}

func (o *opsNode) state(id string) node.State {
	o.base.mu.Lock()
	defer o.base.mu.Unlock()
	return o.base.servers[id].State
}

type opsEnv struct {
	p          *Panel
	web        *httptest.Server
	admin      *client
	alpha      *opsNode
	beta       *opsNode
	alphaID    string
	betaID     string
	betaServer *httptest.Server
}

func newOpsEnv(t *testing.T) *opsEnv {
	p, web, _, _ := setupPanel(t)
	env := &opsEnv{p: p, web: web, admin: newClient(t, web.URL)}
	var alphaServer *httptest.Server
	env.alpha, alphaServer = newOpsNode(t)
	env.beta, env.betaServer = newOpsNode(t)
	a, _ := p.store.CreateNode(Node{Name: "alpha", URL: alphaServer.URL, Token: "token", PublicHost: "alpha.example.org"})
	b, _ := p.store.CreateNode(Node{Name: "beta", URL: env.betaServer.URL, Token: "token", PublicHost: "beta.example.org"})
	env.alphaID, env.betaID = a.ID, b.ID
	env.admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	return env
}

func (e *opsEnv) project(t *testing.T, name, kind string) ProjectDTO {
	var project ProjectDTO
	_ = json.Unmarshal(e.admin.expect(201, "POST", "/api/projects", map[string]string{"name": name, "kind": kind}), &project)
	return project
}

func (e *opsEnv) server(t *testing.T, projectID string, request serverRequest) ServerDTO {
	var dto ServerDTO
	_ = json.Unmarshal(e.admin.expect(201, "POST", "/api/projects/"+projectID+"/servers", request), &dto)
	return dto
}

// member legt einen Benutzer mit einer Rolle im Projekt an und meldet ihn an.
func (e *opsEnv) member(t *testing.T, projectID, name, role string) *client {
	var user User
	_ = json.Unmarshal(e.admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"}), &user)
	if role != "" {
		e.admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+user.ID, map[string]string{"role": role})
	}
	c := newClient(t, e.web.URL)
	c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
	return c
}

func waitOp(t *testing.T, c *client, id string) ServerOp {
	t.Helper()
	var op ServerOp
	waitUntil(t, "Ende des Vorgangs", func() bool {
		_ = json.Unmarshal(c.expect(200, "GET", "/api/server-ops/"+id, nil), &op)
		return op.State != OpRunning
	})
	return op
}

func startOpRequest(t *testing.T, c *client, path string, body any) ServerOp {
	t.Helper()
	var op ServerOp
	_ = json.Unmarshal(c.expect(202, "POST", path, body), &op)
	if op.ID == "" || op.State != OpRunning {
		t.Fatalf("Vorgang: %+v", op)
	}
	return op
}

func TestCloneInNetworkStaysOutOfTry(t *testing.T) {
	env := newOpsEnv(t)
	project := env.project(t, "Netz", "network")
	proxy := env.server(t, project.ID, serverRequest{Name: "Proxy", NodeID: env.alphaID, Software: "velocity", Version: "3.4.0"})
	lobby := env.server(t, project.ID, serverRequest{Name: "Lobby", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})

	// Rechte und Eingaben
	viewer := env.member(t, project.ID, "betrachter", "viewer")
	operator := env.member(t, project.ID, "operator", "operator")
	stranger := env.member(t, project.ID, "fremd", "")
	viewer.expect(403, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "Kopie"})
	operator.expect(403, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "Kopie"})
	stranger.expect(404, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "Kopie"})
	env.admin.expect(400, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": " "})
	env.admin.expect(400, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "Kopie", "extra": 1})
	env.admin.expect(409, "POST", "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "lobby"})
	env.admin.expect(409, "POST", "/api/servers/"+proxy.ID+"/clone", map[string]any{"name": "Proxy 2"})

	op := startOpRequest(t, env.admin, "/api/servers/"+lobby.ID+"/clone", map[string]any{"name": "Lobby 2", "withWorlds": true})
	viewer.expect(200, "GET", "/api/server-ops/"+op.ID, nil)
	stranger.expect(404, "GET", "/api/server-ops/"+op.ID, nil)
	done := waitOp(t, env.admin, op.ID)
	if done.State != OpDone || done.ResultServerID == "" || !strings.Contains(done.Message, "Try") {
		t.Fatalf("Kopie: %+v", done)
	}
	clone, err := env.p.store.ServerByID(done.ResultServerID)
	if err != nil {
		t.Fatal(err)
	}
	if clone.ProjectID != project.ID || clone.Role != RoleBackend || clone.InTry || clone.NodeID != env.alphaID || clone.Name != "Lobby 2" || clone.Slug != "lobby-2" {
		t.Fatalf("Zeile der Kopie: %+v", clone)
	}
	env.alpha.mu.Lock()
	request := env.alpha.clones[0]
	env.alpha.mu.Unlock()
	if !request.WithWorlds || request.ID != clone.ID {
		t.Fatalf("Anfrage an den Node: %+v", request)
	}
	// Der Proxy kennt die Kopie, schickt aber niemanden hin, bis sie eingeschaltet ist
	waitUntil(t, "Autoconfig des Proxys", func() bool {
		env.alpha.base.mu.Lock()
		defer env.alpha.base.mu.Unlock()
		config, ok := env.alpha.base.autoconfigs[proxy.ID]
		if !ok {
			return false
		}
		table, _ := setValue(t, editFor(t, config, "velocity.toml"), "servers").(map[string]any)
		_, known := table["lobby-2"]
		return known
	})
	env.alpha.base.mu.Lock()
	table := setValue(t, editFor(t, env.alpha.base.autoconfigs[proxy.ID], "velocity.toml"), "servers").(map[string]any)
	_, cloneHasConfig := env.alpha.base.autoconfigs[clone.ID]
	env.alpha.base.mu.Unlock()
	if try, _ := table["try"].([]any); slices.Contains(try, any("lobby-2")) {
		t.Fatalf("Die Kopie darf nicht im Try stehen: %v", try)
	}
	if !cloneHasConfig {
		t.Fatal("Die Kopie hat keine Autoconfig bekommen")
	}
	var list []ServerOp
	_ = json.Unmarshal(viewer.expect(200, "GET", "/api/projects/"+project.ID+"/server-ops", nil), &list)
	if len(list) != 1 || list[0].ID != op.ID {
		t.Fatalf("Liste: %+v", list)
	}
	viewer.expect(403, "POST", "/api/server-ops/"+op.ID+"/cancel", nil)
	env.admin.expect(409, "POST", "/api/server-ops/"+op.ID+"/cancel", nil)
}

func TestCloneOfServerProjectGetsOwnProject(t *testing.T) {
	env := newOpsEnv(t)
	project := env.project(t, "Survival", "server")
	main := env.server(t, project.ID, serverRequest{Name: "Survival", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
	op := startOpRequest(t, env.admin, "/api/servers/"+main.ID+"/clone", map[string]any{"name": "Survival Test"})
	done := waitOp(t, env.admin, op.ID)
	if done.State != OpDone {
		t.Fatalf("Kopie: %+v", done)
	}
	clone, _ := env.p.store.ServerByID(done.ResultServerID)
	copied, err := env.p.store.ProjectByID(clone.ProjectID)
	if err != nil || copied.ID == project.ID || copied.Kind != "server" || copied.Name != "Survival Test" || clone.Role != RoleStandalone {
		t.Fatalf("Projekt der Kopie: %+v %+v", copied, clone)
	}
	if !strings.Contains(done.Message, "Welten wurden nicht kopiert") {
		t.Fatalf("Hinweis zu den Welten fehlt: %s", done.Message)
	}
	// Das Original ist unberührt
	if rows, _ := env.p.store.ListServers(project.ID); len(rows) != 1 || rows[0].ID != main.ID {
		t.Fatalf("Original-Projekt: %+v", rows)
	}
}

func TestMigrationMovesServerAndDeletesSourceAfterSuccess(t *testing.T) {
	env := newOpsEnv(t)
	project := env.project(t, "Survival", "server")
	main := env.server(t, project.ID, serverRequest{Name: "Survival", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
	env.admin.expect(200, "PUT", "/api/servers/"+main.ID+"/public-address", map[string]string{"address": "abc.joinmc.link:12345"})
	env.admin.expect(202, "POST", "/api/servers/"+main.ID+"/power", map[string]string{"action": "start"})

	viewer := env.member(t, project.ID, "betrachter", "viewer")
	viewer.expect(403, "POST", "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID})
	env.admin.expect(400, "POST", "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.alphaID})
	env.admin.expect(400, "POST", "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": "gibtsnicht"})
	env.beta.mu.Lock()
	env.beta.free = 512 << 20
	env.beta.mu.Unlock()
	if status, body := env.admin.do("POST", "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID}); status != 409 || !strings.Contains(string(body), "Platz") {
		t.Fatalf("zu wenig Platz: %d %s", status, body)
	}
	env.beta.mu.Lock()
	env.beta.free = 100 << 30
	env.beta.mu.Unlock()

	op := startOpRequest(t, env.admin, "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID})
	env.admin.expect(409, "POST", "/api/servers/"+main.ID+"/clone", map[string]any{"name": "Nebenbei"})
	done := waitOp(t, env.admin, op.ID)
	if done.State != OpDone || done.Done == 0 {
		t.Fatalf("Umzug: %+v", done)
	}
	row, _ := env.p.store.ServerByID(main.ID)
	if row.NodeID != env.betaID || row.PublicAddress != "" {
		t.Fatalf("Zeile nach dem Umzug: %+v", row)
	}
	if len(done.Warnings) != 1 || !strings.Contains(done.Warnings[0], "playit") {
		t.Fatalf("Hinweis zur öffentlichen Adresse fehlt: %+v", done.Warnings)
	}
	if env.alpha.has(main.ID) || !env.beta.has(main.ID) {
		t.Fatal("Der Server muss vom alten Node gelöscht und auf dem neuen sein")
	}
	if env.beta.state(main.ID) != node.StateRunning {
		t.Fatal("Ein vorher laufender Server muss auf dem neuen Node wieder laufen")
	}
	if !strings.Contains(done.Message, "beta.example.org:29000") {
		t.Fatalf("Neue Adresse fehlt: %s", done.Message)
	}
	if kinds := eventKindsOf(t, env.p, project.ID); !slices.Contains(kinds, "transfer") {
		t.Fatalf("Ereignis fehlt: %v", kinds)
	}
}

func TestMigrationFailureLeavesSourceUnchanged(t *testing.T) {
	for _, mode := range []string{"import", "break"} {
		t.Run(mode, func(t *testing.T) {
			env := newOpsEnv(t)
			project := env.project(t, "Netz", "network")
			proxy := env.server(t, project.ID, serverRequest{Name: "Proxy", NodeID: env.alphaID, Software: "velocity", Version: "3.4.0"})
			lobby := env.server(t, project.ID, serverRequest{Name: "Lobby", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
			env.admin.expect(202, "POST", "/api/servers/"+lobby.ID+"/power", map[string]string{"action": "start"})
			if mode == "import" {
				env.beta.mu.Lock()
				env.beta.importFail = true
				env.beta.mu.Unlock()
			} else {
				env.alpha.mu.Lock()
				env.alpha.exportMode = "break"
				env.alpha.mu.Unlock()
			}
			op := startOpRequest(t, env.admin, "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID})
			done := waitOp(t, env.admin, op.ID)
			if done.State != OpFailed || !strings.Contains(done.Message, "unverändert") {
				t.Fatalf("Umzug: %+v", done)
			}
			for _, id := range []string{proxy.ID, lobby.ID} {
				row, _ := env.p.store.ServerByID(id)
				if row.NodeID != env.alphaID || !env.alpha.has(id) || env.beta.has(id) {
					t.Fatalf("%s: Quelle muss bleiben, Ziel leer sein (Node %s, Ziel %v)", row.Name, row.NodeID, env.beta.has(id))
				}
			}
			waitUntil(t, "Neustart der Quelle", func() bool { return env.alpha.state(lobby.ID) == node.StateRunning })
			if env.alpha.state(proxy.ID) != node.StateOffline {
				t.Fatal("Ein vorher ausgeschalteter Server darf nicht gestartet werden")
			}
		})
	}
}

func TestMigrationCanBeCanceled(t *testing.T) {
	env := newOpsEnv(t)
	project := env.project(t, "Survival", "server")
	main := env.server(t, project.ID, serverRequest{Name: "Survival", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
	env.alpha.mu.Lock()
	env.alpha.exportMode = "hang"
	env.alpha.mu.Unlock()
	op := startOpRequest(t, env.admin, "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID})
	select {
	case <-env.alpha.exporting:
	case <-time.After(5 * time.Second):
		t.Fatal("Der Export hat nicht begonnen")
	}
	var canceled ServerOp
	_ = json.Unmarshal(env.admin.expect(202, "POST", "/api/server-ops/"+op.ID+"/cancel", nil), &canceled)
	if !canceled.Cancel {
		t.Fatalf("Abbruch nicht vorgemerkt: %+v", canceled)
	}
	done := waitOp(t, env.admin, op.ID)
	if done.State != OpCanceled || !strings.Contains(done.Message, "unverändert") {
		t.Fatalf("Abbruch: %+v", done)
	}
	row, _ := env.p.store.ServerByID(main.ID)
	if row.NodeID != env.alphaID || !env.alpha.has(main.ID) || env.beta.has(main.ID) {
		t.Fatal("Nach dem Abbruch muss alles beim Alten sein")
	}
	// Danach geht ein neuer Umzug
	env.alpha.mu.Lock()
	env.alpha.exportMode = ""
	env.alpha.mu.Unlock()
	again := startOpRequest(t, env.admin, "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID})
	if finished := waitOp(t, env.admin, again.ID); finished.State != OpDone {
		t.Fatalf("zweiter Versuch: %+v", finished)
	}
}

func TestMigrationPreflight(t *testing.T) {
	env := newOpsEnv(t)
	empty := env.project(t, "Leer", "server")
	env.admin.expect(400, "POST", "/api/projects/"+empty.ID+"/migrate", map[string]string{"nodeId": env.betaID})

	network := env.project(t, "Netz", "network")
	env.server(t, network.ID, serverRequest{Name: "Proxy", NodeID: env.alphaID, Software: "velocity", Version: "3.4.0"})
	if _, err := env.p.store.CreateGroup(Group{ProjectID: network.ID, Name: "Mini", Slug: "mini", Software: "paper", Version: "1.21.4", MemoryMB: 1024, MinInstances: 0, MaxInstances: 1, PlayersPerInstance: 10, IdleMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	env.admin.expect(409, "POST", "/api/projects/"+network.ID+"/migrate", map[string]string{"nodeId": env.betaID})

	project := env.project(t, "Survival", "server")
	env.server(t, project.ID, serverRequest{Name: "Survival", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
	env.betaServer.Close()
	if status, body := env.admin.do("POST", "/api/projects/"+project.ID+"/migrate", map[string]string{"nodeId": env.betaID}); status != 502 || !strings.Contains(string(body), "beta") {
		t.Fatalf("Ziel nicht erreichbar: %d %s", status, body)
	}
}

func TestStorageEndpointsAndWarnings(t *testing.T) {
	env := newOpsEnv(t)
	project := env.project(t, "Survival", "server")
	main := env.server(t, project.ID, serverRequest{Name: "Survival", NodeID: env.alphaID, Software: "paper", Version: "1.21.4", EULA: true})
	viewer := env.member(t, project.ID, "betrachter", "viewer")
	operator := env.member(t, project.ID, "operator", "operator")
	stranger := env.member(t, project.ID, "fremd", "")

	var info node.StorageInfo
	_ = json.Unmarshal(viewer.expect(200, "GET", "/api/servers/"+main.ID+"/storage", nil), &info)
	if info.Bytes != 1<<20 || info.Free == 0 {
		t.Fatalf("Speicher: %+v", info)
	}
	stranger.expect(404, "GET", "/api/servers/"+main.ID+"/storage", nil)
	viewer.expect(403, "POST", "/api/servers/"+main.ID+"/storage/measure", nil)
	operator.expect(202, "POST", "/api/servers/"+main.ID+"/storage/measure", nil)
	operator.expect(403, "PUT", "/api/servers/"+main.ID+"/storage", map[string]int{"limitMB": 1024})
	env.admin.expect(400, "PUT", "/api/servers/"+main.ID+"/storage", map[string]int{"limitMB": 10})
	_ = json.Unmarshal(env.admin.expect(200, "PUT", "/api/servers/"+main.ID+"/storage", map[string]int{"limitMB": 1024}), &info)
	if info.LimitMB != 1024 {
		t.Fatalf("Limit: %+v", info)
	}

	var overview nodeStorageDTO
	_ = json.Unmarshal(env.admin.expect(200, "GET", "/api/nodes/"+env.alphaID+"/storage", nil), &overview)
	if !overview.Online || overview.Used != 1<<20 || len(overview.Servers) != 1 || overview.Servers[0].Name != "Survival" || overview.Servers[0].ProjectName != "Survival" {
		t.Fatalf("Node-Übersicht: %+v", overview)
	}
	overview = nodeStorageDTO{}
	_ = json.Unmarshal(viewer.expect(200, "GET", "/api/nodes/"+env.alphaID+"/storage", nil), &overview)
	if overview.Servers != nil || overview.Used != 1<<20 {
		t.Fatalf("Betrachter sieht die Aufteilung: %+v", overview)
	}
	viewer.expect(404, "GET", "/api/nodes/gibtsnicht/storage", nil)

	// 90 % meldet „Speicher fast voll“ einmal, 100 % noch einmal als Fehler, unter 85 % ist es vergessen
	levels := map[string]int{}
	set := func(megabytes int64) {
		env.alpha.mu.Lock()
		env.alpha.bytes[main.ID] = megabytes << 20
		env.alpha.mu.Unlock()
		env.p.checkStorage(context.Background(), levels)
	}
	count := func(kind string) int {
		n := 0
		for _, k := range eventKindsOf(t, env.p, project.ID) {
			if k == kind {
				n++
			}
		}
		return n
	}
	set(500)
	if count("storage") != 0 {
		t.Fatal("Unter 90 % gibt es keine Meldung")
	}
	set(950)
	set(960)
	if count("storage") != 1 {
		t.Fatalf("Bei 90 %% genau eine Meldung, sind %d", count("storage"))
	}
	set(1100)
	if count("storage") != 2 {
		t.Fatal("Beim Erreichen des Limits eine zweite Meldung")
	}
	set(100)
	set(950)
	if count("storage") != 3 {
		t.Fatal("Nach dem Aufräumen wird wieder gemeldet")
	}
	events, _ := env.p.store.ListEvents(project.ID, 10)
	if !strings.Contains(events[0].Message, "Speicher fast voll") {
		t.Fatalf("Meldung: %+v", events[0])
	}

	// Webhooks können das Ereignis abonnieren
	env.admin.expect(201, "POST", "/api/projects/"+project.ID+"/webhooks", map[string]any{"name": "Discord", "url": "https://discord.com/api/webhooks/1/x", "kind": "discord", "events": []string{"storage", "transfer"}})
}
