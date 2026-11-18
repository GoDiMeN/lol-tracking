package rastreio

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// O bloco compactado: todas as amostras de um passe num formato colunar
// binário (docs/arquitetura/dados.md, "Formato do bloco compactado").
//
//	blob    = "LTC" + byte compressor (1 = flate) + flate(corpo)
//	corpo   = uvarint versão, uvarint n_linhas, uvarint n_colunas, coluna...
//	coluna  = texto nome, byte tipo (0 inteiro, 1 texto), uvarint escala,
//	          [texto: uvarint n_dicionário, texto...],
//	          byte nulos (0 nenhum, 1 todos, 2 corridas: uvarint n, uvarint...),
//	          [se não todos nulos: byte codificação (0 delta, 1 corridas, 2 delta²),
//	           uvarint tamanho, dados]
//	texto   = uvarint tamanho + bytes UTF-8
//
// Só os valores não nulos são codificados. Inteiros em zigzag+varint. A
// codificação de cada coluna é a menor das três (escolhida na hora). Coluna é
// achada pelo NOME: coluna nova (a tarefa 3 acrescenta as da tela) não quebra
// blob antigo — quem lê um blob sem a coluna recebe tudo nulo.

// VersaoFormato é a versão do corpo. Acrescentar coluna NÃO muda a versão;
// mudar a codificação de algo que já existe, muda.
const VersaoFormato = 1

const (
	tipoInteiro = 0
	tipoTexto   = 1

	codDelta   = 0
	codCorrida = 1
	codDelta2  = 2

	// compressorNenhum: o corpo vai cru, porque quem comprime é o arquivo (zstd).
	compressorNenhum = 0
	// compressorFlate: só para LER blob antigo na conversão única da subida.
	compressorFlate = 1
)

// Coluna é uma coluna do bloco. Inteiro: Ints (valor real = Ints/Escala).
// Texto: Textos. Nulos[i] = sem valor na linha i (Nulos nil = nenhum nulo).
type Coluna struct {
	Nome   string
	Texto  bool
	Escala int64
	Ints   []int64
	Textos []string
	Nulos  []bool
}

// Nulo diz se a linha i não tem valor.
func (c *Coluna) Nulo(i int) bool { return c == nil || (c.Nulos != nil && c.Nulos[i]) }

// Bloco é uma tabela colunar.
type Bloco struct {
	Versao  int
	N       int
	Colunas []Coluna
}

// Col acha a coluna pelo nome (nil se o blob é anterior a ela).
func (b *Bloco) Col(nome string) *Coluna {
	for i := range b.Colunas {
		if b.Colunas[i].Nome == nome {
			return &b.Colunas[i]
		}
	}
	return nil
}

type escritor struct{ bytes.Buffer }

func (e *escritor) uvarint(v uint64) {
	var b [binary.MaxVarintLen64]byte
	e.Write(b[:binary.PutUvarint(b[:], v)])
}

func (e *escritor) varint(v int64) { e.uvarint(uint64((v << 1) ^ (v >> 63))) }

func (e *escritor) texto(s string) {
	e.uvarint(uint64(len(s)))
	e.WriteString(s)
}

// Codifica monta o bloco: "LTC" + 0 + corpo, sem compressão, porque quem
// comprime é o arquivo (pacote arquivo, zstd).
func Codifica(b Bloco) ([]byte, error) {
	c, err := corpoDoBloco(b)
	if err != nil {
		return nil, err
	}
	saida := make([]byte, 0, 4+len(c))
	saida = append(saida, 'L', 'T', 'C', compressorNenhum)
	return append(saida, c...), nil
}

func corpoDoBloco(b Bloco) ([]byte, error) {
	var c escritor
	c.uvarint(VersaoFormato)
	c.uvarint(uint64(b.N))
	c.uvarint(uint64(len(b.Colunas)))
	for _, col := range b.Colunas {
		if err := codificaColuna(&c, col, b.N); err != nil {
			return nil, err
		}
	}
	return c.Bytes(), nil
}

func codificaColuna(c *escritor, col Coluna, n int) error {
	if col.Nulos != nil && len(col.Nulos) != n {
		return fmt.Errorf("coluna %s: %d nulos para %d linhas", col.Nome, len(col.Nulos), n)
	}
	c.texto(col.Nome)
	var valores []int64
	if col.Texto {
		if len(col.Textos) != n {
			return fmt.Errorf("coluna %s: %d textos para %d linhas", col.Nome, len(col.Textos), n)
		}
		c.WriteByte(tipoTexto)
		c.uvarint(0)
		dic, idx := []string{}, map[string]int64{}
		for i, s := range col.Textos {
			if col.Nulo(i) {
				continue
			}
			k, ok := idx[s]
			if !ok {
				k = int64(len(dic))
				idx[s] = k
				dic = append(dic, s)
			}
			valores = append(valores, k)
		}
		c.uvarint(uint64(len(dic)))
		for _, s := range dic {
			c.texto(s)
		}
	} else {
		if len(col.Ints) != n {
			return fmt.Errorf("coluna %s: %d valores para %d linhas", col.Nome, len(col.Ints), n)
		}
		c.WriteByte(tipoInteiro)
		esc := col.Escala
		if esc <= 0 {
			esc = 1
		}
		c.uvarint(uint64(esc))
		for i, v := range col.Ints {
			if !col.Nulo(i) {
				valores = append(valores, v)
			}
		}
	}
	// Nulos em corridas alternadas, começando por "não nulo".
	switch {
	case len(valores) == n:
		c.WriteByte(0)
	case len(valores) == 0:
		c.WriteByte(1)
		return nil
	default:
		c.WriteByte(2)
		var corridas []uint64
		atual, conta := false, uint64(0)
		for i := 0; i < n; i++ {
			if col.Nulo(i) != atual {
				corridas = append(corridas, conta)
				atual, conta = !atual, 0
			}
			conta++
		}
		corridas = append(corridas, conta)
		c.uvarint(uint64(len(corridas)))
		for _, r := range corridas {
			c.uvarint(r)
		}
	}
	melhor, cod := []byte(nil), byte(0)
	for _, k := range []byte{codDelta, codCorrida, codDelta2} {
		d := codificaValores(valores, k)
		if melhor == nil || len(d) < len(melhor) {
			melhor, cod = d, k
		}
	}
	c.WriteByte(cod)
	c.uvarint(uint64(len(melhor)))
	c.Write(melhor)
	return nil
}

func codificaValores(v []int64, cod byte) []byte {
	var e escritor
	switch cod {
	case codDelta:
		ant := int64(0)
		for _, x := range v {
			e.varint(x - ant)
			ant = x
		}
	case codDelta2:
		ant, antD := int64(0), int64(0)
		for _, x := range v {
			d := x - ant
			e.varint(d - antD)
			ant, antD = x, d
		}
	case codCorrida:
		ant := int64(0)
		for i := 0; i < len(v); {
			j := i
			for j < len(v) && v[j] == v[i] {
				j++
			}
			e.varint(v[i] - ant)
			e.uvarint(uint64(j - i))
			ant = v[i]
			i = j
		}
	}
	return e.Bytes()
}

// ErrBlobInvalido: o blob não é deste formato ou está corrompido.
var ErrBlobInvalido = errors.New("bloco compactado inválido")

type leitor struct{ *bytes.Reader }

func (l leitor) uvarint() (uint64, error) { return binary.ReadUvarint(l) }

func (l leitor) varint() (int64, error) {
	u, err := binary.ReadUvarint(l)
	return int64(u>>1) ^ -int64(u&1), err
}

func (l leitor) texto() (string, error) {
	n, err := l.uvarint()
	if err != nil || n > 1<<20 {
		return "", ErrBlobInvalido
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(l, b); err != nil {
		return "", ErrBlobInvalido
	}
	return string(b), nil
}

// Decodifica lê o blob.
func Decodifica(blob []byte) (Bloco, error) {
	if len(blob) < 4 || string(blob[:3]) != "LTC" {
		return Bloco{}, ErrBlobInvalido
	}
	var corpo []byte
	switch blob[3] {
	case compressorNenhum:
		corpo = blob[4:]
	case compressorFlate:
		var err error
		if corpo, err = io.ReadAll(flate.NewReader(bytes.NewReader(blob[4:]))); err != nil {
			return Bloco{}, fmt.Errorf("%w: %v", ErrBlobInvalido, err)
		}
	default:
		return Bloco{}, ErrBlobInvalido
	}
	l := leitor{bytes.NewReader(corpo)}
	versao, err := l.uvarint()
	if err != nil || versao != VersaoFormato {
		return Bloco{}, fmt.Errorf("%w: versão %d", ErrBlobInvalido, versao)
	}
	n64, err1 := l.uvarint()
	nc, err2 := l.uvarint()
	if err1 != nil || err2 != nil || n64 > 1<<26 || nc > 1<<12 {
		return Bloco{}, ErrBlobInvalido
	}
	n := int(n64)
	b := Bloco{Versao: int(versao), N: n}
	for k := uint64(0); k < nc; k++ {
		col, err := decodificaColuna(l, n)
		if err != nil {
			return Bloco{}, fmt.Errorf("%w (coluna %d %q)", err, k, col.Nome)
		}
		b.Colunas = append(b.Colunas, col)
	}
	return b, nil
}

func decodificaColuna(l leitor, n int) (Coluna, error) {
	var col Coluna
	var err error
	if col.Nome, err = l.texto(); err != nil {
		return col, err
	}
	tipo, err := l.ReadByte()
	if err != nil {
		return col, ErrBlobInvalido
	}
	esc, err := l.uvarint()
	if err != nil {
		return col, ErrBlobInvalido
	}
	col.Escala = int64(esc)
	var dic []string
	if tipo == tipoTexto {
		col.Texto = true
		nd, err := l.uvarint()
		if err != nil || nd > uint64(n)+1 {
			return col, ErrBlobInvalido
		}
		for i := uint64(0); i < nd; i++ {
			s, err := l.texto()
			if err != nil {
				return col, err
			}
			dic = append(dic, s)
		}
	}
	modo, err := l.ReadByte()
	if err != nil {
		return col, ErrBlobInvalido
	}
	nulos := make([]bool, n)
	validos := n
	switch modo {
	case 0:
	case 1:
		for i := range nulos {
			nulos[i] = true
		}
		validos = 0
	case 2:
		nr, err := l.uvarint()
		if err != nil {
			return col, ErrBlobInvalido
		}
		pos, nulo := 0, false
		validos = 0
		for i := uint64(0); i < nr; i++ {
			r, err := l.uvarint()
			if err != nil || pos+int(r) > n {
				return col, ErrBlobInvalido
			}
			for j := 0; j < int(r); j++ {
				nulos[pos+j] = nulo
			}
			if !nulo {
				validos += int(r)
			}
			pos += int(r)
			nulo = !nulo
		}
		if pos != n {
			return col, ErrBlobInvalido
		}
	default:
		return col, ErrBlobInvalido
	}
	if modo != 0 {
		col.Nulos = nulos
	}
	valores := make([]int64, 0, validos)
	// Só 'todos nulos' não tem dados (com 0 linhas também há codificação e tamanho).
	if modo != 1 {
		cod, err := l.ReadByte()
		if err != nil {
			return col, ErrBlobInvalido
		}
		tam, err := l.uvarint()
		if err != nil || tam > uint64(l.Len()) {
			return col, ErrBlobInvalido
		}
		dados := make([]byte, tam)
		io.ReadFull(l, dados)
		dl := leitor{bytes.NewReader(dados)}
		switch cod {
		case codDelta, codDelta2:
			ant, antD := int64(0), int64(0)
			for len(valores) < validos {
				d, err := dl.varint()
				if err != nil {
					return col, ErrBlobInvalido
				}
				if cod == codDelta2 {
					d += antD
					antD = d
				}
				ant += d
				valores = append(valores, ant)
			}
		case codCorrida:
			ant := int64(0)
			for len(valores) < validos {
				d, err1 := dl.varint()
				r, err2 := dl.uvarint()
				if err1 != nil || err2 != nil || len(valores)+int(r) > validos {
					return col, ErrBlobInvalido
				}
				ant += d
				for j := uint64(0); j < r; j++ {
					valores = append(valores, ant)
				}
			}
		default:
			return col, ErrBlobInvalido
		}
	}
	if col.Texto {
		col.Textos = make([]string, n)
	} else {
		col.Ints = make([]int64, n)
	}
	k := 0
	for i := 0; i < n; i++ {
		if nulos[i] {
			continue
		}
		v := valores[k]
		k++
		if col.Texto {
			if v < 0 || int(v) >= len(dic) {
				return col, ErrBlobInvalido
			}
			col.Textos[i] = dic[v]
		} else {
			col.Ints[i] = v
		}
	}
	return col, nil
}
