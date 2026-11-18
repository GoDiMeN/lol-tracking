package riot

// Regiao liga uma plataforma (o servidor onde a conta joga) aos hosts da API
// da Riot: o da plataforma (league-v4) e o do roteamento regional (account-v1,
// match-v5). A tabela é o único lugar com host da Riot por região: quem
// precisa de outra região acrescenta uma linha aqui, com Habilitada só depois
// de conferida.
type Regiao struct {
	// Codigo é a plataforma na API da Riot (BR1, NA1...).
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
	// RegiaoCliente é o que o cliente do LoL diz em /riotclient/region-locale
	// quando a conta logada é desta plataforma. O replay só abre para partidas
	// da plataforma da conta logada: a coleta confere os dois.
	RegiaoCliente string `json:"regiao_cliente"`
	Habilitada    bool   `json:"habilitada"`
	Plataforma    string `json:"-"`
	Roteamento    string `json:"-"`
}

// regioes: por enquanto só a BR. As outras virão depois (cada uma precisa da
// conta logada na mesma plataforma para o replay abrir).
var regioes = []Regiao{
	{Codigo: "BR1", Nome: "Brasil (BR)", RegiaoCliente: "BR", Habilitada: true,
		Plataforma: BaseRiot, Roteamento: BaseRegional},
}

// RegiaoPadrao é a plataforma usada quando o pedido não diz qual.
const RegiaoPadrao = "BR1"

// Regioes devolve a tabela (cópia).
func Regioes() []Regiao {
	return append([]Regiao(nil), regioes...)
}

// RegiaoPorCodigo acha uma região habilitada pelo código (BR1).
func RegiaoPorCodigo(codigo string) (Regiao, bool) {
	for _, r := range regioes {
		if r.Codigo == codigo && r.Habilitada {
			return r, true
		}
	}
	return Regiao{}, false
}
