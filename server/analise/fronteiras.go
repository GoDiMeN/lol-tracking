package analise

// AS FRONTEIRAS DAS ZONAS DO MAPA (Summoner's Rift, mapa 11), num só lugar
// para ajustar. Coordenadas do jogo (as mesmas da linha do tempo e do
// rastreio): x da esquerda para a direita, y de baixo para cima; base azul no
// canto de baixo à esquerda, vermelha no de cima à direita.
//
// O mapa é simétrico pelo ponto: o que vale para o lado azul vale para o
// vermelho em Simetria − p. Por isso a rota de baixo é a rota do topo
// espelhada e o rio de baixo é o de cima espelhado: ajustar um lado ajusta os
// dois. Conferência visual: docs/arquitetura/zonas.png (gerada por
// TestGeraImagemZonas com LOL_GERA_ZONAS=1).
//
// De onde vieram os números: torres e covas pelas posições da Riot; o
// traçado das rotas e do rio medido sobre docs/calibracao/map11.png (o mapa
// vazio do Data Dragon, 512 px para 14 990 × 15 100 unidades); as larguras,
// pelos pontos reais de docs/calibracao/pontos.json (a rota de cada um aos 4 e
// aos 14 min).

// Limites do mapa 11 (documentação da Riot).
var (
	MapaMin = Ponto{-120, -120}
	MapaMax = Ponto{14870, 14980}
)

// Simetria: p' = Simetria − p leva o lado azul no vermelho. Média dos pares de
// torres espelhadas (externa do topo azul × externa de baixo vermelha etc.).
var Simetria = Ponto{14820, 14910}

// Bases: quarto de disco a partir do canto. As torres do inibidor (a ~4 100–
// 4 900 do canto) ficam dentro; a fonte é o círculo em volta do lugar de
// renascer (medido pela câmera na sonda, o mesmo de rastreio.FonteDoTime).
var (
	CantoBaseAzul = Ponto{300, 300}
	RaioBase      = 4850.0
	FonteAzul     = Ponto{404, 571}
	FonteVermelha = Ponto{14340, 14507}
	RaioFonte     = 1800.0
)

// Rota do topo, da base azul para a vermelha (sobe pela borda esquerda, faz a
// curva no canto e segue pela borda de cima). A rota de baixo é esta espelhada.
var RotaTopo = []Ponto{
	{1124, 3500}, {1124, 11294}, {1490, 12621}, {2222, 13358}, {3540, 13727}, {11300, 13727},
}

// Rota do meio, da base azul para a vermelha (a diagonal; simétrica).
var RotaMeio = []Ponto{{3000, 3000}, {11820, 11910}}

// Meia-largura das rotas: até onde, da linha central, ainda é "na rota" (a
// faixa com os arbustos de rota). No meio é menor porque o rio encosta nele.
var (
	MeiaLarguraRotaLateral = 750.0
	MeiaLarguraRotaMeio    = 850.0
)

// O canto da rota lateral (o "triângulo" onde o rio desemboca na rota) é
// rota: pontos com dx + dy < CortecantoRota, medidos a partir do canto do
// mapa (−120, 14980) no topo (e espelhado embaixo). Aos 4 min o Teemo estava
// em (3101, 11972) trocando com o Malphite: 6 229 do canto.
var (
	CantoTopo      = Ponto{-120, 14980}
	CorteCantoRota = 6500.0
)

// Torres externas: dividem cada rota em trechos. Lado azul = da base azul até
// a externa azul; centro = entre as externas; lado vermelho = da externa
// vermelha até a base vermelha. (As de baixo saem do espelho das do topo.)
var (
	TorreExternaTopoAzul     = Ponto{981, 10441}
	TorreExternaTopoVermelha = Ponto{4318, 13875}
	TorreExternaMeioAzul     = Ponto{5846, 6396}
	TorreExternaMeioVermelha = Ponto{8955, 8510}
)

// TorresDasRotas: as três torres de cada time em cada rota (externa, interna e
// a do inibidor), nas posições dos eventos BUILDING_KILL da linha do tempo (as
// mesmas de ESTRUTURAS em app/src/renderer/mapa.js, conferidas com as 14
// estruturas derrubadas na partida 3304217746). As ondas usam para saber onde a
// onda bate na torre e qual torre de cada time ainda está de pé na rota.
var TorresDasRotas = []TorreDaRota{
	{ZRotaTopo, 100, 1, TorreExternaTopoAzul}, {ZRotaTopo, 100, 2, Ponto{1512, 6699}}, {ZRotaTopo, 100, 3, Ponto{1169, 4287}},
	{ZRotaMeio, 100, 1, TorreExternaMeioAzul}, {ZRotaMeio, 100, 2, Ponto{5048, 4812}}, {ZRotaMeio, 100, 3, Ponto{3651, 3696}},
	{ZRotaBaixo, 100, 1, Ponto{10504, 1029}}, {ZRotaBaixo, 100, 2, Ponto{6919, 1483}}, {ZRotaBaixo, 100, 3, Ponto{4281, 1253}},
	{ZRotaTopo, 200, 1, TorreExternaTopoVermelha}, {ZRotaTopo, 200, 2, Ponto{7943, 13411}}, {ZRotaTopo, 200, 3, Ponto{10481, 13650}},
	{ZRotaMeio, 200, 1, TorreExternaMeioVermelha}, {ZRotaMeio, 200, 2, Ponto{9767, 10113}}, {ZRotaMeio, 200, 3, Ponto{11134, 11207}},
	{ZRotaBaixo, 200, 1, Ponto{13866, 4505}}, {ZRotaBaixo, 200, 2, Ponto{13327, 8226}}, {ZRotaBaixo, 200, 3, Ponto{13624, 10572}},
}

// TorreDaRota é uma torre de rota: Ordem 1 = externa, 2 = interna, 3 = a do inibidor.
type TorreDaRota struct {
	Rota  string
	Time  int
	Ordem int
	P     Ponto
}

// Rio de cima, do canto da rota do topo até o centro do mapa (o de baixo é
// este espelhado). Meia-largura: a faixa de água mais a margem.
var (
	RioCima = []Ponto{
		{2442, 12104}, {3247, 11146}, {3906, 10114}, {4857, 9450}, {5882, 8934}, {6614, 8344}, {7410, 7455},
	}
	MeiaLarguraRio = 600.0
)

// Covas: o centro do monstro (posições da Riot). Dentro do raio menor é a
// cova; até o maior, os arredores (a entrada e o rio em volta).
var (
	CovaBarao         = Ponto{5007, 10471}
	CovaDragao        = Ponto{9866, 4414}
	RaioCova          = 750.0
	RaioArredoresCova = 1300.0
)

// Área jogável: o quadrado de bordas arredondadas do mapa. Fora dela (com
// folga para o erro da posição) a zona é fora_do_mapa — só serve para a grade
// desenhada não pintar o escuro em volta. Bordas retas: coordenada abaixo de
// LimiteArea (ou, espelhada, acima), logo depois da beira das rotas; os cantos sem base (topo à esquerda e, espelhado, embaixo à
// direita) são o arco de centro ArcoCantoCentro e raio ArcoCantoRaio.
var (
	LimiteArea      = 370.0
	ArcoCantoCentro = Ponto{4125, 10703}
	ArcoCantoRaio   = 3813.0
)
