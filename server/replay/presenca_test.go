package replay

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A pessoa usando o computador (digitando em outra janela, ou com Ctrl/Shift
// apertado): nenhuma tecla sai e o jogo não é trazido para a frente; parada
// por LimiarUsoUsuario, a tecla volta a sair. O registro conta as duas coisas.
func TestTeclaRecusadaComPessoaUsandoOComputador(t *testing.T) {
	f, _, d := ambiente()
	d.Presenca = f
	ctx := context.Background()
	s, err := Abre(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Fecha()
	rel := d.Relogio.(*RelogioFalso)

	f.UsuarioUsa(10 * time.Second)
	if err := s.Tecla(ctx, '3'); !errors.Is(err, ErrUsuarioUsando) {
		t.Fatalf("com a pessoa digitando a tecla saiu: %v", err)
	}
	if err := s.ApertaDuplo(ctx, f.Elenco[2].Nome); !errors.Is(err, ErrUsuarioUsando) {
		t.Fatal("o duplo devia recusar")
	}
	if n, err := s.TravaCamera(ctx, f.Elenco[2].Nome); !errors.Is(err, ErrUsuarioUsando) || n != 0 {
		t.Fatalf("TravaCamera com a pessoa digitando: %d %v", n, err)
	}
	if len(f.Teclas) != 0 {
		t.Fatalf("teclas saíram: %v", f.Teclas)
	}
	// Parou de digitar há menos de N: ainda em uso.
	rel.Avanca(10*time.Second + LimiarUsoUsuario/2)
	if err := s.Tecla(ctx, '3'); !errors.Is(err, ErrUsuarioUsando) {
		t.Fatalf("parada há %s ainda conta como uso: %v", LimiarUsoUsuario/2, err)
	}
	rel.Avanca(LimiarUsoUsuario)
	if err := s.Tecla(ctx, '3'); err != nil {
		t.Fatalf("com o computador parado a tecla devia sair: %v", err)
	}
	// Ctrl/Shift apertado: a tecla sairia combinada.
	f.Modificador = true
	if err := s.Tecla(ctx, '3'); !errors.Is(err, ErrUsuarioUsando) {
		t.Fatalf("com modificador apertado a tecla saiu: %v", err)
	}
	f.Modificador = false
	if len(f.Teclas) != 1 || f.Teclas[0] != "3" {
		t.Fatalf("teclas %v", f.Teclas)
	}
	dg := s.Diagnostico()
	if dg.Enviadas != 1 || dg.RecusadasUso < 4 || dg.ModificadorOn != 1 || dg.FrenteAntes[ExeDoJogo] != 1 {
		t.Fatalf("diagnóstico %+v", dg)
	}
	// Teclas fora da lista continuam recusadas antes de tudo.
	if err := s.Tecla(ctx, 'A'); err == nil || errors.Is(err, ErrUsuarioUsando) {
		t.Fatalf("tecla A: %v", err)
	}
}

func TestEmUsoSemPresenca(t *testing.T) {
	if uso, _ := EmUso(nil, time.Now()); uso {
		t.Fatal("sem sinal não pode dar uso")
	}
	if OciosoDesde(nil, time.Now()) < time.Hour {
		t.Fatal("sem sinal = ocioso")
	}
}
