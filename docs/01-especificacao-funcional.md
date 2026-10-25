# PitBox — Especificação Funcional

Oct 11, 2026 · @Nelson Freitas

## 1. Visão geral e âmbito

O PitBox é uma plataforma para equipas de automodelismo 1/8 TT nitro gerirem setups, motores, provas e finais, com telemetria opcional a partir de um módulo instalado no carro. A versão 1.0 serve uma equipa piloto + mecânico; a arquitetura suporta várias equipas desde o primeiro dia.

**Objetivos mensuráveis da v1.0**

- Registar um setup completo em menos de 2 minutos na box, sem rede
- Avisar o mecânico do reabastecimento com pelo menos 2 voltas de antecedência em 95% das paragens de uma final
- Saber, para qualquer manga, que setup, motor e pneus foram usados e qual o resultado
- Zero perda de dados registados offline após sincronização

**Dentro do âmbito**

- App de box (telemóvel e tablet), app web para análise em casa
- Contas, equipas, convites e permissões
- Carros, motores, pneus e peças com histórico de uso
- Fichas de setup, diário de manutenção, provas, mangas e finais
- Importação de resultados de live timing
- Módulo de telemetria (hardware + firmware) e respetiva ingestão

**Fora do âmbito da v1.0**

- Cronometragem própria (usa-se a da prova, MyLaps)
- Rede social pública ou marketplace de peças
- Controlo do carro a partir da app (nunca: o módulo é só de leitura)

**Glossário**

| Termo | Significado |
| --- | --- |
| Manga | Sessão de qualificação cronometrada (Q1, Q2…) |
| Final | Corrida decisiva de longa duração com reabastecimentos (A, B, semi, 1/4…) |
| Setup | Conjunto de afinações mecânicas e de motor do carro numa sessão |
| Depósito | Um enchimento do tanque; unidade de consumo e de vida do motor |
| Flameout | O motor apaga-se em pista; obriga a ir à box ou a ser assistido |
| Agulha alta / baixa | Parafusos de afinação da mistura do carburador (HSN / LSN) |
| Transponder | Emissor MyLaps no carro, lido pela cronometragem da pista |
| Equipa | Grupo de utilizadores que partilham carros e dados |

## 2. Perfis de utilizador e permissões

As permissões são atribuídas por equipa: o mesmo utilizador pode ser mecânico numa equipa e convidado noutra. Por omissão, tudo é privado da equipa.

| Papel | Quem é | Pode | Não pode |
| --- | --- | --- | --- |
| Dono da equipa | Quem criou a equipa | Tudo; convidar e remover membros; mudar papéis; apagar a equipa; exportar dados | Remover-se se for o único dono |
| Mecânico | Responsável técnico | Criar e editar carros, motores, setups, manutenção, provas, finais; gerir módulos de telemetria | Gerir membros; apagar a equipa |
| Piloto | Quem conduz | Ver tudo exceto custos de peças; adicionar notas de comportamento às sessões; editar o próprio perfil de piloto | Alterar setups, motores, custos ou estratégia |
| Convidado | Amigo, outra equipa | Ver apenas os setups e provas que lhe forem partilhados, só leitura | Ver motores, custos, telemetria ou notas |
| Administrador da plataforma | Operador do sistema | Suporte, suspensão de contas, métricas agregadas | Ler dados de equipas sem pedido de suporte registado e consentido |

**Regras gerais**

- Cada ação de escrita fica registada no histórico com autor e hora
- Um setup pode ser partilhado por link de só leitura, com validade (24 h, 7 dias, sem expiração) e revogável
- Acesso administrativo a dados de uma equipa exige um pedido de suporte associado e fica no registo de auditoria visível ao dono

## 3. Autenticação e gestão de conta

O login usa passkeys como método principal, com email + palavra-passe + segundo fator como alternativa, e uma sessão de box que continua válida sem rede durante toda uma prova.

### 3.1 Registo

- Registo por email, com confirmação por link de uso único (validade 30 min)
- Login social opcional: Google e Apple (OIDC)
- Palavra-passe: mínimo 12 caracteres, verificada contra listas de palavras-passe comprometidas (Have I Been Pwned, k-anonymity); sem regras de composição forçadas
- Aceitação dos termos e da política de privacidade, com versão e data guardadas

### 3.2 Login

1. Passkey (WebAuthn) — método recomendado, proposto logo após o registo
2. Email + palavra-passe + segundo fator (TOTP ou passkey)
3. Link mágico por email — só para recuperação, nunca como login normal

- Mensagens de erro genéricas ("credenciais inválidas") que não revelam se o email existe
- Limite de tentativas: 5 falhadas por conta em 15 min → atraso progressivo + CAPTCHA; nunca bloqueio permanente (evita negação de serviço à vítima)
- Aviso por email em login a partir de um novo dispositivo

### 3.3 Segundo fator (MFA)

- Obrigatório para donos de equipa e administradores; recomendado para os restantes
- Métodos: passkey, TOTP (app autenticadora); SMS não é suportado
- 8 códigos de recuperação de uso único, mostrados uma vez e guardados com HMAC

### 3.4 Sessões e dispositivos

- Access token de 10 min; refresh token rotativo com deteção de reutilização (reutilização revoga toda a família de tokens)
- **Modo box offline**: antes de uma prova, o dispositivo fica autorizado a trabalhar sem rede até 72 h; as escritas ficam numa fila local assinada e são validadas no servidor ao sincronizar
- Lista de dispositivos e sessões ativas, com terminar sessão remotamente
- Desbloqueio local da app por biometria do telemóvel após 15 min em segundo plano

### 3.5 Recuperação e alterações sensíveis

- Recuperação de palavra-passe por link de uso único (15 min); após recuperação, todas as outras sessões terminam
- Mudar email, palavra-passe, MFA ou apagar conta exige reautenticação recente (≤ 5 min)
- Mudança de email notifica o endereço antigo, com link para reverter durante 7 dias

### 3.6 Equipas e convites

- Criar equipa (nome, categoria, clube)
- Um utilizador pode ser dono de no máximo 10 equipas; por agora, libertar uma equipa significa transferi-la
- Convidar por email ou link com papel pré-definido; convite expira em 7 dias e é de uso único
- Transferir propriedade da equipa (exige confirmação dos dois lados)

### 3.7 Contas de dispositivo (módulo de telemetria)

- Cada módulo é emparelhado com uma equipa por código QR mostrado na app; recebe uma identidade própria, nunca credenciais de utilizador
- O módulo só pode enviar telemetria; não lê dados da conta
- Desemparelhar revoga a identidade de imediato

## 4. Equipas, carros, motores e inventário

Cada peça importante tem identidade própria e contador de uso, para que motores, embraiagens e pneus possam passar de um chassis para outro sem perder o histórico.

### 4.1 Carros (chassis)

- Marca, modelo, ano, número de série, fotografia, estado (ativo, reserva, retirado)
- Transponder associado (número MyLaps) — usado para casar resultados importados
- Configuração base: o setup "de casa" do qual derivam os setups das sessões
- Escala e motorização: catálogo global (`garage.scale`, `garage.powertrain`); na v0.1 só
  1/8 nitro. Outras escalas e motorizações entram na Fase 6.

### 4.2 Motores

- Marca, modelo, número de série, data de compra
- Contadores automáticos: depósitos, minutos em pista, número de sessões
- Histórico de revisões (ver secção 6)
- Associação a um carro com data de início e fim (um motor pode mudar de carro)

### 4.3 Pneus

- Conjunto de 4 rodas com marca, modelo, composto, inserts, data de colagem
- Contador de mangas e minutos; estado (novo, usado, gasto) com nota de desgaste
- Marcação do conjunto (ex.: "J2") que a equipa escreve nas jantes

### 4.4 Peças e consumíveis

- Catálogo próprio da equipa: velas, sapatas, molas de embraiagem, rolamentos, bielas, filtros, óleos (com viscosidade), combustível (% nitro)
- Stock com quantidade mínima e alerta quando abaixo
- Peça com vida útil (ex.: vela, sapatas) pode ser "montada" num carro ou motor e passa a contar uso
- Custo opcional por peça, para ver o custo de uma época

## 5. Fichas de setup

Um setup é uma ficha versionada: cada sessão grava uma versão imutável, e criar um setup novo começa sempre por copiar outro (o último usado, o de casa, ou o último naquela pista).

### 5.1 Campos

| Grupo | Campos (unidades) |
| --- | --- |
| Diferenciais | Óleo frente, central e trás (cSt ou "k"); quantidade; notas de enchimento |
| Amortecedores (frente e trás) | Óleo (cSt); pistão (n.º furos × diâmetro); mola (marca, cor, taxa); limitadores (mm); comprimento (mm); posição na torre e no braço; tipo de membrana |
| Geometria | Camber (°); convergência frente e trás (°); caster (°); altura ao solo (mm); droop (mm); ackermann; posição das hubs traseiras |
| Barras | Estabilizadora frente e trás (mm, ou removida) |
| Aerodinâmica | Asa (modelo, ângulo, gurney); carroçaria |
| Transmissão | Campana (dentes); coroa (dentes); relação calculada; sapatas (material); molas de embraiagem; folga da embraiagem (mm) |
| Motor | Motor (referência ao inventário); agulha alta e baixa (posição em voltas desde fechada, ou horas); vela (marca, temperatura); combustível (marca, % nitro); escape e coletor; filtro de ar; temperatura alvo da cabeça (°C) |
| Pneus | Conjunto (referência ao inventário); pressão de colagem; preparação (aditivo, tempo) |
| Peso e lastro | Peso total (g); lastro e posição |
| Rádio | EPA, expo e dual rate de direção e acelerador; trava (%) |

- Campos com unidade sempre explícita; valores numéricos com validação de intervalo (ex.: camber entre −5° e +2°)
- Listas de valores sugeridos aprendidas do histórico da equipa (ex.: óleos que a equipa já tem)
- Campos personalizados por equipa (nome, tipo, unidade)

### 5.2 Condições da sessão

- Pista (lista de pistas com coordenadas), data e hora
- Temperatura do ar e do piso (°C), humidade (%), estado do piso: seco, húmido, molhado, com pó, regado, com sulcos
- Preenchimento automático do tempo a partir de um serviço meteorológico quando há rede

### 5.3 Feedback do piloto

- Escalas rápidas (1–5): direção à entrada, a meio e à saída de curva; tração; estabilidade em saltos; travagem; motor em baixos e altos
- Nota de texto ou de voz (transcrita) ligada a uma zona da pista

### 5.4 Operações

- Copiar, comparar dois setups lado a lado com as diferenças destacadas
- "O que mudou desde a manga anterior" no topo de cada ficha
- Marcar como favorito por pista e por condição do piso
- Partilhar por link só de leitura (secção 2) e exportar em PDF para imprimir
- Importar e exportar em JSON para backup ou migração

## 6. Diário do motor e manutenção

O diário diz ao mecânico o que rever antes de partir: cada intervenção fica registada, e as regras de manutenção geram avisos a partir dos contadores de uso.

### 6.1 Registo de uso

- Cada sessão soma ao motor, carro e pneus: depósitos, minutos, número de arranques
- Registo rápido de "aquecimento / rodagem" fora de sessões (ex.: rodagem de motor novo, com número de depósitos)
- Leituras manuais de temperatura da cabeça (pistola de infravermelhos) por sessão, ou automáticas via telemetria

### 6.2 Intervenções

- Tipo (revisão, substituição, limpeza, afinação), data, autor, peças usadas do stock, custo, fotografias
- Exemplos: troca de rolamentos, biela, conjunto pistão/camisa, sapatas, refazer diferenciais, trocar óleo dos amortecedores, verificar folga dos rolamentos das rodas
- Abate de uma peça com motivo (partida, desgaste, perdida)

### 6.3 Regras de manutenção

- Regra = peça ou componente + limite (depósitos, minutos, sessões ou dias) + ação
- Exemplos configuráveis: "rolamentos do motor a cada N depósitos", "óleo dos diferenciais a cada N sessões", "sapatas a cada N minutos"
- Estado por componente: ok, a aproximar-se (≥ 80% do limite), vencido
- Os valores de referência são definidos pela equipa; a app não impõe intervalos de fábrica

### 6.4 Checklist pré-prova

- Gerada a partir das regras vencidas ou a vencer antes da data da próxima prova
- Itens marcáveis na oficina, com indicação do que falta comprar no stock

## 7. Gestão de provas e mangas

Uma prova agrupa tudo o que acontece num fim de semana de corrida: o horário, cada sessão com o seu setup e resultado, e as finais.

### 7.1 Calendário da época

- Provas com nome, pista, datas, campeonato (nacional, regional, open), categoria, link do regulamento e do live timing
- Estado: planeada, inscrita, a decorrer, concluída
- Classificação no campeonato atualizada a partir dos resultados importados

### 7.2 Horário do dia

- Sessões: treinos livres, treinos cronometrados, mangas Q1…Qn, finais
- Introdução manual ou importação do horário publicado pela organização
- Contagem decrescente para a próxima sessão do carro, com aviso a 15 e a 5 min
- Ajuste rápido de atrasos ("o programa está 10 min atrasado" desloca todas as sessões seguintes)

### 7.3 Sessão

- Ligação a: carro, motor, conjunto de pneus, setup usado (versão), condições
- Resultado: voltas, tempo total, melhor volta, posição, voltas individuais (importados, secção 8, ou introduzidos manualmente)
- Incidentes marcáveis por volta: capotou, toque, flameout, saída de pista, avaria
- Depósitos usados e combustível restante no fim

### 7.4 Checklist pré-sessão

- Lista configurável por equipa, com valores por omissão: depósito cheio, vela verificada, filtro de ar limpo e oleado, transponder ligado, bateria do recetor carregada, rodas apertadas, carroçaria presa, fail-safe testado
- Itens obrigatórios bloqueiam o "pronto" até estarem marcados
- Fica registado quem marcou e quando

## 8. Live timing, resultados e análise

Os tempos vêm da cronometragem oficial da prova; a app junta-os ao setup e às condições de cada sessão para mostrar o que realmente fez o carro mais rápido e mais consistente.

### 8.1 Fontes de dados

- Conectores por fornecedor de resultados (ex.: LiveRC, RCM, MyRCM, páginas de resultados de clubes), ativados por prova
- Alternativa sempre disponível: carregar um ficheiro CSV/HTML exportado ou introduzir voltas à mão
- Cada conector só é usado depois de confirmar os termos de utilização do fornecedor; preferência por APIs ou exportações oficiais e, na falta delas, pedidos espaçados e identificados
- Correspondência automática pelo número do transponder; correspondência manual quando falha

### 8.2 Pilotos e rivais

- Os resultados guardam todos os pilotos da sessão (nome público como publicado pela organização)
- A equipa marca rivais a seguir; os dados dos rivais só são visíveis dentro da equipa

### 8.3 Análises por sessão

- Gráfico de tempos por volta, com incidentes marcados
- Melhor volta, média, mediana, desvio-padrão e "melhores 5 voltas consecutivas"
- Consistência: percentagem de voltas a menos de 2% da melhor
- Comparação volta a volta com um rival ou com a própria melhor sessão

### 8.4 Análises ao longo do tempo

- Evolução por pista e por época
- Efeito de uma alteração de setup: média e consistência antes e depois, filtradas por condições semelhantes do piso
- Desgaste de pneus: tempo médio por volta em função das mangas acumuladas do conjunto
- Todas as análises indicam o número de sessões em que se baseiam, para não tirar conclusões de amostras pequenas

### 8.5 Durante a prova

- Atualização ao vivo da posição e da diferença para os carros à frente e atrás (quando o fornecedor publica dados ao vivo)
- Funciona em modo degradado sem rede: mostra o último estado recebido com a hora

## 9. Modo final e estratégia de box

O modo final é um ecrã único para o mecânico durante a corrida: diz quando o carro tem de entrar, regista cada paragem com um toque e funciona sem rede do primeiro ao último minuto.

### 9.1 Antes da final: plano de estratégia

- Entradas: duração da final (min), voltas por depósito medidas nas sessões (média e pior caso), tempo médio de reabastecimento (s), vida prevista dos pneus
- Saídas: número de paragens, volta prevista de cada uma, paragens com troca de pneus ou de vela, margem de segurança
- Cenários alternativos: "e se houver um flameout", "e se fizermos uma paragem a menos com depósito mais longo"
- O plano é guardado com a final e pode ser revisto depois

A volta de aviso para cada reabastecimento calcula-se a partir das voltas desde a última paragem:

```latex
V_{aviso} = V_{\text{última paragem}} + \left\lfloor L_{\text{depósito}} \times (1 - m) \right\rfloor - a
```

onde L é o número de voltas por depósito (pior caso medido), m a margem de segurança (por omissão 10%) e a o número de voltas de antecedência do aviso (por omissão 2). Todos os parâmetros são editáveis.

### 9.2 Durante a final

- Cronómetro da final iniciado com um toque (ou automaticamente pelo live timing)
- Contador de voltas: por live timing, por telemetria ou por toque manual em cada passagem
- Indicador grande: voltas desde o último reabastecimento / voltas até à próxima paragem
- Aviso a 2 voltas da janela de reabastecimento: vibração, som e ecrã a piscar; aviso final na volta de entrada
- Botões grandes (utilizáveis com luvas e mãos sujas de combustível): Reabastecer, Pneus, Vela, Flameout, Avaria, Carro de volta à pista
- Cada paragem regista hora de entrada, hora de saída e duração; o plano recalcula-se automaticamente após um flameout ou uma paragem fora do previsto
- Posição e diferenças para os rivais quando há live timing
- Ecrã sempre ligado, alto contraste, legível ao sol

### 9.3 Depois da final

- Linha do tempo da corrida: paragens, incidentes, posição ao longo da corrida
- Tempo total perdido na box e comparação com o plano
- Atualização automática de voltas por depósito e dos contadores do motor e dos pneus

### 9.4 Partilha entre piloto e mecânico

- O piloto pode ter o mesmo ecrã num segundo telemóvel em só leitura
- Sincronização local entre os dois dispositivos quando não há Internet (rede local ou Bluetooth), secção 11

## 10. Telemetria do módulo no carro

O módulo grava sempre para cartão e, quando permitido, envia um resumo em tempo real para a box por LoRa; a app junta os dados à sessão e ao setup correspondentes.

### 10.1 Sinais medidos

| Sinal | Sensor | Frequência de gravação | Uso |
| --- | --- | --- | --- |
| Temperatura da cabeça do motor | Termopar tipo K (MAX31855) ou infravermelhos (MLX90614) | 5 Hz | Carburação, sobreaquecimento |
| Rotações do motor | Sensor Hall ou ótico no volante | 50 Hz | Carburação, embraiagem, relação |
| Tensão da bateria do recetor | Divisor resistivo + ADC | 2 Hz | Evitar perda de controlo |
| Aceleração e rotação 3 eixos | IMU (ex.: LSM6DSO) | 200 Hz | Saltos, aterragens, impactos |
| Posição de acelerador e direção (opcional) | Leitura do sinal PWM dos servos | 50 Hz | Estilo de condução |

### 10.2 Funcionalidades

- Emparelhamento por QR (secção 3.7), associação a um carro
- Início e fim de gravação automáticos pela rotação do motor; marcação de voltas por correlação com o live timing
- Envio em tempo real (LoRa 868 MHz) de um pacote resumido a 1 Hz: temperatura, rotações máximas, tensão, alertas
- Descarga completa do cartão por USB ou Wi-Fi na box depois da sessão
- Alertas configuráveis: temperatura acima de X °C, tensão abaixo de Y V, impacto acima de Z g
- Gráficos sobrepostos aos tempos por volta; comparação entre sessões
- Atualização de firmware assinada, iniciada a partir da app
- Estado do módulo: bateria, espaço livre, versão de firmware, última ligação

### 10.3 Restrições

- **Regulamento**: antes de cada prova, o mecânico confirma no regulamento se o equipamento a bordo e a transmissão em corrida são permitidos; a app permite desligar a transmissão e manter só a gravação
- **Segurança do carro**: o módulo nunca está no caminho do sinal do rádio para os servos; só lê, e uma avaria no módulo não afeta o controlo do carro
- **Rádio**: nada a 2,4 GHz durante a sessão para não interferir com o rádio do carro; LoRa a 868 MHz dentro dos limites de potência e duty cycle da banda na UE
- **Físicas**: alvo de peso ≤ 40 g com caixa; resistente a vibração, combustível, óleo e pó

## 11. Offline, sincronização e conflitos

Todas as funcionalidades de box funcionam sem Internet; a sincronização acontece em segundo plano quando houver rede, sem o utilizador ter de pensar nisso.

- Dados da equipa necessários à prova (carros, motores, pneus, setups recentes, horário) descarregados para o dispositivo antes da prova, com indicador "pronto para offline"
- Cada escrita offline fica numa fila local com identificador único, para que reenviar nunca duplique dados
- Estado visível: sincronizado, X alterações pendentes, erro (com ação para resolver)
- **Conflitos**: registos de eventos (paragens, voltas, intervenções, notas) nunca entram em conflito, são sempre acrescentados; em fichas editáveis (setup, carro), alterações a campos diferentes juntam-se automaticamente; o mesmo campo alterado em dois dispositivos mostra as duas versões ao mecânico para escolher
- Sincronização local entre dispositivos da mesma equipa na box sem Internet (fase 2)
- Dados no dispositivo cifrados em repouso; terminar sessão apaga-os depois de sincronizados

## 12. Notificações e alertas

| Alerta | Canal | Quando |
| --- | --- | --- |
| Reabastecimento | Ecrã + vibração + som | 2 voltas antes da janela e na volta de entrada |
| Próxima sessão | Notificação local | 15 e 5 min antes |
| Telemetria fora de limites | Ecrã + vibração | Imediato |
| Manutenção vencida | Notificação + email semanal | Ao atingir o limite; resumo antes da prova |
| Stock baixo | Notificação | Ao ficar abaixo do mínimo |
| Segurança da conta | Email | Novo dispositivo, mudança de email, palavra-passe ou MFA |
| Convite de equipa | Email | Ao convidar |

- Preferências por utilizador e por tipo; alertas de segurança não podem ser desligados
- Alertas de corrida são locais ao dispositivo e não dependem de rede

## 13. Requisitos não funcionais

Os requisitos abaixo são os critérios para considerar a plataforma pronta para produção; cada um tem uma medida verificável.

| Área | Requisito | Medida |
| --- | --- | --- |
| Desempenho da app | Abrir a app até ao ecrã da box | ≤ 2 s num telemóvel de gama média, sem rede |
| Desempenho da app | Resposta a um toque no modo final | ≤ 100 ms |
| Desempenho da API | Latência de leitura e escrita | p95 ≤ 300 ms; p99 ≤ 800 ms |
| Disponibilidade | API e sincronização | 99,5% mensal (a box não depende dela durante a corrida) |
| Durabilidade | Dados confirmados pelo servidor | RPO ≤ 15 min; RTO ≤ 4 h |
| Escala inicial | Equipas e telemetria | 500 equipas; 50 módulos a enviar em simultâneo |
| Segurança | Verificação de controlos | OWASP ASVS 5.0 nível 2; OWASP MASVS para a app móvel |
| Segurança | Testes | Teste de intrusão externo antes do lançamento público |
| Acessibilidade | Interface | WCAG 2.2 AA; modo final com alvos de toque ≥ 64 px e contraste ≥ 7:1 |
| Idiomas | Interface | Português e inglês na v1.0; unidades métricas |
| Plataformas | Suporte | Android 10+, iOS 16+, Chrome/Safari/Firefox das 2 últimas versões |
| Bateria | Modo final ligado | ≤ 15% de bateria por hora de final com ecrã sempre ligado |
| Observabilidade | Rastreabilidade | Todas as chamadas com trace ID; erros da app reportados com consentimento |

## 14. Privacidade, RGPD e conformidade

O PitBox trata poucos dados pessoais e foi desenhado para tratar o mínimo necessário.

- **Dados pessoais tratados**: nome, email, fotografia opcional, dispositivos e endereços IP de sessão; nomes de pilotos tal como publicados nos resultados oficiais das provas
- **Fundamento**: execução do contrato (conta e serviço); interesse legítimo para segurança e para os resultados públicos de terceiros, com direito de oposição
- **Direitos do titular** exercidos na própria app: exportar todos os dados (JSON), corrigir, apagar a conta (apagamento efetivo em 30 dias, backups expiram em 35 dias)
- **Localização**: dados armazenados na UE; subcontratantes listados na política de privacidade, com acordos de tratamento de dados
- **Retenção**: registos de segurança 12 meses; telemetria bruta 24 meses (configurável por equipa); dados de conta até à eliminação
- **Cookies**: só os estritamente necessários; análise de utilização apenas com consentimento
- **Menores**: pilotos com menos de 16 anos só entram através da conta de um responsável
- **Violações de dados**: procedimento documentado para notificar a CNPD em 72 h e os titulares afetados quando aplicável
- Registo das atividades de tratamento mantido e revisto anualmente

## 15. Roadmap e critérios de aceitação

O produto cresce em cinco versões, e cada uma só é dada como concluída depois de usada numa prova real, que é o teste que interessa.

| Versão | Conteúdo | Gate para avançar |
| --- | --- | --- |
| v0.1 Fundações | Contas com passkeys e MFA; equipas; carros, motores, pneus e peças; fichas de setup com versões, comparação e partilha | Setups da equipa registados na app durante uma prova real |
| v0.2 Box | Provas, horário e mangas; checklists; diário do motor e manutenção; modo final com estratégia e avisos; offline com fila de sincronização | Uma final completa gerida só na app, sem perda de dados |
| v0.3 Análise | Importação de resultados e live timing; análises por sessão e época; efeito das alterações de setup; desgaste dos pneus | Tempos de uma prova inteira importados e casados pelo transponder |
| v0.4 Telemetria | Módulo com gravação em cartão; descarga e gráficos sobre os tempos; alertas; firmware assinado | 10 sessões gravadas sem qualquer efeito no controlo do carro |
| v1.0 Produção | Envio em tempo real por LoRa; sincronização local na box; teste de intrusão, RGPD, monitorização e lançamento | — |

As funcionalidades das secções 3 a 6 entram na v0.1; as secções 7, 9 e 11 na v0.2; a 8 na v0.3; a 10 na v0.4 e v1.0.

### Critérios de aceitação transversais

- [ ] Cada funcionalidade tem testes automáticos (unitários e de integração) a correr no CI
- [ ] Cada endpoint tem verificação de autorização testada com um utilizador de outra equipa (deve falhar)
- [ ] O modo final funciona 60 min seguidos em modo avião sem perda de dados
- [ ] Reenviar a mesma fila de sincronização duas vezes não duplica registos
- [ ] Exportar e apagar a conta funcionam de ponta a ponta
- [ ] O módulo de telemetria desligado ou avariado não altera o comportamento do carro
- [ ] Requisitos não funcionais da secção 13 medidos e cumpridos antes da v1.0

### Questões em aberto

- Que fornecedores de live timing são usados nas provas onde a equipa corre, e que exportações oferecem?
- O regulamento da competição permite telemetria a bordo nas mangas e nas finais?
- A plataforma será aberta a outras equipas desde a v1.0 ou só por convite?
