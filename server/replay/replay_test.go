package replay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type lcuFalso struct {
	fase    string
	jogo    *Falso
	watches int
}

func (l *lcuFalso) FaseGameflow(ctx context.Context) (string, error) { return l.fase, nil }
func (l *lcuFalso) AssisteReplay(ctx context.Context, id int64) error {
	l.watches++
	l.jogo.AbreReplay()
	return nil
}

func ambiente() (*Falso, *lcuFalso, Deps) {
	rel := NovoRelogioFalso()
	f := NovoFalso(rel)
	l := &lcuFalso{fase: "None", jogo: f}
	return f, l, Deps{LCU: l, API: f, Jogo: f, Teclado: f, Tela: f, Relogio: rel}
}

func TestLePonto(t *testing.T) {
	p := LePonto("1280.5,720.25")
	if !p.Valido || p.X != 1280.5 || p.Y != 720.25 {
		t.Fatalf("%+v", p)
	}
	for _, s := range []string{"340282346638528859811704183484516925440.000,340282346638528859811704183484516925440.000", "", "1,2,3", "a,b"} {
		if LePonto(s).Valido {
			t.Errorf("%q deveria ser inválido", s)
		}
	}
	if !LePonto("10,10").Dentro(100, 100, 4) || LePonto("1,50").Dentro(100, 100, 4) {
		t.Fatal("Dentro")
	}
}

func TestTeclaDo(t *testing.T) {
	js := []Jogador{{Nome: "a", Time: "ORDER"}, {Nome: "b", Time: "ORDER"}, {Nome: "x", Time: "CHAOS"},
		{Nome: "c", Time: "ORDER"}, {Nome: "y", Time: "CHAOS"}}
	casos := map[string]rune{"a": '1', "b": '2', "c": '3', "x": 'Q', "y": 'W'}
	for n, esperado := range casos {
		if k, err := TeclaDo(js, n); err != nil || k != esperado {
			t.Errorf("%s: %q %v", n, k, err)
		}
	}
	if _, err := TeclaDo(js, "z"); !errors.Is(err, ErrJogadorNaoEncontrado) {
		t.Fatal(err)
	}
	for _, k := range "12345QWERT" {
		if !TeclaPermitida(k) {
			t.Fatal(k)
		}
	}
	for _, k := range "06ABYZ \t" {
		if TeclaPermitida(k) {
			t.Fatalf("%q não pode", k)
		}
	}
}

func TestGameCfg(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "game.cfg")
	orig := "[FloatingText]\r\nSpecial_Enabled=1\r\n\r\n[General]\r\nEnableReplayApi=0\r\nWidth=3840\r\n"
	os.WriteFile(cfg, []byte(orig), 0o644)
	mudou, err := GaranteReplayAPI(cfg)
	if err != nil || !mudou {
		t.Fatal(mudou, err)
	}
	b, _ := os.ReadFile(cfg)
	if string(b) != strings.Replace(orig, "EnableReplayApi=0", "EnableReplayApi=1", 1) {
		t.Fatalf("mexeu em mais do que a linha: %q", b)
	}
	bak, _ := os.ReadFile(cfg + SufixoBackup)
	if string(bak) != orig {
		t.Fatal("backup errado")
	}
	if mudou, _ := GaranteReplayAPI(cfg); mudou {
		t.Fatal("já estava ligado")
	}
	// Sem a linha: entra logo depois de [General]; o backup não é refeito.
	os.WriteFile(cfg, []byte("[General]\nWidth=1\n[Outra]\nEnableReplayApi=0\n"), 0o644)
	GaranteReplayAPI(cfg)
	b, _ = os.ReadFile(cfg)
	if string(b) != "[General]\nEnableReplayApi=1\nWidth=1\n[Outra]\nEnableReplayApi=0\n" {
		t.Fatalf("%q", b)
	}
	bak, _ = os.ReadFile(cfg + SufixoBackup)
	if string(bak) != orig {
		t.Fatal("o backup foi sobrescrito")
	}
}

func TestAbreRecusaFaseAoVivoSemTecla(t *testing.T) {
	f, l, d := ambiente()
	for _, fase := range []string{"InProgress", "ChampSelect", "Lobby", "ReadyCheck", "GameStart", "Reconnect"} {
		l.fase = fase
		_, err := Abre(context.Background(), d, 1)
		var ef *ErroFase
		if !errors.As(err, &ef) {
			t.Fatalf("%s: %v", fase, err)
		}
	}
	if l.watches != 0 || len(f.Teclas) != 0 {
		t.Fatalf("watch %d, teclas %v", l.watches, f.Teclas)
	}
}

func TestAbreSemReplayAPINaoMandaTeclaEFecha(t *testing.T) {
	f, l, d := ambiente()
	f.NuncaAbre = true
	f.NaoSai = true
	_, err := Abre(context.Background(), d, 1)
	if !errors.Is(err, ErrSemReplay) {
		t.Fatal(err)
	}
	if l.watches != 1 || len(f.Teclas) != 0 {
		t.Fatalf("watch %d, teclas %v", l.watches, f.Teclas)
	}
	// O watch foi pedido: o processo tem de ter sido encerrado (o /Exit não respondeu).
	if f.Aberto() || f.Encerrados != 1 {
		t.Fatalf("o jogo ficou aberto (encerrados %d)", f.Encerrados)
	}
	// Uma sessão sem confirmação recusa tecla.
	s := &Sessao{d: d}
	if err := s.Tecla(context.Background(), '1'); !errors.Is(err, ErrSemReplay) {
		t.Fatal(err)
	}
	if len(f.Teclas) != 0 {
		t.Fatal("tecla enviada sem replay")
	}
}

func TestTeclaRecusadaComFaseAoVivoOuSemFoco(t *testing.T) {
	f, l, d := ambiente()
	s, err := Abre(context.Background(), d, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fecha()
	l.fase = "ChampSelect"
	if err := s.Tecla(context.Background(), '1'); err == nil {
		t.Fatal("tecla com fase ao vivo")
	}
	l.fase = "WatchInProgress"
	if err := s.Tecla(context.Background(), 'A'); err == nil {
		t.Fatal("tecla fora da lista")
	}
	f.SemFoco = true
	if err := s.Tecla(context.Background(), '1'); err == nil {
		t.Fatal("tecla sem foco")
	}
	if len(f.Teclas) != 0 {
		t.Fatalf("teclas %v", f.Teclas)
	}
	f.SemFoco = false
	if err := s.Tecla(context.Background(), '1'); err != nil || len(f.Teclas) != 1 {
		t.Fatal(err, f.Teclas)
	}
	s.Fecha()
	if err := s.Tecla(context.Background(), '1'); !errors.Is(err, ErrSemReplay) {
		t.Fatal("tecla depois de fechar")
	}
}

func TestTravaCameraTPausadoERetentativa(t *testing.T) {
	f, _, d := ambiente()
	s, err := Abre(context.Background(), d, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fecha()
	ctx := context.Background()
	// Pausado: a trava despausa antes (pausado o T falha).
	f.MudaPlayback(ctx, MudaPlayback{Pausado: Ptr(true)})
	n, err := s.TravaCamera(ctx, "Jogador 10")
	if err != nil || n != 1 {
		t.Fatalf("trava T: %d %v", n, err)
	}
	if pb, _ := f.Playback(ctx); pb.Pausado {
		t.Fatal("deveria ter despausado")
	}
	// Seek derruba; duas falhas antes de prender.
	f.MudaPlayback(ctx, MudaPlayback{Tempo: Ptr(100.0)})
	f.FalhasTrava = 2
	n, err = s.TravaCamera(ctx, "Jogador 3")
	if err != nil || n != 3 {
		t.Fatalf("retentativa: %d %v", n, err)
	}
	r, _ := f.Render(ctx)
	if r.Selecao != "Jogador 3" {
		t.Fatal(r.Selecao)
	}
	// Morto: não trava.
	f.Mortes = []MorteFalsa{{Indice: 1, De: 0, Ate: 10000}}
	if _, err := s.TravaCamera(ctx, "Jogador 2"); !errors.Is(err, ErrMorto) {
		t.Fatal(err)
	}
	// Desiste depois de MaxTentativasTrava.
	f.FalhasTrava = 100
	if n, err := s.TravaCamera(ctx, "Jogador 4"); !errors.Is(err, ErrTravaFalhou) || n != MaxTentativasTrava {
		t.Fatal(n, err)
	}
}

func TestFechaComExitEComEncerra(t *testing.T) {
	f, _, d := ambiente()
	s, err := Abre(context.Background(), d, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Fecha(); err != nil || f.Aberto() || f.Saidas != 1 || f.Encerrados != 0 {
		t.Fatalf("exit: %v saídas %d encerrados %d", err, f.Saidas, f.Encerrados)
	}
	s.Fecha() // idempotente
	if f.Saidas != 1 {
		t.Fatal("fechou duas vezes")
	}
	f2, _, d2 := ambiente()
	f2.NaoSai = true
	s2, _ := Abre(context.Background(), d2, 1)
	inicio := d2.Relogio.Agora()
	s2.Fecha()
	if f2.Aberto() || f2.Encerrados != 1 {
		t.Fatal("não encerrou")
	}
	if esperou := d2.Relogio.Agora().Sub(inicio); esperou < PrazoSair {
		t.Fatalf("encerrou antes de esperar %s (esperou %s)", PrazoSair, esperou)
	}
}

func TestAbreFechaJogoQueSobrou(t *testing.T) {
	f, l, d := ambiente()
	f.AbreReplay()
	d.Relogio.(*RelogioFalso).Avanca(10 * time.Second)
	s, err := Abre(context.Background(), d, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fecha()
	if f.Saidas != 1 || l.watches != 1 || f.Aberturas != 2 {
		t.Fatalf("saídas %d watches %d aberturas %d", f.Saidas, l.watches, f.Aberturas)
	}
}

func TestAPIRealContraServidor(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /replay/playback":
			w.Write([]byte(`{"length":2340.5,"paused":false,"seeking":false,"speed":2.0,"time":361.25}`))
		case "GET /liveclientdata/playerlist":
			w.Write([]byte(`[{"riotIdGameName":"GodiNzt","team":"ORDER","isDead":false,"level":7,
				"scores":{"kills":1,"deaths":2,"assists":3,"creepScore":40,"wardScore":2.5},
				"items":[{"itemID":1055,"slot":0,"count":1}],"screenPositionCenter":"1281.5,700.25"},
				{"riotIdGameName":"X","team":"CHAOS","screenPositionCenter":"340282346638528859811704183484516925440.000,340282346638528859811704183484516925440.000"}]`))
		case "GET /liveclientdata/eventdata":
			w.Write([]byte(`{"Events":[{"EventID":3,"EventName":"ChampionKill","EventTime":528.4,"KillerName":"A","VictimName":"GodiNzt","Assisters":["B"]}]}`))
		case "POST /Exit":
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := NovaAPI(srv.URL)
	ctx := context.Background()
	pb, err := c.Playback(ctx)
	if err != nil || pb.Tempo != 361.25 || pb.Velocidade != 2 {
		t.Fatal(pb, err)
	}
	js, err := c.Jogadores(ctx)
	if err != nil || !js[0].Centro.Valido || js[0].Centro.X != 1281.5 || js[1].Centro.Valido || js[0].Placar.Ward != 2.5 {
		t.Fatalf("%+v %v", js, err)
	}
	evs, err := c.Eventos(ctx)
	if err != nil || len(evs) != 1 || evs[0].Vitima != "GodiNzt" || len(evs[0].Bruto) == 0 {
		t.Fatal(evs, err)
	}
	if err := c.Sai(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Render(ctx); err == nil {
		t.Fatal("404 deveria ser erro")
	}
	// Endereço fora do loopback é recusado antes de conectar.
	if _, err := NovaAPI("https://10.0.0.1:2999").Playback(ctx); err == nil || !strings.Contains(err.Error(), "recusada") {
		t.Fatal(err)
	}
}
