package campeao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Respostas no formato do Data Dragon e do CommunityDragon (resumidas da 16.22).
const championJSON = `{"data":{"Leona":{"id":"Leona","key":"89","name":"Leona","stats":{"movespeed":335,"armor":43,"armorperlevel":4.8,
"spellblock":32,"spellblockperlevel":2.05,"attackdamage":60,"attackdamageperlevel":0,"attackspeedperlevel":2.9,"attackspeed":0.625}}}}`

const itemJSON = `{"data":{"6699":{"name":"Cicloespada Voltaica","description":"<mainText><stats><attention>55</attention> de Dano de Ataque<br><attention>10</attention> de Letalidade</stats><br><br><passive>Galvanizar</passive><br>Causar dano a <keyword>Campeões</keyword>.</mainText>","gold":{"total":3000},"stats":{"FlatPhysicalDamageMod":55}},
"3087":{"name":"Faca de Statikk","description":"<mainText><stats><attention>30%</attention> de Velocidade de Ataque<br><attention>4%</attention> de Velocidade de Movimento</stats></mainText>","gold":{"total":3000},"stats":{}}}}`

const leonaBin = `{"Characters/Leona/CharacterRecords/Root":{"__type":"CharacterRecord","baseDamageModifiable":{"baseValue":60},
"damagePerLevelModifiable":{"baseValue":3},"armorPerLevelModifiable":{"baseValue":4.799999713897705},"attackSpeedRatioModifiable":{"baseValue":0.625}}}`

func servidorFalso(t *testing.T, pedidos *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		switch r.URL.Path {
		case "/api/versions.json":
			w.Write([]byte(`["16.23.1","16.22.1"]`))
		case "/cdn/16.22.1/data/pt_BR/champion.json":
			w.Write([]byte(championJSON))
		case "/cdn/16.22.1/data/pt_BR/item.json":
			w.Write([]byte(itemJSON))
		case "/16.22/game/data/characters/leona/leona.bin.json":
			w.Write([]byte(leonaBin))
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestEstaticosVersaoDaPartidaRegistroDoJogoECache(t *testing.T) {
	var pedidos atomic.Int32
	srv := servidorFalso(t, &pedidos)
	dir := t.TempDir()
	e := NovosEstaticos(dir)
	e.baseDD, e.baseCD = srv.URL, srv.URL
	ctx := context.Background()
	est, err := e.Estaticos(ctx, "16.22.834.8910", []int{89})
	if err != nil {
		t.Fatal(err)
	}
	if est.VersaoDados != "16.22.1" {
		t.Fatalf("versão = %q (devia ser a do patch da partida, não a mais nova)", est.VersaoDados)
	}
	l := est.Campeoes[89]
	// O Data Dragon dá 0 de crescimento de DA; o registro do jogo dá 3.
	if l.Fonte != "jogo" || l.ADNivel != 3 || l.ArmaduraNivel != 4.8 || l.VelAtaqueRazao != 0.625 || l.RM != 32 {
		t.Fatalf("Leona = %+v", l)
	}
	it := est.Itens[6699]
	if it.Custo != 3000 || it.Atributos.AD != 55 || len(it.Linhas) != 2 || !strings.HasPrefix(it.Descricao, "Galvanizar: Causar dano a Campeões") {
		t.Fatalf("item = %+v", it)
	}
	// Atributo que só está no texto do jogo (stats vazio no Data Dragon).
	if s := est.Itens[3087].Atributos; s.VelAtaquePct != 0.3 || s.VelMovPct != 0.04 {
		t.Fatalf("do texto: %+v", s)
	}

	// Outro processo, mesmo diretório, rede fora: tudo do cache.
	srv.Close()
	e2 := NovosEstaticos(dir)
	e2.baseDD, e2.baseCD = srv.URL, srv.URL
	est2, err := e2.Estaticos(ctx, "16.22", []int{89})
	if err != nil {
		t.Fatalf("do cache: %v", err)
	}
	if est2.Campeoes[89].ADNivel != 3 || est2.Itens[6699].Custo != 3000 {
		t.Fatalf("cache = %+v", est2.Campeoes[89])
	}
}

func TestEstaticosOfflineSemCache(t *testing.T) {
	e := NovosEstaticos(t.TempDir())
	e.baseDD, e.baseCD = "http://127.0.0.1:1", "http://127.0.0.1:1"
	if _, err := e.Estaticos(context.Background(), "16.22", []int{89}); !errors.Is(err, ErrSemEstaticos) {
		t.Fatalf("erro = %v", err)
	}
}

func TestDoRegistroDoJogoSemRazaoUsaBase(t *testing.T) {
	var bin map[string]json.RawMessage
	json.Unmarshal([]byte(`{"Characters/X/CharacterRecords/Root":{"attackSpeedModifiable":{"baseValue":0.7}}}`), &bin)
	r, ok := DoRegistroDoJogo(bin, AtributosCampeao{Armadura: 30})
	if !ok || r.VelAtaqueRazao != 0.7 || r.Armadura != 30 || r.ADNivel != 0 {
		t.Fatalf("r = %+v", r)
	}
}

func TestLimpaDescricao(t *testing.T) {
	linhas, desc := LimpaDescricao(`<mainText><stats><attention>40</attention> de Dano de Ataque</stats><br><br><active>Ativo</active> (90s)<br>Posiciona &amp; vê.</mainText>`)
	if len(linhas) != 1 || linhas[0] != "40 de Dano de Ataque" || desc != "Ativo: (90s)\nPosiciona & vê." {
		t.Fatalf("linhas = %q, desc = %q", linhas, desc)
	}
}
