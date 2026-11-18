package rastreio

import (
	"fmt"
	"math"

	"loltracking/server/arquivo"
	"loltracking/server/banco"
)

// Conteúdos do bloco compactado de um passe.
const (
	ConteudoAmostras = "amostras"
	ConteudoTime     = "amostras_time"
)

// ResultadoFinal é o resumo de um passe fechado.
type ResultadoFinal struct {
	Amostras  int
	Estados   int
	Bytes     int
	BytesTime int
	// BytesCadeia: bytes em disco do arquivo da cadeia.
	BytesCadeia int
	Cadeia      int
	Calibracao  Calibracao
	Taxa        float64
}

// recalculaPosicoes aplica a calibração às amostras de câmera e preenche as
// de trava perdida (origem interpolada) entre as vizinhas com posição, até
// 10 s de cada lado.
func recalculaPosicoes(as []Amostra, cal Calibracao) {
	for i := range as {
		a := &as[i]
		if a.Origem == OrigemCamera && a.CamX != nil && a.CamZ != nil && a.TelaX != nil && a.TelaY != nil {
			x, y := cal.Posicao(*a.CamX, *a.CamZ, *a.TelaX, *a.TelaY)
			a.X, a.Y = &x, &y
		}
	}
	for i := range as {
		a := &as[i]
		if a.Origem != OrigemInterpolada {
			continue
		}
		ant, prox := -1, -1
		for k := i - 1; k >= 0 && as[k].TMs >= a.TMs-10000; k-- {
			if as[k].Origem != OrigemInterpolada && temPos(as[k]) && !as[k].Morto {
				ant = k
				break
			}
		}
		for k := i + 1; k < len(as) && as[k].TMs <= a.TMs+10000; k++ {
			if as[k].Origem != OrigemInterpolada && temPos(as[k]) && !as[k].Morto {
				prox = k
				break
			}
		}
		switch {
		case ant >= 0 && prox >= 0:
			f := float64(a.TMs-as[ant].TMs) / float64(as[prox].TMs-as[ant].TMs)
			x := *as[ant].X + (*as[prox].X-*as[ant].X)*f
			y := *as[ant].Y + (*as[prox].Y-*as[ant].Y)*f
			a.X, a.Y = &x, &y
		case ant >= 0:
			x, y := *as[ant].X, *as[ant].Y
			a.X, a.Y = &x, &y
		case prox >= 0:
			x, y := *as[prox].X, *as[prox].Y
			a.X, a.Y = &x, &y
		}
	}
}

// taxaDe é amostras por segundo de jogo.
func taxaDe(as []Amostra) float64 {
	if len(as) < 2 {
		return 0
	}
	dur := float64(as[len(as)-1].TMs-as[0].TMs) / 1000
	if dur <= 0 {
		return 0
	}
	return float64(len(as)-1) / dur
}

// finalizaPasse fecha o passe: recalibra a posição contra a linha do tempo,
// monta o bloco compactado (amostras e estado dos 10) e a cadeia reduzida, e
// numa transação só grava os dois, apaga as linhas de trabalho e marca o
// passe com estadoFinal.
func finalizaPasse(db *banco.DB, p Passe, lt LinhaDoTempo, time int, base Calibracao, estadoFinal, erro string) (ResultadoFinal, error) {
	var r ResultadoFinal
	as, err := leBrutas(db, p.ID, math.MinInt64, math.MaxInt64)
	if err != nil {
		return r, err
	}
	es, err := leEstadosBrutos(db, p.ID, math.MinInt64, math.MaxInt64)
	if err != nil {
		return r, err
	}
	cal := Ajusta(PontosDoPasse(as, lt, p.ParticipanteID), base)
	recalculaPosicoes(as, cal)
	r.Calibracao, r.Amostras, r.Estados, r.Taxa = cal, len(as), len(es), taxaDe(as)

	blobA, err := Codifica(BlocoDeAmostras(as))
	if err != nil {
		return r, fmt.Errorf("compactar as amostras: %w", err)
	}
	blobE, err := Codifica(BlocoDeEstados(es))
	if err != nil {
		return r, fmt.Errorf("compactar o estado dos 10: %w", err)
	}
	// A cadeia sai do bloco já decodificado (posição arredondada, como ficou
	// gravado): assim RegeraCadeia depois dá exatamente a mesma cadeia.
	ba, err := Decodifica(blobA)
	if err != nil {
		return r, fmt.Errorf("conferir o bloco das amostras: %w", err)
	}
	be, err := Decodifica(blobE)
	if err != nil {
		return r, fmt.Errorf("conferir o bloco do estado dos 10: %w", err)
	}
	ctxCadeia := &ContextoCadeia{GameID: p.GameID, ParticipanteID: p.ParticipanteID, Time: time,
		Amostras: AmostrasDoBloco(ba), Estados: EstadosDoBloco(be), Linha: lt}
	completaContexto(db, ctxCadeia)
	itens := GeraCadeia(ctxCadeia)
	r.Cadeia = len(itens)

	semTrava := 0
	for _, a := range as {
		if a.TravaPerdida {
			semTrava++
		}
	}
	// Os arquivos vão antes da transação: se ela falhar, sobra um arquivo que a
	// próxima finalização regrava; o contrário (linha no banco sem arquivo)
	// deixaria o passe ilegível.
	if r.Bytes, err = arquivo.Grava(db, p.GameID, nomeBloco(p.ID, ConteudoAmostras), blobA); err != nil {
		return r, fmt.Errorf("gravar as amostras: %w", err)
	}
	if r.BytesTime, err = arquivo.Grava(db, p.GameID, nomeBloco(p.ID, ConteudoTime), blobE); err != nil {
		return r, fmt.Errorf("gravar o estado dos 10: %w", err)
	}
	if r.BytesCadeia, err = gravaCadeia(db, p.ID, p.GameID, itens); err != nil {
		return r, err
	}
	tx, err := db.Begin()
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	agora := banco.Agora()
	// No banco fica só a referência, com os bytes que estão no disco.
	grava := func(conteudo string, bytesNoDisco int, n int, ini, fim int64) error {
		_, err := tx.Exec(`INSERT INTO passes_compactos (passe_id, conteudo, versao, n, t_ini_ms, t_fim_ms, bytes, gravado_em)
			VALUES (?,?,?,?,?,?,?,?) ON CONFLICT (passe_id, conteudo) DO UPDATE SET versao = excluded.versao, n = excluded.n,
			t_ini_ms = excluded.t_ini_ms, t_fim_ms = excluded.t_fim_ms, bytes = excluded.bytes,
			gravado_em = excluded.gravado_em`, p.ID, conteudo, VersaoFormato, n, ini, fim, bytesNoDisco, agora)
		return err
	}
	ini, fim := int64(0), int64(0)
	if len(as) > 0 {
		ini, fim = as[0].TMs, as[len(as)-1].TMs
	}
	if err := grava(ConteudoAmostras, r.Bytes, len(as), ini, fim); err != nil {
		return r, err
	}
	iniE, fimE := int64(math.MaxInt64), int64(0)
	for _, e := range es {
		iniE, fimE = min(iniE, e.TMs), max(fimE, e.TMs)
	}
	if len(es) == 0 {
		iniE = 0
	}
	if err := grava(ConteudoTime, r.BytesTime, len(es), iniE, fimE); err != nil {
		return r, err
	}

	if _, err := tx.Exec(`DELETE FROM amostras_brutas WHERE passe_id = ?`, p.ID); err != nil {
		return r, err
	}
	if _, err := tx.Exec(`DELETE FROM amostras_time_brutas WHERE passe_id = ?`, p.ID); err != nil {
		return r, err
	}
	calJSON := jsonTexto(map[string]any{"x0": cal.X0, "x_px": cal.XPx, "y0": cal.Y0, "y_px": cal.YPx, "cx": cal.CX, "cy": cal.CY,
		"largura": cal.Largura, "altura": cal.Altura, "modo": cal.Modo, "descartados": cal.Descartados})
	if _, err := tx.Exec(`UPDATE passes SET estado = ?, erro = ?, amostras = ?, amostras_sem_trava = ?, taxa = ?, calibracao = ?,
			calibracao_pontos = ?, erro_calibracao_mediano = ?, erro_calibracao_maximo = ?, terminado_em = ? WHERE id = ?`,
		estadoFinal, erro, len(as), semTrava, r.Taxa, calJSON, cal.Pontos, cal.ErroMediano, cal.ErroMaximo, agora, p.ID); err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, err
	}
	esqueceCache(p.ID)
	return r, nil
}
