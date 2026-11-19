package aprendizado

import "log"

// O que o pacote captura (pausar/retomar a captura em andamento, seja qual
// for) precisa da coleta: qual roda agora, pausá-la AGORA (não só ao fim do
// passe) e achar a coleta dona de um rastreio pausado.

// ColetaAtual devolve a coleta rodando e a execução dela (false = nenhuma).
func (k *Coletor) ColetaAtual() (int64, string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.atual == nil {
		return 0, "", false
	}
	return k.atual.coletaID, k.atual.ex.ID(), true
}

// PausaAgora pausa a coleta já: a pausa graciosa de sempre (nada novo começa)
// e o rastreio em andamento para agora (o replay fecha, o passe guarda o
// ponto e a retomada da coleta continua dele). Na descoberta ou no download,
// para ao fim do passo em andamento, como a pausa graciosa.
func (k *Coletor) PausaAgora(id int64) error {
	if err := k.Pausa(id); err != nil {
		return err
	}
	k.mu.Lock()
	c := k.atual
	k.mu.Unlock()
	if c == nil || c.coletaID != id || c.rastreio.Load() == 0 {
		return nil
	}
	if _, err := k.d.Fila.PausaAgora(); err != nil {
		log.Printf("aprendizado: pausar agora o rastreio da coleta %d: %v", id, err)
	}
	return nil
}

// ColetaDoRastreio: a coleta que criou o rastreio (false = rastreio avulso).
func (k *Coletor) ColetaDoRastreio(rastreioID int64) (int64, bool) {
	var id int64
	if err := k.d.DB.QueryRow(`SELECT coleta_id FROM aprendizado_partidas WHERE rastreio_id = ? ORDER BY id DESC LIMIT 1`,
		rastreioID).Scan(&id); err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// EstadoDaColeta: o estado gravado da coleta ("" = não existe).
func (k *Coletor) EstadoDaColeta(id int64) (string, string) {
	c, err := LeColeta(k.d.DB, id)
	if err != nil {
		return "", ""
	}
	return c.Estado, c.Erro
}
